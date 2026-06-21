package tsnetauth

import (
	"context"
	"fmt"
	"log/slog"

	"tailscale.com/tsnet"
)

// PostPivotOptions configures PostPivot.
type PostPivotOptions struct {
	// Hostname must match what PreAuth used; reusing the persisted
	// state requires the same hostname.
	Hostname string
	// SSHAddr is the bind address for Tailscale SSH (default ":22").
	SSHAddr string
}

// PostPivot is invoked from `xmorph --init` after the pivot. It opens
// tsnet at /var/lib/tailscale (the path the persisted PreAuth state
// landed at) and listens for Tailscale SSH on SSHAddr. AuthKey is empty
// — the state on disk has us already authenticated.
//
// Blocks until ctx is cancelled or the listener errors. The caller
// runs this in a goroutine alongside Supervise.
func PostPivot(ctx context.Context, opts PostPivotOptions) error {
	addr := opts.SSHAddr
	if addr == "" {
		addr = ":22"
	}

	srv := &tsnet.Server{
		Dir:      "/var/lib/tailscale",
		Hostname: opts.Hostname,
	}
	defer srv.Close()

	if _, err := srv.Up(ctx); err != nil {
		return fmt.Errorf("post-pivot tsnet up: %w", err)
	}

	// ListenSSH wires the in-process Tailscale SSH server to the
	// tailnet listener (tsnet.Server.ListenSSH in v1.100.0). The
	// returned net.Listener serves SSH connections — accepting and
	// closing them is handled internally by tsnet.
	ln, err := srv.ListenSSH(addr)
	if err != nil {
		return fmt.Errorf("tsnet ListenSSH %s: %w", addr, err)
	}
	defer ln.Close()

	slog.Info("tsnet SSH up", "hostname", opts.Hostname, "addr", addr)

	// Block until ctx is done; the SSH listener runs in the background
	// inside tsnet. Closing srv on defer tears down the listener.
	<-ctx.Done()
	return ctx.Err()
}
