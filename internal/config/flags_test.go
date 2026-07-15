package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// parsePivot is a test helper that wires the pivot flag set, parses args,
// and runs Normalize. The env map provides fake env-var lookups.
func parsePivot(t *testing.T, args []string, env map[string]string) Config {
	t.Helper()
	cfg := New()
	fs := pflag.NewFlagSet("pivot", pflag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{}) // swallow help
	BindPivot(fs, &cfg)

	if err := fs.Parse(args); err != nil {
		t.Fatalf("Parse(%v): %v", args, err)
	}
	cfg.Normalize(fs, fs.Args(), func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
	return cfg
}

func boolPtr(b bool) *bool    { return &b }
func u16Ptr(v uint16) *uint16 { return &v }

func TestParsePivotDefaults(t *testing.T) {
	cfg := parsePivot(t, nil, nil)

	if cfg.Subcommand != SubcommandPivot {
		t.Errorf("Subcommand = %v, want pivot", cfg.Subcommand)
	}
	if len(cfg.Layers) != 1 || cfg.Layers[0].Kind != LayerImage || cfg.Layers[0].Ref != DefaultImage {
		t.Errorf("default layer = %+v, want one image %q", cfg.Layers, DefaultImage)
	}
	if cfg.Entrypoint != DefaultEntrypoint {
		t.Errorf("Entrypoint = %q, want %q", cfg.Entrypoint, DefaultEntrypoint)
	}
	if cfg.KeepOldRoot != DefaultKeepOldRoot {
		t.Errorf("KeepOldRoot = %q, want %q", cfg.KeepOldRoot, DefaultKeepOldRoot)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %d, want %d", cfg.Timeout, DefaultTimeout)
	}
	if cfg.EntrypointExplicit {
		t.Error("EntrypointExplicit = true on defaults")
	}
}

func TestParsePivotLayerOrder(t *testing.T) {
	// --image and --rootfs interleave in argv order.
	cfg := parsePivot(t, []string{
		"--image", "alpine",
		"--rootfs", "/tmp/a",
		"--image", "ubuntu:22.04",
	}, nil)

	want := []Layer{
		{Kind: LayerImage, Ref: "alpine"},
		{Kind: LayerRootfs, Path: "/tmp/a"},
		{Kind: LayerImage, Ref: "ubuntu:22.04"},
	}
	if len(cfg.Layers) != len(want) {
		t.Fatalf("Layers = %+v, want %+v", cfg.Layers, want)
	}
	for i := range want {
		if cfg.Layers[i] != want[i] {
			t.Errorf("layer %d = %+v, want %+v", i, cfg.Layers[i], want[i])
		}
	}
}

func TestParsePivotImpliedFlags(t *testing.T) {
	// --systemd-mode implies --no-init-coord + --force (src/config.zig:260-263).
	cfg := parsePivot(t, []string{"--systemd-mode"}, nil)
	if !cfg.SystemdMode || !cfg.NoInitCoord || !cfg.Force {
		t.Errorf("--systemd-mode: SystemdMode=%v NoInitCoord=%v Force=%v",
			cfg.SystemdMode, cfg.NoInitCoord, cfg.Force)
	}
}

func TestParsePivotSSHEnable(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		want     bool
		wantPort *uint16
	}{
		{"unset", nil, false, nil},
		{"explicit enable", []string{"--ssh.enable"}, true, u16Ptr(22)},
		{"explicit false", []string{"--ssh.enable=false"}, false, nil},
		{"by password", []string{"--ssh.password=hunter2"}, true, u16Ptr(22)},
		{"by authorized-keys", []string{"--ssh.authorized-keys=ssh-ed25519 AAAA"}, true, u16Ptr(22)},
		{"explicit port", []string{"--ssh.port=2222"}, true, u16Ptr(2222)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parsePivot(t, tc.args, nil)
			if got := cfg.SSHEnabled(); got != tc.want {
				t.Errorf("SSHEnabled = %v, want %v", got, tc.want)
			}
			if (cfg.SSHPort == nil) != (tc.wantPort == nil) {
				t.Errorf("SSHPort presence = %v, want %v", cfg.SSHPort != nil, tc.wantPort != nil)
			} else if cfg.SSHPort != nil && *cfg.SSHPort != *tc.wantPort {
				t.Errorf("SSHPort = %d, want %d", *cfg.SSHPort, *tc.wantPort)
			}
		})
	}
}

func TestParsePivotTailscaleEnable(t *testing.T) {
	cfg := parsePivot(t, []string{"--tailscale.authkey=tskey-auth-foo"}, nil)
	if !cfg.TailscaleEnabled() {
		t.Error("TailscaleEnabled = false with authkey set")
	}
	if cfg.TailscaleAuthkey != "tskey-auth-foo" {
		t.Errorf("TailscaleAuthkey = %q", cfg.TailscaleAuthkey)
	}

	// Tri-state: explicit false overrides authkey presence.
	cfg = parsePivot(t, []string{
		"--tailscale.authkey=tskey-auth-foo",
		"--tailscale.enable=false",
	}, nil)
	if cfg.TailscaleEnabled() {
		t.Error("TailscaleEnabled = true after explicit --tailscale.enable=false")
	}
}

func TestParsePivotEnvOverride(t *testing.T) {
	// CACHE_DIRECTORY overrides the default but flag wins (src/config.zig:188-200).
	cfg := parsePivot(t, nil, map[string]string{"CACHE_DIRECTORY": "/tmp/cache"})
	if cfg.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q, want %q", cfg.CacheDir, "/tmp/cache")
	}

	cfg = parsePivot(t,
		[]string{"--cache-dir", "/flag/wins"},
		map[string]string{"CACHE_DIRECTORY": "/tmp/cache"},
	)
	if cfg.CacheDir != "/flag/wins" {
		t.Errorf("explicit flag should win over env, got %q", cfg.CacheDir)
	}

	// RUNTIME_DIRECTORY appends /rootfs.
	cfg = parsePivot(t, nil, map[string]string{"RUNTIME_DIRECTORY": "/run/xmorph"})
	if cfg.WorkDir != "/run/xmorph/rootfs" {
		t.Errorf("WorkDir = %q, want /run/xmorph/rootfs", cfg.WorkDir)
	}
}

func TestParsePivotKeepOldRoot(t *testing.T) {
	cfg := parsePivot(t, []string{"--no-keep-old-root"}, nil)
	if cfg.KeepOldRoot != "" {
		t.Errorf("KeepOldRoot = %q, want empty", cfg.KeepOldRoot)
	}

	cfg = parsePivot(t, []string{"--keep-old-root=/somewhere"}, nil)
	if cfg.KeepOldRoot != "/somewhere" {
		t.Errorf("KeepOldRoot = %q", cfg.KeepOldRoot)
	}

	// Bare --keep-old-root keeps the default. pflag's NoOptDefVal wiring.
	cfg = parsePivot(t, []string{"--keep-old-root"}, nil)
	if cfg.KeepOldRoot != DefaultKeepOldRoot {
		t.Errorf("bare --keep-old-root: KeepOldRoot = %q, want %q",
			cfg.KeepOldRoot, DefaultKeepOldRoot)
	}
}

func TestParsePivotCommandAlias(t *testing.T) {
	// --cmd and --command share a backing slice, always-appending.
	cfg := parsePivot(t, []string{
		"--command", "-c",
		"--cmd", "echo hi",
		"--command", "trailing",
	}, nil)
	want := []string{"-c", "echo hi", "trailing"}
	if !equalStrings(cfg.Command, want) {
		t.Errorf("Command = %v, want %v", cfg.Command, want)
	}
}

func TestParsePivotDashDash(t *testing.T) {
	// Everything after `--` becomes positional, appended to Command.
	cfg := parsePivot(t, []string{"--image", "alpine", "--", "-c", "echo hi"}, nil)
	want := []string{"-c", "echo hi"}
	if !equalStrings(cfg.Command, want) {
		t.Errorf("Command after `--` = %v, want %v", cfg.Command, want)
	}
}

func TestParsePivotEntrypointExplicit(t *testing.T) {
	cfg := parsePivot(t, nil, nil)
	if cfg.EntrypointExplicit {
		t.Error("default EntrypointExplicit should be false")
	}
	cfg = parsePivot(t, []string{"--entrypoint=/sbin/init"}, nil)
	if !cfg.EntrypointExplicit {
		t.Error("explicit --entrypoint should set EntrypointExplicit")
	}
	if cfg.Entrypoint != "/sbin/init" {
		t.Errorf("Entrypoint = %q", cfg.Entrypoint)
	}
}

func TestParsePivotLayerDedupe(t *testing.T) {
	// Two equivalent alpine refs collapse to one; second-position wins.
	cfg := parsePivot(t, []string{
		"--image", "alpine",
		"--image", "alpine:latest",
	}, nil)
	if len(cfg.Layers) != 1 {
		t.Fatalf("Layers = %+v, want 1 entry", cfg.Layers)
	}
	if cfg.Layers[0].Ref != "alpine:latest" {
		t.Errorf("dedup kept %q, want %q", cfg.Layers[0].Ref, "alpine:latest")
	}
}

func TestValidateTimeoutZero(t *testing.T) {
	cfg := parsePivot(t, []string{"--timeout=0"}, nil)
	if err := cfg.Validate(&bytes.Buffer{}); err != ErrInvalidTimeout {
		t.Errorf("Validate timeout=0: %v, want ErrInvalidTimeout", err)
	}
}

func TestValidateTailscaleArgsWithoutAuthkey(t *testing.T) {
	cfg := parsePivot(t, []string{"--tailscale.args=--ssh"}, nil)
	var w bytes.Buffer
	if err := cfg.Validate(&w); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if !strings.Contains(w.String(), "tailscale.args") {
		t.Errorf("expected warning about --tailscale.args, got %q", w.String())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
