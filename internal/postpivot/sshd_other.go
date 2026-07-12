//go:build !linux

package postpivot

import (
	"context"
	"errors"
	"net"
)

// TailnetListener parity with the Linux build.
type TailnetListener interface {
	Listen(network, addr string) (net.Listener, error)
}

// StartSSHServer is a stub on non-Linux platforms. The pty + os/exec
// shell handling requires Linux semantics, so we don't build the
// server elsewhere.
func StartSSHServer(_ context.Context, _ *SSHConfig, _ TailnetListener) error {
	return errors.New("sshd: not supported on this platform")
}
