package oci

import (
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// ImageConfig is the subset of the OCI ImageConfig spec that the rootfs
// builder threads through layer merges. Mirrors src/cmd/containerfile_exec.zig's
// `BuildResult.ImageConfig` (the fields used downstream by runPivot when
// deciding what to exec post-pivot).
//
// Any of these may be nil; merge semantics in MergeImageConfig follow
// "overlay wins for non-nil fields; env is merged by KEY=" — see
// src/cmd/containerfile_exec.zig:172-243.
type ImageConfig struct {
	Entrypoint []string
	Cmd        []string
	Env        []string
	WorkingDir string
}

// ReadImageConfig extracts the relevant ImageConfig fields from a v1.Image.
// Returns nil (not an error) if the image lacks an OCI config blob.
func ReadImageConfig(img v1.Image) (*ImageConfig, error) {
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read image config: %w", err)
	}
	if cf == nil {
		return nil, nil
	}
	return &ImageConfig{
		Entrypoint: append([]string(nil), cf.Config.Entrypoint...),
		Cmd:        append([]string(nil), cf.Config.Cmd...),
		Env:        append([]string(nil), cf.Config.Env...),
		WorkingDir: cf.Config.WorkingDir,
	}, nil
}

// MergeImageConfig overlays `overlay` onto `base`. Non-nil fields on
// `overlay` win for Entrypoint, Cmd, WorkingDir. Env is merged by key
// (`KEY=` prefix): later value wins for the same key, new keys append.
// Mirrors src/cmd/containerfile_exec.zig:172-243.
func MergeImageConfig(base, overlay *ImageConfig) *ImageConfig {
	if base == nil {
		if overlay == nil {
			return nil
		}
		clone := *overlay
		return &clone
	}
	if overlay == nil {
		clone := *base
		return &clone
	}

	out := &ImageConfig{
		Entrypoint: base.Entrypoint,
		Cmd:        base.Cmd,
		Env:        base.Env,
		WorkingDir: base.WorkingDir,
	}
	if overlay.Entrypoint != nil {
		out.Entrypoint = overlay.Entrypoint
	}
	if overlay.Cmd != nil {
		out.Cmd = overlay.Cmd
	}
	if overlay.WorkingDir != "" {
		out.WorkingDir = overlay.WorkingDir
	}
	if overlay.Env != nil {
		out.Env = mergeEnv(out.Env, overlay.Env)
	}
	return out
}

func mergeEnv(base, overlay []string) []string {
	out := append([]string(nil), base...)
	for _, e := range overlay {
		key := envKey(e)
		replaced := false
		for i, existing := range out {
			if envKey(existing) == key {
				out[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, e)
		}
	}
	return out
}

func envKey(entry string) string {
	for i := 0; i < len(entry); i++ {
		if entry[i] == '=' {
			return entry[:i]
		}
	}
	return entry
}
