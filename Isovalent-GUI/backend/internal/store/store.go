// Package store persists flows, events, and alerts for historical
// investigation ("time-travel"). The default MemoryStore keeps a bounded ring
// per kind; PostgresStore (postgres.go) persists to a database when
// IC_DB_DSN is set.
package store

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Kinds of stored records.
const (
	KindFlow  = "flow"
	KindEvent = "event"
	KindAlert = "alert"
)

// Record is a timestamped JSON payload.
type Record struct {
	Time    time.Time       `json:"time"`
	Payload json.RawMessage `json:"payload"`
}

// Store persists and queries historical records.
type Store interface {
	Save(ctx context.Context, kind string, t time.Time, payload any) error
	Query(ctx context.Context, kind string, since, until time.Time, limit int) ([]Record, error)
	Close() error
}

// MemoryStore is a bounded in-memory ring per kind (default when no DB). It is
// bounded both by count and, optionally, by age (the retention window).
type MemoryStore struct {
	mu     sync.RWMutex
	cap    int
	maxAge time.Duration
	data   map[string][]Record
}

// NewMemoryStore returns a store keeping up to capPerKind records per kind.
func NewMemoryStore(capPerKind int) *MemoryStore {
	if capPerKind <= 0 {
		capPerKind = 5000
	}
	return &MemoryStore{cap: capPerKind, data: map[string][]Record{}}
}

// SetMaxAge sets the retention window (records older than this are pruned).
func (m *MemoryStore) SetMaxAge(d time.Duration) {
	m.mu.Lock()
	m.maxAge = d
	m.mu.Unlock()
}

// Save appends a record, trimming by count and age.
func (m *MemoryStore) Save(_ context.Context, kind string, t time.Time, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := append(m.data[kind], Record{Time: t, Payload: raw})
	if m.maxAge > 0 {
		cutoff := t.Add(-m.maxAge)
		i := 0
		for i < len(s) && s[i].Time.Before(cutoff) {
			i++
		}
		s = s[i:]
	}
	if len(s) > m.cap {
		s = s[len(s)-m.cap:]
	}
	m.data[kind] = s
	return nil
}

// Query returns records in [since, until] (newest first), up to limit.
func (m *MemoryStore) Query(_ context.Context, kind string, since, until time.Time, limit int) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Record
	for _, r := range m.data[kind] {
		if (!since.IsZero() && r.Time.Before(since)) || (!until.IsZero() && r.Time.After(until)) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Close is a no-op for the memory store.
func (m *MemoryStore) Close() error { return nil }
