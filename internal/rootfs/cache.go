// Package rootfs assembles a filesystem from layered sources and writes
// it to disk (M2) or to a tmpfs that survives pivot_root (M3+).
package rootfs

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/ananthb/xmorph/internal/config"
)

// ComputeCacheKey returns the 64-character lowercase-hex SHA-256 of the
// normalized layer list. The format is byte-identical to src/cache.zig's
// computeBuildCacheKey (lines 12-34) so cache directories produced by the
// Zig binary remain reachable from the Go binary and vice versa.
//
// Format (concatenated, no trailing newline elision):
//
//	"image:" <NormalizeImageRef(ref)> "\n"
//	"rootfs:" <path> "\n"
func ComputeCacheKey(layers []config.Layer) string {
	h := sha256.New()
	for _, l := range layers {
		switch l.Kind {
		case config.LayerImage:
			h.Write([]byte("image:"))
			h.Write([]byte(config.NormalizeImageRef(l.Ref)))
		case config.LayerRootfs:
			h.Write([]byte("rootfs:"))
			h.Write([]byte(l.Path))
		}
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
