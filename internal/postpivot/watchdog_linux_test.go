//go:build linux

package postpivot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStartWatchdogKernelPath points watchdogDev at a temp file so we
// can verify the pet loop actually writes and that Close disables cleanly.
func TestStartWatchdogKernelPath(t *testing.T) {
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

	w := StartWatchdog(300 * time.Millisecond)
	if w == nil {
		t.Fatal("StartWatchdog returned nil")
	}
	if w.fd == nil {
		t.Fatal("expected kernel-path (fd non-nil) for fake device")
	}

	// Give the pet goroutine a couple of ticks.
	time.Sleep(300 * time.Millisecond)
	w.Close()

	// After Close, last byte should be the magic-close 'V'.
	data, err := os.ReadFile(dev)
	if err != nil {
		t.Fatalf("read fake wdev: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != wdMagicClose {
		t.Fatalf("expected trailing 'V' magic; got %q", data)
	}

	// Close is idempotent.
	w.Close()
}

// TestStartWatchdogZeroReturnsNil covers the guard.
func TestStartWatchdogZeroReturnsNil(t *testing.T) {
	if StartWatchdog(0) != nil {
		t.Fatal("StartWatchdog(0) should return nil")
	}
	if StartWatchdog(-time.Second) != nil {
		t.Fatal("StartWatchdog(<0) should return nil")
	}
}

// TestWatchdogCloseNilSafe: nil-receiver Close mustn't panic.
func TestWatchdogCloseNilSafe(t *testing.T) {
	var w *Watchdog
	w.Close()
}
