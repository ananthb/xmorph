// Package postpivot writes the JSON config that `xmorph --init` reads
// after the pivot, and (M4) implements the supervised entrypoint loop.
// Mirrors src/initscript.zig + src/xenomorph-init.zig.
package postpivot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ConfigPath is the absolute path inside the new rootfs where the JSON
// config is written. Read back by Run after the re-exec.
const ConfigPath = "/etc/xmorph-init.json"

// BinaryPath is where the xmorph binary lives inside the new rootfs.
// `xmorph pivot` copies /proc/self/exe to this path before pivot_root.
const BinaryPath = "/usr/local/bin/xmorph"

// Config is the JSON schema written to ConfigPath. Mirrors the schema
// at the top of src/xenomorph-init.zig.
type Config struct {
	FlushFirewall   bool `json:"flush_firewall"`
	RebootOnFailure bool `json:"reboot_on_failure"`
	// WatchdogTimeoutSeconds arms /dev/watchdog (or a userspace timer
	// fallback) that resets the box if the post-pivot supervisor hangs
	// longer than this. 0 disables it.
	WatchdogTimeoutSeconds int `json:"watchdog_timeout_seconds,omitempty"`
	// KeepOldRoot is the path where the pre-pivot root remains mounted
	// (default /mnt/oldroot). Consumed by the reboot path so we can
	// unmount cleanly before LINUX_REBOOT_CMD_RESTART — the kernel
	// restart doesn't run any userspace shutdown, so a journaled fs on
	// the old root would otherwise be left dirty. Empty means the pivot
	// step already unmounted it.
	KeepOldRoot string     `json:"keep_old_root,omitempty"`
	SSH         *SSHConfig `json:"ssh,omitempty"`
	Tailscale   *TSConfig  `json:"tailscale,omitempty"`
	Entrypoint  []string   `json:"entrypoint,omitempty"`
	Command     []string   `json:"command,omitempty"`
}

// SSHConfig describes the in-rootfs SSH setup (dropbear for now).
type SSHConfig struct {
	Port           int    `json:"port"`
	Password       string `json:"password,omitempty"`
	AuthorizedKeys string `json:"authorized_keys,omitempty"`
}

// TSConfig is the legacy tailscale-via-image schema. Under tsnet we
// don't actually use these fields at re-exec time (the parent process
// brings up tsnet pre-pivot and the state persists in /var/lib/tailscale);
// the struct is kept so the JSON schema stays stable for ops scripts.
type TSConfig struct {
	AuthKey string `json:"authkey,omitempty"`
	Args    string `json:"args,omitempty"`
}

// WriteConfig writes cfg as JSON to rootfsRoot + ConfigPath, creating
// the parent dir if needed. Errors are returned to the caller — at
// this point in the pivot sequence aborting is still safe.
func WriteConfig(rootfsRoot string, cfg *Config) error {
	path := filepath.Join(rootfsRoot, ConfigPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for config: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}

// CopyBinary copies the running xmorph executable into the new rootfs
// at BinaryPath, so post-pivot we can re-exec it as PID 1's supervisor.
// Uses /proc/self/exe to resolve the actual binary (handles symlinks,
// renamed processes, etc.).
func CopyBinary(rootfsRoot string) error {
	src, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("read /proc/self/exe: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open running binary %s: %w", src, err)
	}
	defer in.Close()

	dstPath := filepath.Join(rootfsRoot, BinaryPath)
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for binary: %w", err)
	}
	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dstPath, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}
	return nil
}

// LoadConfig reads ConfigPath inside the current root (i.e. after the
// pivot). Used by Run in M4.
func LoadConfig() (*Config, error) {
	f, err := os.Open(ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	cfg := &Config{}
	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode %s: %w", ConfigPath, err)
	}
	return cfg, nil
}
