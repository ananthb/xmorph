package postpivot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ananthb/xmorph/internal/tsnetauth"
)

// Run is the entry point for `xmorph --init` after the pivot. Reads
// /etc/xmorph-init.json, ensures device nodes + firewall flush, then
// supervises the entrypoint. argv is the command-line passed to
// xmorph --init <entrypoint> <args...>; if the JSON has its own
// entrypoint+command those take precedence.
//
// Mirrors the lifecycle of src/xenomorph-init.zig:main:
//
//  1. EnsureDeviceNodes
//  2. Load /etc/xmorph-init.json (optional)
//  3. Optional FlushFirewall
//  4. Set up SSH / tailscale env vars (M6)
//  5. Supervise the entrypoint
func Run(argv []string) int {
	EnsureDeviceNodes()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "xmorph --init: warning: %v\n", err)
	}

	if cfg != nil && cfg.FlushFirewall {
		FlushFirewall()
	}

	// Watchdog first — arm it before we touch the network or supervise the
	// entrypoint, so a hang anywhere in this function still triggers reset.
	var wd *Watchdog
	if cfg != nil && cfg.WatchdogTimeoutSeconds > 0 {
		wd = StartWatchdog(time.Duration(cfg.WatchdogTimeoutSeconds) * time.Second)
		defer wd.Close()
	}

	if cfg != nil && cfg.SSH != nil {
		go func() {
			if err := StartSSHServer(context.Background(), cfg.SSH); err != nil {
				slog.Error("sshd", "err", err)
			}
		}()
	}

	// Tailscale: re-open the tsnet state persisted by the pre-pivot
	// PreAuth and serve SSH on the tailnet. Runs in a goroutine so the
	// entrypoint supervisor still takes the foreground.
	if cfg != nil && cfg.Tailscale != nil {
		hostname := hostnameFromArgs(cfg.Tailscale.Args)
		go func() {
			if err := tsnetauth.PostPivot(context.Background(), tsnetauth.PostPivotOptions{
				Hostname: hostname,
			}); err != nil {
				slog.Error("tsnet post-pivot", "err", err)
			}
		}()
	}

	// Decide what to exec. Config-supplied entrypoint+command beats argv.
	var supervised []string
	if cfg != nil && len(cfg.Entrypoint) > 0 {
		supervised = append(supervised, cfg.Entrypoint...)
		supervised = append(supervised, cfg.Command...)
	} else {
		supervised = argv
	}
	if len(supervised) == 0 {
		fmt.Fprintln(os.Stderr, "xmorph --init: no entrypoint")
		return 1
	}

	rebootOnFailure := cfg == nil || cfg.RebootOnFailure
	code, err := Supervise(SuperviseOptions{
		Argv:            supervised,
		RebootOnFailure: rebootOnFailure,
		Watchdog:        wd,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "xmorph --init: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
