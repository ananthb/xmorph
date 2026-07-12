//go:build linux

package postpivot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fakeWatchdog(t *testing.T) string {
	t.Helper()
	prev := watchdogDev
	dir := t.TempDir()
	dev := filepath.Join(dir, "watchdog")
	f, err := os.Create(dev)
	if err != nil {
		t.Fatalf("create fake wdev: %v", err)
	}
	f.Close()
	watchdogDev = dev
	t.Cleanup(func() { watchdogDev = prev })
	return dev
}

func TestStartWatchdogKernelPath(t *testing.T) {
	dev := fakeWatchdog(t)

	w, err := StartWatchdog(300 * time.Millisecond)
	if err != nil {
		t.Fatalf("StartWatchdog: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil watchdog")
	}

	time.Sleep(300 * time.Millisecond)
	w.Close()

	data, err := os.ReadFile(dev)
	if err != nil {
		t.Fatalf("read fake wdev: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != wdMagicClose {
		t.Fatalf("expected trailing 'V' magic; got %q", data)
	}
	w.Close() // idempotent
}

func TestStartWatchdogZeroReturnsNil(t *testing.T) {
	w, err := StartWatchdog(0)
	if w != nil || err != nil {
		t.Errorf("StartWatchdog(0) = (%v, %v), want (nil, nil)", w, err)
	}
	w, err = StartWatchdog(-time.Second)
	if w != nil || err != nil {
		t.Errorf("StartWatchdog(<0) = (%v, %v), want (nil, nil)", w, err)
	}
}

func TestWatchdogCloseNilSafe(t *testing.T) {
	var w *Watchdog
	w.Close()
}

func TestOpenWatchdogModprobesSoftdog(t *testing.T) {
	prev := watchdogDev
	prevModprobe := modprobeCmd
	dir := t.TempDir()
	dev := filepath.Join(dir, "watchdog-missing")
	watchdogDev = dev

	called := false
	modprobeCmd = func() error {
		called = true
		// Simulate softdog loading by creating the device.
		f, err := os.Create(dev)
		if err != nil {
			return err
		}
		return f.Close()
	}
	t.Cleanup(func() {
		watchdogDev = prev
		modprobeCmd = prevModprobe
	})

	f, err := openWatchdog()
	if err != nil {
		t.Fatalf("openWatchdog: %v", err)
	}
	f.Close()
	if !called {
		t.Error("expected modprobe softdog to be invoked")
	}
}

func TestOpenWatchdogFailsWhenSoftdogCantLoad(t *testing.T) {
	prev := watchdogDev
	prevModprobe := modprobeCmd
	watchdogDev = filepath.Join(t.TempDir(), "watchdog-missing")
	modprobeCmd = func() error { return errors.New("no softdog for you") }
	t.Cleanup(func() {
		watchdogDev = prev
		modprobeCmd = prevModprobe
	})

	if _, err := openWatchdog(); err == nil {
		t.Fatal("expected openWatchdog error")
	}
}

func TestEnsureWatchdogAvailableDisarms(t *testing.T) {
	dev := fakeWatchdog(t)

	if err := EnsureWatchdogAvailable(); err != nil {
		t.Fatalf("EnsureWatchdogAvailable: %v", err)
	}
	data, err := os.ReadFile(dev)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) != 1 || data[0] != wdMagicClose {
		t.Errorf("expected magic-close byte written; got %q", data)
	}
}
