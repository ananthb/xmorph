//go:build !linux

package postpivot

import "time"

// Watchdog is a no-op on non-Linux platforms.
type Watchdog struct{}

// StartWatchdog is a no-op stub. The watchdog device and reboot(2) are
// Linux-specific; on other platforms the caller gets a nil watchdog.
func StartWatchdog(timeout time.Duration) *Watchdog { return nil }

// Close is a no-op.
func (*Watchdog) Close() {}

// Ping is a no-op.
func (*Watchdog) Ping() {}

// PetInterval is zero — external petting is unnecessary on this platform.
func (*Watchdog) PetInterval() time.Duration { return 0 }
