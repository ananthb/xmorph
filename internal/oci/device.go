//go:build linux

package oci

import (
	"archive/tar"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// makeDevice attempts to mknod a device node entry from a tar header.
// Returns an error on real failures; the caller treats permission
// errors as non-fatal (so the build can run as a user, matching the
// Zig version's tolerance).
func makeDevice(target string, h *tar.Header) error {
	var mode uint32
	switch h.Typeflag {
	case tar.TypeChar:
		mode = unix.S_IFCHR
	case tar.TypeBlock:
		mode = unix.S_IFBLK
	case tar.TypeFifo:
		mode = unix.S_IFIFO
	default:
		return errors.New("not a device entry")
	}
	mode |= uint32(h.Mode) & 0o777
	dev := unix.Mkdev(uint32(h.Devmajor), uint32(h.Devminor))
	if err := unix.Mknod(target, mode, int(dev)); err != nil {
		// EPERM commonly means we're a user, not root — surface so the
		// caller can decide whether to ignore.
		if errors.Is(err, syscall.EPERM) {
			return os.ErrPermission
		}
		return err
	}
	return nil
}
