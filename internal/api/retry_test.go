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

// retryPath is the SPEC 27 endpoint. Like cancel, it takes no body and needs
// no Content-Type.
func retryPath(id string) string { return "/jobs/" + id + "/retry" }

// driveToFailed creates a "fail" job and drives it to FAILED with exactly
// attempts recorded attempts, using only the transitions a worker would
// perform: Claim (which is what increments attempt_count, SPEC 14) then Fail,
// with a store-level Retry in between cycles. It deliberately does not
// hand-write a row with a chosen attempt_count, so the attempt-limit fixture
// is reachable by the real state machine rather than asserted into existence.
func driveToFailed(t *testing.T, h http.Handler, s *store.Store, attempts int) jobDTO {
	t.Helper()
	if attempts < 1 || attempts > jobs.MaxAttempts {
		t.Fatalf("driveToFailed: attempts = %d, must be 1..%d", attempts, jobs.MaxAttempts)
	}
	ctx := context.Background()

	created := createJobDTO(t, h, "fail", `{}`)
	for i := 1; i <= attempts; i++ {
		claimJob(t, s, created.ID)
		if _, outcome, err := s.Fail(ctx, created.ID, jobs.IntentionalFailure()); err != nil || outcome != store.OutcomeWon {
			t.Fatalf("Fail (attempt %d): outcome = %v, err = %v", i, outcome, err)
		}
		if i < attempts {
			if _, outcome, err := s.Retry(ctx, created.ID); err != nil || outcome != store.OutcomeWon {
				t.Fatalf("Retry (after attempt %d): outcome = %v, err = %v", i, outcome, err)
			}
		}
	}

	failed := getJobDTO(t, h, created.ID)
	if failed.Status != "FAILED" || failed.AttemptCount != attempts {
		t.Fatalf("fixture = {status:%s attempt_count:%d}, want {FAILED %d}",
			failed.Status, failed.AttemptCount, attempts)
	}
	return failed
}

// --- Retry ----------------------------------------------------------------

func TestRetryJob_FailedBelowAttemptLimit(t *testing.T) {
	h, s := newTestHandler(t)
	failed := driveToFailed(t, h, s, 1)

	rec := doRequest(h, http.MethodPost, retryPath(failed.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)

	got := decodeJob(t, rec.Body.Bytes())
	if got.Status != "QUEUED" {
		t.Errorf("status = %q, want QUEUED", got.Status)
	}
	// SPEC 27: attempt_count is not incremented by the retry itself, and the
	// previous cycle's outcome and timestamps are cleared.
	if got.AttemptCount != failed.AttemptCount {
		t.Errorf("attempt_count = %d, want it unchanged at %d", got.AttemptCount, failed.AttemptCount)
	}
	if got.StartedAt != nil {
		t.Errorf("started_at = %q, want null", *got.StartedAt)
	}
	if got.FinishedAt != nil {
		t.Errorf("finished_at = %q, want null", *got.FinishedAt)
	}
	if !isJSONNull(got.Result) || !isJSONNull(got.Err) {
		t.Errorf("result = %s, error = %s, want both null", got.Result, got.Err)
	}
	if got.QueuedAt <= failed.QueuedAt {
		t.Errorf("queued_at = %q, want it refreshed past %q", got.QueuedAt, failed.QueuedAt)
	}
	if got.CreatedAt != failed.CreatedAt {
		t.Errorf("created_at = %q, want it never to change (%q)", got.CreatedAt, failed.CreatedAt)
	}

	if persisted := getJobDTO(t, h, failed.ID); persisted.Status != "QUEUED" {
		t.Errorf("persisted status = %q, want QUEUED", persisted.Status)
	}
}

func TestRetryJob_AtAttemptLimit(t *testing.T) {
	h, s := newTestHandler(t)
	failed := driveToFailed(t, h, s, jobs.MaxAttempts)

	rec := doRequest(h, http.MethodPost, retryPath(failed.ID), "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "ATTEMPT_LIMIT_REACHED" {
		t.Errorf("code = %q, want ATTEMPT_LIMIT_REACHED", e.Error.Code)
	}

	persisted := getJobDTO(t, h, failed.ID)
	if persisted.Status != "FAILED" || persisted.AttemptCount != jobs.MaxAttempts {
		t.Errorf("persisted = {status:%s attempt_count:%d}, want {FAILED %d} untouched",
			persisted.Status, persisted.AttemptCount, jobs.MaxAttempts)
	}
}

// TestRetryJob_NonFailedStates covers SPEC 27's "retrying any other state
// returns 409" for every non-FAILED state a job can actually be in.
func TestRetryJob_NonFailedStates(t *testing.T) {
	setups := map[string]func(t *testing.T, h http.Handler, s *store.Store) string{
		"QUEUED": func(t *testing.T, h http.Handler, s *store.Store) string {
			return createJobDTO(t, h, "hash", `{"text":"a"}`).ID
		},
		"RUNNING": func(t *testing.T, h http.Handler, s *store.Store) string {
			id := createJobDTO(t, h, "delay", `{"milliseconds":100}`).ID
			claimJob(t, s, id)
			return id
		},
		"COMPLETED": func(t *testing.T, h http.Handler, s *store.Store) string {
			id := createJobDTO(t, h, "hash", `{"text":"a"}`).ID
			claimJob(t, s, id)
			if _, outcome, err := s.Complete(context.Background(), id, json.RawMessage(`{"sha256":"x"}`)); err != nil || outcome != store.OutcomeWon {
				t.Fatalf("Complete: outcome = %v, err = %v", outcome, err)
			}
			return id
		},
		"CANCELLED": func(t *testing.T, h http.Handler, s *store.Store) string {
			id := createJobDTO(t, h, "hash", `{"text":"a"}`).ID
			rec := doRequest(h, http.MethodPost, cancelPath(id), "", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("cancel: status = %d, body=%s", rec.Code, rec.Body.String())
			}
			return id
		},
	}

	for state, setup := range setups {
		t.Run(state, func(t *testing.T) {
			h, s := newTestHandler(t)
			id := setup(t, h, s)

			rec := doRequest(h, http.MethodPost, retryPath(id), "", nil)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
			}
			if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_STATE_TRANSITION" {
				t.Errorf("code = %q, want INVALID_STATE_TRANSITION", e.Error.Code)
			}
			if persisted := getJobDTO(t, h, id); persisted.Status != state {
				t.Errorf("persisted status = %q, want %q to be left untouched", persisted.Status, state)
			}
		})
	}
}

func TestRetryJob_UnknownID(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, retryPath("00000000-0000-4000-8000-000000000000"), "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "JOB_NOT_FOUND" {
		t.Errorf("code = %q, want JOB_NOT_FOUND", e.Error.Code)
	}
}

// TestRetryJob_ConcurrentRetries is the SPEC 28 / SPEC 40 "Concurrent Retry"
// case: real goroutines released together against one failed job. Retry is not
// idempotent, so exactly one caller may perform FAILED -> QUEUED and answer
// 200; every other caller must get 409, and the job must end up queued exactly
// once — no duplicate queue entry and no extra attempt.
func TestRetryJob_ConcurrentRetries(t *testing.T) {
	const callers = 8

	h, s := newTestHandler(t)
	failed := driveToFailed(t, h, s, 1)

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		mu    sync.Mutex
		codes = map[int]int{}
		errs  = map[string]int{}
	)

	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			rec := doRequest(h, http.MethodPost, retryPath(failed.ID), "", nil)

			mu.Lock()
			defer mu.Unlock()
			codes[rec.Code]++
			if rec.Code != http.StatusOK {
				var e struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
					errs["<undecodable: "+rec.Body.String()+">"]++
					return
				}
				errs[e.Error.Code]++
			}
		}()
	}
	close(start)
	wg.Wait()

	if codes[http.StatusOK] != 1 {
		t.Errorf("200 count = %d, want exactly 1 (SPEC 28); status codes = %v", codes[http.StatusOK], codes)
	}
	if codes[http.StatusConflict] != callers-1 {
		t.Errorf("409 count = %d, want %d; status codes = %v", codes[http.StatusConflict], callers-1, codes)
	}
	if n := errs["INVALID_STATE_TRANSITION"]; n != callers-1 {
		t.Errorf("INVALID_STATE_TRANSITION count = %d, want %d; error codes = %v", n, callers-1, errs)
	}

	final := getJobDTO(t, h, failed.ID)
	if final.Status != "QUEUED" {
		t.Errorf("final status = %q, want QUEUED", final.Status)
	}
	if final.AttemptCount != failed.AttemptCount {
		t.Errorf("attempt_count = %d, want it unchanged at %d: retry must not count as an attempt",
			final.AttemptCount, failed.AttemptCount)
	}

	// "No duplicate queue entry": the racing retries must leave exactly one
	// queued job, not one per winner-shaped response.
	rec := doRequest(h, http.MethodGet, "/jobs?status=QUEUED", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list queued: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	queued := decodeListJobs(t, rec.Body.Bytes())
	if len(queued.Jobs) != 1 || queued.Jobs[0].ID != failed.ID {
		t.Errorf("queued jobs = %+v, want exactly one entry for %s", queued.Jobs, failed.ID)
	}
}
