//go:build linux

package pivot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// pivotChildEnv, when set, tells TestPivotRootChild to actually perform the
// pivot (it is otherwise a no-op that a normal `go test` run skips). Its
// value is the path to the new root.
const pivotChildEnv = "XMORPH_TEST_PIVOT_ROOT"

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

// TestPivotRootEndToEnd performs a real pivot_root and asserts /proc, /sys,
// and /dev are mounted and usable *after* the swap — the full end-to-end
// version of the mount-ordering regression. It runs the pivot in a separate
// child process (re-execing this test binary) so the swap can't disturb the
// test driver's own root.
func TestPivotRootEndToEnd(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root + CAP_SYS_ADMIN for pivot_root")
	}
	root := minimalRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "mnt", "oldroot"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run", "TestPivotRootChild", "-test.v")
	cmd.Env = append(os.Environ(), pivotChildEnv+"="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pivot child failed: %v\n%s", err, out)
	}
}

// TestPivotRootChild is the child half of TestPivotRootEndToEnd: it prepares
// and pivots into the root passed via pivotChildEnv, then verifies the
// essentials survived. Without the env var it is skipped, so it stays inert
// during a normal test run.
func TestPivotRootChild(t *testing.T) {
	root := os.Getenv(pivotChildEnv)
	if root == "" {
		t.Skip("child half of TestPivotRootEndToEnd")
	}
	runtime.LockOSThread() // never unlocked: this process pivots and exits
	if err := Prepare(PrepareOptions{
		NewRoot:         root,
		SkipVerify:      true,
		CreateNamespace: true,
	}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := PivotRoot(root, "mnt/oldroot"); err != nil {
		t.Fatalf("PivotRoot: %v", err)
	}
	// "/" is now the pivoted root; the essentials must be present and usable.
	for _, p := range []string{"/proc/self/status", "/sys/kernel", "/dev/null"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s missing after pivot_root: %v", p, err)
		}
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
