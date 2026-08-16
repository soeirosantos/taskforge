package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// --- Claim ---------------------------------------------------------------

// TestClaim_ExactlyOneWinnerAndSingleIncrement covers acceptance criterion 1:
// N goroutines racing to claim the same single queued job must produce
// exactly one winner, and attempt_count must end at exactly 1 (never
// double-incremented by two winners or by a retried claim).
func TestClaim_ExactlyOneWinnerAndSingleIncrement(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := mustNewJob(t, jobs.TypeHash, `{"data":"race"}`, time.Now())
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const n = 8
	var wins int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			got, err := s.Claim(ctx)
			switch {
			case err == nil:
				atomic.AddInt32(&wins, 1)
				if got.ID != j.ID {
					t.Errorf("claimed unexpected job id %q", got.ID)
				}
			case errors.Is(err, store.ErrNoJobAvailable):
				// expected for losers
			default:
				t.Errorf("Claim: unexpected error %v", err)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1", wins)
	}

	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobs.StatusRunning {
		t.Errorf("Status = %q, want RUNNING", got.Status)
	}
	if got.AttemptCount != 1 {
		t.Errorf("AttemptCount = %d, want 1", got.AttemptCount)
	}
	if got.StartedAt == nil {
		t.Error("StartedAt = nil, want set")
	}
}

// TestClaim_NoJobAvailable covers the empty-queue case.
func TestClaim_NoJobAvailable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.Claim(ctx)
	if !errors.Is(err, store.ErrNoJobAvailable) {
		t.Fatalf("Claim on empty store: got %v, want ErrNoJobAvailable", err)
	}
}

// TestClaim_OrdersByQueuedAtThenIDTiebreak covers acceptance criterion 5:
// claim order follows queued_at ASC, id ASC, including a constructed tie on
// queued_at so the id tie-break is genuinely exercised.
func TestClaim_OrdersByQueuedAtThenIDTiebreak(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	tie := base.Add(time.Minute) // two jobs share this queued_at
	later := base.Add(time.Hour) // strictly after the tie

	jEarliest := mustNewJob(t, jobs.TypeHash, `{"data":"earliest"}`, base)

	jTieB := mustNewJob(t, jobs.TypeHash, `{"data":"tie-b"}`, tie)
	jTieA := mustNewJob(t, jobs.TypeHash, `{"data":"tie-a"}`, tie)
	// Force a deterministic id order for the tied pair so the expected claim
	// order is unambiguous regardless of the random ids NewJob generated.
	if jTieA.ID > jTieB.ID {
		jTieA.ID, jTieB.ID = jTieB.ID, jTieA.ID
	}

	jLatest := mustNewJob(t, jobs.TypeHash, `{"data":"latest"}`, later)

	// Create out of order so claim order can't be riding insertion order.
	for _, j := range []*jobs.Job{jLatest, jTieB, jEarliest, jTieA} {
		if err := s.Create(ctx, j); err != nil {
			t.Fatalf("Create(%s): %v", j.ID, err)
		}
	}

	wantOrder := []string{jEarliest.ID, jTieA.ID, jTieB.ID, jLatest.ID}
	for i, wantID := range wantOrder {
		got, err := s.Claim(ctx)
		if err != nil {
			t.Fatalf("Claim() #%d: %v", i, err)
		}
		if got.ID != wantID {
			t.Fatalf("Claim() #%d = %q, want %q", i, got.ID, wantID)
		}
	}

	if _, err := s.Claim(ctx); !errors.Is(err, store.ErrNoJobAvailable) {
		t.Fatalf("Claim() after queue drained: got %v, want ErrNoJobAvailable", err)
	}
}

// TestClaim_NeverClaimsAtAttemptLimit covers acceptance criterion 6: a
// QUEUED job already at attempt_count = 3 is never claimed, even when it is
// the only job in the store.
func TestClaim_NeverClaimsAtAttemptLimit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := mustNewJob(t, jobs.TypeHash, `{"data":"exhausted"}`, time.Now())
	j.AttemptCount = jobs.MaxAttempts
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Claim(ctx); !errors.Is(err, store.ErrNoJobAvailable) {
		t.Fatalf("Claim on attempt-exhausted job: got %v, want ErrNoJobAvailable", err)
	}

	got, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != jobs.StatusQueued || got.AttemptCount != jobs.MaxAttempts {
		t.Fatalf("job mutated by failed claim: status=%q attempt_count=%d", got.Status, got.AttemptCount)
	}
}

// --- Complete / Fail -------------------------------------------------------

// runningJob creates a job already in the RUNNING state, built directly
// rather than via Claim so it is unaffected by any other QUEUED job that
// might exist in the store (Claim always picks the globally oldest queued
// job, which would not necessarily be the one just created here).
func runningJob(t *testing.T, s *store.Store, ctx context.Context, typ jobs.Type, payload string) *jobs.Job {
	t.Helper()
	j := mustNewJob(t, typ, payload, time.Now())
	j.Status = jobs.StatusRunning
	j.AttemptCount = 1
	started := j.CreatedAt.Add(time.Millisecond)
	j.StartedAt = &started
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return j
}

func TestComplete_WinsFromRunning(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := runningJob(t, s, ctx, jobs.TypeHash, `{"data":"x"}`)
	result := json.RawMessage(`{"sha256":"deadbeef"}`)

	got, outcome, err := s.Complete(ctx, j.ID, result)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("outcome = %v, want OutcomeWon", outcome)
	}
	if got.Status != jobs.StatusCompleted {
		t.Errorf("Status = %q, want COMPLETED", got.Status)
	}
	if string(got.Result) != string(result) {
		t.Errorf("Result = %s, want %s", got.Result, result)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt = nil, want set")
	}
}

// TestComplete_DoesNotOverwriteACancelledJob covers acceptance criterion 4:
// a complete attempt against a job already moved out of RUNNING must not
// overwrite it.
func TestComplete_DoesNotOverwriteACancelledJob(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := runningJob(t, s, ctx, jobs.TypeDelay, `{"ms":1}`)

	cancelled, outcome, err := s.Cancel(ctx, j.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != store.OutcomeWon || cancelled.Status != jobs.StatusCancelled {
		t.Fatalf("Cancel did not win: outcome=%v status=%q", outcome, cancelled.Status)
	}

	got, outcome, err := s.Complete(ctx, j.ID, json.RawMessage(`{"ok":true}`))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if outcome != store.OutcomeLost {
		t.Fatalf("outcome = %v, want OutcomeLost", outcome)
	}
	if got.Status != jobs.StatusCancelled {
		t.Fatalf("Complete outcome job Status = %q, want CANCELLED (unmodified)", got.Status)
	}

	final, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != jobs.StatusCancelled {
		t.Fatalf("final Status = %q, want CANCELLED", final.Status)
	}
	if final.Result != nil {
		t.Fatalf("final Result = %s, want nil (must not be overwritten by the losing Complete)", final.Result)
	}
}

func TestComplete_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	got, outcome, err := s.Complete(ctx, "does-not-exist", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if outcome != store.OutcomeNotFound {
		t.Fatalf("outcome = %v, want OutcomeNotFound", outcome)
	}
	if got != nil {
		t.Fatalf("job = %v, want nil", got)
	}
}

func TestFail_WinsFromRunningAndPersistsError(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := runningJob(t, s, ctx, jobs.TypeFail, `{}`)

	got, outcome, err := s.Fail(ctx, j.ID, jobs.IntentionalFailure())
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("outcome = %v, want OutcomeWon", outcome)
	}
	if got.Status != jobs.StatusFailed {
		t.Errorf("Status = %q, want FAILED", got.Status)
	}
	want := `{"code":"INTENTIONAL_FAILURE","message":"job failed intentionally"}`
	if string(got.Err) != want {
		t.Errorf("Err = %s, want %s", got.Err, want)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt = nil, want set")
	}
}

func TestFail_LosesWhenNotRunning(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := mustNewJob(t, jobs.TypeHash, `{"data":"x"}`, time.Now())
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// j is still QUEUED, never claimed.
	got, outcome, err := s.Fail(ctx, j.ID, jobs.IntentionalFailure())
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if outcome != store.OutcomeLost {
		t.Fatalf("outcome = %v, want OutcomeLost", outcome)
	}
	if got.Status != jobs.StatusQueued {
		t.Fatalf("Status = %q, want QUEUED (unmodified)", got.Status)
	}
}

// --- Cancel ----------------------------------------------------------------

// TestCancel_ConcurrentAllObserveFinalCancelled covers acceptance criterion
// 3: N concurrent cancels of one job all observe a final CANCELLED state,
// exactly one performs the transition, and no invalid transition occurs.
func TestCancel_ConcurrentAllObserveFinalCancelled(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := mustNewJob(t, jobs.TypeDelay, `{"ms":1}`, time.Now())
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const n = 8
	var wins int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			got, outcome, err := s.Cancel(ctx, j.ID)
			if err != nil {
				t.Errorf("Cancel: %v", err)
				return
			}
			if got == nil || got.Status != jobs.StatusCancelled {
				t.Errorf("Cancel outcome job status = %v, want CANCELLED", got)
			}
			if outcome == store.OutcomeWon {
				atomic.AddInt32(&wins, 1)
			} else if outcome != store.OutcomeLost {
				t.Errorf("outcome = %v, want OutcomeWon or OutcomeLost", outcome)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1", wins)
	}

	final, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != jobs.StatusCancelled {
		t.Fatalf("final Status = %q, want CANCELLED", final.Status)
	}
}

func TestCancel_ClearsResultAndError(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := runningJob(t, s, ctx, jobs.TypeHash, `{"data":"x"}`)

	got, outcome, err := s.Cancel(ctx, j.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("outcome = %v, want OutcomeWon", outcome)
	}
	if got.Result != nil || got.Err != nil {
		t.Fatalf("Result/Err not cleared: result=%s err=%s", got.Result, got.Err)
	}
}

func TestCancel_LosesAgainstTerminalStates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := runningJob(t, s, ctx, jobs.TypeHash, `{"data":"x"}`)
	if _, outcome, err := s.Complete(ctx, j.ID, json.RawMessage(`{"ok":true}`)); err != nil || outcome != store.OutcomeWon {
		t.Fatalf("Complete setup: outcome=%v err=%v", outcome, err)
	}

	got, outcome, err := s.Cancel(ctx, j.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != store.OutcomeLost {
		t.Fatalf("outcome = %v, want OutcomeLost", outcome)
	}
	if got.Status != jobs.StatusCompleted {
		t.Fatalf("Status = %q, want COMPLETED (unmodified)", got.Status)
	}
}

func TestCancel_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	got, outcome, err := s.Cancel(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != store.OutcomeNotFound {
		t.Fatalf("outcome = %v, want OutcomeNotFound", outcome)
	}
	if got != nil {
		t.Fatalf("job = %v, want nil", got)
	}
}

// --- Retry -------------------------------------------------------------

func failedJob(t *testing.T, s *store.Store, ctx context.Context, attemptCount int) *jobs.Job {
	t.Helper()
	j := mustNewJob(t, jobs.TypeFail, `{}`, time.Now())
	j.Status = jobs.StatusFailed
	j.AttemptCount = attemptCount
	started := j.CreatedAt.Add(time.Second)
	finished := j.CreatedAt.Add(2 * time.Second)
	j.StartedAt = &started
	j.FinishedAt = &finished
	j.Err = jobs.IntentionalFailure().JSON()
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return j
}

// TestRetry_ConcurrentExactlyOneWinner covers acceptance criterion 2: N
// concurrent retries of one failed job succeed exactly once, the rest
// report a lost race, and the job is queued exactly once (attempt_count
// unchanged).
func TestRetry_ConcurrentExactlyOneWinner(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := failedJob(t, s, ctx, 1)

	const n = 8
	var wins int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, outcome, err := s.Retry(ctx, j.ID)
			if err != nil {
				t.Errorf("Retry: %v", err)
				return
			}
			switch outcome {
			case store.OutcomeWon:
				atomic.AddInt32(&wins, 1)
			case store.OutcomeLost:
				// expected for losers
			default:
				t.Errorf("outcome = %v, want OutcomeWon or OutcomeLost", outcome)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1", wins)
	}

	final, err := s.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != jobs.StatusQueued {
		t.Fatalf("final Status = %q, want QUEUED", final.Status)
	}
	if final.AttemptCount != 1 {
		t.Fatalf("final AttemptCount = %d, want 1 (unchanged by retry)", final.AttemptCount)
	}
	if final.StartedAt != nil || final.FinishedAt != nil || final.Result != nil || final.Err != nil {
		t.Fatalf("retry did not clear attempt-specific fields: started=%v finished=%v result=%s err=%s",
			final.StartedAt, final.FinishedAt, final.Result, final.Err)
	}
}

func TestRetry_ClearsFieldsAndRefreshesQueuedAt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := failedJob(t, s, ctx, 2)

	got, outcome, err := s.Retry(ctx, j.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("outcome = %v, want OutcomeWon", outcome)
	}
	if got.Status != jobs.StatusQueued {
		t.Fatalf("Status = %q, want QUEUED", got.Status)
	}
	if got.AttemptCount != 2 {
		t.Fatalf("AttemptCount = %d, want 2 (unchanged)", got.AttemptCount)
	}
	if !got.QueuedAt.After(j.QueuedAt) {
		t.Fatalf("QueuedAt = %v, want refreshed to after original %v", got.QueuedAt, j.QueuedAt)
	}
	if got.StartedAt != nil || got.FinishedAt != nil || got.Result != nil || got.Err != nil {
		t.Fatalf("retry did not clear fields: started=%v finished=%v result=%s err=%s",
			got.StartedAt, got.FinishedAt, got.Result, got.Err)
	}
}

// TestRetry_AttemptLimitReached covers acceptance criterion 6: a job at
// attempt_count = 3 cannot be retried, and this is reported as a distinct
// outcome from a plain wrong-state loss.
func TestRetry_AttemptLimitReached(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := failedJob(t, s, ctx, jobs.MaxAttempts)

	got, outcome, err := s.Retry(ctx, j.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if outcome != store.OutcomeAttemptLimitReached {
		t.Fatalf("outcome = %v, want OutcomeAttemptLimitReached", outcome)
	}
	if got.Status != jobs.StatusFailed || got.AttemptCount != jobs.MaxAttempts {
		t.Fatalf("job mutated by failed retry: status=%q attempt_count=%d", got.Status, got.AttemptCount)
	}
}

func TestRetry_WrongStateIsLostNotAttemptLimit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	j := mustNewJob(t, jobs.TypeHash, `{"data":"x"}`, time.Now())
	if err := s.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// j is QUEUED, not FAILED: retry is simply the wrong state, regardless
	// of attempt_count.
	got, outcome, err := s.Retry(ctx, j.ID)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if outcome != store.OutcomeLost {
		t.Fatalf("outcome = %v, want OutcomeLost", outcome)
	}
	if got.Status != jobs.StatusQueued {
		t.Fatalf("Status = %q, want QUEUED (unmodified)", got.Status)
	}
}

func TestRetry_NotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	got, outcome, err := s.Retry(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if outcome != store.OutcomeNotFound {
		t.Fatalf("outcome = %v, want OutcomeNotFound", outcome)
	}
	if got != nil {
		t.Fatalf("job = %v, want nil", got)
	}
}

// --- Recover -----------------------------------------------------------

// TestRecover covers acceptance criterion 7: recovery converts RUNNING to
// FAILED with INTERRUPTED_EXECUTION, result null, finished_at populated,
// attempt_count unchanged — and leaves non-RUNNING jobs untouched.
func TestRecover(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	running1 := runningJob(t, s, ctx, jobs.TypeDelay, `{"ms":1000}`)
	running2 := runningJob(t, s, ctx, jobs.TypeHash, `{"data":"y"}`)

	queued := mustNewJob(t, jobs.TypeHash, `{"data":"still-queued"}`, time.Now())
	if err := s.Create(ctx, queued); err != nil {
		t.Fatalf("Create queued: %v", err)
	}

	completed := runningJob(t, s, ctx, jobs.TypeHash, `{"data":"done"}`)
	if _, outcome, err := s.Complete(ctx, completed.ID, json.RawMessage(`{"ok":true}`)); err != nil || outcome != store.OutcomeWon {
		t.Fatalf("Complete setup: outcome=%v err=%v", outcome, err)
	}

	n, err := s.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("Recover returned %d, want 2", n)
	}

	wantErr := `{"code":"INTERRUPTED_EXECUTION","message":"job execution was interrupted by server termination"}`
	for _, orig := range []*jobs.Job{running1, running2} {
		got, err := s.Get(ctx, orig.ID)
		if err != nil {
			t.Fatalf("Get(%s): %v", orig.ID, err)
		}
		if got.Status != jobs.StatusFailed {
			t.Errorf("job %s Status = %q, want FAILED", orig.ID, got.Status)
		}
		if string(got.Err) != wantErr {
			t.Errorf("job %s Err = %s, want %s", orig.ID, got.Err, wantErr)
		}
		if got.Result != nil {
			t.Errorf("job %s Result = %s, want nil", orig.ID, got.Result)
		}
		if got.FinishedAt == nil {
			t.Errorf("job %s FinishedAt = nil, want set", orig.ID)
		}
		if got.AttemptCount != orig.AttemptCount {
			t.Errorf("job %s AttemptCount = %d, want %d (unchanged)", orig.ID, got.AttemptCount, orig.AttemptCount)
		}
		if got.StartedAt == nil || !got.StartedAt.Equal(*orig.StartedAt) {
			t.Errorf("job %s StartedAt = %v, want unchanged %v", orig.ID, got.StartedAt, orig.StartedAt)
		}
		if !got.QueuedAt.Equal(orig.QueuedAt) {
			t.Errorf("job %s QueuedAt = %v, want unchanged %v", orig.ID, got.QueuedAt, orig.QueuedAt)
		}
		if !got.CreatedAt.Equal(orig.CreatedAt) {
			t.Errorf("job %s CreatedAt = %v, want unchanged %v", orig.ID, got.CreatedAt, orig.CreatedAt)
		}
	}

	// Non-RUNNING jobs are untouched.
	stillQueued, err := s.Get(ctx, queued.ID)
	if err != nil {
		t.Fatalf("Get(queued): %v", err)
	}
	if stillQueued.Status != jobs.StatusQueued {
		t.Errorf("queued job Status = %q, want QUEUED (untouched by Recover)", stillQueued.Status)
	}

	stillCompleted, err := s.Get(ctx, completed.ID)
	if err != nil {
		t.Fatalf("Get(completed): %v", err)
	}
	if stillCompleted.Status != jobs.StatusCompleted {
		t.Errorf("completed job Status = %q, want COMPLETED (untouched by Recover)", stillCompleted.Status)
	}
}

func TestRecover_NoRunningJobsIsANoOp(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n, err := s.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if n != 0 {
		t.Fatalf("Recover on empty store = %d, want 0", n)
	}
}
