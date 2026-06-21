package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ananthb/xmorph/internal/config"
	"github.com/google/go-containerregistry/pkg/v1/layout"
)

// TestRunBuildFromLocalRootfs exercises the full M2 build path against a
// synthetic --rootfs directory: build into work-dir, save cache, write
// OCI layout to -o. No network, no registry pulls.
func TestRunBuildFromLocalRootfs(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin/hello"), []byte("hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "etc-hostname"), []byte("xmorph-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.New()
	cfg.Layers = []config.Layer{{Kind: config.LayerRootfs, Path: src}}
	cfg.CacheDir = filepath.Join(tmp, "cache")
	cfg.WorkDir = filepath.Join(tmp, "work")
	cfg.Output = filepath.Join(tmp, "out")

	if err := runBuild(context.Background(), &cfg); err != nil {
		t.Fatalf("runBuild: %v", err)
	}

	// Output OCI layout should be valid: load it back via go-containerregistry.
	p, err := layout.FromPath(cfg.Output)
	if err != nil {
		t.Fatalf("layout.FromPath: %v", err)
	}
	idx, err := p.ImageIndex()
	if err != nil {
		t.Fatalf("ImageIndex: %v", err)
	}
	man, err := idx.IndexManifest()
	if err != nil {
		t.Fatalf("IndexManifest: %v", err)
	}
	if len(man.Manifests) == 0 {
		t.Fatalf("output layout has no manifests")
	}

	// Cache should be populated at /{cache_dir}/builds/{key}/index.json.
	files, _ := filepath.Glob(filepath.Join(cfg.CacheDir, "builds", "*", "index.json"))
	if len(files) != 1 {
		t.Errorf("expected 1 cache index.json, got %d (%v)", len(files), files)
	}
}

// TestRunBuildCacheHit asserts that a second build with the same layers
// short-circuits via the cache (the work dir gets recreated only on miss).
func TestRunBuildCacheHit(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "marker"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.New()
	cfg.Layers = []config.Layer{{Kind: config.LayerRootfs, Path: src}}
	cfg.CacheDir = filepath.Join(tmp, "cache")
	cfg.WorkDir = filepath.Join(tmp, "work")

	if err := runBuild(context.Background(), &cfg); err != nil {
		t.Fatalf("first build: %v", err)
	}

	// Sabotage the source so a rebuild would produce different content.
	if err := os.WriteFile(filepath.Join(src, "marker"), []byte("v2-NEVER"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Delete work dir so we can detect whether the second build touched it.
	if err := os.RemoveAll(cfg.WorkDir); err != nil {
		t.Fatal(err)
	}

	if err := runBuild(context.Background(), &cfg); err != nil {
		t.Fatalf("second build: %v", err)
	}
	if _, err := os.Stat(cfg.WorkDir); !os.IsNotExist(err) {
		t.Errorf("cache hit should not recreate work dir; stat err=%v", err)
	}
}

// TestRunBuildNoCacheForcesRebuild: --no-cache must skip the cache and
// always rebuild even when the layout already exists.
func TestRunBuildNoCacheForcesRebuild(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.New()
	cfg.Layers = []config.Layer{{Kind: config.LayerRootfs, Path: src}}
	cfg.CacheDir = filepath.Join(tmp, "cache")
	cfg.WorkDir = filepath.Join(tmp, "work")

	if err := runBuild(context.Background(), &cfg); err != nil {
		t.Fatalf("first build: %v", err)
	}

	if err := os.RemoveAll(cfg.WorkDir); err != nil {
		t.Fatal(err)
	}

	cfg.NoCache = true
	if err := runBuild(context.Background(), &cfg); err != nil {
		t.Fatalf("--no-cache build: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cfg.WorkDir, "marker")); err != nil {
		t.Errorf("--no-cache should rebuild the work dir; stat marker err=%v", err)
	}
}
