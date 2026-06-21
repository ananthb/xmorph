package oci

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// whiteout markers from the OCI image-layer-filesystem-changeset spec
// (https://github.com/opencontainers/image-spec/blob/main/layer.md#whiteouts):
//
//   - .wh.<name>     → delete <name> from earlier layers
//   - .wh..wh..opq   → opaque-directory marker; delete everything in the
//     containing directory that was contributed by
//     earlier layers, then apply this layer's contents.
const (
	whiteoutPrefix = ".wh."
	whiteoutOpaque = ".wh..wh..opq"
)

// ExtractImage flattens an OCI v1.Image into targetDir, applying each
// layer in order and honoring whiteouts. The targetDir must exist and
// should be empty for clean output (subsequent layers from a separate
// ExtractImage call would compose with what's already there, which is
// what the rootfs builder relies on for multi-image merging).
func ExtractImage(img v1.Image, targetDir string) error {
	if err := ensureDir(targetDir); err != nil {
		return err
	}
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("read image layers: %w", err)
	}
	for i, l := range layers {
		rc, err := l.Uncompressed()
		if err != nil {
			return fmt.Errorf("uncompress layer %d: %w", i, err)
		}
		err = extractTar(tar.NewReader(rc), targetDir)
		rc.Close()
		if err != nil {
			return fmt.Errorf("extract layer %d: %w", i, err)
		}
	}
	return nil
}

// ExtractTarball reads a single rootfs tarball (plain — NOT a docker-save
// multi-layer archive) into targetDir. Used for the --rootfs flag with
// .tar / .tar.gz files. The caller has already classified the file via
// ClassifyRootfs; we sniff gzip via the first two magic bytes.
func ExtractTarball(path, targetDir string) error {
	if err := ensureDir(targetDir); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open rootfs tarball: %w", err)
	}
	defer f.Close()

	var reader io.Reader = f
	// Detect gzip via magic bytes (1f 8b). Avoids relying on file
	// extension, which Zig also did per src/rootfs/builder.zig:273-277.
	hdr := make([]byte, 2)
	n, _ := io.ReadFull(f, hdr)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if n == 2 && hdr[0] == 0x1f && hdr[1] == 0x8b {
		gz, err := newGzipReader(f)
		if err != nil {
			return fmt.Errorf("gzip rootfs tarball: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	return extractTar(tar.NewReader(reader), targetDir)
}

// extractTar walks a tar stream and writes entries into targetDir,
// resolving .wh.* whiteout markers against what's already on disk.
func extractTar(tr *tar.Reader, targetDir string) error {
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		name := filepath.Clean(h.Name)
		if name == "." || name == "/" {
			continue
		}
		// Strip leading "/" and "./" — tar entries are typically relative.
		name = strings.TrimPrefix(name, "/")
		name = strings.TrimPrefix(name, "./")

		// Reject path traversal. filepath.Clean has already normalized
		// "../" but a leading "../" can still appear.
		if strings.HasPrefix(name, "..") || strings.Contains(name, "/../") {
			return fmt.Errorf("tar entry %q escapes target", h.Name)
		}

		dir, base := filepath.Split(name)
		dir = strings.TrimSuffix(dir, "/")

		// Whiteout handling.
		if base == whiteoutOpaque {
			parent := filepath.Join(targetDir, dir)
			if err := clearDirContents(parent); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("opaque whiteout %s: %w", h.Name, err)
			}
			continue
		}
		if strings.HasPrefix(base, whiteoutPrefix) {
			target := filepath.Join(targetDir, dir, strings.TrimPrefix(base, whiteoutPrefix))
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("whiteout %s: %w", h.Name, err)
			}
			continue
		}

		target := filepath.Join(targetDir, name)

		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(h.Mode)&os.ModePerm); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}

		case tar.TypeReg, tar.TypeRegA:
			if err := ensureDir(filepath.Dir(target)); err != nil {
				return err
			}
			// Remove any existing entry — later layers always win.
			_ = os.RemoveAll(target)
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(h.Mode)&os.ModePerm)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close %s: %w", target, err)
			}

		case tar.TypeSymlink:
			if err := ensureDir(filepath.Dir(target)); err != nil {
				return err
			}
			_ = os.RemoveAll(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", target, h.Linkname, err)
			}

		case tar.TypeLink:
			if err := ensureDir(filepath.Dir(target)); err != nil {
				return err
			}
			_ = os.RemoveAll(target)
			source := filepath.Join(targetDir, filepath.Clean(h.Linkname))
			if err := os.Link(source, target); err != nil {
				return fmt.Errorf("hardlink %s -> %s: %w", target, source, err)
			}

		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			// Device nodes require CAP_MKNOD; the builder is invoked as
			// root for the real pivot path. For M2's `build -o dir` use
			// case we may be running as a user — skip gracefully.
			if err := makeDevice(target, h); err != nil {
				// Non-fatal: log via return-up-the-stack would mean
				// missing devices. The Zig version (src/rootfs/builder.zig
				// near layer extraction) also tolerates EPERM on mknod.
				continue
			}

		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// Per-entry / global pax headers — informational only.
			continue

		default:
			// Unknown / unsupported entry type. The Zig version skips
			// these silently; do the same to stay byte-compatible.
			continue
		}
	}
}

// ensureDir creates dir (and parents) with 0755 if not present.
func ensureDir(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ensureDir %s: %w", dir, err)
	}
	return nil
}

// clearDirContents removes all children of dir but leaves dir itself.
// Used for opaque-whiteout markers.
func clearDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
