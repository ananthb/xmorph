package log

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerCapturesToBufferAndStderr(t *testing.T) {
	var stderr bytes.Buffer
	h := NewHandler(&stderr, slog.LevelInfo, false)
	logger := slog.New(h)

	logger.Info("first message", "key", "value")
	logger.Warn("second message")
	logger.Debug("filtered") // below level

	stderrOut := stderr.String()
	bufOut := string(h.Buffer())

	if !strings.Contains(stderrOut, "first message") {
		t.Errorf("stderr missing first message: %q", stderrOut)
	}
	if !strings.Contains(bufOut, "first message") {
		t.Errorf("buffer missing first message: %q", bufOut)
	}
	if !strings.Contains(stderrOut, "key=value") {
		t.Errorf("stderr missing attr: %q", stderrOut)
	}
	if !strings.Contains(stderrOut, "[WARN]") {
		t.Errorf("stderr missing WARN tag: %q", stderrOut)
	}
	if strings.Contains(stderrOut, "filtered") || strings.Contains(bufOut, "filtered") {
		t.Errorf("debug line should have been filtered: stderr=%q buf=%q", stderrOut, bufOut)
	}
}

func TestFlushBufferTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "xmorph.log")

	h := NewHandler(&bytes.Buffer{}, slog.LevelInfo, false)
	logger := slog.New(h)
	logger.Info("alpha")
	logger.Info("beta")

	if err := h.FlushBufferTo(path); err != nil {
		t.Fatalf("FlushBufferTo: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Errorf("file missing entries: %q", got)
	}
}

func TestFlushEmpty(t *testing.T) {
	// Empty buffer → no file written, no error.
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.log")
	h := NewHandler(&bytes.Buffer{}, slog.LevelInfo, false)
	if err := h.FlushBufferTo(path); err != nil {
		t.Fatalf("FlushBufferTo: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should not exist, err=%v", err)
	}
}

func TestHandlerEnabled(t *testing.T) {
	h := NewHandler(&bytes.Buffer{}, slog.LevelWarn, false)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("INFO enabled at WARN level")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("ERROR should be enabled at WARN level")
	}
}
