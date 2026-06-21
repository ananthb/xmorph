// Package initsys detects the running init system (systemd, OpenRC,
// SysV init, …) and coordinates a graceful shutdown of services before
// xmorph pivots into the new rootfs. Mirrors src/init/* in the Zig
// version. Package name is `initsys` to dodge the Go `init` keyword.
package initsys

import (
	"bytes"
	"errors"
	"os"
	"strings"
)

// Kind tags the detected init system.
type Kind int

const (
	Unknown Kind = iota
	Systemd
	OpenRC
	SysVinit
	Runit
	S6
	Upstart
)

func (k Kind) String() string {
	switch k {
	case Systemd:
		return "systemd"
	case OpenRC:
		return "OpenRC"
	case SysVinit:
		return "SysV init"
	case Runit:
		return "runit"
	case S6:
		return "s6"
	case Upstart:
		return "Upstart"
	}
	return "unknown"
}

// Detect reads PID 1's comm and various marker files to identify the
// running init system. Mirrors src/init/detector.zig:42-120.
func Detect() Kind {
	if k, ok := detectViaPID1Comm(); ok {
		return k
	}
	if exists("/run/systemd/system") {
		return Systemd
	}
	if exists("/run/openrc/softlevel") || exists("/etc/init.d/openrc") {
		return OpenRC
	}
	if exists("/etc/service") {
		return Runit
	}
	if exists("/service") || exists("/etc/s6") {
		return S6
	}
	if exists("/etc/init") && exists("/sbin/initctl") {
		return Upstart
	}
	if exists("/etc/inittab") {
		return SysVinit
	}
	return Unknown
}

// detectViaPID1Comm peeks at /proc/1/comm. Cheap and covers the
// overwhelming majority of cases.
func detectViaPID1Comm() (Kind, bool) {
	data, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return Unknown, false
	}
	comm := strings.TrimSpace(string(bytes.TrimRight(data, "\x00\n")))
	switch comm {
	case "systemd":
		return Systemd, true
	case "openrc", "openrc-init":
		return OpenRC, true
	case "init":
		// "init" is ambiguous — could be SysV or busybox. Treat as SysV
		// unless other markers say otherwise; the caller can override.
		return SysVinit, true
	case "runit", "runsvdir":
		return Runit, true
	case "s6-svscan":
		return S6, true
	}
	return Unknown, false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
