// Package store provides a SQLite-backed persistence layer for jobs.
//
// The store creates its schema on open (SPEC 5: no manual setup is
// required before startup) and serializes all access through a single
// connection (SetMaxOpenConns(1)), matching SQLite's single-writer model.
//
// This package implements only creation, retrieval, filtered listing, and a
// health check. State transitions (claim, complete, fail, cancel, retry,
// startup recovery) belong to a later package and are intentionally absent
// here.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// schema is applied with CREATE TABLE/INDEX IF NOT EXISTS so that opening the
// store is idempotent and requires no external migration step (SPEC 5).
//
// Timestamps are stored as TEXT in jobs.TimestampLayout. That layout is
// fixed-width and zero-padded, so lexicographic TEXT ordering in SQL is
// identical to chronological ordering — this is what makes
// "ORDER BY created_at DESC, id ASC" and the (status, queued_at, id) index
// correct.
const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id            TEXT PRIMARY KEY,
	type          TEXT NOT NULL,
	status        TEXT NOT NULL,
	payload       TEXT NOT NULL,
	result        TEXT,
	error         TEXT,
	attempt_count INTEGER NOT NULL,
	created_at    TEXT NOT NULL,
	queued_at     TEXT NOT NULL,
	started_at    TEXT,
	finished_at   TEXT,
	updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_status_queued_at_id
	ON jobs (status, queued_at, id);
`

// Store is a SQLite-backed job store. A Store is safe for concurrent use;
// the underlying *sql.DB is limited to a single open connection, so callers
// are serialized by the database/sql connection pool.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and
// ensures the schema exists. The returned Store must be closed with Close
// when no longer needed.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}

	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: create schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks that the database is reachable. It returns ErrUnavailable
// rather than the raw driver error, so callers never leak database detail
// to clients (SPEC 30).
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}
