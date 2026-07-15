package initsys

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	systemdDest = "org.freedesktop.systemd1"
	systemdPath = dbus.ObjectPath("/org/freedesktop/systemd1")
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
	if c.Kind == Systemd {
		// basic.target keeps journald + dbus up but stops user services;
		// "isolate" stops everything the target doesn't require. Done over
		// D-Bus rather than shelling out to systemctl.
		return systemdStartUnit("basic.target", "isolate")
	}
	// OpenRC/SysVinit/runit/… expose no native control channel we can talk
	// to without shelling out, and the transition is advisory anyway: the
	// native process terminator (process.Terminate) stops what's running.
	return nil
}

// systemdStartUnit invokes Manager.StartUnit(name, mode) on the system bus.
func systemdStartUnit(name, mode string) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	defer conn.Close()
	var job dbus.ObjectPath
	if err := conn.Object(systemdDest, systemdPath).Call(
		systemdDest+".Manager.StartUnit", 0, name, mode,
	).Store(&job); err != nil {
		return fmt.Errorf("StartUnit %s (%s): %w", name, mode, err)
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
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil // can't measure; trust the timeout
	}
	defer conn.Close()
	mgr := conn.Object(systemdDest, systemdPath)

	deadline := time.Now().Add(c.Timeout)
	for time.Now().Before(deadline) {
		n, err := systemdJobCount(mgr)
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

// systemdJob mirrors one entry of Manager.ListJobs (D-Bus a(usssoo)).
type systemdJob struct {
	ID       uint32
	Unit     string
	Type     string
	State    string
	JobPath  dbus.ObjectPath
	UnitPath dbus.ObjectPath
}

// systemdJobCount returns the number of jobs systemd currently has queued.
func systemdJobCount(mgr dbus.BusObject) (int, error) {
	var jobs []systemdJob
	if err := mgr.Call(systemdDest+".Manager.ListJobs", 0).Store(&jobs); err != nil {
		return 0, err
	}
	return len(jobs), nil
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
