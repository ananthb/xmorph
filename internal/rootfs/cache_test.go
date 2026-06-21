package rootfs

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ananthb/xmorph/internal/config"
)

// TestComputeCacheKeyByteParity exercises the byte-for-byte parity claim
// against src/cache.zig:12-34. The preimage column documents exactly
// what gets fed into sha256 so reviewers can verify the algorithm without
// running it.
func TestComputeCacheKeyByteParity(t *testing.T) {
	cases := []struct {
		name     string
		layers   []config.Layer
		preimage string
	}{
		{
			name:     "single alpine image",
			layers:   []config.Layer{{Kind: config.LayerImage, Ref: "alpine"}},
			preimage: "image:registry-1.docker.io/library/alpine:latest\n",
		},
		{
			name: "alpine + ubuntu",
			layers: []config.Layer{
				{Kind: config.LayerImage, Ref: "alpine"},
				{Kind: config.LayerImage, Ref: "ubuntu:22.04"},
			},
			preimage: "image:registry-1.docker.io/library/alpine:latest\n" +
				"image:registry-1.docker.io/library/ubuntu:22.04\n",
		},
		{
			name: "image then rootfs",
			layers: []config.Layer{
				{Kind: config.LayerImage, Ref: "alpine"},
				{Kind: config.LayerRootfs, Path: "/tmp/extra"},
			},
			preimage: "image:registry-1.docker.io/library/alpine:latest\n" +
				"rootfs:/tmp/extra\n",
		},
		{
			name:     "rootfs path is taken as-is",
			layers:   []config.Layer{{Kind: config.LayerRootfs, Path: "/var/lib/foo.tar.gz"}},
			preimage: "rootfs:/var/lib/foo.tar.gz\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeCacheKey(tc.layers)
			sum := sha256.Sum256([]byte(tc.preimage))
			want := hex.EncodeToString(sum[:])
			if got != want {
				t.Errorf("ComputeCacheKey = %s\n           want %s\n  for preimage %q",
					got, want, tc.preimage)
			}
			if len(got) != 64 {
				t.Errorf("hex digest length = %d, want 64", len(got))
			}
		})
	}
}

// TestComputeCacheKeyStable: two equivalent image refs hash the same.
// Note: "docker.io" and "registry-1.docker.io" are NOT treated as
// equivalent — that mirrors the Zig version's deliberate behavior at
// src/config.zig tests lines 660-664.
func TestComputeCacheKeyStable(t *testing.T) {
	a := ComputeCacheKey([]config.Layer{{Kind: config.LayerImage, Ref: "alpine"}})
	b := ComputeCacheKey([]config.Layer{{Kind: config.LayerImage, Ref: "library/alpine:latest"}})
	if a != b {
		t.Errorf("equivalent refs produced different keys: %s vs %s", a, b)
	}
}

// TestComputeCacheKeyOrderMatters: reordering layers changes the key.
func TestComputeCacheKeyOrderMatters(t *testing.T) {
	a := ComputeCacheKey([]config.Layer{
		{Kind: config.LayerImage, Ref: "alpine"},
		{Kind: config.LayerImage, Ref: "ubuntu"},
	})
	b := ComputeCacheKey([]config.Layer{
		{Kind: config.LayerImage, Ref: "ubuntu"},
		{Kind: config.LayerImage, Ref: "alpine"},
	})
	if a == b {
		t.Errorf("expected order-sensitive key, both produced %s", a)
	}
}
