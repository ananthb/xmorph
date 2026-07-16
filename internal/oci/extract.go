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
//
// Extraction runs as root into what becomes the live root filesystem, so
// containment is a security boundary, not a nicety: every path is resolved
// through secureJoin, which follows the symlinks earlier layers legitimately
// plant (e.g. usr-merge's /bin -> usr/bin) but clamps them inside targetDir,
// so a hostile layer cannot redirect a write or delete onto the host via an
// absolute/".." symlink. The final path component is always joined literally
// and any pre-existing entry there is unlinked (never followed) before we
// write, so a planted symlink at the target itself is replaced, not traversed.
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

		// Defense in depth: reject obvious traversal in the entry name.
		// secureJoin below is the real guard (it also contains symlinked
		// parents), but this rejects the blatant case early and clearly.
		if name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return fmt.Errorf("tar entry %q escapes target", h.Name)
		}

		dir, base := filepath.Split(name)
		dir = strings.TrimSuffix(dir, "/")

		// Resolve the entry's PARENT directory inside targetDir, following
		// symlinked parents but clamping them to the root.
		parent, err := secureJoin(targetDir, dir)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", h.Name, err)
		}

		// Whiteout handling.
		if base == whiteoutOpaque {
			if err := clearDirContents(parent); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("opaque whiteout %s: %w", h.Name, err)
			}
			continue
		}
		if strings.HasPrefix(base, whiteoutPrefix) {
			target := filepath.Join(parent, strings.TrimPrefix(base, whiteoutPrefix))
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("whiteout %s: %w", h.Name, err)
			}
			continue
		}

		if err := ensureDir(parent); err != nil {
			return err
		}
		target := filepath.Join(parent, base)

		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
			if err := chmodEntry(target, h); err != nil {
				return err
			}

		case tar.TypeReg, tar.TypeRegA:
			// Remove any existing entry first — later layers always win, and
			// RemoveAll unlinks a planted symlink here rather than following
			// it. O_EXCL then guarantees we create a fresh file, never open
			// through a link. Mode is tightened later by chmodEntry.
			_ = os.RemoveAll(target)
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
			if err := chmodEntry(target, h); err != nil {
				return err
			}

		case tar.TypeSymlink:
			_ = os.RemoveAll(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", target, h.Linkname, err)
			}

		case tar.TypeLink:
			_ = os.RemoveAll(target)
			// The hardlink source must also stay inside targetDir — otherwise
			// a layer could hardlink a host file (e.g. /etc/shadow) into the
			// rootfs and expose its contents. secureJoin clamps it.
			source, err := secureJoin(targetDir, h.Linkname)
			if err != nil {
				return fmt.Errorf("resolve hardlink target %q: %w", h.Linkname, err)
			}
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

// secureJoin resolves unsafePath under root and returns the absolute path,
// following symlinks encountered along the way but treating root as "/" for
// them — an absolute symlink target is re-rooted at root and a "../" can
// never climb above root. This is the containment primitive that keeps a
// hostile tar layer from redirecting a write/delete onto the host through a
// symlink an earlier entry planted. Modeled on github.com/cyphar/filepath-securejoin.
//
// unsafePath is a slash-separated path relative to root (tar entry names
// always use "/"). The returned path may point at something that does not
// exist yet; non-existent components are taken literally.
func secureJoin(root, unsafePath string) (string, error) {
	const maxLinks = 255
	linksWalked := 0
	// resolved is the portion already resolved, relative to root and always
	// kept inside it. path is what remains to process.
	resolved := ""
	path := unsafePath
	for path != "" {
		var part string
		if i := strings.IndexByte(path, '/'); i >= 0 {
			part, path = path[:i], path[i+1:]
		} else {
			part, path = path, ""
		}
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			if resolved == "." || resolved == string(filepath.Separator) {
				resolved = ""
			}
			continue
		}

		next := filepath.Join(resolved, part)
		full := filepath.Join(root, next)
		fi, err := os.Lstat(full)
		if err != nil {
			if os.IsNotExist(err) {
				// Nothing here yet — take it literally and keep going; any
				// remaining components resolve underneath it.
				resolved = next
				continue
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			resolved = next
			continue
		}

		linksWalked++
		if linksWalked > maxLinks {
			return "", fmt.Errorf("too many symlinks while resolving %q", unsafePath)
		}
		dest, err := os.Readlink(full)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(dest) {
			// Absolute link target is interpreted relative to root.
			resolved = ""
			path = filepath.Join(dest, path)
		} else {
			// Relative link target resolves against the link's parent dir,
			// which is the current `resolved` (part was appended to it).
			path = filepath.Join(dest, path)
		}
	}
	return filepath.Join(root, resolved), nil
}

// chmodEntry sets target's permission bits from the tar header, including the
// setuid/setgid/sticky bits. Extraction creates files/dirs with a tight base
// mode, so this restores the real mode (undoing both the ModePerm truncation
// and the umask narrowing that OpenFile/Mkdir would otherwise impose) — a
// setuid /usr/bin/sudo in the image stays setuid in the built rootfs.
func chmodEntry(target string, h *tar.Header) error {
	fm := h.FileInfo().Mode()
	perm := fm.Perm()
	perm |= fm & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	if err := os.Chmod(target, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", target, err)
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
