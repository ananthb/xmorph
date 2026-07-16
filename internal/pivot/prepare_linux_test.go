//go:build linux

package pivot

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// minimalRoot creates a directory that looks enough like a rootfs for
// Prepare to mount the essentials into it.
func minimalRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"bin", "lib"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// inNewMountNS runs fn on a dedicated OS thread that has its own mount
// namespace, so the mounts fn makes don't leak into the rest of the test
// binary. The thread is never unlocked, so it is discarded (along with its
// namespace) when the goroutine returns. preUnshare=false lets fn do its
// own unshare (e.g. via Prepare).
func inNewMountNS(preUnshare bool, fn func() error) error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // intentionally never unlocked
		errc <- func() error {
			if preUnshare {
				if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
					return fmt.Errorf("unshare: %w", err)
				}
				if err := MakePrivate("/"); err != nil {
					return err
				}
			}
			return fn()
		}()
	}()
	return <-errc
}

func mountsVisible(newRoot string) error {
	// If /proc, /sys, /dev are actually mounted AND not shadowed, these
	// paths (which only exist inside the respective mounts) resolve.
	for _, p := range []string{"proc/self", "sys/kernel", "dev/null"} {
		if _, err := os.Stat(filepath.Join(newRoot, p)); err != nil {
			return fmt.Errorf("%s not visible under new root: %w", p, err)
		}
	}
	return nil
}

// TestPrepareEssentialsVisible is the regression test for the mount-ordering
// bug: the new root must be self-bound BEFORE the essentials are mounted,
// otherwise the non-recursive self-bind stacks a mount that shadows /proc,
// /sys, and /dev and the pivoted system comes up without them.
func TestPrepareEssentialsVisible(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root + CAP_SYS_ADMIN for mount namespaces")
	}
	newRoot := minimalRoot(t)
	err := inNewMountNS(false, func() error {
		if err := Prepare(PrepareOptions{
			NewRoot:         newRoot,
			SkipVerify:      true,
			CreateNamespace: true,
		}); err != nil {
			return fmt.Errorf("Prepare: %w", err)
		}
		return mountsVisible(newRoot)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestBuggyOrderShadowsEssentials documents *why* the order matters: mounting
// the essentials first and self-binding afterwards (the old sequence) shadows
// /proc, so proc/self is no longer reachable under the new root. If this ever
// stops holding, the ordering constraint in Prepare/PivotRoot can be relaxed.
func TestBuggyOrderShadowsEssentials(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root + CAP_SYS_ADMIN for mount namespaces")
	}
	newRoot := minimalRoot(t)
	err := inNewMountNS(true, func() error {
		// Old (buggy) order: essentials, THEN self-bind.
		if err := SetupEssentials(newRoot); err != nil {
			return fmt.Errorf("SetupEssentials: %w", err)
		}
		if err := EnsureMountPoint(newRoot); err != nil {
			return fmt.Errorf("EnsureMountPoint: %w", err)
		}
		if err := MakePrivate(newRoot); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(newRoot, "proc/self")); err == nil {
			return fmt.Errorf("expected the buggy order to shadow /proc, but proc/self is visible")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
