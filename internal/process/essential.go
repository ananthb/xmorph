package process

import (
	"os"
	"strings"
)

// essentialNames mirrors src/process/essential.zig:22-63 — processes
// that must NOT be terminated before pivot_root. Match by full comm OR
// by prefix (e.g. "kworker/0:0" matches "kworker").
var essentialNames = []string{
	// Kernel threads
	"kthreadd", "ksoftirqd", "kworker", "migration", "watchdog",
	"kcompactd", "khugepaged", "kswapd", "kblockd",
	// Init systems
	"systemd", "init", "openrc", "runit", "s6-svscan",
	// Device management
	"udevd", "systemd-udevd", "eudev", "mdev",
	// Logging
	"journald", "systemd-journald", "rsyslogd", "syslog-ng",
	// Networking
	"dhclient", "dhcpcd", "NetworkManager", "wpa_supplicant",
	// Storage
	"lvmetad", "multipathd", "iscsid",
}

// IsEssential reports whether i is essential and should be preserved.
// Mirrors src/process/essential.zig:isEssentialProcess.
func IsEssential(i *Info) bool {
	if i.PID == 1 {
		return true
	}
	if i.IsKernelThread() {
		return true
	}
	if i.PID == os.Getpid() {
		return true
	}
	for _, name := range essentialNames {
		if i.Comm == name || strings.HasPrefix(i.Comm, name) {
			return true
		}
	}
	// Bracketed comm names are kernel threads in /proc on some kernels.
	if strings.HasPrefix(i.Comm, "[") {
		return true
	}
	return false
}

// IsEssentialName matches by the same allowlist as IsEssential but
// without an Info struct. Used by tests and the dry-run path.
func IsEssentialName(name string) bool {
	for _, n := range essentialNames {
		if name == n || strings.HasPrefix(name, n) {
			return true
		}
	}
	return false
}
