// Package tsnetauth runs Tailscale in-process via tsnet, replacing the
// "ship tailscaled+tailscale into the rootfs" pattern from the Zig
// version.
//
// Two halves:
//
//   - PreAuth (host-side, before pivot_root): opens a tsnet.Server with
//     Dir pointed at {rootfs}/var/lib/tailscale and AuthKey from --tailscale.authkey,
//     calls srv.Up(ctx) to authenticate against the control server,
//     then srv.Close() so the state flushes to disk inside the new
//     rootfs. If auth fails, we return the error and abort BEFORE
//     destroying the running OS.
//
//   - PostPivot (rootfs-side, after pivot_root, from xmorph --init):
//     re-opens the same Dir (no AuthKey needed; state persisted) and
//     calls srv.RunSSH(ctx) to handle inbound Tailscale SSH.
package tsnetauth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"tailscale.com/tsnet"
)

// PreAuthOptions configures PreAuth.
type PreAuthOptions struct {
	// RootfsPath is the absolute path to the new rootfs. The tsnet
	// state lives in RootfsPath/var/lib/tailscale.
	RootfsPath string
	// AuthKey from --tailscale.authkey. Must start with tskey-.
	AuthKey string
	// Hostname is the Tailscale machine name. Empty defaults to the
	// system hostname plus "-xmorph" (handled by the caller via
	// helpers.ResolveTailscaleArgs).
	Hostname string
	// ControlURL is the coordination server, from --tailscale.server.
	// Empty = Tailscale-hosted (login.tailscale.com).
	ControlURL string
}

// PreAuth brings up tsnet to validate the authkey + control endpoint
// against the live network, then closes it so the state flushes to disk
// inside the new rootfs. On auth failure, returns the error so the
// caller can abort the pivot before the running OS is destroyed.
//
// Mirrors the intent of src/cmd/pivot.zig:316-335 with a different
// mechanism (tsnet vs spawning tailscaled in the rootfs).
func PreAuth(ctx context.Context, opts PreAuthOptions) error {
	dir := filepath.Join(opts.RootfsPath, "var/lib/tailscale")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tsnet state dir: %w", err)
	}

	srv := &tsnet.Server{
		Dir:        dir,
		AuthKey:    opts.AuthKey,
		Hostname:   opts.Hostname,
		ControlURL: opts.ControlURL,
		// Stick to ephemeral logging; the daemon would otherwise try
		// to phone home a stable identifier.
		Ephemeral: false,
	}
	defer srv.Close()

	// srv.Up blocks until the node is authenticated and the local
	// engine reports running. Errors here are the real auth failures
	// we want to surface.
	if _, err := srv.Up(ctx); err != nil {
		return fmt.Errorf("tsnet up: %w", err)
	}
	return nil
}
