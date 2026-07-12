package postpivot

import (
	"bytes"
	"strings"
	"testing"
)

func TestSuperviseTeesToLogWriter(t *testing.T) {
	var buf bytes.Buffer
	code, err := Supervise(SuperviseOptions{
		Argv:      []string{"/bin/sh", "-c", "echo hi; echo bad >&2"},
		LogWriter: &buf,
	})
	if err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	out := buf.String()
	if !strings.Contains(out, "hi") || !strings.Contains(out, "bad") {
		t.Errorf("log buffer %q missing stdout or stderr", out)
	}
}
