//go:build linux

package postpivot

import (
	"log/slog"
	"os"

	"github.com/ananthb/xmorph/internal/pivot"
	"golang.org/x/sys/unix"
)

// doReboot syncs, unmounts oldRoot (so its journal closes cleanly),
// then triggers LINUX_REBOOT_CMD_RESTART. Empty oldRoot skips unmount.
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
		unix.Sync()
	}
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
	os.Exit(1)
}
