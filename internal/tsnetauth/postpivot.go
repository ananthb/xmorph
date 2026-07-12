package tsnetauth

import (
	"context"
	"fmt"
	"log/slog"

	"tailscale.com/tsnet"
)

// PostPivotOptions configures NewPostPivotServer.
type PostPivotOptions struct {
	// Hostname must match what PreAuth used.
	Hostname string
}

// NewPostPivotServer opens tsnet from the state persisted by PreAuth
// and brings the node up on the tailnet. Caller owns Close.
func NewPostPivotServer(ctx context.Context, opts PostPivotOptions) (*tsnet.Server, error) {
	srv := &tsnet.Server{
		Dir:      "/var/lib/tailscale",
		Hostname: opts.Hostname,
	}
	if _, err := srv.Up(ctx); err != nil {
		srv.Close()
		return nil, fmt.Errorf("post-pivot tsnet up: %w", err)
	}
	return srv, nil
}

// ServeSSH runs Tailscale-native SSH (tsnet ListenSSH). Blocks until
// ctx is cancelled. addr empty defaults to ":22".
func ServeSSH(ctx context.Context, srv *tsnet.Server, addr string) error {
	if addr == "" {
		addr = ":22"
	}
	ln, err := srv.ListenSSH(addr)
	if err != nil {
		return fmt.Errorf("tsnet ListenSSH %s: %w", addr, err)
	}
	defer ln.Close()
	slog.Info("tsnet SSH up", "hostname", srv.Hostname, "addr", addr)
	<-ctx.Done()
	return ctx.Err()
}

// PostPivot preserves the old one-call API: create + serve. Kept for
// callers that don't need the *tsnet.Server for their own listeners.
func PostPivot(ctx context.Context, opts PostPivotOptions) error {
	srv, err := NewPostPivotServer(ctx, opts)
	if err != nil {
		return err
	}
	defer srv.Close()
	return ServeSSH(ctx, srv, ":22")
}
