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

// TailscaleHostname returns the hostname tsnet should advertise to the
// tailnet. Defaults to "<system-hostname>-xmorph"; if the user supplied
// a custom --tailscale.args with --hostname=, we honor that prefix.
func TailscaleHostname(cfg *config.Config) string {
	// Honor an explicit --hostname= in --tailscale.args.
	if cfg.TailscaleArgs != "" {
		for _, tok := range splitArgs(cfg.TailscaleArgs) {
			if rest, ok := stringsCutPrefix(tok, "--hostname="); ok && rest != "" {
				return rest
			}
		}
	}
	return readHostname() + "-xmorph"
}

func splitArgs(s string) []string {
	var out []string
	var cur []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			if len(cur) > 0 {
				out = append(out, string(cur))
				cur = cur[:0]
			}
			continue
		}
		cur = append(cur, c)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func stringsCutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}
