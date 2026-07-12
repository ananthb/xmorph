//go:build linux

package postpivot

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Watchdog holds /dev/watchdog open and pets it from a goroutine.
// Kernel-only — no userspace fallback. If /dev/watchdog is missing,
// StartWatchdog auto-loads softdog and errors out if that fails.
type Watchdog struct {
	timeout time.Duration
	fd      *os.File
	done    chan struct{}
	once    sync.Once
}

// _WDIOC_SETTIMEOUT sets the watchdog timeout in seconds.
// Layout: _IOWR('W', 6, int) — include/uapi/linux/watchdog.h.
const _WDIOC_SETTIMEOUT = 0xc0045706

// wdMagicClose disables the watchdog on close (writing 'V' before
// closing the fd; else the kernel still fires the reset).
const wdMagicClose byte = 'V'

// watchdogDev is the kernel watchdog device path. var so tests can swap it.
var watchdogDev = "/dev/watchdog"

// modprobeCmd auto-loads softdog when /dev/watchdog is absent. var
// so tests can swap it for a fake.
var modprobeCmd = func() error {
	return exec.Command("modprobe", "softdog").Run()
}

// EnsureWatchdogAvailable opens /dev/watchdog (loading softdog if
// needed), disarms it, and closes. Use pre-pivot so we fail fast
// while the old OS is still alive.
func EnsureWatchdogAvailable() error {
	f, err := openWatchdog()
	if err != nil {
		return err
	}
	_, _ = f.Write([]byte{wdMagicClose})
	_ = f.Close()
	return nil
}

func openWatchdog() (*os.File, error) {
	f, err := os.OpenFile(watchdogDev, os.O_WRONLY, 0)
	if err == nil {
		return f, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("open %s: %w", watchdogDev, err)
	}
	if merr := modprobeCmd(); merr != nil {
		return nil, fmt.Errorf("%s missing and modprobe softdog failed: %w", watchdogDev, merr)
	}
	f, err = os.OpenFile(watchdogDev, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s after modprobe softdog: %w", watchdogDev, err)
	}
	return f, nil
}

// StartWatchdog arms the kernel watchdog for timeout. Non-positive
// timeout returns (nil, nil) as a no-op. Returns an error if
// /dev/watchdog is missing and softdog can't be loaded.
func StartWatchdog(timeout time.Duration) (*Watchdog, error) {
	if timeout <= 0 {
		return nil, nil
	}
	f, err := openWatchdog()
	if err != nil {
		return nil, err
	}
	secs := int(timeout / time.Second)
	if secs < 1 {
		secs = 1
	}
	if err := unix.IoctlSetPointerInt(int(f.Fd()), _WDIOC_SETTIMEOUT, secs); err != nil {
		slog.Warn("watchdog: set timeout failed; using kernel default", "err", err)
	}
	slog.Info("watchdog: armed", "timeout", timeout)

	w := &Watchdog{
		timeout: timeout,
		fd:      f,
		done:    make(chan struct{}),
	}
	go w.pet()
	return w, nil
}

func (w *Watchdog) pet() {
	tick := time.NewTicker(w.timeout / 3)
	defer tick.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-tick.C:
			if _, err := w.fd.Write([]byte{0}); err != nil {
				slog.Warn("watchdog: write failed", "err", err)
			}
		}
	}
}

// Close disarms the watchdog. Safe on nil, idempotent.
func (w *Watchdog) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		close(w.done)
		_, _ = w.fd.Write([]byte{wdMagicClose})
		_ = w.fd.Close()
	})
}
