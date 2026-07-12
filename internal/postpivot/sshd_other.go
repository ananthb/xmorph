//go:build !linux

package postpivot

import (
	"context"
	"errors"
)

// StartSSHServer is a stub on non-Linux platforms. The pty + os/exec
// shell handling requires Linux semantics, so we don't build the
// server elsewhere.
func StartSSHServer(_ context.Context, _ *SSHConfig) error {
	return errors.New("sshd: not supported on this platform")
}
