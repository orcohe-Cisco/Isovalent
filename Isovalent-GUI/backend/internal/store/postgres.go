package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq" // postgres driver
)

// PostgresStore persists records to a single jsonb table. Enable it by setting
// IC_DB_DSN, e.g. "postgres://user:pass@postgres:5432/isovalent?sslmode=disable".
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore connects and ensures the schema exists.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetConnMaxIdleTime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	return &PostgresStore{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS ic_records (
  id      BIGSERIAL PRIMARY KEY,
  kind    TEXT NOT NULL,
  ts      TIMESTAMPTZ NOT NULL,
  payload JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS ic_records_kind_ts ON ic_records (kind, ts DESC);`

// Save inserts a record.
func (p *PostgresStore) Save(ctx context.Context, kind string, t time.Time, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx,
		`INSERT INTO ic_records (kind, ts, payload) VALUES ($1, $2, $3)`, kind, t, raw)
	return err
}

// Query returns records in the window, newest first.
func (p *PostgresStore) Query(ctx context.Context, kind string, since, until time.Time, limit int) ([]Record, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	if since.IsZero() {
		since = time.Unix(0, 0)
	}
	if until.IsZero() {
		until = time.Now().Add(time.Hour)
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT ts, payload FROM ic_records
		 WHERE kind = $1 AND ts BETWEEN $2 AND $3
		 ORDER BY ts DESC LIMIT $4`, kind, since, until, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		var raw []byte
		if err := rows.Scan(&r.Time, &raw); err != nil {
			return nil, err
		}
		r.Payload = json.RawMessage(raw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Close closes the pool.
func (p *PostgresStore) Close() error { return p.db.Close() }

const auditSchema = `
CREATE TABLE IF NOT EXISTS ic_audit (
  id      BIGINT PRIMARY KEY,
  ts      TIMESTAMPTZ NOT NULL,
  entry   JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS ic_audit_ts ON ic_audit (ts DESC);`

// EnsureAuditSchema creates the audit table. Called once at startup so the
// audit sink cannot fail on its first write.
func (p *PostgresStore) EnsureAuditSchema(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, auditSchema)
	return err
}

// SaveAuditRaw persists one audit entry keyed by id. Entries are immutable, so
// a repeat of the same id is ignored rather than overwritten.
func (p *PostgresStore) SaveAuditRaw(ctx context.Context, id int64, t time.Time, entry []byte) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO ic_audit (id, ts, entry) VALUES ($1, $2, $3) ON CONFLICT (id) DO NOTHING`,
		id, t, entry)
	return err
}

// QueryAuditRaw returns audit entries in the window, newest first.
func (p *PostgresStore) QueryAuditRaw(ctx context.Context, since, until time.Time, limit int) ([][]byte, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	if since.IsZero() {
		since = time.Unix(0, 0)
	}
	if until.IsZero() {
		until = time.Now().Add(time.Hour)
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT entry FROM ic_audit WHERE ts BETWEEN $1 AND $2 ORDER BY ts DESC LIMIT $3`,
		since, until, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}
