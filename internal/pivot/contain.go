package pivot

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// ContainOptions describes the --contain test mode: run the rootfs
// inside a fresh mount+PID namespace WITHOUT executing a real
// pivot_root. Used for testing the rootfs build pipeline on a host
// you don't want to risk pivoting.
type ContainOptions struct {
	NewRoot    string
	Entrypoint string
	Args       []string
	Env        []string
}

// Contain re-execs Argv[0] inside an unshared mount+PID namespace,
// using exec.Command with SysProcAttr.Cloneflags. Inside the child,
// /dev /run /proc /sys get fresh mounts via SetupEssentials and the
// shell is chrooted into NewRoot. No pivot_root.
//
// This is the M3 implementation; src/cmd/pivot.zig:338-373 calls
// runz.run.runContainer which does the equivalent. Here we use the
// stdlib's CLONE_NEWNS|CLONE_NEWPID support directly.
func Contain(opts ContainOptions) error {
	runtime.LockOSThread() // namespace ops are per-thread
	defer runtime.UnlockOSThread()

	if opts.Entrypoint == "" {
		return fmt.Errorf("contain: empty entrypoint")
	}

	argv := append([]string{opts.Entrypoint}, opts.Args...)

	// We run the entrypoint INSIDE the new namespace via a shell-style
	// exec: spawn a /bin/sh -c that chroots and execs. Simpler than
	// re-exec'ing xmorph itself with another sentinel.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: unix.CLONE_NEWNS | unix.CLONE_NEWPID,
		// Chroot so the child sees NewRoot as /. PID-1 inside the new
		// PID namespace is the entrypoint itself; supervision is the
		// caller's problem (M4's postpivot.Run handles it).
		Chroot: opts.NewRoot,
	}

	// Ensure essential mounts exist on disk inside NewRoot — this
	// happens in the PARENT namespace before clone, because Chroot
	// applies to the child but mount() at this level affects only the
	// caller. The child can run with a stale /proc/sys until it
	// re-mounts; for --contain that's acceptable.
	if err := SetupEssentials(opts.NewRoot); err != nil {
		return fmt.Errorf("contain: setup essentials: %w", err)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("contain: child exited: %w", err)
	}
	return nil
}
