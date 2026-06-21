package config

import (
	"errors"
	"fmt"
	"io"
)

// ErrInvalidTimeout is returned when --timeout=0.
var ErrInvalidTimeout = errors.New("timeout must be greater than zero")

// Validate runs the same set of post-parse checks as src/config.zig:598-626,
// minus the containerfile mutual-exclusion rule (containerfile support was
// dropped). Warnings go to warnW (typically os.Stderr).
func (c *Config) Validate(warnW io.Writer) error {
	if c.Timeout == 0 {
		return ErrInvalidTimeout
	}

	if c.TailscaleAuthkey == "" {
		if c.TailscaleArgs != "" {
			fmt.Fprintln(warnW, "Warning: --tailscale.args without --tailscale.authkey won't start tailscale")
		}
		if c.TailscaleImage != DefaultTailscaleImg {
			fmt.Fprintln(warnW, "Warning: --tailscale.image without --tailscale.authkey won't start tailscale")
		}
	}

	// --tailscale.image is meaningless under tsnet; warn so users know it'll
	// be removed in a future release (see plan, risk #5).
	if c.TailscaleImage != DefaultTailscaleImg {
		fmt.Fprintln(warnW, "Warning: --tailscale.image is deprecated and ignored — xmorph runs Tailscale via tsnet in-process")
	}

	if c.Headless && !c.TailscaleEnabled() && c.SSHPort == nil {
		fmt.Fprintln(warnW, "Warning: --headless without remote access — ensure your entrypoint provides access")
	}

	return nil
}
