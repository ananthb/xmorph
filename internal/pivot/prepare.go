package pivot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PrepareOptions controls Prepare. Mirrors src/pivot/prepare.zig.
type PrepareOptions struct {
	NewRoot string
	// SkipVerify skips the "looks like a rootfs" sanity check. M2's
	// builder already verifies, so the orchestrator passes true here.
	SkipVerify bool
	// CreateNamespace = true triggers unshare(CLONE_NEWNS). The
	// `runPivot` orchestrator does this elsewhere (so a thread can be
	// locked first); --contain mode calls Prepare with this true.
	CreateNamespace bool
}

// Prepare sets up everything needed before pivot_root: optional
// namespace creation, verification, essential mounts, and self-binding
// of the new root so pivot_root accepts it.
func Prepare(opts PrepareOptions) error {
	if !opts.SkipVerify {
		if err := verifyRoot(opts.NewRoot); err != nil {
			return err
		}
	}
	if opts.CreateNamespace {
		if err := CreateMountNamespace(); err != nil {
			return err
		}
	}
	if err := SetupEssentials(opts.NewRoot); err != nil {
		return fmt.Errorf("setup essentials: %w", err)
	}
	// Self-bind so pivot_root sees newRoot as a real mount point. The
	// kernel rejects pivot_root unless this holds.
	_ = EnsureMountPoint(opts.NewRoot) // tolerated: already-mounted is OK
	if err := MakePrivate(opts.NewRoot); err != nil {
		return fmt.Errorf("make new root private: %w", err)
	}
	return nil
}

// verifyRoot checks that newRoot looks like a usable rootfs (bin/ and
// lib/ exist, and at least one of /sbin/init, /bin/sh, /bin/bash is
// present). Mirrors src/pivot/pivot.zig:172-219.
func verifyRoot(newRoot string) error {
	for _, sub := range []string{"bin", "lib"} {
		if _, err := os.Stat(filepath.Join(newRoot, sub)); err != nil {
			return fmt.Errorf("verify: missing %s: %w", sub, err)
		}
	}
	for _, exe := range []string{"sbin/init", "bin/sh", "bin/bash"} {
		if _, err := os.Stat(filepath.Join(newRoot, exe)); err == nil {
			return nil
		}
	}
	return errors.New("verify: no init or shell found in new root")
}
