package oci

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// LoadImage materializes a v1.Image from a registry reference, docker-save
// tarball, or OCI image layout directory. The caller decides which by
// passing the SourceKind from ClassifyImage.
//
// For SourceRegistry, opts are passed to remote.Image (auth, transport,
// etc). For local sources opts are ignored.
func LoadImage(ref string, kind SourceKind, opts ...remote.Option) (v1.Image, error) {
	switch kind {
	case SourceRegistry:
		nm, err := name.ParseReference(ref, name.WithDefaultRegistry(name.DefaultRegistry))
		if err != nil {
			return nil, fmt.Errorf("parse image reference %q: %w", ref, err)
		}
		// Default to anonymous auth via the keychain — picks up
		// docker.io/library/* without credentials, ~/.docker/config.json
		// when present, etc. M2 doesn't expose auth knobs yet; that
		// arrives if/when a user needs private registries.
		img, err := remote.Image(nm, opts...)
		if err != nil {
			// Surface transport errors with a marker for callers that
			// want to distinguish network problems from manifest issues.
			if te, ok := err.(*transport.Error); ok {
				return nil, fmt.Errorf("pull %s: HTTP %d: %w", nm, te.StatusCode, err)
			}
			return nil, fmt.Errorf("pull %s: %w", nm, err)
		}
		return img, nil

	case SourceDockerTarball:
		// tarball.ImageFromPath wants the optional tag selector to find
		// one image among many in a multi-image tarball. Nil = first.
		img, err := tarball.ImageFromPath(ref, nil)
		if err != nil {
			return nil, fmt.Errorf("load docker-save tarball %q: %w", ref, err)
		}
		return img, nil

	case SourceOCILayout:
		p, err := layout.FromPath(ref)
		if err != nil {
			return nil, fmt.Errorf("open OCI layout %q: %w", ref, err)
		}
		ii, err := p.ImageIndex()
		if err != nil {
			return nil, fmt.Errorf("read OCI layout index %q: %w", ref, err)
		}
		// Multi-arch layouts can hold many manifests. Take the first
		// image manifest; multi-arch selection is a future concern.
		manifest, err := ii.IndexManifest()
		if err != nil {
			return nil, fmt.Errorf("parse OCI layout index %q: %w", ref, err)
		}
		for _, desc := range manifest.Manifests {
			img, err := p.Image(desc.Digest)
			if err == nil {
				return img, nil
			}
		}
		return nil, fmt.Errorf("OCI layout %q has no image manifests", ref)

	default:
		return nil, fmt.Errorf("oci.LoadImage: unsupported source kind %d for %q", kind, ref)
	}
}
