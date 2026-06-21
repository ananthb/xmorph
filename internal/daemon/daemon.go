// Package daemon implements headless detach for `xmorph pivot --headless`.
// The parent process forks (via raw clone with SIGCHLD because Go's
// syscall.ForkExec always execs and we want to keep running the Go
// program in the child), prints the child PID + log path, and exits.
// The child becomes session leader and redirects stdio.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// LogFileName is the basename written under cfg.LogDir for headless mode.
// Path becomes filepath.Join(logDir, LogFileName).
const LogFileName = "xmorph.log"

// Daemonize forks once via raw clone(SIGCHLD,…). The parent prints the
// child PID + log path and exits. The child:
//
//  1. setsid() so SIGHUP from the dying sshd session doesn't kill it.
//  2. opens the log file and redirects stderr → it.
//  3. opens /dev/null and redirects stdin + stdout → it.
//
// Risk #2 in the port plan: Go's syscall.ForkExec always execs, so we
// use unix.Syscall(SYS_CLONE, SIGCHLD, 0, 0) directly — mirrors
// src/daemon.zig:28-31. This runs BEFORE any goroutine fan-out so the
// Go runtime's locks are not in flight.
func Daemonize(logDir string) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, LogFileName)
	logFD, err := unix.Open(logPath, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}

	pid, _, errno := unix.Syscall(unix.SYS_CLONE, uintptr(syscall.SIGCHLD), 0, 0)
	if errno != 0 {
		_ = unix.Close(logFD)
		return fmt.Errorf("clone: %v", errno)
	}

	if pid > 0 {
		// Parent: print status and exit.
		fmt.Printf("xmorph: daemonized (pid=%d, log=%s)\n", pid, logPath)
		_ = unix.Close(logFD)
		os.Exit(0)
	}

	// Child path.
	if _, err := unix.Setsid(); err != nil {
		// Continue even if Setsid fails — best-effort detach.
		_ = err
	}

	nullFD, err := unix.Open("/dev/null", unix.O_RDWR, 0)
	if err == nil {
		_ = unix.Dup2(nullFD, 0) // stdin  -> /dev/null
		_ = unix.Dup2(nullFD, 1) // stdout -> /dev/null
		if nullFD > 2 {
			_ = unix.Close(nullFD)
		}
	}
	_ = unix.Dup2(logFD, 2) // stderr -> logfile
	if logFD > 2 {
		_ = unix.Close(logFD)
	}
	return nil
}
