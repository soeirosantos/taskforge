package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// cancelPath is the SPEC 24 endpoint. Cancel takes no request body and
// requires no Content-Type — SPEC 10's JSON input rules scope that requirement
// to POST /jobs — so every request below is sent with a nil body and no
// Content-Type header, which is itself part of what these tests assert.
func cancelPath(id string) string { return "/jobs/" + id + "/cancel" }

// claimJob drives id QUEUED -> RUNNING through the store, exactly as a worker
// would (SPEC 21), and fails the test if a different job was claimed — with a
// single queued job that cannot happen, and asserting it keeps the fixture
// honest if one is ever added.
func claimJob(t *testing.T, s *store.Store, id string) {
	t.Helper()
	claimed, err := s.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ID != id {
		t.Fatalf("claimed %s, want %s (SPEC 20 ordering assumption broken)", claimed.ID, id)
	}
}

// --- Cancel ---------------------------------------------------------------

func TestCancelJob_Queued(t *testing.T) {
	h, _ := newTestHandler(t)
	created := createJobDTO(t, h, "hash", `{"text":"a"}`)

	rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)

	got := decodeJob(t, rec.Body.Bytes())
	if got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
	if got.Status != "CANCELLED" {
		t.Errorf("status = %q, want CANCELLED", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("finished_at is null, want a timestamp (SPEC 13)")
	}
	if got.StartedAt != nil {
		t.Errorf("started_at = %q, want null for a job cancelled while QUEUED", *got.StartedAt)
	}

	if persisted := getJobDTO(t, h, created.ID); persisted.Status != "CANCELLED" {
		t.Errorf("persisted status = %q, want CANCELLED", persisted.Status)
	}
}

// TestCancelJob_Running also pins the SPEC 25/26 ordering contract: the
// handler signals the cancellation registry only after the store has confirmed
// it won QUEUED|RUNNING -> CANCELLED. The registered execution's context must
// be cancelled by the time the 200 is written.
func TestCancelJob_Running(t *testing.T) {
	h, s, reg := newTestHandlerWithRegistry(t)
	created := createJobDTO(t, h, "delay", `{"milliseconds":100}`)
	claimJob(t, s, created.ID)

	execCtx, release := reg.Register(context.Background(), created.ID)
	defer release()
	if execCtx.Err() != nil {
		t.Fatalf("test setup: execution context already cancelled: %v", execCtx.Err())
	}

	rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeJob(t, rec.Body.Bytes())
	if got.Status != "CANCELLED" {
		t.Errorf("status = %q, want CANCELLED", got.Status)
	}
	if got.StartedAt == nil {
		t.Error("started_at is null, want the claim's timestamp preserved")
	}
	if got.FinishedAt == nil {
		t.Error("finished_at is null, want a timestamp (SPEC 13)")
	}

	if execCtx.Err() == nil {
		t.Error("running execution was not signalled after the store transition won (SPEC 26)")
	}
}

// TestCancelJob_QueuedDoesNotRequireARegisteredExecution covers the other half
// of the registry contract: a QUEUED job has no running execution, so
// registry.Cancel returns false, and that must be a normal 200 rather than an
// error.
func TestCancelJob_QueuedDoesNotRequireARegisteredExecution(t *testing.T) {
	h, _, reg := newTestHandlerWithRegistry(t)
	created := createJobDTO(t, h, "hash", `{"text":"a"}`)

	if reg.Cancel(created.ID) {
		t.Fatal("test setup: registry unexpectedly has an execution for a QUEUED job")
	}

	rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCancelJob_AlreadyCancelled(t *testing.T) {
	h, _ := newTestHandler(t)
	created := createJobDTO(t, h, "hash", `{"text":"a"}`)

	first := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first cancel: status = %d, want 200; body=%s", first.Code, first.Body.String())
	}
	firstJob := decodeJob(t, first.Body.Bytes())

	second := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second cancel: status = %d, want 200 (idempotent, SPEC 24); body=%s",
			second.Code, second.Body.String())
	}
	requireJSONContentType(t, second)

	secondJob := decodeJob(t, second.Body.Bytes())
	if secondJob.Status != "CANCELLED" {
		t.Errorf("status = %q, want CANCELLED", secondJob.Status)
	}
	// Idempotent means the existing job is returned unchanged, not
	// re-cancelled: the terminal timestamps must not have moved.
	if secondJob.UpdatedAt != firstJob.UpdatedAt {
		t.Errorf("updated_at = %q, want it unchanged at %q (the second cancel must not rewrite the row)",
			secondJob.UpdatedAt, firstJob.UpdatedAt)
	}
}

func TestCancelJob_Completed(t *testing.T) {
	h, s := newTestHandler(t)
	created := createJobDTO(t, h, "hash", `{"text":"a"}`)
	claimJob(t, s, created.ID)

	if _, outcome, err := s.Complete(context.Background(), created.ID, json.RawMessage(`{"sha256":"x"}`)); err != nil || outcome != store.OutcomeWon {
		t.Fatalf("Complete: outcome = %v, err = %v", outcome, err)
	}

	rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_STATE_TRANSITION" {
		t.Errorf("code = %q, want INVALID_STATE_TRANSITION", e.Error.Code)
	}

	if persisted := getJobDTO(t, h, created.ID); persisted.Status != "COMPLETED" {
		t.Errorf("persisted status = %q, want COMPLETED to be left untouched (SPEC 25)", persisted.Status)
	}
}

// TestCancelJob_CompletedDoesNotSignalRegistry pins the negative half of the
// SPEC 26 ordering contract: on a lost race the handler must not signal the
// registry at all. The execution registered here models the window in which a
// worker has just won RUNNING -> COMPLETED but has not yet released its entry.
func TestCancelJob_CompletedDoesNotSignalRegistry(t *testing.T) {
	h, s, reg := newTestHandlerWithRegistry(t)
	created := createJobDTO(t, h, "hash", `{"text":"a"}`)
	claimJob(t, s, created.ID)

	execCtx, release := reg.Register(context.Background(), created.ID)
	defer release()

	if _, outcome, err := s.Complete(context.Background(), created.ID, json.RawMessage(`{"sha256":"x"}`)); err != nil || outcome != store.OutcomeWon {
		t.Fatalf("Complete: outcome = %v, err = %v", outcome, err)
	}

	rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if execCtx.Err() != nil {
		t.Error("registry was signalled on a lost cancellation race (SPEC 26 ordering violated)")
	}
}

func TestCancelJob_Failed(t *testing.T) {
	h, s := newTestHandler(t)
	created := createJobDTO(t, h, "fail", `{}`)
	claimJob(t, s, created.ID)

	if _, outcome, err := s.Fail(context.Background(), created.ID, jobs.IntentionalFailure()); err != nil || outcome != store.OutcomeWon {
		t.Fatalf("Fail: outcome = %v, err = %v", outcome, err)
	}

	rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_STATE_TRANSITION" {
		t.Errorf("code = %q, want INVALID_STATE_TRANSITION", e.Error.Code)
	}

	if persisted := getJobDTO(t, h, created.ID); persisted.Status != "FAILED" {
		t.Errorf("persisted status = %q, want FAILED to be left untouched", persisted.Status)
	}
}

func TestCancelJob_UnknownID(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, cancelPath("00000000-0000-4000-8000-000000000000"), "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "JOB_NOT_FOUND" {
		t.Errorf("code = %q, want JOB_NOT_FOUND", e.Error.Code)
	}
}

// TestCancelJob_ConcurrentCancels is the SPEC 40 "Concurrent Cancellation"
// case: real goroutines released together against one job, not sequential
// calls. Exactly one of them wins the atomic transition; every other one
// observes an already-CANCELLED job and must answer 200 idempotently
// (SPEC 24), never 409 — a 409 here would mean a cancel was reported as an
// invalid transition against a job that is in fact CANCELLED.
func TestCancelJob_ConcurrentCancels(t *testing.T) {
	const callers = 8

	h, _ := newTestHandler(t)
	created := createJobDTO(t, h, "delay", `{"milliseconds":100}`)

	var (
		wg     sync.WaitGroup
		start  = make(chan struct{})
		mu     sync.Mutex
		codes  = map[int]int{}
		bodies []string
	)

	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)

			mu.Lock()
			defer mu.Unlock()
			codes[rec.Code]++
			if rec.Code != http.StatusOK {
				bodies = append(bodies, rec.Body.String())
			}
		}()
	}
	close(start)
	wg.Wait()

	if codes[http.StatusOK] != callers {
		t.Errorf("200 count = %d, want %d; status codes = %v; non-200 bodies = %v",
			codes[http.StatusOK], callers, codes, bodies)
	}
	if codes[http.StatusConflict] != 0 {
		t.Errorf("409 count = %d, want 0: no concurrent cancel may be reported as an invalid transition; bodies = %v",
			codes[http.StatusConflict], bodies)
	}

	final := getJobDTO(t, h, created.ID)
	if final.Status != "CANCELLED" {
		t.Errorf("final status = %q, want CANCELLED", final.Status)
	}
	if !isJSONNull(final.Result) || !isJSONNull(final.Err) {
		t.Errorf("cancelled job has result=%s error=%s, want both null", final.Result, final.Err)
	}
}
