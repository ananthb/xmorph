package config

import "testing"

func TestNormalizeImageRef(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Mirrors src/config.zig tests at lines 642-680.
		{"alpine", "registry-1.docker.io/library/alpine:latest"},
		{"alpine:3.18", "registry-1.docker.io/library/alpine:3.18"},
		{"docker.io/library/alpine", "docker.io/library/alpine:latest"},
		{"ghcr.io/user/repo:v1.0", "ghcr.io/user/repo:v1.0"},
		// Digest is stripped.
		{"alpine@sha256:deadbeef", "registry-1.docker.io/library/alpine:latest"},
		// Port in registry doesn't get confused with tag.
		{"localhost:5000/foo", "localhost:5000/foo:latest"},
		{"localhost:5000/foo:v2", "localhost:5000/foo:v2"},
	}
	for _, tc := range cases {
		got := NormalizeImageRef(tc.in)
		if got != tc.want {
			t.Errorf("NormalizeImageRef(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeImageRefEquivalence(t *testing.T) {
	// All alpine variants normalize to the same thing.
	a := NormalizeImageRef("alpine")
	b := NormalizeImageRef("library/alpine:latest")
	if a != b {
		t.Errorf("alpine variants differ: %q vs %q", a, b)
	}
}

func TestDeduplicateLayers(t *testing.T) {
	in := []Layer{
		{Kind: LayerImage, Ref: "alpine"},
		{Kind: LayerRootfs, Path: "/tmp/a"},
		{Kind: LayerImage, Ref: "alpine:latest"}, // duplicate of first after normalize
		{Kind: LayerImage, Ref: "ubuntu:22.04"},
		{Kind: LayerRootfs, Path: "/tmp/a"}, // duplicate of second
	}
	got := DeduplicateLayers(in)
	want := []Layer{
		// "alpine" was deduped because "alpine:latest" came after.
		// "/tmp/a" was deduped because a second /tmp/a came after.
		// Result order is original-minus-dupes, last-position wins.
		{Kind: LayerImage, Ref: "alpine:latest"},
		{Kind: LayerImage, Ref: "ubuntu:22.04"},
		{Kind: LayerRootfs, Path: "/tmp/a"},
	}
	if len(got) != len(want) {
		t.Fatalf("DeduplicateLayers length = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("layer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
