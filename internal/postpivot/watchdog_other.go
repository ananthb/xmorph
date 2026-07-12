//go:build !linux

package postpivot

import (
	"errors"
	"time"
)

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

// Close is a no-op.
func (*Watchdog) Close() {}
