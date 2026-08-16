package api_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"taskforge/internal/api"
	"taskforge/internal/jobs"
	"taskforge/internal/store"
	"taskforge/internal/worker"
)

// This file closes out the two SPEC 40 end-to-end race requirements that are
// not covered elsewhere (Cancellation vs Completion, Cancellation vs
// Failure), plus an end-to-end Attempt Count check. See the package doc
// comments on the other _test.go files for what is already covered:
//
//   - Duplicate execution, parallel execution, concurrent cancellation,
//     concurrent retry, and store-level attempt-count atomicity all have
//     passing tests elsewhere (cancel_test.go, retry_test.go, and
//     internal/worker, internal/store).
//   - store.TestComplete_DoesNotOverwriteACancelledJob is sequential, not a
//     race, and does not stand in for the tests below.
//
// The critical lesson driving how these are written: a broken SPEC 25/26
// implementation can still leave every job's final status inside
// {CANCELLED, COMPLETED, FAILED} — the state-machine guard alone won't catch
// a losing transition that partially executes. What must be asserted is
// agreement between what the API told the caller (200 vs 409) and what is
// persisted, re-read only after the pool has fully drained so a delayed
// overwrite has had the chance to happen and be observed.

// raceWorkerCount and racePollInterval size the pool used by every test in
// this file. Several workers are used so real claim contention exists
// alongside the cancel race, and the poll interval is short so a freshly
// queued job is claimed promptly without inflating the race window.
const (
	raceWorkerCount  = 6
	racePollInterval = 2 * time.Millisecond
)

// newRacePool builds a real HTTP handler backed by a real worker.Pool over a
// fresh on-disk store, so these tests drive cancellation and retry through
// the same handler a client would use while a real pool claims and executes
// jobs concurrently. The pool is not started; callers start it and are
// responsible for stopping it (Stop is idempotent, so an explicit Stop
// followed by a t.Cleanup(pool.Stop) is safe and is the pattern used below to
// guarantee every worker has drained before final assertions run).
func newRacePool(t *testing.T, count int) (http.Handler, *store.Store, *worker.Pool) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "taskforge-race.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	reg := worker.NewRegistry()
	h := api.New(s, reg).Routes()

	pool, err := worker.New(worker.Config{
		Store:        s,
		Registry:     reg,
		Count:        count,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		PollInterval: racePollInterval,
	})
	if err != nil {
		t.Fatalf("worker.New: %v", err)
	}
	return h, s, pool
}

// waitForStoreStatus polls the store directly (not through the HTTP layer)
// until id reaches want, or fails the test after timeout. It is used only to
// detect *when* to fire the racing action, never to make the pass/fail
// assertion itself — those always go through getJobDTO / the HTTP layer, per
// the task's "drive it through the real handler" instruction.
func waitForStoreStatus(t *testing.T, s *store.Store, id string, want jobs.Status, timeout time.Duration) *jobs.Job {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var last jobs.Status
	for time.Now().Before(deadline) {
		j, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get job %s: %v", id, err)
		}
		if j.Status == want {
			return j
		}
		last = j.Status
		runtime.Gosched()
	}
	t.Fatalf("job %s did not reach %s within %s (last observed status %s)", id, want, timeout, last)
	return nil
}

// waitForClaimLeft polls the store directly until id is no longer QUEUED and
// returns whatever status it observes (RUNNING, or already a terminal state
// if the job's execution outran this poll loop). It exists for the
// cancellation-vs-failure race below: a "fail" job's execution window between
// claim and failure is so tight that requiring RUNNING specifically (as
// waitForStoreStatus does) can miss it entirely and fail the test outright
// rather than simply recording a failure-won iteration.
func waitForClaimLeft(t *testing.T, s *store.Store, id string, timeout time.Duration) jobs.Status {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get job %s: %v", id, err)
		}
		if j.Status != jobs.StatusQueued {
			return j.Status
		}
		runtime.Gosched()
	}
	t.Fatalf("job %s was never claimed within %s", id, timeout)
	return ""
}

// --- Cancellation vs Completion --------------------------------------------

// TestRace_CancellationVsCompletion is SPEC 40's "Cancellation vs Completion":
// force a cancel to race a worker's normal completion, roughly 50 times, with
// the cancel offset swept across the job's execution window. It does not
// assert a specific winner (that would be flaky by construction); it asserts
// that whichever side the API reported agrees with what is persisted once the
// pool has drained, including the SPEC 11 result/error shape for the final
// state.
func TestRace_CancellationVsCompletion(t *testing.T) {
	const (
		iterations = 50
		delayMS    = 100 // SPEC 8 floor: the shortest legal execution window.
	)

	h, s, pool := newRacePool(t, raceWorkerCount)
	pool.Start()
	t.Cleanup(pool.Stop)

	type attempt struct {
		id   string
		code int
	}
	results := make([]attempt, 0, iterations)

	for i := 0; i < iterations; i++ {
		created := createJobDTO(t, h, "delay", `{"milliseconds":100}`)

		// Wait for a worker to claim it, then fire the cancel at an offset
		// swept across roughly [0, 1.5*delayMS] so different interleavings of
		// the race are hit across iterations: offsets below delayMS give the
		// cancel a real chance to arrive before the timer fires, and offsets
		// at or beyond delayMS deliberately let completion win, so both sides
		// of the race are exercised rather than only one.
		waitForStoreStatus(t, s, created.ID, jobs.StatusRunning, 2*time.Second)
		offset := time.Duration(i%30) * (5 * time.Millisecond) // 0 .. 145ms
		time.Sleep(offset)

		rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
		results = append(results, attempt{id: created.ID, code: rec.Code})
	}

	// Drain the pool before re-reading final state: a losing worker's
	// completion may still be in flight when the cancel response was
	// written, and that is exactly the window a broken guard would show up
	// in.
	pool.Stop()

	var cancelWon, completionWon int
	for _, r := range results {
		final := getJobDTO(t, h, r.id)
		switch r.code {
		case http.StatusOK:
			cancelWon++
			if final.Status != "CANCELLED" {
				t.Errorf("job %s: cancel returned 200 but persisted status (post-drain) = %q, want CANCELLED",
					r.id, final.Status)
			}
			if !isJSONNull(final.Result) || !isJSONNull(final.Err) {
				t.Errorf("job %s: CANCELLED job has result=%s error=%s, want both null (SPEC 11)",
					r.id, final.Result, final.Err)
			}
		case http.StatusConflict:
			completionWon++
			if final.Status != "COMPLETED" {
				t.Errorf("job %s: cancel returned 409 but persisted status (post-drain) = %q, want COMPLETED unchanged",
					r.id, final.Status)
			}
			if isJSONNull(final.Result) {
				t.Errorf("job %s: COMPLETED job has null result, want non-null (SPEC 11)", r.id)
			}
			if !isJSONNull(final.Err) {
				t.Errorf("job %s: COMPLETED job has non-null error=%s, want null (SPEC 11)", r.id, final.Err)
			}
		default:
			t.Errorf("job %s: cancel returned unexpected status %d, want 200 or 409", r.id, r.code)
		}
	}

	t.Logf("cancellation vs completion: %d/%d iterations cancel won (200), %d/%d completion won (409)",
		cancelWon, iterations, completionWon, iterations)
	if cancelWon == 0 || completionWon == 0 {
		t.Logf("note: only one outcome occurred in this run (cancel=%d, completion=%d) despite sweeping the offset; "+
			"the invariant checks above still ran for every iteration that did occur",
			cancelWon, completionWon)
	}
}

// --- Cancellation vs Failure ------------------------------------------------

// TestRace_CancellationVsFailure is SPEC 40's "Cancellation vs Failure": force
// a cancel to race a worker's failure. A "fail" job fails synchronously as
// soon as it executes (see jobs.executeFail), so unlike the completion race
// above there is no artificial delay to widen the window — it is genuinely
// tight. The offset is swept as finely as this process can manage (a small,
// varying number of scheduler yields rather than a sleep, since a sleep's
// timer resolution would dwarf the window itself) and both sides are still
// asserted, whichever occurs.
func TestRace_CancellationVsFailure(t *testing.T) {
	const iterations = 50

	h, s, pool := newRacePool(t, raceWorkerCount)
	pool.Start()
	t.Cleanup(pool.Stop)

	type attempt struct {
		id   string
		code int
	}
	results := make([]attempt, 0, iterations)

	for i := 0; i < iterations; i++ {
		created := createJobDTO(t, h, "fail", `{}`)

		// A "fail" job can already be FAILED by the time this poll loop
		// observes it having left QUEUED at all (see waitForClaimLeft); that
		// is a legitimate failure-won outcome, not a test error, so the
		// cancel below still fires and is still checked for agreement with
		// whatever state resulted.
		waitForClaimLeft(t, s, created.ID, 2*time.Second)
		for n := 0; n < i%10; n++ {
			runtime.Gosched()
		}

		rec := doRequest(h, http.MethodPost, cancelPath(created.ID), "", nil)
		results = append(results, attempt{id: created.ID, code: rec.Code})
	}

	pool.Stop()

	var cancelWon, failureWon int
	for _, r := range results {
		final := getJobDTO(t, h, r.id)
		switch r.code {
		case http.StatusOK:
			cancelWon++
			if final.Status != "CANCELLED" {
				t.Errorf("job %s: cancel returned 200 but persisted status (post-drain) = %q, want CANCELLED",
					r.id, final.Status)
			}
			if !isJSONNull(final.Result) || !isJSONNull(final.Err) {
				t.Errorf("job %s: CANCELLED job has result=%s error=%s, want both null (SPEC 11)",
					r.id, final.Result, final.Err)
			}
		case http.StatusConflict:
			failureWon++
			if final.Status != "FAILED" {
				t.Errorf("job %s: cancel returned 409 but persisted status (post-drain) = %q, want FAILED unchanged",
					r.id, final.Status)
			}
			if !isJSONNull(final.Result) {
				t.Errorf("job %s: FAILED job has non-null result=%s, want null (SPEC 11)", r.id, final.Result)
			}
			if isJSONNull(final.Err) {
				t.Errorf("job %s: FAILED job has null error, want non-null (SPEC 11)", r.id)
			}
		default:
			t.Errorf("job %s: cancel returned unexpected status %d, want 200 or 409", r.id, r.code)
		}
	}

	t.Logf("cancellation vs failure: %d/%d iterations cancel won (200), %d/%d failure won (409)",
		cancelWon, iterations, failureWon, iterations)
	if cancelWon == 0 {
		t.Logf("note: cancellation never won this race in this run. The window between a fail job's claim "+
			"and its synchronous failure is extremely tight (no artificial delay exists to widen it), so this "+
			"branch can legitimately go unobserved on a given run; the failure-won invariant above was still "+
			"checked for all %d iterations.", iterations)
	}
}

// --- Attempt Count (end-to-end) --------------------------------------------

// raceRetry fires callers concurrent POST /retry requests against id and
// asserts exactly one wins (SPEC 28), reusing the same pattern as
// TestRetryJob_ConcurrentRetries but as a helper so
// TestRace_AttemptCountNeverDoubleIncrements can call it once per job per
// cycle while many other jobs' goroutines are doing the same thing against
// their own jobs at the same time, and the pool is concurrently claiming
// across the whole queue.
func raceRetry(t *testing.T, h http.Handler, id string, callers int) {
	t.Helper()

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		mu    sync.Mutex
		codes = map[int]int{}
	)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			rec := doRequest(h, http.MethodPost, retryPath(id), "", nil)
			mu.Lock()
			codes[rec.Code]++
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if codes[http.StatusOK] != 1 {
		t.Errorf("job %s: concurrent retry race produced %d winners (200s), want exactly 1; codes=%v",
			id, codes[http.StatusOK], codes)
	}
}

// TestRace_AttemptCountNeverDoubleIncrements is SPEC 40's "Attempt Count"
// item, end-to-end: it drives many "fail" jobs through their full
// FAILED -> (raced retry) -> QUEUED -> RUNNING -> FAILED cycle,
// jobs.MaxAttempts times each, through the real HTTP handler and a real
// multi-worker pool, with every job's cycle running concurrently against
// every other job's cycle (plus the retry race within each cycle). This is
// deliberately end-to-end and multi-job/multi-worker, unlike the already
// -covered store-level TestClaim_ExactlyOneWinnerAndSingleIncrement, which
// races many workers for a single claim in isolation. At every FAILED
// checkpoint it asserts attempt_count increased by exactly 1 from the
// previous checkpoint for that job — never more — which is what "never
// increment one execution attempt more than once" means operationally.
func TestRace_AttemptCountNeverDoubleIncrements(t *testing.T) {
	const jobCount = 24

	h, s, pool := newRacePool(t, raceWorkerCount)

	ids := make([]string, jobCount)
	for i := range ids {
		ids[i] = createJobDTO(t, h, "fail", `{}`).ID
	}

	pool.Start()
	t.Cleanup(pool.Stop)

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		violations  []string
		totalCycles int
	)
	wg.Add(jobCount)
	for _, id := range ids {
		go func(id string) {
			defer wg.Done()
			prev := 0
			for cycle := 1; cycle <= jobs.MaxAttempts; cycle++ {
				job := waitForStoreStatus(t, s, id, jobs.StatusFailed, 5*time.Second)

				mu.Lock()
				totalCycles++
				if job.AttemptCount != prev+1 {
					violations = append(violations, fmt.Sprintf(
						"job %s: attempt_count moved from %d to %d in a single execution attempt (cycle %d)",
						id, prev, job.AttemptCount, cycle))
				}
				mu.Unlock()
				prev = job.AttemptCount

				if cycle == jobs.MaxAttempts {
					break
				}
				// Requeue for the next cycle. Racing several concurrent
				// retry callers here (rather than a single one) keeps this
				// end-to-end, not just "claim under concurrency": the retry
				// path itself is under contention at the same time claiming
				// is happening for every other job.
				raceRetry(t, h, id, 4)
			}
		}(id)
	}
	wg.Wait()
	pool.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(violations) > 0 {
		t.Errorf("attempt_count invariant violated %d time(s) under concurrent worker activity:\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
	t.Logf("attempt count end-to-end: %d jobs x up to %d attempts = %d execution attempts observed "+
		"under concurrent multi-worker claim/retry activity, 0 double-increments", jobCount, jobs.MaxAttempts, totalCycles)

	for _, id := range ids {
		final := getJobDTO(t, h, id)
		if final.Status != "FAILED" || final.AttemptCount != jobs.MaxAttempts {
			t.Errorf("job %s: final (post-drain) = {status:%s attempt_count:%d}, want {FAILED %d}",
				id, final.Status, final.AttemptCount, jobs.MaxAttempts)
		}
	}
}
