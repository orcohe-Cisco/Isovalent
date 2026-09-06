package audit

import (
	"context"
	"encoding/json"
	"time"
)

// RawStore is the subset of the Postgres store the audit sink needs. Keeping
// it as an interface here (rather than importing internal/store) avoids an
// import cycle and keeps the audit package storage-agnostic.
type RawStore interface {
	EnsureAuditSchema(ctx context.Context) error
	SaveAuditRaw(ctx context.Context, id int64, t time.Time, entry []byte) error
	QueryAuditRaw(ctx context.Context, since, until time.Time, limit int) ([][]byte, error)
}

// PostgresSink adapts a RawStore to Sink.
type PostgresSink struct{ Raw RawStore }

// NewPostgresSink prepares the schema and returns the sink.
func NewPostgresSink(ctx context.Context, raw RawStore) (*PostgresSink, error) {
	if err := raw.EnsureAuditSchema(ctx); err != nil {
		return nil, err
	}
	return &PostgresSink{Raw: raw}, nil
}

// SaveAudit implements Sink.
func (s *PostgresSink) SaveAudit(ctx context.Context, e Entry) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.Raw.SaveAuditRaw(ctx, e.ID, e.Time, raw)
}

// QueryAudit implements Sink.
func (s *PostgresSink) QueryAudit(ctx context.Context, since, until time.Time, limit int) ([]Entry, error) {
	rows, err := s.Raw.QueryAuditRaw(ctx, since, until, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, raw := range rows {
		var e Entry
		if json.Unmarshal(raw, &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}
