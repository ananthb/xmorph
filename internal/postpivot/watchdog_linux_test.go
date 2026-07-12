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
	w.Ping()
	if w.PetInterval() != 0 {
		t.Error("nil PetInterval should be 0")
	}
}

// TestUserspacePingExtendsDeadline: when /dev/watchdog is unavailable
// the userspace timer resets on Ping. We can't wait full seconds in a
// unit test, so we drive petUserspace via a tiny timeout and rely on
// the reset channel semantics.
func TestUserspacePingExtendsDeadline(t *testing.T) {
	prev := watchdogDev
	watchdogDev = "/dev/nonexistent-force-userspace"
	t.Cleanup(func() { watchdogDev = prev })

	w := StartWatchdog(200 * time.Millisecond)
	if w == nil {
		t.Fatal("StartWatchdog returned nil")
	}
	if w.fd != nil {
		t.Fatal("expected userspace path (fd should be nil)")
	}
	if got := w.PetInterval(); got != 200*time.Millisecond/3 {
		t.Errorf("PetInterval = %v, want %v", got, 200*time.Millisecond/3)
	}
	// Ping repeatedly, faster than timeout, and confirm the goroutine
	// hasn't reached doReboot after 3 timeouts elapsed. We can't observe
	// doReboot() without dying, so this is a smoke test: no reboot ⇒ pass.
	for range 6 {
		w.Ping()
		time.Sleep(60 * time.Millisecond)
	}
	w.Close()
}

// TestKernelPathPingNoop: on the kernel path Ping and PetInterval must
// not exercise the userspace machinery.
func TestKernelPathPingNoop(t *testing.T) {
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

	w := StartWatchdog(time.Second)
	if w == nil || w.fd == nil {
		t.Fatal("expected kernel path")
	}
	defer w.Close()
	if got := w.PetInterval(); got != 0 {
		t.Errorf("kernel PetInterval = %v, want 0", got)
	}
	w.Ping() // must not panic or block
}
