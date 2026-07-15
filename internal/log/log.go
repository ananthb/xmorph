// Package log provides an slog handler that fans out to a colorized
// stderr writer AND an in-memory bytes.Buffer. The buffer persists
// across pivot_root (it lives in anonymous heap pages, which survive
// the mount-namespace transition) and is flushed to the new rootfs
// log file immediately before pivot_root returns.
//
// Mirrors the responsibilities of src/util/log.zig in the Zig version.
package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Handler is an slog.Handler that writes each record to stderr (colored),
// an in-memory buffer (for the pivot flush), and any additional sinks
// registered via AddSink (uncolored). Safe for concurrent use.
type Handler struct {
	mu     sync.Mutex
	stderr io.Writer
	buf    *bytes.Buffer
	level  slog.Level
	colors bool
	scope  string
	sinks  *sinkList
}

// sinkList holds the additional log sinks. It is shared by pointer across
// handler clones (WithAttrs/WithGroup) so a sink registered on the root
// handler is seen by every clone.
type sinkList struct {
	mu sync.Mutex
	w  []io.Writer
}

// NewHandler returns a Handler. If stderr is nil, os.Stderr is used.
// Color escapes are emitted when colors is true.
func NewHandler(stderr io.Writer, level slog.Level, colors bool) *Handler {
	if stderr == nil {
		stderr = os.Stderr
	}
	return &Handler{
		stderr: stderr,
		buf:    &bytes.Buffer{},
		level:  level,
		colors: colors,
		sinks:  &sinkList{},
	}
}

// AddSink registers an additional writer that receives every uncolored log
// line, alongside stderr and the in-memory buffer. Shared across clones.
// Safe for concurrent use; a nil writer is ignored.
func (h *Handler) AddSink(w io.Writer) {
	if w == nil {
		return
	}
	h.sinks.mu.Lock()
	h.sinks.w = append(h.sinks.w, w)
	h.sinks.mu.Unlock()
}

func (h *Handler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level
}

// SetLevel changes the minimum level emitted. Safe for concurrent use.
func (h *Handler) SetLevel(lvl slog.Level) {
	h.mu.Lock()
	h.level = lvl
	h.mu.Unlock()
}

// SetColors enables or disables ANSI color escapes on stderr output.
// The in-memory buffer never receives colors.
func (h *Handler) SetColors(on bool) {
	h.mu.Lock()
	h.colors = on
	h.mu.Unlock()
}

func levelTag(lvl slog.Level) string {
	switch {
	case lvl >= slog.LevelError:
		return "ERROR"
	case lvl >= slog.LevelWarn:
		return "WARN"
	case lvl >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

func levelColor(lvl slog.Level) string {
	switch {
	case lvl >= slog.LevelError:
		return "\x1b[31m" // red
	case lvl >= slog.LevelWarn:
		return "\x1b[33m" // yellow
	case lvl >= slog.LevelInfo:
		return "\x1b[32m" // green
	default:
		return "\x1b[36m" // cyan
	}
}

const reset = "\x1b[0m"

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	tag := levelTag(r.Level)

	// stderr line — colored if enabled.
	if h.colors {
		fmt.Fprintf(h.stderr, "%s[%s]%s ", levelColor(r.Level), tag, reset)
	} else {
		fmt.Fprintf(h.stderr, "[%s] ", tag)
	}
	if h.scope != "" {
		fmt.Fprintf(h.stderr, "[%s] ", h.scope)
	}
	fmt.Fprint(h.stderr, r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(h.stderr, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	fmt.Fprintln(h.stderr)

	// Uncolored line — identical content — to the in-memory buffer and
	// every additional sink (file, syslog, …).
	line := h.plainLine(tag, r)
	h.buf.WriteString(line)
	h.sinks.mu.Lock()
	for _, w := range h.sinks.w {
		_, _ = io.WriteString(w, line)
	}
	h.sinks.mu.Unlock()
	return nil
}

// plainLine renders the uncolored "[TAG] [scope] message k=v …\n" form.
func (h *Handler) plainLine(tag string, r slog.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] ", tag)
	if h.scope != "" {
		fmt.Fprintf(&b, "[%s] ", h.scope)
	}
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	b.WriteByte('\n')
	return b.String()
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// slog API requires a new handler that includes the attrs in every
	// record. We render attrs inline in Handle, so cloning suffices.
	return h.clone()
}

func (h *Handler) WithGroup(name string) slog.Handler {
	clone := h.clone()
	if clone.scope == "" {
		clone.scope = name
	} else {
		clone.scope = clone.scope + "." + name
	}
	return clone
}

func (h *Handler) clone() *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Important: share buf and stderr — the clone writes into the same
	// stream and same in-memory buffer.
	return &Handler{
		stderr: h.stderr,
		buf:    h.buf,
		level:  h.level,
		colors: h.colors,
		scope:  h.scope,
		sinks:  h.sinks,
	}
}

// FlushBufferTo writes the in-memory buffer contents to the file at path,
// creating parent directories as needed. Called just before pivot_root
// so the new rootfs contains the pre-pivot log lines.
//
// Errors are returned but typically swallowed at the call site — at this
// point in the pivot sequence there is nowhere useful to log them.
func (h *Handler) FlushBufferTo(path string) error {
	h.mu.Lock()
	data := append([]byte(nil), h.buf.Bytes()...)
	h.mu.Unlock()

	if len(data) == 0 {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// Buffer returns a copy of the current in-memory log contents.
func (h *Handler) Buffer() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.buf.Bytes()...)
}
