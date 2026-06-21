package config

import "strings"

const defaultRegistry = "registry-1.docker.io"

// NormalizeImageRef converts an OCI image reference to a canonical
// registry/repository:tag form. Behavior mirrors src/config.zig:420-463.
//
// Examples:
//
//	"alpine"                       → "registry-1.docker.io/library/alpine:latest"
//	"alpine:3.18"                  → "registry-1.docker.io/library/alpine:3.18"
//	"docker.io/library/alpine"     → "docker.io/library/alpine:latest"
//	"ghcr.io/user/repo:v1.0"       → "ghcr.io/user/repo:v1.0"
//
// Digests (@sha256:...) are stripped — the dedup key intentionally ignores them.
func NormalizeImageRef(ref string) string {
	remaining := ref
	registry := defaultRegistry
	tag := "latest"

	// Strip digest (@sha256:...).
	if i := strings.IndexByte(remaining, '@'); i >= 0 {
		remaining = remaining[:i]
	}

	// Extract tag after last ':' — but only if the candidate has no '/' in it
	// (otherwise it's a port, not a tag).
	if i := strings.LastIndexByte(remaining, ':'); i >= 0 {
		cand := remaining[i+1:]
		if !strings.ContainsRune(cand, '/') {
			tag = cand
			remaining = remaining[:i]
		}
	}

	// Extract registry: the part before the first '/' is a registry if it
	// looks like a hostname (contains '.' or ':') or is exactly "localhost".
	if i := strings.IndexByte(remaining, '/'); i >= 0 {
		cand := remaining[:i]
		if strings.ContainsAny(cand, ".:") || cand == "localhost" {
			registry = cand
			remaining = remaining[i+1:]
		}
	}

	repository := remaining

	// Docker Hub library shortcut: "alpine" → "library/alpine".
	if registry == defaultRegistry && !strings.ContainsRune(repository, '/') {
		return registry + "/library/" + repository + ":" + tag
	}
	return registry + "/" + repository + ":" + tag
}

// DeduplicateLayers removes duplicate layers, keeping the last occurrence
// of each. Image refs are compared after normalization; rootfs paths
// compare as-is. Mirrors src/config.zig:379-416 — including the
// "walk backwards, last occurrence wins position" semantics.
func DeduplicateLayers(layers []Layer) []Layer {
	seen := make(map[string]struct{}, len(layers))
	out := make([]Layer, 0, len(layers))

	for i := len(layers) - 1; i >= 0; i-- {
		l := layers[i]
		var key string
		switch l.Kind {
		case LayerImage:
			key = "image:" + NormalizeImageRef(l.Ref)
		case LayerRootfs:
			key = "rootfs:" + l.Path
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, l)
	}

	// Reverse to restore original order (with duplicates removed).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
