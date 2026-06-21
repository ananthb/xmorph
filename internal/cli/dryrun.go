package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/ananthb/xmorph/internal/config"
	"github.com/ananthb/xmorph/internal/helpers"
	"github.com/ananthb/xmorph/internal/initsys"
)

// printDryRun writes the same byte-for-byte layout as src/cmd/pivot.zig:486-563
// to w. xenomorph → xmorph rename applied to the logfile path.
//
// The "Coordinate with init system" step calls initsys.Detect so the
// printed label matches what the real run would do.
func printDryRun(w io.Writer, cfg *config.Config) {
	tsArgs := helpers.ResolveTailscaleArgs(cfg)
	layers := cfg.Layers

	fmt.Fprintf(w, "\n=== DRY RUN ===\n\n")

	fmt.Fprintf(w, "Layers (merged in order):\n")
	for i, l := range layers {
		label, kind := layerLabelKind(l)
		switch {
		case i == 0:
			fmt.Fprintf(w, "  %d: %s (%s, base)\n", i+1, label, kind)
		case l.Kind == config.LayerImage && strings.Contains(l.Ref, "tailscale"):
			fmt.Fprintf(w, "  %d: %s (%s, tailscale)\n", i+1, label, kind)
		default:
			fmt.Fprintf(w, "  %d: %s (%s)\n", i+1, label, kind)
		}
	}

	fmt.Fprintf(w, "\nEntrypoint: %s\n", cfg.Entrypoint)
	fmt.Fprintf(w, "Keep old root: %s\n", cfg.KeepOldRoot)
	fmt.Fprintf(w, "Contain: %s\n", boolStr(cfg.Contain))
	fmt.Fprintf(w, "Timeout: %ds\n", cfg.Timeout)
	if cfg.Headless {
		fmt.Fprintf(w, "Mode: headless (will fork and detach, log to /var/log/xmorph.log)\n")
	}

	fmt.Fprintf(w, "\nSteps that would be performed:\n")
	step := 1

	// First layer: build.
	label, _ := layerLabelKind(layers[0])
	if layers[0].Kind == config.LayerImage {
		fmt.Fprintf(w, "  %d. Build rootfs from image %s\n", step, label)
	} else {
		fmt.Fprintf(w, "  %d. Build rootfs from %s\n", step, label)
	}
	step++

	// Remaining layers: merge.
	for _, l := range layers[1:] {
		label, _ := layerLabelKind(l)
		if l.Kind == config.LayerImage {
			fmt.Fprintf(w, "  %d. Merge image %s\n", step, label)
		} else {
			fmt.Fprintf(w, "  %d. Merge rootfs %s\n", step, label)
		}
		step++
	}

	fmt.Fprintf(w, "  %d. Verify rootfs structure\n", step)
	step++

	if cfg.TailscaleEnabled() {
		fmt.Fprintf(w, "  %d. Create Tailscale startup script\n", step)
		step++
		fmt.Fprintf(w, "     - Auth key: %s\n", maskAuthKey(cfg.TailscaleAuthkey))
		fmt.Fprintf(w, "     - Args: %s\n", tsArgs)
	}

	if !cfg.NoInitCoord {
		fmt.Fprintf(w, "  %d. Coordinate with init system (%s)\n", step, initsys.Detect())
		step++
	}

	fmt.Fprintf(w, "  %d. Terminate non-essential processes\n", step)
	step++
	fmt.Fprintf(w, "  %d. Execute pivot_root\n", step)
	step++
	fmt.Fprintf(w, "  %d. Execute %s\n", step, cfg.Entrypoint)

	fmt.Fprintf(w, "\n=== END DRY RUN ===\n")
}

func layerLabelKind(l config.Layer) (label, kind string) {
	switch l.Kind {
	case config.LayerImage:
		return l.Ref, "image"
	case config.LayerRootfs:
		return l.Path, "rootfs"
	}
	return "?", "?"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// maskAuthKey renders the first 8 and last 4 chars of an authkey,
// matching src/cmd/pivot.zig:542-545 ("tskey-au...c3d4" style).
func maskAuthKey(key string) string {
	if key == "" {
		return "(unset)"
	}
	front := key
	if len(key) > 8 {
		front = key[:8]
	}
	tail := ""
	if len(key) > 12 {
		tail = key[len(key)-4:]
	}
	return front + "..." + tail
}
