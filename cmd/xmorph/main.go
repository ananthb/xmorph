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
	"fmt"
	"os"

	"github.com/ananthb/xmorph/internal/cli"
	"github.com/ananthb/xmorph/internal/helpers"
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
	// M4 wires this to postpivot.Run.
	if len(os.Args) > 1 && os.Args[1] == "--init" {
		fmt.Fprintln(os.Stderr, "xmorph --init: post-pivot init runtime arrives in M4")
		os.Exit(1)
	}

	if err := cli.NewRootCmd(os.Stdout, os.Stderr).Execute(); err != nil {
		os.Exit(1)
	}
}
