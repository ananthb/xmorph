package postpivot

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// SuperviseOptions configures Supervise.
type SuperviseOptions struct {
	// Argv is the entrypoint with its arguments. argv[0] is exec'd.
	Argv []string
	// Env is the environment passed to the entrypoint. Nil = inherit.
	Env []string
	// RebootOnFailure: if true and the entrypoint exits non-zero (or by
	// signal), sync the filesystem and trigger LINUX_REBOOT_CMD_RESTART
	// so the original OS comes back. Mirrors src/xenomorph-init.zig:336-352.
	RebootOnFailure bool
	// OldRootPath is unmounted before reboot; empty skips.
	OldRootPath string
	// LogWriter, if non-nil, tees the child's stdout + stderr.
	LogWriter io.Writer
}

// Supervise spawns Argv as a child process and forwards
// TERM/INT/HUP/USR1/USR2 to it. When the child exits, reaps any other
// orphans, then exits with the child's status (or reboots).
//
// This is the post-pivot equivalent of tini — the M4 xmorph --init
// path calls Supervise after EnsureDeviceNodes + FlushFirewall +
// service setup. Mirrors src/xenomorph-init.zig:217-353.
func Supervise(opts SuperviseOptions) (exitCode int, err error) {
	if len(opts.Argv) == 0 {
		return 1, errors.New("supervise: empty argv")
	}

	cmd := exec.Command(opts.Argv[0], opts.Argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if opts.LogWriter != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, opts.LogWriter)
		cmd.Stderr = io.MultiWriter(os.Stderr, opts.LogWriter)
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}

	if err := cmd.Start(); err != nil {
		return 127, fmt.Errorf("exec %s: %w", opts.Argv[0], err)
	}

	sigCh := make(chan os.Signal, 8)
	signal.Notify(sigCh,
		syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2,
	)
	defer signal.Stop(sigCh)

	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()

	for {
		select {
		case sig := <-sigCh:
			_ = cmd.Process.Signal(sig)
		case err := <-doneCh:
			signal.Stop(sigCh)
			reapOrphans()
			code := exitStatusFrom(cmd, err)
			if opts.RebootOnFailure && code != 0 {
				rebootSystem(opts.OldRootPath)
			}
			return code, nil
		}
	}
}

// reapOrphans waits for any remaining children (non-blocking) so the
// kernel doesn't accumulate zombies under us.
func reapOrphans() {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			return
		}
	}
}

func exitStatusFrom(cmd *exec.Cmd, waitErr error) int {
	if cmd.ProcessState != nil {
		if cmd.ProcessState.Exited() {
			return cmd.ProcessState.ExitCode()
		}
		// Signaled or stopped — surface as 128 + signal number, matching
		// shell conventions and the Zig version (src/xenomorph-init.zig:321-326).
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				return 128 + int(ws.Signal())
			}
		}
	}
	if waitErr != nil {
		return 1
	}
	return 0
}

// rebootSystem sleeps 5s (for log flush), then hands off to the
// platform doReboot.
func rebootSystem(oldRoot string) {
	time.Sleep(5 * time.Second)
	doReboot(oldRoot)
}
