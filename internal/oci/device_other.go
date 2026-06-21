//go:build !linux

package oci

import (
	"archive/tar"
	"errors"
)

// makeDevice is a no-op on non-Linux platforms. Build still goes through
// the same path so tests can run on developer macOS/etc; the entry is
// silently skipped, matching the Linux EPERM tolerance.
func makeDevice(_ string, _ *tar.Header) error {
	return errors.New("mknod unsupported on this platform")
}
