package cli

import (
	"errors"
	"fmt"

	"github.com/ananthb/xmorph/internal/config"
	"github.com/spf13/cobra"
)

// errNotImplemented is returned for subcommand bodies still under construction.
// Replaced milestone-by-milestone (M2: build; M3: dry-run + contain; M5: real pivot).
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
			if cfg.DryRun {
				printDryRun(cmd.OutOrStdout(), &cfg)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"xmorph pivot: %v — runtime arrives in M5.\n",
				errNotImplemented)
			return errNotImplemented
		},
	}
	config.BindPivot(cmd.Flags(), &cfg)
	return cmd
}
