//go:build !linux

package postpivot

import (
	"errors"
	"os"
	"time"
)

// WatchdogFDEnv mirrors the linux constant so cross-platform callers compile.
const WatchdogFDEnv = "XMORPH_WATCHDOG_FD"

// Watchdog is a no-op on non-Linux platforms.
type Watchdog struct{}

// EnsureWatchdogAvailable always fails — no watchdog on this platform.
func EnsureWatchdogAvailable() error {
	return errors.New("watchdog: not supported on this platform")
}

// StartWatchdog always fails — no watchdog on this platform.
func StartWatchdog(_ time.Duration) (*Watchdog, error) {
	return nil, errors.New("watchdog: not supported on this platform")
}

// ArmWatchdogPrePivot always fails — no watchdog on this platform.
func ArmWatchdogPrePivot(_ time.Duration) (*os.File, error) {
	return nil, errors.New("watchdog: not supported on this platform")
}

// AdoptWatchdog is a no-op stub on non-Linux platforms.
func AdoptWatchdog(_ int, _ time.Duration) *Watchdog {
	return &Watchdog{}
}

// Close is a no-op.
func (*Watchdog) Close() {}
