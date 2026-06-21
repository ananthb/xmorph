package postpivot

import (
	"fmt"
	"log/slog"
	"os"
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

	// SSH setup: M4's stub. Dropbear bring-up arrives at M5/M6 — for
	// now we just log so behavior is visible in --contain integration
	// tests.
	if cfg != nil && cfg.SSH != nil {
		slog.Info("ssh requested", "port", cfg.SSH.Port)
	}

	// Tailscale env vars: M6 wires tsnet.RunSSH; M4 just leaves the
	// env state alone.
	if cfg != nil && cfg.Tailscale != nil {
		slog.Info("tailscale config present", "args", cfg.Tailscale.Args)
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
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "xmorph --init: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
