// Package pivot performs the actual pivot_root(2) dance: unshare a mount
// namespace, mount the essentials into the new rootfs, pivot, optionally
// clean up the old root. The mount sequence mirrors src/pivot/mounts.zig
// and src/pivot/pivot.zig exactly — kernels reject reorderings with EINVAL.
package pivot

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// EssentialMount describes a mount that must exist inside the new rootfs
// before pivot_root. Order matters: /dev and /run get recursive binds
// from the old root, /proc and /sys get fresh mounts.
type EssentialMount struct {
	Source string
	Target string // relative to new_root
	FSType string
	Bind   bool // true = bind mount from source; false = fresh mount of FSType
}

// EssentialMounts is the list applied by SetupEssentials. Mirrors
// src/pivot/mounts.zig:19-24 exactly.
var EssentialMounts = []EssentialMount{
	{Source: "/dev", Target: "dev", FSType: "", Bind: true},
	{Source: "proc", Target: "proc", FSType: "proc", Bind: false},
	{Source: "sysfs", Target: "sys", FSType: "sysfs", Bind: false},
	{Source: "/run", Target: "run", FSType: "", Bind: true},
}

// MakePrivate marks target's mount as MS_PRIVATE so propagation events
// don't leak into peer namespaces. Per the kernel docs this must be
// applied to "/" before doing any pivot_root work.
func MakePrivate(target string) error {
	return unix.Mount("", target, "", unix.MS_REC|unix.MS_PRIVATE, "")
}

// CreateMountNamespace unshares into a fresh mount namespace and makes
// "/" private. The caller MUST own the calling thread or pre-lock it
// (runtime.LockOSThread) — namespace state is per-thread in Linux and
// the Go runtime moves goroutines between threads.
func CreateMountNamespace() error {
	if err := unix.Unshare(unix.CLONE_NEWNS); err != nil {
		return fmt.Errorf("unshare CLONE_NEWNS: %w", err)
	}
	if err := MakePrivate("/"); err != nil {
		return fmt.Errorf("make / private: %w", err)
	}
	return nil
}

// EnsureMountPoint bind-mounts target onto itself so pivot_root sees it
// as a "real" mount point (a kernel requirement). Idempotent: if target
// is already a mount, the bind is a no-op.
func EnsureMountPoint(target string) error {
	if err := unix.Mount(target, target, "", unix.MS_BIND, ""); err != nil {
		// EINVAL/EBUSY here usually means "already a mount" — let the
		// caller decide whether to treat as fatal.
		return err
	}
	return nil
}

// SetupEssentials applies EssentialMounts inside newRoot. Recursive
// bind mounts are used for /dev and /run (matches src/pivot/mounts.zig:48-54).
// /proc and /sys get fresh mounts with nosuid,noexec,nodev.
func SetupEssentials(newRoot string) error {
	for _, m := range EssentialMounts {
		target := joinPath(newRoot, m.Target)
		if err := ensureDir(target); err != nil {
			return fmt.Errorf("create mount target %s: %w", target, err)
		}
		if m.Bind {
			err := unix.Mount(m.Source, target, "", unix.MS_BIND|unix.MS_REC, "")
			if err != nil {
				// /run can be missing on minimal systems — Zig tolerates
				// the failure (src/pivot/mounts.zig:50-53).
				if m.Source == "/run" {
					continue
				}
				return fmt.Errorf("bind %s -> %s: %w", m.Source, target, err)
			}
			continue
		}
		flags := uintptr(unix.MS_NOSUID | unix.MS_NOEXEC | unix.MS_NODEV)
		if err := unix.Mount(m.Source, target, m.FSType, flags, ""); err != nil {
			return fmt.Errorf("mount %s (%s) -> %s: %w", m.Source, m.FSType, target, err)
		}
	}
	return nil
}

// PivotRoot executes the sequence that swaps the process's rootfs:
//
//  1. MakePrivate("/")
//  2. EnsureMountPoint(newRoot) + MakePrivate(newRoot)
//  3. pivot_root(newRoot, newRoot/oldRootMount)
//  4. chdir("/")
//
// EssentialMounts must already be set up by the caller (e.g. via
// SetupEssentials). After return, "/" is newRoot and the old root is
// at "/" + oldRootMount.
func PivotRoot(newRoot, oldRootMount string) error {
	// Step 1.
	if err := MakePrivate("/"); err != nil {
		// MS_PRIVATE on "/" can fail in environments where "/" isn't
		// actually a mount (some containers). Log via return — caller
		// decides whether fatal.
		return fmt.Errorf("make / private: %w", err)
	}

	// Step 2.
	if err := EnsureMountPoint(newRoot); err != nil {
		// If already a mount point, bind is harmless; continue.
		_ = err
	}
	if err := MakePrivate(newRoot); err != nil {
		return fmt.Errorf("make new root private: %w", err)
	}

	// Step 3. pivot_root requires the old-root path to exist *inside*
	// new_root; the caller should have called ensureDir(newRoot + "/" + oldRootMount)
	// before now.
	putOld := joinPath(newRoot, oldRootMount)
	if err := unix.PivotRoot(newRoot, putOld); err != nil {
		return fmt.Errorf("pivot_root(%s, %s): %w", newRoot, putOld, err)
	}

	// Step 4.
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("chdir(/): %w", err)
	}
	return nil
}
