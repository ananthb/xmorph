package sysmem

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleMeminfo = `MemTotal:        8000000 kB
MemFree:         3000000 kB
MemAvailable:    4000000 kB
Buffers:          200000 kB
Cached:          1500000 kB
SwapCached:            0 kB
Active:          1000000 kB
Inactive:        2000000 kB
`

func TestReadParsesKnownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(path, []byte(sampleMeminfo), 0o644); err != nil {
		t.Fatal(err)
	}
	mi, err := readFrom(path)
	if err != nil {
		t.Fatalf("readFrom: %v", err)
	}
	const KB = uint64(1024)
	if mi.Total != 8000000*KB {
		t.Errorf("Total = %d, want %d", mi.Total, 8000000*KB)
	}
	if mi.Free != 3000000*KB {
		t.Errorf("Free = %d", mi.Free)
	}
	if mi.Available != 4000000*KB {
		t.Errorf("Available = %d", mi.Available)
	}
}

func TestReadMemAvailableFallback(t *testing.T) {
	// Old kernels without MemAvailable: Read should fall back to Free.
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal: 1000 kB\nMemFree: 500 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mi, err := readFrom(path)
	if err != nil {
		t.Fatalf("readFrom: %v", err)
	}
	if mi.Available != 500*1024 {
		t.Errorf("Available fallback = %d, want %d", mi.Available, 500*1024)
	}
}

func TestHeadroomCheck(t *testing.T) {
	mi := &MemInfo{Total: 8000 * 1024 * 1024, Available: 4000 * 1024 * 1024}

	// Fits comfortably: 500 MiB rootfs, 3500 MiB headroom > 800 MiB (10% of total).
	warn, err := mi.HeadroomCheck(500 * 1024 * 1024)
	if err != nil {
		t.Errorf("comfortable fit returned error: %v", err)
	}
	if warn {
		t.Errorf("comfortable fit should not warn")
	}

	// Tight but acceptable: rootfs takes most of available but leaves > 10% of total.
	// Available - rootfs must stay above 800 MiB AND below 2000 MiB (25% of total).
	warn, err = mi.HeadroomCheck(2500 * 1024 * 1024)
	if err != nil {
		t.Errorf("tight fit returned error: %v", err)
	}
	if !warn {
		t.Errorf("tight fit should warn")
	}

	// Too big: rootfs exceeds the 10% headroom rule.
	_, err = mi.HeadroomCheck(3500 * 1024 * 1024)
	if err == nil {
		t.Errorf("oversized rootfs should error")
	}
}
