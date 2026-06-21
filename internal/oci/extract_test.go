package oci

import (
	"archive/tar"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// makeTar builds a tarball in-memory from a sequence of entries. Used by
// the extractor tests; allows fine-grained control over whiteout placement
// and file ordering without touching the filesystem.
func makeTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{
			Name:     e.name,
			Mode:     int64(e.mode),
			Size:     int64(len(e.body)),
			Typeflag: e.typ,
			Linkname: e.linkname,
		}
		if h.Mode == 0 && (h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA) {
			h.Mode = 0o644
		}
		if h.Mode == 0 && h.Typeflag == tar.TypeDir {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("WriteHeader %s: %v", e.name, err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("Write %s: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name     string
	typ      byte
	mode     uint32
	body     []byte
	linkname string
}

func TestExtractTarBasic(t *testing.T) {
	dir := t.TempDir()
	data := makeTar(t, []tarEntry{
		{name: "etc/", typ: tar.TypeDir, mode: 0o755},
		{name: "etc/hostname", typ: tar.TypeReg, mode: 0o644, body: []byte("alpine\n")},
		{name: "bin/", typ: tar.TypeDir, mode: 0o755},
		{name: "bin/sh", typ: tar.TypeSymlink, linkname: "busybox"},
	})

	if err := extractTar(tar.NewReader(bytes.NewReader(data)), dir); err != nil {
		t.Fatalf("extractTar: %v", err)
	}

	if b, err := os.ReadFile(filepath.Join(dir, "etc/hostname")); err != nil || string(b) != "alpine\n" {
		t.Errorf("etc/hostname = %q err=%v", b, err)
	}
	info, err := os.Lstat(filepath.Join(dir, "bin/sh"))
	if err != nil {
		t.Fatalf("Lstat bin/sh: %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("bin/sh is not a symlink: mode=%v", info.Mode())
	}
}

func TestExtractTarWhiteoutFile(t *testing.T) {
	dir := t.TempDir()

	// First layer: writes /etc/{a,b}.
	layer1 := makeTar(t, []tarEntry{
		{name: "etc/", typ: tar.TypeDir, mode: 0o755},
		{name: "etc/a", typ: tar.TypeReg, mode: 0o644, body: []byte("keep")},
		{name: "etc/b", typ: tar.TypeReg, mode: 0o644, body: []byte("delete me")},
	})
	if err := extractTar(tar.NewReader(bytes.NewReader(layer1)), dir); err != nil {
		t.Fatalf("layer1: %v", err)
	}

	// Second layer: whiteout for /etc/b only.
	layer2 := makeTar(t, []tarEntry{
		{name: "etc/.wh.b", typ: tar.TypeReg, mode: 0o644},
	})
	if err := extractTar(tar.NewReader(bytes.NewReader(layer2)), dir); err != nil {
		t.Fatalf("layer2: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "etc/a")); err != nil {
		t.Errorf("etc/a should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc/b")); !os.IsNotExist(err) {
		t.Errorf("etc/b should be removed; err=%v", err)
	}
}

func TestExtractTarOpaqueWhiteout(t *testing.T) {
	dir := t.TempDir()

	// First layer populates /opt/{a,b}.
	layer1 := makeTar(t, []tarEntry{
		{name: "opt/", typ: tar.TypeDir, mode: 0o755},
		{name: "opt/a", typ: tar.TypeReg, mode: 0o644, body: []byte("a1")},
		{name: "opt/b", typ: tar.TypeReg, mode: 0o644, body: []byte("b1")},
	})
	if err := extractTar(tar.NewReader(bytes.NewReader(layer1)), dir); err != nil {
		t.Fatalf("layer1: %v", err)
	}

	// Second layer opaque-whiteouts /opt then adds /opt/c.
	layer2 := makeTar(t, []tarEntry{
		{name: "opt/.wh..wh..opq", typ: tar.TypeReg, mode: 0o644},
		{name: "opt/c", typ: tar.TypeReg, mode: 0o644, body: []byte("c2")},
	})
	if err := extractTar(tar.NewReader(bytes.NewReader(layer2)), dir); err != nil {
		t.Fatalf("layer2: %v", err)
	}

	for _, gone := range []string{"opt/a", "opt/b"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed by opaque whiteout; err=%v", gone, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "opt/c")); err != nil || string(b) != "c2" {
		t.Errorf("opt/c = %q err=%v", b, err)
	}
}

func TestExtractTarPathTraversal(t *testing.T) {
	dir := t.TempDir()
	data := makeTar(t, []tarEntry{
		{name: "../escape", typ: tar.TypeReg, mode: 0o644, body: []byte("bad")},
	})
	err := extractTar(tar.NewReader(bytes.NewReader(data)), dir)
	if err == nil {
		t.Fatalf("expected path-traversal error, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "..", "escape")); statErr == nil {
		t.Errorf("traversal allowed: file created outside target")
	}
}

func TestExtractTarLaterLayerWins(t *testing.T) {
	dir := t.TempDir()
	layer1 := makeTar(t, []tarEntry{
		{name: "f", typ: tar.TypeReg, mode: 0o644, body: []byte("first")},
	})
	layer2 := makeTar(t, []tarEntry{
		{name: "f", typ: tar.TypeReg, mode: 0o644, body: []byte("second")},
	})
	if err := extractTar(tar.NewReader(bytes.NewReader(layer1)), dir); err != nil {
		t.Fatal(err)
	}
	if err := extractTar(tar.NewReader(bytes.NewReader(layer2)), dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "f"))
	if err != nil || string(b) != "second" {
		t.Errorf("f = %q err=%v, want %q", b, err, "second")
	}
}
