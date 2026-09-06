// Package audit records every state-changing action taken through the console:
// who did it, when, against what, what changed, and whether it worked.
//
// The entries are deliberately self-contained (the before/after manifests are
// stored, not referenced) so an audit entry still answers "what did this
// change?" after the object it refers to has been deleted.
package audit

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// Outcome of an audited action.
const (
	OutcomeSuccess = "success"
	OutcomeDenied  = "denied"
	OutcomeError   = "error"
)

// Entry is one audited action.
type Entry struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Actor   string    `json:"actor"`
	Roles   []string  `json:"roles,omitempty"`
	Source  string    `json:"sourceIP,omitempty"`
	Action  string    `json:"action"`
	Target  string    `json:"target,omitempty"`
	Outcome string    `json:"outcome"`
	Message string    `json:"message,omitempty"`
	// Before/After are JSON snapshots when the action changed an object.
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
	// Diff is a human-readable summary of what changed.
	Diff []string `json:"diff,omitempty"`
}

// Sink persists entries beyond the in-memory ring (optional).
type Sink interface {
	SaveAudit(ctx context.Context, e Entry) error
	QueryAudit(ctx context.Context, since, until time.Time, limit int) ([]Entry, error)
}

// Log is a bounded in-memory audit ring with an optional durable sink.
type Log struct {
	mu      sync.RWMutex
	entries []Entry
	cap     int
	nextID  int64
	sink    Sink
}

// New returns a log keeping up to capacity entries in memory.
func New(capacity int) *Log {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Log{cap: capacity, nextID: 1}
}

// SetSink attaches durable storage.
func (l *Log) SetSink(s Sink) { l.mu.Lock(); l.sink = s; l.mu.Unlock() }

// Record appends an entry, filling in ID and time.
func (l *Log) Record(e Entry) Entry {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if e.Outcome == "" {
		e.Outcome = OutcomeSuccess
	}
	l.mu.Lock()
	e.ID = l.nextID
	l.nextID++
	l.entries = append(l.entries, e)
	if len(l.entries) > l.cap {
		l.entries = l.entries[len(l.entries)-l.cap:]
	}
	sink := l.sink
	l.mu.Unlock()
	if sink != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = sink.SaveAudit(ctx, e)
		}()
	}
	return e
}

// Query returns entries in [since, until] newest first, optionally filtered by
// a case-insensitive substring across actor/action/target/message.
func (l *Log) Query(ctx context.Context, since, until time.Time, q string, limit int) []Entry {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	l.mu.RLock()
	sink := l.sink
	pool := make([]Entry, len(l.entries))
	copy(pool, l.entries)
	l.mu.RUnlock()

	if sink != nil {
		if durable, err := sink.QueryAudit(ctx, since, until, limit*4); err == nil && len(durable) > 0 {
			pool = mergeByID(pool, durable)
		}
	}

	q = strings.ToLower(strings.TrimSpace(q))
	out := make([]Entry, 0, limit)
	for _, e := range pool {
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		if !until.IsZero() && e.Time.After(until) {
			continue
		}
		if q != "" && !matches(e, q) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func mergeByID(a, b []Entry) []Entry {
	seen := map[int64]bool{}
	out := make([]Entry, 0, len(a)+len(b))
	for _, e := range append(a, b...) {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return out
}

func matches(e Entry, q string) bool {
	for _, f := range []string{e.Actor, e.Action, e.Target, e.Message, e.Outcome} {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// Stats summarizes the log for the dashboard.
func (l *Log) Stats() map[string]int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := map[string]int64{"total": int64(len(l.entries))}
	for _, e := range l.entries {
		out[e.Outcome]++
	}
	return out
}

// DiffManifests produces a readable list of changed JSON paths between two
// manifests. It is intentionally shallow-but-complete: every leaf that
// differs is reported with its dotted path, which is what you want when the
// question is "what did this apply actually change?".
func DiffManifests(before, after json.RawMessage) []string {
	var a, b any
	if len(before) > 0 {
		_ = json.Unmarshal(before, &a)
	}
	if len(after) > 0 {
		_ = json.Unmarshal(after, &b)
	}
	var out []string
	diffValue("", a, b, &out)
	sort.Strings(out)
	if len(out) > 60 {
		out = append(out[:60], "… and more")
	}
	return out
}

func diffValue(path string, a, b any, out *[]string) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			*out = append(*out, path+": replaced")
			return
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		for k := range keys {
			// Server-managed fields change on every apply and say nothing
			// about intent.
			if k == "resourceVersion" || k == "generation" || k == "managedFields" || k == "uid" {
				continue
			}
			diffValue(join(path, k), av[k], bv[k], out)
		}
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			*out = append(*out, path+": list changed")
			return
		}
		for i := range av {
			diffValue(path+"["+itoa(i)+"]", av[i], bv[i], out)
		}
	default:
		if !equalScalar(a, b) {
			*out = append(*out, path+": "+render(a)+" → "+render(b))
		}
	}
}

func equalScalar(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func render(v any) string {
	if v == nil {
		return "(absent)"
	}
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func join(p, k string) string {
	if p == "" {
		return k
	}
	return p + "." + k
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
