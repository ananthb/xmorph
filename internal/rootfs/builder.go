package rootfs

import (
	"fmt"
	"os"

	"github.com/ananthb/xmorph/internal/config"
	"github.com/ananthb/xmorph/internal/oci"
)

// BuildResult is what the builder hands back to the orchestrator.
type BuildResult struct {
	// TargetDir is where the flattened rootfs lives.
	TargetDir string

	// LayerCount is the number of layers that contributed (counts each
	// --image and each --rootfs as one, regardless of how many OCI
	// sub-layers an --image expanded to).
	LayerCount int

	// Config is the merged ImageConfig threaded through all --image
	// layers (later wins per MergeImageConfig). Nil if no layer carried
	// an OCI config (e.g. only --rootfs layers).
	Config *oci.ImageConfig
}

// Build extracts the configured layers into targetDir in order, merging
// later-wins semantics on file conflicts (extract.go) and threading
// the OCI ImageConfig through MergeImageConfig.
//
// targetDir is created if missing. For M2 (`xmorph build -o ...`) this is
// a regular directory under cfg.WorkDir. M3+ swaps in a tmpfs at the
// same path; the builder doesn't care which.
func Build(layers []config.Layer, targetDir string) (*BuildResult, error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, fmt.Errorf("create target dir %s: %w", targetDir, err)
	}

	result := &BuildResult{TargetDir: targetDir}
	for i, l := range layers {
		cfg, err := extractLayer(l, targetDir)
		if err != nil {
			return nil, fmt.Errorf("layer %d (%s): %w", i, layerLabel(l), err)
		}
		result.LayerCount++
		if cfg != nil {
			result.Config = oci.MergeImageConfig(result.Config, cfg)
		}
	}
	return result, nil
}

// extractLayer routes a single layer through the appropriate loader.
// Returns the layer's ImageConfig if it was an OCI image, nil for
// raw rootfs sources.
func extractLayer(l config.Layer, targetDir string) (*oci.ImageConfig, error) {
	switch l.Kind {
	case config.LayerImage:
		kind := oci.ClassifyImage(l.Ref)
		img, err := oci.LoadImage(l.Ref, kind)
		if err != nil {
			return nil, err
		}
		if err := oci.ExtractImage(img, targetDir); err != nil {
			return nil, err
		}
		cfg, err := oci.ReadImageConfig(img)
		if err != nil {
			return nil, err
		}
		return cfg, nil

	case config.LayerRootfs:
		kind := oci.ClassifyRootfs(l.Path)
		switch kind {
		case oci.SourceRawTarball:
			return nil, oci.ExtractTarball(l.Path, targetDir)
		case oci.SourceRawDirectory:
			return nil, copyDir(l.Path, targetDir)
		default:
			return nil, fmt.Errorf("rootfs source classification %d unsupported", kind)
		}
	}
	return nil, fmt.Errorf("unknown layer kind %d", l.Kind)
}

func layerLabel(l config.Layer) string {
	switch l.Kind {
	case config.LayerImage:
		return l.Ref
	case config.LayerRootfs:
		return l.Path
	}
	return "unknown"
}
