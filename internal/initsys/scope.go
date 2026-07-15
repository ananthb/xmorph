package initsys

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

// systemdProperty is a (string, variant) pair, the element type of
// StartTransientUnit's properties argument (D-Bus signature a(sv)).
type systemdProperty struct {
	Name  string
	Value dbus.Variant
}

// systemdAux mirrors StartTransientUnit's aux argument (a(sa(sv))). We pass
// an empty slice — no auxiliary units.
type systemdAux struct {
	Name       string
	Properties []systemdProperty
}

// RelocateToTransientScope asks systemd, over the system bus, to adopt the
// calling process into a fresh transient .scope unit. This is exactly what
// `systemd-run --scope` does, applied to the already-running process: it
// moves us out of whatever cgroup launched us — a run0/systemd-run
// transient .service, or an interactive SSH session scope — into a
// systemd-owned scope that is NOT reaped when the launcher exits (a
// .service stops with KillMode=control-group and SIGKILLs its leftover
// cgroup; a session scope's teardown does the same on logout).
//
// No fork and no exec: systemd simply reassigns our cgroup. Returns the
// created scope's unit name. Only meaningful on systemd hosts reachable
// over D-Bus; callers gate on Detect() == Systemd.
func RelocateToTransientScope(description string) (string, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return "", fmt.Errorf("connect system bus: %w", err)
	}
	defer conn.Close()

	// Scope names must be unique and end in ".scope"; tie it to our PID.
	name := fmt.Sprintf("xmorph-%d.scope", os.Getpid())
	props := []systemdProperty{
		{Name: "Description", Value: dbus.MakeVariant(description)},
		{Name: "PIDs", Value: dbus.MakeVariant([]uint32{uint32(os.Getpid())})},
		// Garbage-collect the scope once it goes inactive/failed rather
		// than leaving a dead unit behind (we never stop it cleanly — the
		// process pivots away).
		{Name: "CollectMode", Value: dbus.MakeVariant("inactive-or-failed")},
	}

	mgr := conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))
	// mode "fail": don't clobber an existing unit of the same name.
	var job dbus.ObjectPath
	if err := mgr.Call(
		"org.freedesktop.systemd1.Manager.StartTransientUnit", 0,
		name, "fail", props, []systemdAux{},
	).Store(&job); err != nil {
		return "", fmt.Errorf("StartTransientUnit %s: %w", name, err)
	}
	return name, nil
}

// RunningOverSSH reports whether the process was launched over an SSH
// session, per the variables sshd/tailscale-ssh export into the session.
func RunningOverSSH() bool {
	return os.Getenv("SSH_CONNECTION") != "" ||
		os.Getenv("SSH_CLIENT") != "" ||
		os.Getenv("SSH_TTY") != ""
}
