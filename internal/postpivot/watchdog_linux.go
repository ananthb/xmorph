//go:build linux

package postpivot

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Watchdog resets the box if the post-pivot supervisor stops petting it
// before the timeout elapses. On Linux we prefer /dev/watchdog (kernel
// or hardware, driven by kernel timers even when userspace deadlocks).
// When that device is unavailable we fall back to a Go time.Timer that
// calls reboot(2) on expiry — that path still catches an entrypoint
// that hangs indefinitely, but not a wedged Go runtime.
type Watchdog struct {
	timeout time.Duration
	fd      *os.File
	done    chan struct{}
	once    sync.Once
}

// _WDIOC_SETTIMEOUT sets the watchdog timeout in seconds.
// Layout: _IOWR('W', 6, int) — see include/uapi/linux/watchdog.h.
const _WDIOC_SETTIMEOUT = 0xc0045706

// wdMagicClose disables the watchdog when written before close. Without
// it, closing the fd still lets the watchdog fire.
const wdMagicClose byte = 'V'

// watchdogDev is the path to the kernel watchdog device. Exposed as a
// var so tests can point it at a temp path.
var watchdogDev = "/dev/watchdog"

// StartWatchdog arms the watchdog for the given timeout. timeout must
// be > 0; callers should guard on that. Returns a Watchdog that must be
// Close()d on success — otherwise the timer fires and the box resets.
func StartWatchdog(timeout time.Duration) *Watchdog {
	if timeout <= 0 {
		return nil
	}
	w := &Watchdog{timeout: timeout, done: make(chan struct{})}
	if f, err := os.OpenFile(watchdogDev, os.O_WRONLY, 0); err == nil {
		secs := int(timeout / time.Second)
		if secs < 1 {
			secs = 1
		}
		if err := unix.IoctlSetPointerInt(int(f.Fd()), _WDIOC_SETTIMEOUT, secs); err != nil {
			slog.Warn("watchdog: set timeout failed; using kernel default", "err", err)
		}
		w.fd = f
		slog.Info("watchdog: kernel /dev/watchdog armed", "timeout", timeout)
		go w.petKernel()
		return w
	} else {
		slog.Warn("watchdog: kernel unavailable, using userspace timer", "err", err)
	}
	go w.petUserspace()
	return w
}

func (w *Watchdog) petKernel() {
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

func (w *Watchdog) petUserspace() {
	select {
	case <-w.done:
		return
	case <-time.After(w.timeout):
		slog.Error("watchdog: userspace timer expired; rebooting")
		doReboot()
	}
}

// Close disables the watchdog and stops the pet goroutine. Safe to call
// on a nil receiver or multiple times.
func (w *Watchdog) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		close(w.done)
		if w.fd != nil {
			_, _ = w.fd.Write([]byte{wdMagicClose})
			_ = w.fd.Close()
		}
	})
}
