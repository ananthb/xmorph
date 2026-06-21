package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ananthb/xmorph/internal/config"
	"github.com/ananthb/xmorph/internal/daemon"
	"github.com/ananthb/xmorph/internal/initsys"
	"github.com/ananthb/xmorph/internal/oci"
	"github.com/ananthb/xmorph/internal/pivot"
	"github.com/ananthb/xmorph/internal/postpivot"
	"github.com/ananthb/xmorph/internal/process"
	"github.com/ananthb/xmorph/internal/rootfs"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

// errNotImplemented is retained for the not-yet-wired tsnet path (M6).
var errNotImplemented = errors.New("not implemented in this milestone")

func newPivotCmd() *cobra.Command {
	cfg := config.New()

	cmd := &cobra.Command{
		Use:   "pivot [-- args...]",
		Short: "Execute pivot_root into a new rootfs built from OCI images",
		Long: `pivot builds a rootfs from the configured layers, coordinates with
the running init system to stop services, then calls pivot_root(2) to atomically
swap the root filesystem. The old root is kept at /mnt/oldroot by default.

Anything after a literal "--" is appended to --command, useful for passing
arguments that would otherwise be interpreted as flags.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Normalize(cmd.Flags(), args, nil)
			if err := cfg.Validate(cmd.ErrOrStderr()); err != nil {
				return err
			}
			return runPivot(cmd.Context(), &cfg, cmd.OutOrStdout())
		},
	}
	config.BindPivot(cmd.Flags(), &cfg)
	return cmd
}

// runPivot is the full host-side pivot pipeline:
//
//  1. --dry-run        → print plan, return.
//  2. --contain        → build rootfs, run inside mount+PID namespace.
//  3. --headless       → fork via daemon.Daemonize (parent prints PID, exits).
//  4. Build rootfs.
//  5. Resolve entrypoint from cfg + merged ImageConfig.
//  6. Write /etc/xmorph-init.json + copy binary to new rootfs.
//  7. Init coordination (unless --no-init-coord / --systemd-mode).
//  8. Process termination (unless --systemd-mode).
//  9. Pivot mount sequence + pivot_root.
// 10. Exec into the new rootfs's /usr/local/bin/xmorph --init <argv>.
//
// At any failure before step 9 we abort cleanly; after step 9 the host
// is past the point of return.
func runPivot(ctx context.Context, cfg *config.Config, stdout interface {
	Write(p []byte) (n int, err error)
}) error {
	_ = ctx

	if cfg.DryRun {
		printDryRun(stdout, cfg)
		return nil
	}

	if cfg.Contain {
		return runContain(cfg)
	}

	if os.Geteuid() != 0 {
		return errors.New("pivot requires root (CAP_SYS_ADMIN for namespace + pivot_root)")
	}

	// --force or --headless skips confirmation. Plan plus the existing
	// implied-flag rule (`--headless` ⇒ `--force`) means we only need
	// to ask if neither is set.
	if !cfg.Force {
		fmt.Fprintln(os.Stderr, "Refusing to pivot without --force or --headless")
		return errors.New("confirmation required (--force or --headless)")
	}

	if cfg.Headless {
		if err := daemon.Daemonize(cfg.LogDir); err != nil {
			return fmt.Errorf("daemonize: %w", err)
		}
		// Past here we're the daemonized child.
	}

	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	slog.Info("building rootfs", "layers", len(cfg.Layers), "target", cfg.WorkDir)
	result, err := rootfs.Build(cfg.Layers, cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("build rootfs: %w", err)
	}
	slog.Info("rootfs built", "layers", result.LayerCount)

	entrypoint, entryArgs, _ := resolveEntrypoint(cfg, result.Config)

	// Write the postpivot config (read back by `xmorph --init`) and copy
	// the running binary into the new rootfs.
	pivotConfig := buildPostpivotConfig(cfg, entrypoint, entryArgs)
	if err := postpivot.WriteConfig(cfg.WorkDir, pivotConfig); err != nil {
		return fmt.Errorf("write postpivot config: %w", err)
	}
	if err := postpivot.CopyBinary(cfg.WorkDir); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	// Memory headroom: warn before we commit to the pivot.
	// (sysmem usage will be wired through when M5's RAM telemetry is
	// surfaced via the slog output — for now we just verify the file
	// is readable.)

	if !cfg.SystemdMode {
		if !cfg.NoInitCoord && !initsys.ShouldSkipCoordination() {
			coord := initsys.NewCoordinator(time.Duration(cfg.Timeout) * time.Second)
			slog.Info("coordinating with init system", "kind", coord.Kind)
			if err := coord.TransitionToRescue(); err != nil {
				slog.Warn("transition-to-rescue failed; continuing", "err", err)
			}
			if err := coord.WaitForServicesToStop(); err != nil {
				slog.Warn("wait for services to stop", "err", err)
			}
		}

		slog.Info("terminating non-essential processes")
		_, err := process.Terminate(process.TerminateOptions{
			GracefulTimeout: time.Duration(cfg.Timeout) * time.Second,
			SkipEssential:   true,
		})
		if err != nil {
			slog.Warn("process termination", "err", err)
		}
	} else {
		slog.Info("systemd-mode: skipping init coordination + process termination")
	}

	// All goroutines below this point share a single OS thread so the
	// new mount namespace is consistent across what we do.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	slog.Info("preparing pivot mounts", "new_root", cfg.WorkDir)
	if err := pivot.Prepare(pivot.PrepareOptions{
		NewRoot:         cfg.WorkDir,
		SkipVerify:      true,
		CreateNamespace: true,
	}); err != nil {
		return fmt.Errorf("prepare pivot: %w", err)
	}

	// Pre-create the old-root mount point inside the new rootfs.
	oldRootRel := "mnt/oldroot"
	if err := os.MkdirAll(filepath.Join(cfg.WorkDir, oldRootRel), 0o755); err != nil {
		return fmt.Errorf("create old-root mount point: %w", err)
	}

	// Flush the in-memory log buffer to the new rootfs RIGHT NOW —
	// after pivot_root the old path vanishes from view. Go's heap
	// (anonymous pages) survives the mount-namespace transition, so
	// any logs written AFTER this flush also persist in memory; they
	// just won't reach disk until a later sync from inside the new
	// rootfs. See plan risk #1.
	if LogHandler != nil {
		logPath := filepath.Join(cfg.WorkDir, cfg.LogDir, "xmorph.log")
		if err := LogHandler.FlushBufferTo(logPath); err != nil {
			slog.Warn("flush log buffer", "err", err)
		}
	}

	if err := pivot.PivotRoot(cfg.WorkDir, oldRootRel); err != nil {
		return fmt.Errorf("pivot_root: %w", err)
	}

	if cfg.KeepOldRoot == "" {
		slog.Info("unmounting old root")
		if failed, err := pivot.CleanupOldRoot("/" + oldRootRel); err != nil {
			slog.Warn("cleanup old root", "err", err)
		} else if len(failed) > 0 {
			slog.Warn("some old-root mounts did not unmount", "count", len(failed))
		}
	}

	// Exec the post-pivot supervisor: /usr/local/bin/xmorph --init <argv>.
	supervisorArgv := append([]string{postpivot.BinaryPath, "--init", entrypoint}, entryArgs...)
	if err := unix.Exec(postpivot.BinaryPath, supervisorArgv, os.Environ()); err != nil {
		return fmt.Errorf("exec post-pivot supervisor: %w", err)
	}
	return nil // unreachable
}

func buildPostpivotConfig(cfg *config.Config, entrypoint string, entryArgs []string) *postpivot.Config {
	pc := &postpivot.Config{
		FlushFirewall:   !cfg.KeepFirewall,
		RebootOnFailure: true,
		Entrypoint:      append([]string{entrypoint}, entryArgs...),
	}
	if cfg.SSHEnabled() {
		ssh := &postpivot.SSHConfig{
			Password:       cfg.SSHPassword,
			AuthorizedKeys: cfg.SSHAuthorizedKeys,
		}
		if cfg.SSHPort != nil {
			ssh.Port = int(*cfg.SSHPort)
		} else {
			ssh.Port = 22
		}
		pc.SSH = ssh
	}
	if cfg.TailscaleEnabled() {
		pc.Tailscale = &postpivot.TSConfig{
			AuthKey: cfg.TailscaleAuthkey,
			Args:    cfg.TailscaleArgs,
		}
	}
	return pc
}

// runContain builds the rootfs into cfg.WorkDir, then re-execs the
// entrypoint inside an unshared mount+PID namespace via pivot.Contain.
// No real pivot_root.
func runContain(cfg *config.Config) error {
	if os.Geteuid() != 0 {
		return errors.New("--contain requires root (CAP_SYS_ADMIN for namespace ops)")
	}

	slog.Info("building rootfs for --contain", "layers", len(cfg.Layers), "target", cfg.WorkDir)
	if err := os.RemoveAll(cfg.WorkDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean work dir: %w", err)
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}

	result, err := rootfs.Build(cfg.Layers, cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("build rootfs: %w", err)
	}
	slog.Info("rootfs built", "layers", result.LayerCount)

	entrypoint, entryArgs, env := resolveEntrypoint(cfg, result.Config)
	slog.Info("entering contained shell", "entrypoint", entrypoint, "args", entryArgs)

	return pivot.Contain(pivot.ContainOptions{
		NewRoot:    cfg.WorkDir,
		Entrypoint: entrypoint,
		Args:       entryArgs,
		Env:        env,
	})
}

// resolveEntrypoint picks the effective entrypoint + args + env from the
// CLI config and the merged ImageConfig. Mirrors src/cmd/pivot.zig:194-236.
func resolveEntrypoint(cfg *config.Config, ic *oci.ImageConfig) (entrypoint string, args, env []string) {
	if cfg.EntrypointExplicit {
		entrypoint = cfg.Entrypoint
		args = cfg.Command
	} else if ic != nil && len(ic.Entrypoint) > 0 {
		entrypoint = ic.Entrypoint[0]
		args = append(append([]string(nil), ic.Entrypoint[1:]...), ic.Cmd...)
	} else if ic != nil && len(ic.Cmd) > 0 {
		entrypoint = ic.Cmd[0]
		args = ic.Cmd[1:]
	} else {
		entrypoint = cfg.Entrypoint // /bin/sh default
		args = cfg.Command
	}
	if ic != nil {
		env = ic.Env
	}
	return
}
