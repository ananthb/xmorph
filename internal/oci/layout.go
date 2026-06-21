package oci

import (
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
)

// WriteLayout writes img as an OCI Image Layout into dir. If dir doesn't
// exist or doesn't contain a valid oci-layout file, it's initialized.
// Subsequent calls into the same dir append additional images to the
// index (useful for multi-arch / merged caches).
//
// Returns the manifest digest of the appended image.
func WriteLayout(dir string, img v1.Image) (v1.Hash, error) {
	p, err := layout.FromPath(dir)
	if err != nil {
		p, err = layout.Write(dir, empty.Index)
		if err != nil {
			return v1.Hash{}, fmt.Errorf("init OCI layout at %s: %w", dir, err)
		}
	}
	if err := p.AppendImage(img); err != nil {
		return v1.Hash{}, fmt.Errorf("append image to layout %s: %w", dir, err)
	}
	digest, err := img.Digest()
	if err != nil {
		return v1.Hash{}, fmt.Errorf("compute image digest: %w", err)
	}
	return digest, nil
}
