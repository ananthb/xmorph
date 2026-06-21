package cli

import (
	"fmt"

	"github.com/ananthb/xmorph/internal/config"
	"github.com/spf13/cobra"
)

func newBuildCmd() *cobra.Command {
	cfg := config.New()

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build an OCI image from configured layers without pivoting",
		Long: `build assembles a rootfs from the configured layers and, with -o,
writes the result as an OCI layout directory. Without -o it just populates
the build cache so a later pivot is fast.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Normalize(cmd.Flags(), args, nil)
			if err := cfg.Validate(cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"xmorph build: %v — config parsing works, runtime arrives in M2.\n",
				errNotImplemented)
			return errNotImplemented
		},
	}
	config.BindBuild(cmd.Flags(), &cfg)
	return cmd
}
