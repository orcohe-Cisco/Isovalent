// Package logbuf keeps the most recent log lines in memory so the console can
// show its own logs — backend and browser — without anyone needing to run
// `kubectl logs`.
//
// The point is diagnosis, not archival: when the UI shows an empty flow table,
// the answer is almost always in a log line ("hubble relay: connection
// refused") that nobody thinks to go and read.
package logbuf

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Line is one captured log record.
type Line struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Origin  string            `json:"origin"` // backend | frontend
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Buffer is a bounded ring of log lines with subscriber fan-out.
type Buffer struct {
	mu     sync.RWMutex
	lines  []Line
	cap    int
	subs   map[chan Line]struct{}
	counts map[string]int64
	pub    func(Line)
}

// SetPublisher attaches a fan-out callback (the WebSocket hub). It is set
// after construction because the logger has to exist before the hub does —
// the first thing worth logging is the hub failing to start.
func (b *Buffer) SetPublisher(fn func(Line)) {
	b.mu.Lock()
	b.pub = fn
	b.mu.Unlock()
}

// New returns a buffer of the given capacity.
func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Buffer{cap: capacity, subs: map[chan Line]struct{}{}, counts: map[string]int64{}}
}

// Append stores a line and notifies subscribers (non-blocking).
func (b *Buffer) Append(l Line) {
	if l.Time.IsZero() {
		l.Time = time.Now().UTC()
	}
	if l.Origin == "" {
		l.Origin = "backend"
	}
	b.mu.Lock()
	b.lines = append(b.lines, l)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
	b.counts[strings.ToUpper(l.Level)]++
	subs := make([]chan Line, 0, len(b.subs))
	for c := range b.subs {
		subs = append(subs, c)
	}
	pub := b.pub
	b.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- l:
		default: // slow consumer: drop rather than stall the logger
		}
	}
	if pub != nil {
		pub(l)
	}
}

// Query returns lines newest-first, filtered by level, origin and substring.
func (b *Buffer) Query(level, origin, q string, limit int) []Line {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	level = strings.ToUpper(strings.TrimSpace(level))
	q = strings.ToLower(strings.TrimSpace(q))
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Line, 0, limit)
	for i := len(b.lines) - 1; i >= 0 && len(out) < limit; i-- {
		l := b.lines[i]
		if level != "" && level != "ALL" && !levelAtLeast(l.Level, level) {
			continue
		}
		if origin != "" && origin != "all" && l.Origin != origin {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(l.Message), q) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Counts returns per-level totals since start.
func (b *Buffer) Counts() map[string]int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := map[string]int64{}
	for k, v := range b.counts {
		out[k] = v
	}
	return out
}

var levelRank = map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}

func levelAtLeast(have, want string) bool {
	return levelRank[strings.ToUpper(have)] >= levelRank[want]
}

// Subscribe returns a channel of new lines and an unsubscribe function.
func (b *Buffer) Subscribe() (<-chan Line, func()) {
	ch := make(chan Line, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}

// Handler is an slog.Handler that writes through to a delegate and also
// captures each record into the buffer.
type Handler struct {
	slog.Handler
	buf   *Buffer
	attrs []slog.Attr
}

// NewHandler wraps delegate so every record is also captured.
func NewHandler(delegate slog.Handler, buf *Buffer) *Handler {
	return &Handler{Handler: delegate, buf: buf}
}

// Handle captures the record then forwards it.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	fields := map[string]string{}
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.String()
		return true
	})
	h.buf.Append(Line{
		Time: r.Time, Level: r.Level.String(), Origin: "backend",
		Message: r.Message, Fields: fields,
	})
	return h.Handler.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs), buf: h.buf, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name), buf: h.buf, attrs: h.attrs}
}
