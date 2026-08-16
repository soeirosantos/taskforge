package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// testPollInterval keeps the claim loop responsive so the suite stays fast; the
// timing assertions below are all far larger than it.
const testPollInterval = 2 * time.Millisecond

// newPool builds a pool over a fresh on-disk store. The pool is not started.
func newPool(t *testing.T, count int) (*Pool, *store.Store, *Registry) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "taskforge-test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	reg := NewRegistry()
	pool, err := New(Config{
		Store:        st,
		Registry:     reg,
		Count:        count,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		PollInterval: testPollInterval,
	})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Stop)

	return pool, st, reg
}

func createJob(t *testing.T, st *store.Store, typ jobs.Type, payload string) *jobs.Job {
	t.Helper()

	job, err := jobs.NewJob(typ, json.RawMessage(payload), time.Now())
	if err != nil {
		t.Fatalf("new job: %v", err)
	}
	if err := jobs.ValidatePayload(typ, job.Payload); err != nil {
		t.Fatalf("invalid test payload: %v", err)
	}
	if err := st.Create(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	return job
}

func getJob(t *testing.T, st *store.Store, id string) *jobs.Job {
	t.Helper()

	job, err := st.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get job %s: %v", id, err)
	}
	return job
}

// waitForStatus polls until the job reaches want, and fails the test on
// timeout or if the job reaches some other terminal state first.
func waitForStatus(t *testing.T, st *store.Store, id string, want jobs.Status, timeout time.Duration) *jobs.Job {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var last jobs.Status
	for time.Now().Before(deadline) {
		job := getJob(t, st, id)
		if job.Status == want {
			return job
		}
		last = job.Status
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s within %s (last status %s)", id, want, timeout, last)
	return nil
}

// waitForRegistration waits until the worker that claimed id has published its
// cancel func. Tests that want to exercise the *signalled* cancellation path
// use it so they do not accidentally test the claim/registration window
// instead; TestCancelRacingClaimNeverRunsCancelledJob covers that window.
func waitForRegistration(t *testing.T, reg *Registry, id string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if reg.has(id) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s was never registered within %s", id, timeout)
}

func decodeJobError(t *testing.T, raw json.RawMessage) jobs.JobError {
	t.Helper()

	var jobErr jobs.JobError
	if err := json.Unmarshal(raw, &jobErr); err != nil {
		t.Fatalf("decode job error %q: %v", string(raw), err)
	}
	return jobErr
}

func TestNewValidatesConfig(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "cfg.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	cases := map[string]Config{
		"no store":    {Registry: NewRegistry(), Count: 1},
		"no registry": {Store: st, Count: 1},
		"zero count":  {Store: st, Registry: NewRegistry(), Count: 0},
		"negative":    {Store: st, Registry: NewRegistry(), Count: -1},
	}
	for name, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: New returned no error", name)
		}
	}

	pool, err := New(Config{Store: st, Registry: NewRegistry(), Count: 2})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if pool.pollInterval != DefaultPollInterval {
		t.Errorf("poll interval = %s, want the default %s", pool.pollInterval, DefaultPollInterval)
	}
	if pool.log == nil {
		t.Error("logger was not defaulted")
	}
}

// Acceptance 1: many workers competing for one queued job produce exactly one
// execution (SPEC 40, duplicate execution and attempt count).
func TestSingleQueuedJobRunsExactlyOnce(t *testing.T) {
	pool, st, _ := newPool(t, 8)
	job := createJob(t, st, jobs.TypeHash, `{"text":"hello world"}`)

	pool.Start()
	done := waitForStatus(t, st, job.ID, jobs.StatusCompleted, 5*time.Second)

	if done.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", done.AttemptCount)
	}
	const want = `{"sha256":"b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"}`
	if string(done.Result) != want {
		t.Errorf("result = %s, want %s", done.Result, want)
	}
	if done.FinishedAt == nil || done.StartedAt == nil {
		t.Error("completed job is missing started_at or finished_at")
	}

	// Give the losing workers time to do something wrong, then confirm the
	// job was neither re-claimed nor re-executed.
	time.Sleep(50 * time.Millisecond)
	again := getJob(t, st, job.ID)
	if again.AttemptCount != 1 || again.Status != jobs.StatusCompleted {
		t.Errorf("job changed after completion: status %s, attempt_count %d",
			again.Status, again.AttemptCount)
	}
}

// Many jobs, many workers: every job must be executed exactly once.
func TestEveryJobIsClaimedExactlyOnce(t *testing.T) {
	pool, st, _ := newPool(t, 8)

	const jobCount = 24
	ids := make([]string, 0, jobCount)
	for i := 0; i < jobCount; i++ {
		ids = append(ids, createJob(t, st, jobs.TypeHash, `{"text":"x"}`).ID)
	}

	pool.Start()
	for _, id := range ids {
		done := waitForStatus(t, st, id, jobs.StatusCompleted, 10*time.Second)
		if done.AttemptCount != 1 {
			t.Errorf("job %s attempt_count = %d, want 1", id, done.AttemptCount)
		}
	}

	// SPEC 32: exactly WORKER_COUNT goroutines, created once — no goroutine
	// per job.
	if got := pool.spawned.Load(); got != int64(pool.count) {
		t.Errorf("spawned worker goroutines = %d, want %d", got, pool.count)
	}
	if got := pool.live.Load(); got != int64(pool.count) {
		t.Errorf("live worker goroutines = %d, want %d", got, pool.count)
	}
}

// Acceptance 2 (SPEC 40, parallel execution): with WORKER_COUNT > 1, N
// independent delay jobs must finish in demonstrably less than N x duration.
func TestIndependentDelayJobsRunConcurrently(t *testing.T) {
	const (
		workers  = 4
		delayMS  = 100
		jobCount = 4
	)
	pool, st, _ := newPool(t, workers)

	ids := make([]string, 0, jobCount)
	for i := 0; i < jobCount; i++ {
		ids = append(ids, createJob(t, st, jobs.TypeDelay, `{"milliseconds":100}`).ID)
	}

	start := time.Now()
	pool.Start()
	for _, id := range ids {
		waitForStatus(t, st, id, jobs.StatusCompleted, 5*time.Second)
	}
	elapsed := time.Since(start)

	sequential := jobCount * delayMS * time.Millisecond
	if elapsed >= sequential {
		t.Fatalf("elapsed %s is not less than sequential %s: jobs did not run concurrently",
			elapsed, sequential)
	}
	// A generous but meaningful bound: four concurrent 100ms delays should
	// take a little over 100ms, nowhere near the 400ms of serial execution.
	if limit := 300 * time.Millisecond; elapsed > limit {
		t.Errorf("elapsed %s exceeds %s; execution looks insufficiently concurrent", elapsed, limit)
	}
}

// Acceptance 3: cancelling a running delay job returns it promptly, well
// before the delay elapses, and the final state is CANCELLED.
func TestCancelRunningJobReturnsPromptly(t *testing.T) {
	pool, st, reg := newPool(t, 1)
	job := createJob(t, st, jobs.TypeDelay, `{"milliseconds":5000}`)

	pool.Start()
	waitForStatus(t, st, job.ID, jobs.StatusRunning, 5*time.Second)
	waitForRegistration(t, reg, job.ID, 5*time.Second)

	// The canceller's contract: win the database transition first, signal the
	// registry only afterwards.
	cancelled, outcome, err := st.Cancel(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("cancel outcome = %s, want won", outcome)
	}
	if cancelled.Status != jobs.StatusCancelled {
		t.Fatalf("cancelled job status = %s", cancelled.Status)
	}
	if !reg.Cancel(job.ID) {
		t.Fatal("registry had no entry for the running job")
	}

	// The single worker can only claim the marker once the delay job's
	// execution has returned, so the marker completing quickly proves the
	// delay job abandoned its 5s wait.
	start := time.Now()
	marker := createJob(t, st, jobs.TypeHash, `{"text":"marker"}`)
	waitForStatus(t, st, marker.ID, jobs.StatusCompleted, 4*time.Second)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("worker took %s to become available; cancellation was not prompt", elapsed)
	}

	final := getJob(t, st, job.ID)
	if final.Status != jobs.StatusCancelled {
		t.Errorf("final status = %s, want CANCELLED", final.Status)
	}
	if final.Result != nil || final.Err != nil {
		t.Errorf("cancelled job kept result %s / error %s", final.Result, final.Err)
	}
	if reg.has(job.ID) {
		t.Error("registry entry outlived the execution")
	}
}

// TestCancelBetweenClaimAndRegistrationNeverRuns is the deterministic SPEC 26
// test. It reproduces the exact forbidden interleaving instead of hoping to
// hit it by timing:
//
//  1. a worker's claim wins and RUNNING is persisted;
//  2. before that worker registers, a cancellation wins the database
//     transition to CANCELLED and signals the registry, which has no entry
//     for the job — the signal is genuinely lost;
//  3. the worker then proceeds into execute.
//
// The pool must not begin meaningful execution. The job is a 3s delay, so a
// worker that missed the cancellation would sit in it; returning promptly is
// what proves the window is closed. Removing the confirming read from
// Pool.execute makes this test fail, which is how it was validated.
func TestCancelBetweenClaimAndRegistrationNeverRuns(t *testing.T) {
	pool, st, reg := newPool(t, 1)
	// The pool is deliberately not started: this test drives one worker's
	// claim-and-execute sequence by hand so the interleaving is exact.
	job := createJob(t, st, jobs.TypeDelay, `{"milliseconds":3000}`)

	claimed, err := st.Claim(context.Background())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.ID != job.ID || claimed.Status != jobs.StatusRunning {
		t.Fatalf("claimed %s (%s), want %s RUNNING", claimed.ID, claimed.Status, job.ID)
	}

	_, outcome, err := st.Cancel(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("cancel outcome = %s, want won", outcome)
	}
	if reg.Cancel(job.ID) {
		t.Fatal("the registry had an entry; this test must run before registration")
	}

	start := time.Now()
	pool.execute(claimed)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("execute took %s: the worker ran a job whose CANCELLED state was already persisted", elapsed)
	}
	final := getJob(t, st, job.ID)
	if final.Status != jobs.StatusCancelled {
		t.Errorf("final status = %s, want CANCELLED", final.Status)
	}
	if final.Result != nil || final.Err != nil {
		t.Errorf("cancelled job was overwritten: result %s, error %s", final.Result, final.Err)
	}
	if reg.size() != 0 {
		t.Errorf("registry size = %d after execute returned, want 0", reg.size())
	}
}

// Acceptance 4: a cancellation that wins while the job is still QUEUED means
// the job is never claimed and never executed.
func TestCancelWhileQueuedNeverExecutes(t *testing.T) {
	pool, st, reg := newPool(t, 4)
	job := createJob(t, st, jobs.TypeDelay, `{"milliseconds":100}`)

	_, outcome, err := st.Cancel(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if outcome != store.OutcomeWon {
		t.Fatalf("cancel outcome = %s, want won", outcome)
	}
	if reg.Cancel(job.ID) {
		t.Error("registry reported an execution for a job that was never claimed")
	}

	pool.Start()
	time.Sleep(150 * time.Millisecond)

	final := getJob(t, st, job.ID)
	if final.Status != jobs.StatusCancelled {
		t.Errorf("status = %s, want CANCELLED", final.Status)
	}
	if final.AttemptCount != 0 {
		t.Errorf("attempt_count = %d, want 0: the job was claimed", final.AttemptCount)
	}
	if final.StartedAt != nil {
		t.Error("started_at is set: the job was claimed")
	}
}

// TestCancelRacingClaimNeverRunsCancelledJob is the SPEC 26 test: it aims the
// cancellation straight at the interval around the claim, including the
// hairline between the claim committing and the execution being registered.
//
// A missed signal is not directly observable, so the test makes it observable:
// the pool has one worker and the racing job is a 3s delay, so if any
// iteration ever begins or continues meaningful execution after CANCELLED was
// persisted, that worker is stuck in the delay and the marker job queued
// straight afterwards cannot complete for seconds.
func TestCancelRacingClaimNeverRunsCancelledJob(t *testing.T) {
	pool, st, reg := newPool(t, 1)
	pool.Start()

	const iterations = 60
	for i := 0; i < iterations; i++ {
		job := createJob(t, st, jobs.TypeDelay, `{"milliseconds":3000}`)

		// Land the cancel anywhere from "before the claim" to "just after the
		// claim committed", which is the window SPEC 26 is about.
		time.Sleep(time.Duration(rand.IntN(4000)) * time.Microsecond)

		_, outcome, err := st.Cancel(context.Background(), job.ID)
		if err != nil {
			t.Fatalf("iteration %d: cancel: %v", i, err)
		}
		if outcome != store.OutcomeWon {
			t.Fatalf("iteration %d: cancel outcome = %s, want won (a 3s delay cannot have finished)",
				i, outcome)
		}
		reg.Cancel(job.ID)

		start := time.Now()
		marker := createJob(t, st, jobs.TypeHash, `{"text":"marker"}`)
		waitForStatus(t, st, marker.ID, jobs.StatusCompleted, 5*time.Second)
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("iteration %d: the worker was unavailable for %s after CANCELLED was persisted; "+
				"it was still executing the cancelled job", i, elapsed)
		}

		final := getJob(t, st, job.ID)
		if final.Status != jobs.StatusCancelled {
			t.Fatalf("iteration %d: final status = %s, want CANCELLED", i, final.Status)
		}
		if final.Result != nil {
			t.Fatalf("iteration %d: cancelled job has a result %s", i, final.Result)
		}
	}
}

// Acceptance 5: a failing job must not take down a worker (SPEC 23).
func TestFailingJobKeepsPoolProcessing(t *testing.T) {
	pool, st, _ := newPool(t, 2)
	failing := createJob(t, st, jobs.TypeFail, `{}`)

	pool.Start()
	failed := waitForStatus(t, st, failing.ID, jobs.StatusFailed, 5*time.Second)

	jobErr := decodeJobError(t, failed.Err)
	if jobErr.Code != jobs.CodeIntentionalFailure || jobErr.Message != jobs.MsgIntentionalFailure {
		t.Errorf("persisted error = %+v, want the intentional failure", jobErr)
	}
	if failed.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", failed.AttemptCount)
	}
	if failed.FinishedAt == nil {
		t.Error("failed job has no finished_at")
	}

	// The pool survived: later jobs still run, and no worker was lost.
	later := createJob(t, st, jobs.TypeHash, `{"text":"after failure"}`)
	waitForStatus(t, st, later.ID, jobs.StatusCompleted, 5*time.Second)

	if got := pool.live.Load(); got != int64(pool.count) {
		t.Errorf("live workers = %d, want %d", got, pool.count)
	}

	// SPEC 50: no automatic retry.
	time.Sleep(50 * time.Millisecond)
	again := getJob(t, st, failing.ID)
	if again.Status != jobs.StatusFailed || again.AttemptCount != 1 {
		t.Errorf("failed job was retried automatically: status %s, attempt_count %d",
			again.Status, again.AttemptCount)
	}
}

// Acceptance 6a: stopping claims lets the in-flight job finish, leaves queued
// jobs QUEUED, and leaks no goroutine.
func TestStopClaimingLetsInFlightJobFinish(t *testing.T) {
	pool, st, _ := newPool(t, 1)
	running := createJob(t, st, jobs.TypeDelay, `{"milliseconds":200}`)

	pool.Start()
	waitForStatus(t, st, running.ID, jobs.StatusRunning, 5*time.Second)

	queued := createJob(t, st, jobs.TypeHash, `{"text":"never claimed"}`)
	pool.StopClaiming()
	pool.Wait()

	if got := getJob(t, st, running.ID); got.Status != jobs.StatusCompleted {
		t.Errorf("in-flight job status = %s, want COMPLETED", got.Status)
	}
	if got := getJob(t, st, queued.ID); got.Status != jobs.StatusQueued {
		t.Errorf("queued job status = %s, want QUEUED", got.Status)
	}
	if got := pool.live.Load(); got != 0 {
		t.Errorf("live worker goroutines after Wait = %d, want 0", got)
	}
	if got := pool.spawned.Load(); got != int64(pool.count) {
		t.Errorf("spawned worker goroutines = %d, want %d", got, pool.count)
	}
}

// Acceptance 6b: Stop interrupts a running job, records SERVER_SHUTDOWN for
// it (SPEC 35), and returns promptly with no worker left running.
func TestStopInterruptsRunningJob(t *testing.T) {
	pool, st, reg := newPool(t, 2)
	running := createJob(t, st, jobs.TypeDelay, `{"milliseconds":5000}`)

	pool.Start()
	waitForStatus(t, st, running.ID, jobs.StatusRunning, 5*time.Second)
	waitForRegistration(t, reg, running.ID, 5*time.Second)

	start := time.Now()
	pool.Stop()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Stop took %s; running jobs were not interrupted", elapsed)
	}

	final := getJob(t, st, running.ID)
	if final.Status != jobs.StatusFailed {
		t.Fatalf("status = %s, want FAILED", final.Status)
	}
	jobErr := decodeJobError(t, final.Err)
	if jobErr.Code != jobs.CodeServerShutdown || jobErr.Message != jobs.MsgServerShutdown {
		t.Errorf("persisted error = %+v, want the server shutdown error", jobErr)
	}
	if pool.live.Load() != 0 {
		t.Errorf("live worker goroutines after Stop = %d, want 0", pool.live.Load())
	}
	if reg.size() != 0 {
		t.Errorf("registry size after Stop = %d, want 0", reg.size())
	}

	// Stop is idempotent, and a stopped pool claims nothing further.
	pool.Stop()
	leftover := createJob(t, st, jobs.TypeHash, `{"text":"after stop"}`)
	time.Sleep(50 * time.Millisecond)
	if got := getJob(t, st, leftover.ID); got.Status != jobs.StatusQueued {
		t.Errorf("job created after Stop has status %s, want QUEUED", got.Status)
	}
}

// A user cancellation that wins while the job is executing must stay
// CANCELLED: the worker's uniform RUNNING -> FAILED attempt is a guarded
// no-op (SPEC 25, 35).
func TestUserCancellationIsNotOverwrittenByShutdownError(t *testing.T) {
	pool, st, reg := newPool(t, 1)
	job := createJob(t, st, jobs.TypeDelay, `{"milliseconds":5000}`)

	pool.Start()
	waitForStatus(t, st, job.ID, jobs.StatusRunning, 5*time.Second)
	waitForRegistration(t, reg, job.ID, 5*time.Second)

	if _, outcome, err := st.Cancel(context.Background(), job.ID); err != nil || outcome != store.OutcomeWon {
		t.Fatalf("cancel: outcome %s, err %v", outcome, err)
	}
	reg.Cancel(job.ID)

	marker := createJob(t, st, jobs.TypeHash, `{"text":"marker"}`)
	waitForStatus(t, st, marker.ID, jobs.StatusCompleted, 5*time.Second)

	final := getJob(t, st, job.ID)
	if final.Status != jobs.StatusCancelled {
		t.Errorf("status = %s, want CANCELLED", final.Status)
	}
	if final.Err != nil {
		t.Errorf("cancelled job recorded an error %s; the shutdown error overwrote it", final.Err)
	}
}

func TestExecutionFailureClassification(t *testing.T) {
	intentional := jobs.IntentionalFailure()
	if got := executionFailure(intentional); got != intentional {
		t.Errorf("a *jobs.JobError must be persisted unchanged, got %+v", got)
	}
	if got := executionFailure(context.Canceled); got.Code != jobs.CodeServerShutdown {
		t.Errorf("context.Canceled -> %s, want %s", got.Code, jobs.CodeServerShutdown)
	}
	if got := executionFailure(context.DeadlineExceeded); got.Code != jobs.CodeServerShutdown {
		t.Errorf("context.DeadlineExceeded -> %s, want %s", got.Code, jobs.CodeServerShutdown)
	}
	if got := executionFailure(errors.New("boom")); got.Code != codeExecutionError {
		t.Errorf("unexpected error -> %s, want %s", got.Code, codeExecutionError)
	}
}
