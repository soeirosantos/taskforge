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

// Outcome classifies the result of a guarded state transition attempt so
// callers (ultimately the HTTP layer) can distinguish "I performed the
// transition" from the two different ways a transition can fail to happen.
type Outcome int

const (
	// OutcomeWon means this call's guarded UPDATE affected the row: the
	// caller performed the transition and the returned job reflects it.
	OutcomeWon Outcome = iota

	// OutcomeLost means a job with the given id exists but was not in the
	// state this transition requires — either another transition already
	// won the race, or the job was never in a state from which this
	// transition is legal. The returned job is the current, unmodified
	// snapshot.
	OutcomeLost

	// OutcomeNotFound means no job exists with the given id at all.
	OutcomeNotFound

	// OutcomeAttemptLimitReached is returned only by Retry: the job is
	// FAILED (the right state), but attempt_count is already at
	// jobs.MaxAttempts, so SPEC 27 requires a distinct outcome from a plain
	// wrong-state Lost. The returned job is the current snapshot.
	OutcomeAttemptLimitReached
)

func (o Outcome) String() string {
	switch o {
	case OutcomeWon:
		return "won"
	case OutcomeLost:
		return "lost"
	case OutcomeNotFound:
		return "not_found"
	case OutcomeAttemptLimitReached:
		return "attempt_limit_reached"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// ErrNoJobAvailable is returned by Claim when no queued job is eligible to
// be claimed (none QUEUED with attempt_count < jobs.MaxAttempts).
var ErrNoJobAvailable = errors.New("store: no job available")

// Claim atomically selects the next eligible queued job — ordered
// queued_at ASC, id ASC among status = QUEUED AND attempt_count <
// jobs.MaxAttempts (SPEC 20) — and transitions it QUEUED -> RUNNING,
// incrementing attempt_count and setting started_at/updated_at in the same
// statement (SPEC 14, 21).
//
// This is a single UPDATE ... RETURNING statement: the candidate row is
// chosen by a subquery evaluated as part of the same statement the engine
// uses to perform the update, so there is no read-then-write gap for a
// second claimant to land in. If no row qualifies, Claim returns
// ErrNoJobAvailable.
func (s *Store) Claim(ctx context.Context) (*jobs.Job, error) {
	now := jobs.FormatTime(time.Now())

	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = 'RUNNING', attempt_count = attempt_count + 1,
			started_at = ?, updated_at = ?
		WHERE id = (
			SELECT id FROM jobs
			WHERE status = 'QUEUED' AND attempt_count < ?
			ORDER BY queued_at ASC, id ASC
			LIMIT 1
		)
		RETURNING `+jobColumns, now, now, jobs.MaxAttempts)

	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoJobAvailable
	}
	if err != nil {
		return nil, fmt.Errorf("store: claim job: %w", err)
	}
	return j, nil
}

// Complete atomically transitions id RUNNING -> COMPLETED (SPEC 22),
// persisting result, finished_at, and updated_at. It succeeds only if the
// job is still RUNNING at the moment of the update.
func (s *Store) Complete(ctx context.Context, id string, result json.RawMessage) (*jobs.Job, Outcome, error) {
	now := jobs.FormatTime(time.Now())

	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = 'COMPLETED', result = ?, finished_at = ?, updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
		RETURNING `+jobColumns, nullableJSON(result), now, now, id)

	j, err := scanJob(row)
	if err == nil {
		return j, OutcomeWon, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("store: complete job: %w", err)
	}
	return s.classifyMiss(ctx, id, "complete job")
}

// Fail atomically transitions id RUNNING -> FAILED (SPEC 23), persisting
// the execution error, finished_at, and updated_at. It succeeds only if the
// job is still RUNNING at the moment of the update.
func (s *Store) Fail(ctx context.Context, id string, jobErr *jobs.JobError) (*jobs.Job, Outcome, error) {
	now := jobs.FormatTime(time.Now())

	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = 'FAILED', error = ?, finished_at = ?, updated_at = ?
		WHERE id = ? AND status = 'RUNNING'
		RETURNING `+jobColumns, nullableJSON(jobErr.JSON()), now, now, id)

	j, err := scanJob(row)
	if err == nil {
		return j, OutcomeWon, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("store: fail job: %w", err)
	}
	return s.classifyMiss(ctx, id, "fail job")
}

// Cancel atomically transitions id QUEUED|RUNNING -> CANCELLED (SPEC 24),
// clearing result and error to NULL and setting finished_at/updated_at.
//
// Cancelling a job that is already CANCELLED, or one that is COMPLETED or
// FAILED, does not match the guard and reports OutcomeLost with the
// job's current (unmodified) snapshot — including a job already
// CANCELLED, so a caller implementing SPEC 24's idempotent-cancel rule can
// inspect the returned status. That policy decision (200 for
// already-CANCELLED vs 409 for COMPLETED/FAILED) belongs to the API layer,
// not the store.
func (s *Store) Cancel(ctx context.Context, id string) (*jobs.Job, Outcome, error) {
	now := jobs.FormatTime(time.Now())

	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = 'CANCELLED', result = NULL, error = NULL,
			finished_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('QUEUED', 'RUNNING')
		RETURNING `+jobColumns, now, now, id)

	j, err := scanJob(row)
	if err == nil {
		return j, OutcomeWon, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("store: cancel job: %w", err)
	}
	return s.classifyMiss(ctx, id, "cancel job")
}

// Retry atomically transitions id FAILED -> QUEUED (SPEC 27), guarded on
// attempt_count < jobs.MaxAttempts. attempt_count itself is left unchanged;
// queued_at is refreshed and started_at/finished_at/result/error are
// cleared.
//
// When the guarded update affects no rows, Retry reads the row once to
// classify why (SPEC 27 requires OutcomeAttemptLimitReached to be
// distinguishable from a plain wrong-state OutcomeLost) — the one read
// permitted after a failed guarded write, never before it.
func (s *Store) Retry(ctx context.Context, id string) (*jobs.Job, Outcome, error) {
	now := jobs.FormatTime(time.Now())

	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs
		SET status = 'QUEUED', queued_at = ?, started_at = NULL,
			finished_at = NULL, result = NULL, error = NULL, updated_at = ?
		WHERE id = ? AND status = 'FAILED' AND attempt_count < ?
		RETURNING `+jobColumns, now, now, id, jobs.MaxAttempts)

	j, err := scanJob(row)
	if err == nil {
		return j, OutcomeWon, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("store: retry job: %w", err)
	}

	existing, err := s.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, OutcomeNotFound, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("store: retry job: %w", err)
	}
	if existing.Status == jobs.StatusFailed && existing.AttemptCount >= jobs.MaxAttempts {
		return existing, OutcomeAttemptLimitReached, nil
	}
	return existing, OutcomeLost, nil
}

// classifyMiss is shared by Complete, Fail, and Cancel: once their guarded
// UPDATE has already affected no rows, it reads the row exactly once to
// tell OutcomeLost (the job exists, in some other state) apart from
// OutcomeNotFound (no such job) — never to decide whether to write.
func (s *Store) classifyMiss(ctx context.Context, id, op string) (*jobs.Job, Outcome, error) {
	existing, err := s.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, OutcomeNotFound, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("store: %s: %w", op, err)
	}
	return existing, OutcomeLost, nil
}

// Recover atomically transitions every RUNNING job to FAILED with
// jobs.InterruptedExecution() as its error (SPEC 34). result is cleared to
// NULL; attempt_count, created_at, queued_at, and started_at are preserved
// untouched since they are not part of the SET clause. It is a single
// unconditional UPDATE (recovery is not racing anything: it runs before
// workers or the HTTP server start accepting work), and returns how many
// rows it changed.
func (s *Store) Recover(ctx context.Context) (int64, error) {
	now := jobs.FormatTime(time.Now())

	res, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'FAILED', error = ?, result = NULL,
			finished_at = ?, updated_at = ?
		WHERE status = 'RUNNING'
	`, string(jobs.InterruptedExecution().JSON()), now, now)
	if err != nil {
		return 0, fmt.Errorf("store: recover jobs: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: recover jobs: %w", err)
	}
	return n, nil
}
