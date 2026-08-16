package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// openTestStore opens a fresh Store backed by its own database file under
// t.TempDir(). Each test gets its own file: with SetMaxOpenConns(1), two
// live handles on one file can block each other.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close: %v", err)
		}
	})
	return s
}

func mustNewJob(t *testing.T, typ jobs.Type, payload string, now time.Time) *jobs.Job {
	t.Helper()
	j, err := jobs.NewJob(typ, json.RawMessage(payload), now)
	if err != nil {
		t.Fatalf("jobs.NewJob: %v", err)
	}
	return j
}

// TestOpen_FreshPathNoManualSetup covers acceptance criterion 1: a fresh
// path produces a working database with no manual setup, and Open is
// idempotent (opening the same path twice must not fail on "table exists").
func TestOpen_FreshPathNoManualSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open(%q): %v", path, err)
	}
	defer s.Close()

	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping on freshly opened store: %v", err)
	}

	// Immediately usable: Create + Get without any manual schema step.
	now := time.Now().UTC()
	j := mustNewJob(t, jobs.TypeHash, `{"data":"abc"}`, now)
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create on fresh store: %v", err)
	}
	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get on fresh store: %v", err)
	}
	if got.ID != j.ID {
		t.Fatalf("got id %q, want %q", got.ID, j.ID)
	}
}

// TestOpen_ReopenSamePathIsIdempotent ensures schema creation uses
// CREATE TABLE/INDEX IF NOT EXISTS and does not error on a second open.
func TestOpen_ReopenSamePathIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")

	s1, err := store.Open(path)
	if err != nil {
		t.Fatalf("first store.Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	s2, err := store.Open(path)
	if err != nil {
		t.Fatalf("second store.Open on existing file: %v", err)
	}
	defer s2.Close()
}

// TestGet_UnknownIDReturnsErrNotFound covers acceptance criterion 2.
func TestGet_UnknownIDReturnsErrNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("Get with unknown id: got nil error, want ErrNotFound")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get with unknown id: got %v, want ErrNotFound", err)
	}
}

// TestCreateGet_RoundTripsAllFields covers acceptance criterion 3: every
// field, including nil Result/Err and nil StartedAt/FinishedAt, survives
// close-and-reopen byte-identically.
func TestCreateGet_RoundTripsAllFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")

	s1, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	now := time.Date(2026, 8, 15, 12, 30, 0, 123456789, time.UTC)
	j := mustNewJob(t, jobs.TypeHash, `{"data":"hello world"}`, now)
	// j is QUEUED with nil Result/Err and nil StartedAt/FinishedAt straight
	// out of NewJob — exercise that shape first.

	ctx := context.Background()
	if err := s1.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen store.Open: %v", err)
	}
	defer s2.Close()

	got, err := s2.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}

	if got.ID != j.ID {
		t.Errorf("ID = %q, want %q", got.ID, j.ID)
	}
	if got.Type != j.Type {
		t.Errorf("Type = %q, want %q", got.Type, j.Type)
	}
	if got.Status != j.Status {
		t.Errorf("Status = %q, want %q", got.Status, j.Status)
	}
	if string(got.Payload) != string(j.Payload) {
		t.Errorf("Payload = %q, want %q", got.Payload, j.Payload)
	}
	if got.Result != nil {
		t.Errorf("Result = %q, want nil", got.Result)
	}
	if got.Err != nil {
		t.Errorf("Err = %q, want nil", got.Err)
	}
	if got.AttemptCount != j.AttemptCount {
		t.Errorf("AttemptCount = %d, want %d", got.AttemptCount, j.AttemptCount)
	}
	if got.StartedAt != nil {
		t.Errorf("StartedAt = %v, want nil", got.StartedAt)
	}
	if got.FinishedAt != nil {
		t.Errorf("FinishedAt = %v, want nil", got.FinishedAt)
	}
	if !got.CreatedAt.Equal(j.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, j.CreatedAt)
	}
	if !got.QueuedAt.Equal(j.QueuedAt) {
		t.Errorf("QueuedAt = %v, want %v", got.QueuedAt, j.QueuedAt)
	}
	if !got.UpdatedAt.Equal(j.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, j.UpdatedAt)
	}
	// Exact formatted string round trip too, since equality alone would not
	// catch a sub-nanosecond formatting truncation.
	if jobs.FormatTime(got.CreatedAt) != jobs.FormatTime(j.CreatedAt) {
		t.Errorf("formatted CreatedAt = %q, want %q", jobs.FormatTime(got.CreatedAt), jobs.FormatTime(j.CreatedAt))
	}
}

// TestCreateGet_RoundTripsNonNilOptionalFields exercises the other shape:
// Result, Err, StartedAt, and FinishedAt all populated, plus a non-zero
// AttemptCount, to make sure the non-nil path round-trips too.
func TestCreateGet_RoundTripsNonNilOptionalFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")

	s1, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC)
	j := mustNewJob(t, jobs.TypeDelay, `{"ms":10}`, now)

	started := now.Add(1 * time.Second)
	finished := now.Add(2 * time.Second)
	j.Status = jobs.StatusFailed
	j.AttemptCount = 2
	j.StartedAt = &started
	j.FinishedAt = &finished
	j.Result = json.RawMessage(`{"ok":true}`)
	j.Err = json.RawMessage(`{"code":"INTENTIONAL_FAILURE","message":"job failed intentionally"}`)
	j.UpdatedAt = finished

	ctx := context.Background()
	if err := s1.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen store.Open: %v", err)
	}
	defer s2.Close()

	got, err := s2.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}

	if got.AttemptCount != 2 {
		t.Errorf("AttemptCount = %d, want 2", got.AttemptCount)
	}
	if string(got.Result) != string(j.Result) {
		t.Errorf("Result = %q, want %q", got.Result, j.Result)
	}
	if string(got.Err) != string(j.Err) {
		t.Errorf("Err = %q, want %q", got.Err, j.Err)
	}
	if got.StartedAt == nil || jobs.FormatTime(*got.StartedAt) != jobs.FormatTime(started) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, started)
	}
	if got.FinishedAt == nil || jobs.FormatTime(*got.FinishedAt) != jobs.FormatTime(finished) {
		t.Errorf("FinishedAt = %v, want %v", got.FinishedAt, finished)
	}
}

// TestList_NoFilter covers acceptance criterion 4 (no filter case) and the
// created_at DESC, id ASC ordering, including a genuine created_at tie.
func TestList_NoFilter(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	tie := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	later := tie.Add(time.Hour)

	// Two jobs sharing the exact same created_at timestamp (a genuine tie),
	// created out of id order so the test would fail if the tie-break
	// silently relied on insertion order instead of the id ASC clause.
	jTieB := mustNewJob(t, jobs.TypeHash, `{"data":"b"}`, tie)
	jTieA := mustNewJob(t, jobs.TypeHash, `{"data":"a"}`, tie)
	jLater := mustNewJob(t, jobs.TypeDelay, `{"ms":1}`, later)

	// Force a deterministic id order for the tied pair regardless of the
	// random ids NewJob generated, so the expected order is unambiguous.
	if jTieA.ID > jTieB.ID {
		jTieA.ID, jTieB.ID = jTieB.ID, jTieA.ID
	}

	for _, j := range []*jobs.Job{jTieB, jTieA, jLater} {
		if err := s.Create(ctx, j); err != nil {
			t.Fatalf("Create(%s): %v", j.ID, err)
		}
	}

	got, err := s.List(ctx, store.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d jobs, want 3", len(got))
	}

	wantOrder := []string{jLater.ID, jTieA.ID, jTieB.ID}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Errorf("position %d: got id %q, want %q", i, got[i].ID, id)
		}
	}
}

// TestList_StatusAndTypeFilters covers acceptance criterion 4: status only,
// type only, and both filters combined.
func TestList_StatusAndTypeFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

	hashQueued := mustNewJob(t, jobs.TypeHash, `{"data":"1"}`, base)

	delayQueued := mustNewJob(t, jobs.TypeDelay, `{"ms":1}`, base.Add(time.Second))

	hashCompleted := mustNewJob(t, jobs.TypeHash, `{"data":"2"}`, base.Add(2*time.Second))
	hashCompleted.Status = jobs.StatusCompleted
	hashCompleted.Result = json.RawMessage(`{"hash":"deadbeef"}`)

	delayCompleted := mustNewJob(t, jobs.TypeDelay, `{"ms":2}`, base.Add(3*time.Second))
	delayCompleted.Status = jobs.StatusCompleted
	delayCompleted.Result = json.RawMessage(`{}`)

	all := []*jobs.Job{hashQueued, delayQueued, hashCompleted, delayCompleted}
	for _, j := range all {
		if err := s.Create(ctx, j); err != nil {
			t.Fatalf("Create(%s): %v", j.ID, err)
		}
	}

	t.Run("status only", func(t *testing.T) {
		status := jobs.StatusCompleted
		got, err := s.List(ctx, store.ListFilter{Status: &status})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		assertIDSet(t, got, hashCompleted.ID, delayCompleted.ID)
	})

	t.Run("type only", func(t *testing.T) {
		typ := jobs.TypeHash
		got, err := s.List(ctx, store.ListFilter{Type: &typ})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		assertIDSet(t, got, hashQueued.ID, hashCompleted.ID)
	})

	t.Run("status and type combined", func(t *testing.T) {
		status := jobs.StatusCompleted
		typ := jobs.TypeHash
		got, err := s.List(ctx, store.ListFilter{Status: &status, Type: &typ})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		assertIDSet(t, got, hashCompleted.ID)
	})

	t.Run("no matches", func(t *testing.T) {
		status := jobs.StatusFailed
		got, err := s.List(ctx, store.ListFilter{Status: &status})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("List with no matches: got %d jobs, want 0", len(got))
		}
	})
}

func assertIDSet(t *testing.T, got []*jobs.Job, wantIDs ...string) {
	t.Helper()
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d jobs, want %d", len(got), len(wantIDs))
	}
	want := make(map[string]bool, len(wantIDs))
	for _, id := range wantIDs {
		want[id] = true
	}
	for _, j := range got {
		if !want[j.ID] {
			t.Errorf("unexpected job id %q in results", j.ID)
		}
		delete(want, j.ID)
	}
	for id := range want {
		t.Errorf("expected job id %q missing from results", id)
	}
}

// TestPing covers acceptance criterion 5: succeeds against an open
// database, fails against a closed one.
func TestPing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping on open store: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = s.Ping(ctx)
	if err == nil {
		t.Fatal("Ping on closed store: got nil error, want ErrUnavailable")
	}
	if !errors.Is(err, store.ErrUnavailable) {
		t.Fatalf("Ping on closed store: got %v, want ErrUnavailable", err)
	}
}
