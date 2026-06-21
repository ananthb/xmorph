//go:build linux

package postpivot

import (
	"os"

	"golang.org/x/sys/unix"
)

func doReboot() {
	unix.Sync()
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
	// If reboot returned, the kernel rejected it (unprivileged or
	// disabled). Exit non-zero so a caller waiting on PID 1 can react.
	os.Exit(1)
}
