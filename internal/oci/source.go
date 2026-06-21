// Package oci wraps go-containerregistry to load OCI images from three
// sources — a registry reference, a docker-save tarball, or an OCI image
// layout directory — and to extract their flattened root filesystem with
// .wh.* whiteout handling.
package oci

import (
	"os"
	"path/filepath"
)

// SourceKind tags the result of Classify.
type SourceKind int

const (
	// SourceRegistry is anything not pointing at a local file — go-containerregistry
	// parses it as a name.Reference and pulls via the registry transport.
	SourceRegistry SourceKind = iota

	// SourceDockerTarball is a local .tar / .tar.gz file produced by
	// `docker save` or compatible. Loaded via tarball.ImageFromPath.
	SourceDockerTarball

	// SourceOCILayout is a local directory containing an OCI Image Layout
	// (oci-layout marker + index.json). Loaded via layout.ImageFromPath.
	SourceOCILayout

	// SourceRawTarball is a plain rootfs tarball (NOT a docker-save) — used
	// only for the --rootfs flag, never for --image. Extracted with the
	// stdlib archive/tar walker, not via go-containerregistry.
	SourceRawTarball

	// SourceRawDirectory is a plain rootfs directory — used only for the
	// --rootfs flag. Copied recursively into the target.
	SourceRawDirectory
)

// ClassifyImage decides how to load an --image argument: registry pull,
// docker-save tarball, or OCI image layout dir. Used by the build/pivot
// orchestrator before handing off to Load.
//
// The heuristic: an existing path is examined; anything else is assumed
// to be a registry reference (matches `docker pull` semantics for
// non-absolute strings).
func ClassifyImage(ref string) SourceKind {
	if !looksLikeLocalPath(ref) {
		return SourceRegistry
	}
	info, err := os.Stat(ref)
	if err != nil {
		return SourceRegistry
	}
	if info.IsDir() {
		// OCI layout vs random directory: check for the spec marker file.
		if _, err := os.Stat(filepath.Join(ref, "oci-layout")); err == nil {
			return SourceOCILayout
		}
		// A directory without an oci-layout file isn't usable as --image;
		// callers should reject it. We still classify it as OCILayout so the
		// loader's error message is informative.
		return SourceOCILayout
	}
	// File: treat as a docker-save tarball.
	return SourceDockerTarball
}

// ClassifyRootfs decides how to load a --rootfs argument: tarball or
// plain directory. Registry references are not valid for --rootfs.
func ClassifyRootfs(path string) SourceKind {
	info, err := os.Stat(path)
	if err != nil {
		// Be lenient: assume tarball so the loader returns a meaningful
		// "file not found" error to the user.
		return SourceRawTarball
	}
	if info.IsDir() {
		return SourceRawDirectory
	}
	return SourceRawTarball
}

func looksLikeLocalPath(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Absolute path or explicit relative path.
	if s[0] == '/' || s[0] == '.' {
		return true
	}
	// Anything else (alpine, ubuntu:22.04, ghcr.io/foo/bar) is a registry ref.
	return false
}
