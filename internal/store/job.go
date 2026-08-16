package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"taskforge/internal/jobs"
)

const jobColumns = `id, type, status, payload, result, error, attempt_count,
	created_at, queued_at, started_at, finished_at, updated_at`

// Create persists a new job. It does not validate the job's contents —
// callers are expected to construct jobs via jobs.NewJob, which already
// enforces SPEC's invariants.
func (s *Store) Create(ctx context.Context, j *jobs.Job) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (`+jobColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		j.ID,
		string(j.Type),
		string(j.Status),
		string(j.Payload),
		nullableJSON(j.Result),
		nullableJSON(j.Err),
		j.AttemptCount,
		jobs.FormatTime(j.CreatedAt),
		jobs.FormatTime(j.QueuedAt),
		nullableTime(j.StartedAt),
		nullableTime(j.FinishedAt),
		jobs.FormatTime(j.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}
	return nil
}

// Get retrieves a job by id. If no job exists with that id, Get returns
// ErrNotFound so callers can distinguish "not found" from a real failure.
func (s *Store) Get(ctx context.Context, id string) (*jobs.Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+jobColumns+`
		FROM jobs
		WHERE id = ?
	`, id)

	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get job: %w", err)
	}
	return j, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, letting scanJob
// serve Get (single row) and List (multiple rows) alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanJob reads one row in jobColumns order into a jobs.Job, converting
// nullable TEXT columns to nil *time.Time / nil json.RawMessage as
// appropriate.
func scanJob(sc rowScanner) (*jobs.Job, error) {
	var (
		id, jobType, status, payload string
		result, jobErr               sql.NullString
		attemptCount                 int
		createdAt, queuedAt          string
		startedAt, finishedAt        sql.NullString
		updatedAt                    string
	)

	err := sc.Scan(
		&id,
		&jobType,
		&status,
		&payload,
		&result,
		&jobErr,
		&attemptCount,
		&createdAt,
		&queuedAt,
		&startedAt,
		&finishedAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	j := &jobs.Job{
		ID:           id,
		Type:         jobs.Type(jobType),
		Status:       jobs.Status(status),
		Payload:      json.RawMessage(payload),
		AttemptCount: attemptCount,
	}

	if result.Valid {
		j.Result = json.RawMessage(result.String)
	}
	if jobErr.Valid {
		j.Err = json.RawMessage(jobErr.String)
	}

	if j.CreatedAt, err = jobs.ParseTime(createdAt); err != nil {
		return nil, fmt.Errorf("store: parse created_at: %w", err)
	}
	if j.QueuedAt, err = jobs.ParseTime(queuedAt); err != nil {
		return nil, fmt.Errorf("store: parse queued_at: %w", err)
	}
	if j.UpdatedAt, err = jobs.ParseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("store: parse updated_at: %w", err)
	}

	if startedAt.Valid {
		t, err := jobs.ParseTime(startedAt.String)
		if err != nil {
			return nil, fmt.Errorf("store: parse started_at: %w", err)
		}
		j.StartedAt = &t
	}
	if finishedAt.Valid {
		t, err := jobs.ParseTime(finishedAt.String)
		if err != nil {
			return nil, fmt.Errorf("store: parse finished_at: %w", err)
		}
		j.FinishedAt = &t
	}

	return j, nil
}

// nullableJSON converts a possibly-nil json.RawMessage into a driver value:
// nil stays a genuine SQL NULL rather than becoming an empty string, so the
// round trip through Get/List is exact.
func nullableJSON(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	return string(raw)
}

// nullableTime converts a possibly-nil *time.Time into a driver value,
// formatted with jobs.FormatTime so ordering and round-tripping stay exact.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return jobs.FormatTime(*t)
}
