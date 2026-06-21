//go:build linux

package postpivot

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// device describes one essential device node to create if missing.
type device struct {
	path  string
	major uint32
	minor uint32
}

// essentialDevices are the device nodes the post-pivot init ensures
// exist. Mirrors src/xenomorph-init.zig:70-101.
var essentialDevices = []device{
	{path: "/dev/null", major: 1, minor: 3},
	{path: "/dev/zero", major: 1, minor: 5},
	{path: "/dev/random", major: 1, minor: 8},
	{path: "/dev/urandom", major: 1, minor: 9},
	{path: "/dev/tty", major: 5, minor: 0},
}

// EnsureDeviceNodes creates the essential character devices if they
// don't already exist. The /dev bind mount inherited from the old
// root usually carries these, but minimal rootfses might not.
// /dev/net/tun is created on best effort for VPN/WireGuard.
func EnsureDeviceNodes() {
	for _, d := range essentialDevices {
		if _, err := os.Stat(d.path); err == nil {
			continue
		}
		mode := uint32(unix.S_IFCHR | 0o666)
		dev := unix.Mkdev(d.major, d.minor)
		_ = unix.Mknod(d.path, mode, int(dev))
	}
	if _, err := os.Stat("/dev/net/tun"); errors.Is(err, os.ErrNotExist) {
		_ = os.MkdirAll("/dev/net", 0o755)
		dev := unix.Mkdev(10, 200)
		_ = unix.Mknod("/dev/net/tun", uint32(unix.S_IFCHR|0o666), int(dev))
	}
}
