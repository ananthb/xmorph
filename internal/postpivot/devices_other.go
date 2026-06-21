//go:build !linux

package postpivot

// EnsureDeviceNodes is a no-op on non-Linux platforms. The real
// post-pivot init only runs under Linux (it requires the kernel
// pivot_root semantics), so the no-op only exists so test builds
// succeed on developer machines.
func EnsureDeviceNodes() {}
