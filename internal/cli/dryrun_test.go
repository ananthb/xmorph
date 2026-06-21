package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ananthb/xmorph/internal/config"
)

// TestDryRunOutputShape locks in the Zig-parity format from
// src/cmd/pivot.zig:486-563. Byte-exact diffs against a live Zig binary
// happen in the parity tests (added at M5 or earlier); this is the
// in-package snapshot.
func TestDryRunOutputShape(t *testing.T) {
	cfg := config.New()
	cfg.Layers = []config.Layer{
		{Kind: config.LayerImage, Ref: "alpine"},
		{Kind: config.LayerRootfs, Path: "/tmp/extra"},
	}
	cfg.Headless = true
	cfg.TailscaleAuthkey = "tskey-auth-abcdefghijklmnopqrstuvwxyz"
	cfg.TailscaleArgs = "--ssh --hostname=test-xmorph"
	cfg.NoInitCoord = true // skip detector path so the test is hermetic

	var buf bytes.Buffer
	printDryRun(&buf, &cfg)
	out := buf.String()

	// Required substrings — same order they appear in the Zig output.
	wants := []string{
		"=== DRY RUN ===",
		"Layers (merged in order):",
		"  1: alpine (image, base)",
		"  2: /tmp/extra (rootfs)",
		"Entrypoint: /bin/sh",
		"Keep old root: /mnt/oldroot",
		"Contain: false",
		"Timeout: 30s",
		"Mode: headless (will fork and detach, log to /var/log/xmorph.log)",
		"Steps that would be performed:",
		"  1. Build rootfs from image alpine",
		"  2. Merge rootfs /tmp/extra",
		"  3. Verify rootfs structure",
		"  4. Create Tailscale startup script",
		"     - Auth key: tskey-au...wxyz",
		"     - Args: --ssh --hostname=test-xmorph",
		"  5. Terminate non-essential processes",
		"  6. Execute pivot_root",
		"  7. Execute /bin/sh",
		"=== END DRY RUN ===",
	}

	prev := 0
	for _, w := range wants {
		idx := strings.Index(out[prev:], w)
		if idx < 0 {
			t.Errorf("dry-run output missing or out of order: %q\nfull output:\n%s", w, out)
			return
		}
		prev += idx + len(w)
	}
}

func TestDryRunTailscaleImageLayerTagged(t *testing.T) {
	cfg := config.New()
	cfg.Layers = []config.Layer{
		{Kind: config.LayerImage, Ref: "alpine"},
		{Kind: config.LayerImage, Ref: "docker.io/tailscale/tailscale:latest"},
	}
	cfg.NoInitCoord = true

	var buf bytes.Buffer
	printDryRun(&buf, &cfg)
	out := buf.String()
	if !strings.Contains(out, "docker.io/tailscale/tailscale:latest (image, tailscale)") {
		t.Errorf("tailscale-tagged layer not labeled: %s", out)
	}
}

func TestMaskAuthKey(t *testing.T) {
	cases := map[string]string{
		"":                    "(unset)",
		"tskey":               "tskey...",
		"tskey-auth":          "tskey-au...",
		"tskey-auth-abcdefgh": "tskey-au...efgh",
	}
	for in, want := range cases {
		if got := maskAuthKey(in); got != want {
			t.Errorf("maskAuthKey(%q) = %q, want %q", in, got, want)
		}
	}
}
