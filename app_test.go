package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// testLogger discards output so test runs stay quiet; slog.Default() would
// otherwise spam stderr with every SPEC 36 job event these tests generate.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// testListener binds an OS-assigned port so parallel test runs never collide
// (SPEC 42).
func testListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on port 0: %v", err)
	}
	return ln
}

// apiJob mirrors the SPEC 11 wire representation of a job, enough of it for
// these tests to assert on.
type apiJob struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	Result       json.RawMessage `json:"result"`
	Err          json.RawMessage `json:"error"`
	AttemptCount int             `json:"attempt_count"`
	FinishedAt   *string         `json:"finished_at"`
}

func postJob(t *testing.T, addr, jobType, payload string) apiJob {
	t.Helper()
	body := fmt.Sprintf(`{"type":%q,"payload":%s}`, jobType, payload)
	resp, err := http.Post("http://"+addr+"/jobs", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST /jobs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /jobs: status = %d, want 201", resp.StatusCode)
	}
	var j apiJob
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return j
}

func getJob(t *testing.T, addr, id string) apiJob {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/jobs/" + id)
	if err != nil {
		t.Fatalf("GET /jobs/%s: %v", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /jobs/%s: status = %d, want 200", id, resp.StatusCode)
	}
	var j apiJob
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	return j
}

// waitForStatus polls GET /jobs/{id} until it reports one of want, or fails
// the test once deadline is reached. Every wait in this file is bounded so a
// stuck application hangs the test instead of the suite budget (SPEC 42).
func waitForStatus(t *testing.T, addr, id string, deadline time.Duration, want ...string) apiJob {
	t.Helper()
	end := time.Now().Add(deadline)
	var last apiJob
	for time.Now().Before(end) {
		last = getJob(t, addr, id)
		for _, w := range want {
			if last.Status == w {
				return last
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s: status = %q after %v, want one of %v", id, last.Status, deadline, want)
	return apiJob{}
}

// insertRunningJob opens its own store handle on path, persists a job
// directly in RUNNING status (as if a previous process had left it there),
// and closes the store again. It returns the job's id.
func insertRunningJob(t *testing.T, path string, attemptCount int) string {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store to seed RUNNING job: %v", err)
	}
	defer st.Close()

	now := time.Now()
	j, err := jobs.NewJob(jobs.TypeHash, json.RawMessage(`{"text":"stale"}`), now)
	if err != nil {
		t.Fatalf("build seed job: %v", err)
	}
	j.Status = jobs.StatusRunning
	j.AttemptCount = attemptCount
	j.StartedAt = &now

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.Create(ctx, j); err != nil {
		t.Fatalf("persist seed RUNNING job: %v", err)
	}
	return j.ID
}

// --- Acceptance criterion 2 & 5: startup recovery ---

func TestStartupRecoveryCompletesBeforeFirstRequestIsServed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "taskforge.db")

	staleID := insertRunningJob(t, dbPath, 1)

	cfg := Config{Port: 0, WorkerCount: 1, DatabasePath: dbPath}
	app, err := newApp(cfg, testLogger(), testListener(t))
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	app.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		app.Shutdown(ctx)
	}()

	// By the time Start returns, recovery already ran inside newApp — this
	// request is the observable proof that no RUNNING row survives to be
	// seen by a client or claimed by a worker.
	j := getJob(t, app.Addr(), staleID)
	if j.Status != string(jobs.StatusFailed) {
		t.Fatalf("stale RUNNING job status = %q, want FAILED (recovered before serving)", j.Status)
	}

	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(j.Err, &errBody); err != nil {
		t.Fatalf("decode job error: %v", err)
	}
	if errBody.Code != jobs.CodeInterruptedExecution {
		t.Errorf("error code = %q, want %q", errBody.Code, jobs.CodeInterruptedExecution)
	}
}

func TestStartupRecoveryPreservesAttemptCountAndSetsFinishedAt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "taskforge.db")

	staleID := insertRunningJob(t, dbPath, 2)

	cfg := Config{Port: 0, WorkerCount: 1, DatabasePath: dbPath}
	app, err := newApp(cfg, testLogger(), testListener(t))
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}

	// Read directly through the store this test's own App wired, before
	// starting anything else, to check the exact SPEC 34 field-level
	// contract (result null, finished_at populated, attempt_count
	// untouched).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	j, err := app.store.Get(ctx, staleID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if j.Status != jobs.StatusFailed {
		t.Errorf("status = %q, want FAILED", j.Status)
	}
	if j.Result != nil {
		t.Errorf("result = %s, want null", j.Result)
	}
	if j.FinishedAt == nil {
		t.Error("finished_at is nil, want populated")
	}
	if j.AttemptCount != 2 {
		t.Errorf("attempt_count = %d, want unchanged at 2", j.AttemptCount)
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(j.Err, &errBody); err != nil {
		t.Fatalf("decode job error: %v", err)
	}
	if errBody.Code != jobs.CodeInterruptedExecution {
		t.Errorf("error code = %q, want %q", errBody.Code, jobs.CodeInterruptedExecution)
	}

	app.store.Close()
	app.listener.Close()
}

// --- Acceptance criterion 4: graceful shutdown ---

func TestGracefulShutdownInterruptsRunningLeavesQueuedRecordsServerShutdown(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "taskforge.db")

	cfg := Config{Port: 0, WorkerCount: 1, DatabasePath: dbPath}
	app, err := newApp(cfg, testLogger(), testListener(t))
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	app.Start()
	addr := app.Addr()

	// A single worker claims the first job it sees; a long delay job keeps
	// it busy for the rest of the test so the second job is guaranteed to
	// stay QUEUED, and so cancellation has something in-flight to interrupt.
	running := postJob(t, addr, "delay", `{"milliseconds":30000}`)
	queued := postJob(t, addr, "hash", `{"text":"never runs"}`)

	waitForStatus(t, addr, running.ID, 2*time.Second, string(jobs.StatusRunning))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := app.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	elapsed := time.Since(start)

	// Cooperative cancellation must cut the 30s delay short almost
	// immediately, not merely finish before the 5s bound by coincidence.
	if elapsed > 3*time.Second {
		t.Errorf("Shutdown took %v, want well under its 5s bound (cancellation should be near-instant)", elapsed)
	}

	// Re-open the database independently, exactly as the next process start
	// would, to check final persisted state.
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("re-open store after shutdown: %v", err)
	}
	defer st.Close()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()

	runningJob, err := st.Get(ctx, running.ID)
	if err != nil {
		t.Fatalf("get running job: %v", err)
	}
	if runningJob.Status != jobs.StatusFailed {
		t.Errorf("interrupted job status = %q, want FAILED", runningJob.Status)
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(runningJob.Err, &errBody); err != nil {
		t.Fatalf("decode error field: %v", err)
	}
	if errBody.Code != jobs.CodeServerShutdown {
		t.Errorf("interrupted job error code = %q, want %q", errBody.Code, jobs.CodeServerShutdown)
	}

	queuedJob, err := st.Get(ctx, queued.ID)
	if err != nil {
		t.Fatalf("get queued job: %v", err)
	}
	if queuedJob.Status != jobs.StatusQueued {
		t.Errorf("untouched job status = %q, want QUEUED (claiming must have stopped)", queuedJob.Status)
	}
}

func TestGracefulShutdownDoesNotOverwriteAlreadyWonTerminalState(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "taskforge.db")

	cfg := Config{Port: 0, WorkerCount: 1, DatabasePath: dbPath}
	app, err := newApp(cfg, testLogger(), testListener(t))
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}
	app.Start()
	addr := app.Addr()

	completed := postJob(t, addr, "hash", `{"text":"fast"}`)
	done := waitForStatus(t, addr, completed.ID, 2*time.Second, string(jobs.StatusCompleted))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("re-open store after shutdown: %v", err)
	}
	defer st.Close()

	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCtx()
	after, err := st.Get(ctx, completed.ID)
	if err != nil {
		t.Fatalf("get completed job: %v", err)
	}
	if after.Status != jobs.StatusCompleted {
		t.Errorf("already-COMPLETED job status = %q after shutdown, want unchanged COMPLETED", after.Status)
	}
	if after.FinishedAt == nil {
		t.Error("already-COMPLETED job finished_at is nil, want populated and unchanged")
	}
	if done.FinishedAt == nil {
		t.Fatal("job reported COMPLETED via the API with a nil finished_at")
	}
	if after.FinishedAt != nil && jobs.FormatTime(*after.FinishedAt) != *done.FinishedAt {
		t.Errorf("finished_at changed across shutdown: was %s, now %s", *done.FinishedAt, jobs.FormatTime(*after.FinishedAt))
	}
}

// --- Acceptance criterion 1: newApp surfaces construction errors ---

func TestNewAppFailsOnUnwritableDatabasePath(t *testing.T) {
	// A directory used as the database path can never be opened as a
	// SQLite file, so this deterministically exercises the "open and
	// initialize the database" failure path without depending on
	// filesystem permissions.
	dir := t.TempDir()
	cfg := Config{Port: 0, WorkerCount: 1, DatabasePath: dir}
	if _, err := newApp(cfg, testLogger(), nil); err == nil {
		t.Fatal("newApp with a directory as DatabasePath: expected error, got nil")
	}
}
