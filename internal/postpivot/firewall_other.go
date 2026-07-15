//go:build !linux

package postpivot

// FlushFirewall is a no-op off Linux. xmorph only pivots on Linux; this
// stub keeps the package building for host-side tests on other platforms.
func FlushFirewall() {}
