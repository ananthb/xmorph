// Package cli defines the cobra command tree.
//
// The runtime entrypoint cmd/xmorph/main.go branches on os.Args[1] == "--init"
// before invoking cobra — that's the re-exec path for post-pivot init.
// Everything else routes through this package.
package cli

import (
	"fmt"
	"io"

	"github.com/ananthb/xmorph/internal/helpers"
	"github.com/spf13/cobra"
)

// NewRootCmd builds the top-level `xmorph` command with `pivot`, `build`,
// and `version` subcommands. Output streams default to the cobra
// command's own out/err; pass through for testing.
func NewRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "xmorph",
		Short: "Replace the running Linux rootfs with an in-memory rootfs from OCI images",
		Long: `xmorph replaces a running Linux rootfs with a new in-memory rootfs
built from OCI images and/or rootfs tarballs, by way of pivot_root(2).
It coordinates with systemd/openrc/sysvinit to stop services first and
supports headless operation over Tailscale (in-process via tsnet).`,
		Version:       helpers.Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetVersionTemplate("xmorph {{.Version}}\n")
	if stdout != nil {
		root.SetOut(stdout)
	}
	if stderr != nil {
		root.SetErr(stderr)
	}

	// Zig used -V for --version and -v for --verbose (per-subcommand).
	// Register --version with -V BEFORE adding subcommands so cobra's
	// InitDefaultVersionFlag sees an existing flag and leaves it alone.
	root.Flags().BoolP("version", "V", false, "print version and exit")

	root.AddCommand(newPivotCmd(), newBuildCmd(), newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "xmorph %s\n", helpers.Version)
		},
	}
}
