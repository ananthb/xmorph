package helpers

import (
	"bytes"
	"os"

	"github.com/ananthb/xmorph/internal/config"
)

// ResolveTailscaleArgs returns the effective `tailscale up` argument
// string. If the user supplied --tailscale.args, that's used verbatim;
// otherwise the default is "--ssh --hostname=<hostname>-xmorph" plus
// --login-server when --tailscale.server was given.
//
// Mirrors src/helpers.zig:38-65, with the xenomorph → xmorph hostname
// suffix rename.
func ResolveTailscaleArgs(cfg *config.Config) string {
	if !cfg.TailscaleEnabled() {
		return "--ssh"
	}
	if cfg.TailscaleArgs != "" {
		return cfg.TailscaleArgs
	}

	host := readHostname()
	var b bytes.Buffer
	b.WriteString("--ssh --hostname=")
	b.WriteString(host)
	b.WriteString("-xmorph")
	if cfg.TailscaleServer != "" {
		b.WriteString(" --login-server=")
		b.WriteString(cfg.TailscaleServer)
	}
	return b.String()
}

func readHostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "host"
}
