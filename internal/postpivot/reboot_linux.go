//go:build linux

package postpivot

import (
	"log/slog"
	"os"

	"github.com/ananthb/xmorph/internal/pivot"
	"golang.org/x/sys/unix"
)

// doReboot unmounts the old root (if oldRoot is non-empty), flushes,
// and issues LINUX_REBOOT_CMD_RESTART. Unmounting first keeps the
// journal on the pre-pivot filesystem clean — the kernel restart
// doesn't run any userspace shutdown scripts, so without this the
// old root lands dirty and the next boot needs fsck (or worse, drops
// into emergency.target when a downstream mount unit fails).
func doReboot(oldRoot string) {
	unix.Sync()
	if oldRoot != "" {
		failed, err := pivot.UnmountOldRoot(oldRoot)
		if err != nil {
			slog.Warn("reboot: enumerate old-root mounts", "err", err)
		}
		for _, f := range failed {
			slog.Warn("reboot: unmount failed", "mount", f)
		}
		unix.Sync() // catch anything the unmount flushed
	}
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
	os.Exit(1)
}
