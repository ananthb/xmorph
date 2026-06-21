package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ananthb/xmorph/internal/oci"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// composeImage builds a single-layer v1.Image from the flattened rootfs
// at rootfsDir and threads the merged ImageConfig into the OCI config
// blob. Used by `xmorph build` to package the cache entry / -o output.
func composeImage(rootfsDir string, cfg *oci.ImageConfig) (v1.Image, error) {
	// Pack the rootfs into a tar bytes buffer. Tarballs of multi-GB
	// rootfses would chew memory; for M2 we accept that and revisit
	// in M3+ if the QEMU integration test starts struggling.
	var buf bytes.Buffer
	if err := tarDirectory(rootfsDir, &buf); err != nil {
		return nil, fmt.Errorf("tar rootfs: %w", err)
	}

	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	}, tarball.WithMediaType(types.OCILayer))
	if err != nil {
		return nil, fmt.Errorf("wrap layer: %w", err)
	}

	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, fmt.Errorf("append layer: %w", err)
	}

	if cfg != nil {
		cf, err := img.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		cf.Config.Entrypoint = cfg.Entrypoint
		cf.Config.Cmd = cfg.Cmd
		cf.Config.Env = cfg.Env
		cf.Config.WorkingDir = cfg.WorkingDir
		img, err = mutate.ConfigFile(img, cf)
		if err != nil {
			return nil, fmt.Errorf("set config: %w", err)
		}
	}

	return img, nil
}

// writeRootfsTarball writes the rootfs at srcDir as a single gzip-compressed
// tarball at dst. Used by `xmorph build --rootfs-output`.
func writeRootfsTarball(srcDir, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if strings.HasSuffix(dst, ".gz") || strings.HasSuffix(dst, ".tgz") {
		gz := gzip.NewWriter(out)
		defer gz.Close()
		return tarDirectory(srcDir, gz)
	}
	return tarDirectory(srcDir, out)
}

// copyLayoutDir mirrors an OCI layout directory tree from src to dst.
// Used on cache hit when -o was requested: we don't need to rebuild,
// just hand the same bytes through.
func copyLayoutDir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
		return mkErr
	}
	_ = os.RemoveAll(dst)
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// tarDirectory packs srcDir into a tar stream on w. Symlinks are written
// as symlinks (not followed). Used by composeImage and writeRootfsTarball.
func tarDirectory(srcDir string, w io.Writer) error {
	srcDir = filepath.Clean(srcDir)
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		// Use forward-slash relative path so the tarball is portable.
		h.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			h.Name += "/"
		}

		if err := tw.WriteHeader(h); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, f)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return tw.Close()
}
