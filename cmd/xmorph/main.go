// Command xmorph replaces a running Linux rootfs with an in-memory rootfs
// built from OCI images and rootfs tarballs.
//
// The binary has two entry modes:
//
//  1. Normal: cobra-driven CLI (xmorph pivot|build|version).
//  2. Post-pivot init: when os.Args[1] == "--init", run the supervised
//     entrypoint loop instead of the CLI. Implemented in M4.
package main

import (
	"log/slog"
	"os"

	"github.com/ananthb/xmorph/internal/cli"
	"github.com/ananthb/xmorph/internal/helpers"
	"github.com/ananthb/xmorph/internal/postpivot"

	"golang.org/x/term"
)

// version is overridden at link time via -ldflags="-X main.version=..."
// and propagates into the helpers package at init.
var version = "dev"

func init() {
	if version != "dev" {
		helpers.Version = version
	}
}

func main() {
	// Re-exec sentinel for the post-pivot init path. Checked BEFORE cobra
	// so flag parsing doesn't trip over the absence of a subcommand.
	if len(os.Args) > 1 && os.Args[1] == "--init" {
		os.Exit(postpivot.Run(os.Args[2:]))
	}

	cli.InstallLogger(slog.LevelInfo, term.IsTerminal(int(os.Stderr.Fd())))

	if err := cli.NewRootCmd(os.Stdout, os.Stderr).Execute(); err != nil {
		os.Exit(1)
	}
}
