package initsys

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Coordinator gracefully transitions the running init system to a
// minimal state (rescue / single-user / equivalent) before pivot_root.
// Mirrors src/init/interface.zig:InitCoordinator.
type Coordinator struct {
	Kind    Kind
	Timeout time.Duration
}

// NewCoordinator detects the init system and returns a Coordinator with
// the supplied timeout.
func NewCoordinator(timeout time.Duration) Coordinator {
	return Coordinator{Kind: Detect(), Timeout: timeout}
}

// TransitionToRescue calls the init-system-specific command to drop
// down to rescue / single-user mode. Errors are advisory: failure
// shouldn't abort the pivot (the process terminator catches what
// the init system missed).
func (c Coordinator) TransitionToRescue() error {
	switch c.Kind {
	case Systemd:
		return c.run("systemctl", "isolate", "rescue.target")
	case OpenRC:
		return c.run("openrc", "single")
	case SysVinit:
		return c.run("telinit", "1")
	}
	return nil
}

// WaitForServicesToStop polls until pending jobs reach zero or the
// timeout expires. systemd is the only init that exposes a job count;
// for others we just sleep briefly.
func (c Coordinator) WaitForServicesToStop() error {
	if c.Kind != Systemd {
		// Best effort: a small grace period for the init to do its work.
		time.Sleep(min(c.Timeout, 2*time.Second))
		return nil
	}
	deadline := time.Now().Add(c.Timeout)
	for time.Now().Before(deadline) {
		n, err := c.systemdPendingJobs()
		if err != nil {
			return nil // can't measure; trust the timeout
		}
		if n == 0 {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timeout waiting for services to stop")
}

func (c Coordinator) systemdPendingJobs() (int, error) {
	out, err := exec.Command("systemctl", "list-jobs", "--no-legend").Output()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}

func (c Coordinator) run(argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w (%s)", strings.Join(argv, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ShouldSkipCoordination is true when we're inside a container — the
// init system there is the container runtime's, not the host's, and
// trying to coordinate would be wrong. Mirrors src/init/interface.zig:215-234.
func ShouldSkipCoordination() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	for _, marker := range []string{"docker", "lxc", "kubepods"} {
		if bytes.Contains(data, []byte(marker)) {
			return true
		}
	}
	return false
}
