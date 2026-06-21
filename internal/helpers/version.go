// Package helpers contains small CLI-facing utilities: the linker-injected
// version string, an interactive TTY confirmation prompt, and the
// resolveTailscaleArgs port from src/helpers.zig.
package helpers

// Version is overridden at link time via -ldflags="-X main.version=..."
// in the main package, which propagates here. When unset (go test, dev
// builds) it stays "dev".
var Version = "dev"
