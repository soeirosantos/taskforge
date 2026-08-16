package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// DefaultPollInterval is how long a worker waits before looking for work again
// after finding the queue empty. It is short enough that a newly created job
// starts almost immediately and cheap enough that idle workers cost close to
// nothing: an empty poll is one indexed SQLite statement.
const DefaultPollInterval = 25 * time.Millisecond

// transitionTimeout bounds the store calls a worker makes around a job's
// execution. Those calls deliberately do not use the execution's context: a
// cancelled or shutting-down execution must still be able to record its
// terminal state (SPEC 35 step 4).
const transitionTimeout = 5 * time.Second

// codeExecutionError is recorded for an executor error that is neither the
// job's own *jobs.JobError nor a cancellation. Payloads are validated before a
// job is ever persisted, so this is a defensive branch; it exists so that an
// unexpected error still moves the job out of RUNNING instead of stranding it
// there until the next startup recovery.
const codeExecutionError = "EXECUTION_ERROR"

// Config describes a worker pool. Store and Registry are owned by the caller
// (main) and shared with the HTTP layer.
type Config struct {
	Store    *store.Store
	Registry *Registry

	// Count is the number of worker goroutines to run: exactly WORKER_COUNT
	// goroutines, created once (SPEC 32).
	Count int

	// Logger receives the SPEC 36 job events. Defaults to slog.Default().
	Logger *slog.Logger

	// PollInterval overrides DefaultPollInterval when non-zero.
	PollInterval time.Duration
}

// Pool is a fixed set of worker goroutines that claim queued jobs and execute
// them. A Pool is single-use: after Stop (or StopClaiming plus Wait) it cannot
// be started again.
type Pool struct {
	store        *store.Store
	registry     *Registry
	count        int
	log          *slog.Logger
	pollInterval time.Duration

	// claimCtx is cancelled by StopClaiming and governs the claim loop only:
	// workers stop looking for new jobs but finish the job in hand.
	claimCtx     context.Context
	stopClaiming context.CancelFunc

	// jobCtx is the parent of every execution context and is cancelled by
	// CancelRunning. It is deliberately separate from claimCtx because SPEC 35
	// interleaves an HTTP drain between "stop claiming" (step 1) and "signal
	// running jobs" (step 3). Deriving execution contexts from it also covers
	// a job that has been claimed but not yet registered.
	jobCtx        context.Context
	cancelRunning context.CancelFunc

	startOnce sync.Once
	wg        sync.WaitGroup

	// live counts worker goroutines that have not yet returned, and spawned
	// counts how many were ever created. Both exist so tests can assert the
	// SPEC 32 bound (exactly Count goroutines, no goroutine per job) and that
	// none outlive Stop.
	live    atomic.Int64
	spawned atomic.Int64
}

// New validates cfg and returns a pool that has not been started yet.
func New(cfg Config) (*Pool, error) {
	if cfg.Store == nil {
		return nil, errors.New("worker: store is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("worker: registry is required")
	}
	if cfg.Count < 1 {
		return nil, fmt.Errorf("worker: count must be at least 1, got %d", cfg.Count)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = DefaultPollInterval
	}

	p := &Pool{
		store:        cfg.Store,
		registry:     cfg.Registry,
		count:        cfg.Count,
		log:          logger,
		pollInterval: poll,
	}
	p.claimCtx, p.stopClaiming = context.WithCancel(context.Background())
	p.jobCtx, p.cancelRunning = context.WithCancel(context.Background())
	return p, nil
}

// Start launches exactly Count worker goroutines. Calling it more than once
// has no effect, so the pool can never grow past its bound (SPEC 32).
func (p *Pool) Start() {
	p.startOnce.Do(func() {
		p.wg.Add(p.count)
		for i := 0; i < p.count; i++ {
			go p.work(i)
		}
		p.log.Info("worker pool started", "worker_count", p.count)
	})
}

// StopClaiming stops workers from claiming new jobs (SPEC 35 step 1). Jobs
// already executing are left alone; queued jobs stay QUEUED. It is idempotent
// and does not wait.
func (p *Pool) StopClaiming() {
	p.stopClaiming()
}

// CancelRunning signals cooperative cancellation to every execution in flight
// (SPEC 35 step 3), including a job that has been claimed but not yet
// registered. Each affected worker then attempts RUNNING -> FAILED with
// SERVER_SHUTDOWN, which is a no-op for a job whose state some other
// transition already won. It is idempotent and does not wait.
func (p *Pool) CancelRunning() {
	p.cancelRunning()
}

// Wait blocks until every worker goroutine has returned (SPEC 35 step 5). It
// only returns once claiming has been stopped.
func (p *Pool) Wait() {
	p.wg.Wait()
}

// Stop stops claiming, interrupts running jobs, and waits for every worker to
// return. Graceful shutdown calls the three steps separately so it can drain
// the HTTP server in between.
func (p *Pool) Stop() {
	p.StopClaiming()
	p.CancelRunning()
	p.Wait()
}

// work is the claim loop of a single worker goroutine. Exactly Count of these
// run for the lifetime of the pool; a job never gets a goroutine of its own.
func (p *Pool) work(id int) {
	p.spawned.Add(1)
	p.live.Add(1)
	// Deferred calls run last-in-first-out, so wg.Done is registered first in
	// order to run last: a Wait that has returned must never see a worker
	// still counted as live.
	defer p.wg.Done()
	defer p.live.Add(-1)

	for p.claimCtx.Err() == nil {
		claimed, err := p.claimAndExecute()
		switch {
		case err != nil:
			// A store failure must not kill the worker (SPEC 32); back off
			// for one interval and try again.
			p.log.Error("worker claim failed", "worker", id, "error", err)
			p.pause()
		case !claimed:
			p.pause()
		}
	}
}

// claimAndExecute claims at most one job and, if it won one, executes it. It
// reports whether a job was claimed.
func (p *Pool) claimAndExecute() (bool, error) {
	job, err := p.store.Claim(p.claimCtx)
	switch {
	case errors.Is(err, store.ErrNoJobAvailable):
		return false, nil
	case err != nil:
		if p.claimCtx.Err() != nil {
			return false, nil // shutting down; not a failure
		}
		return false, err
	}

	p.execute(job)
	return true, nil
}

// execute runs one claimed job and records its terminal state.
//
// # Why the SPEC 26 window is unreachable
//
// SPEC 26 forbids an interleaving in which CANCELLED is persisted and the
// worker then begins or continues meaningful execution because it missed the
// signal. Two things make that unreachable, and neither may be removed:
//
//   - Registration happens here, before any work is done, and the terminal
//     transition below happens before the entry is released. So for the whole
//     interval in which this job could be doing work, a canceller that looks
//     the id up finds it.
//   - Registration cannot literally precede the claim, because store.Claim
//     both selects the job and transitions it in one statement: the id does
//     not exist for this worker until RUNNING has already been persisted.
//     That leaves a hairline interval between the claim committing and the
//     entry appearing, and confirmRunning below is what closes it.
//
// The proof is a case split on the registry mutex, given that the canceller
// signals only after its UPDATE to CANCELLED has committed (Registry's
// contract), and that a cancel can only win against this job after the claim
// committed (before that the row is QUEUED, and cancelling it there means the
// claim never selected the row):
//
//   - The canceller's lookup happens after this Register. It finds the entry
//     and cancels the execution context. Execution has not begun yet (the
//     ctx.Err check below), or is under way and observes the cancellation
//     cooperatively (jobs.Execute selects on ctx.Done). If the entry is
//     already gone, execution has finished and only the guarded terminal
//     transition — which the canceller beat — is left, so there is no
//     meaningful execution to stop.
//   - The canceller's lookup happens before this Register. Then its UPDATE
//     committed before the lookup, the lookup precedes Register, and Register
//     precedes the read in confirmRunning — so that read begins after the
//     UPDATE committed and therefore observes CANCELLED (the store commits
//     each transition before returning). The worker returns without starting
//     work.
//
// There is no third case: the mutex totally orders the lookup and the
// registration. The confirming read is only ever used to decide *not* to run;
// every write remains an atomic guarded transition in the store.
//
// Do not "simplify" this by dropping the confirming read: the second case is
// then a live SPEC 26 violation, and the guarded transitions would still keep
// the persisted state correct, so nothing else would notice. The interleaving
// is reproduced deterministically by
// TestCancelBetweenClaimAndRegistrationNeverRuns, which fails if the read is
// removed.
func (p *Pool) execute(job *jobs.Job) {
	execCtx, release := p.registry.Register(p.jobCtx, job.ID)
	defer release() // runs after the terminal transition below

	p.log.Info("job claimed", jobAttrs(job)...)

	if !p.confirmRunning(job) {
		return
	}
	if err := execCtx.Err(); err != nil {
		// The signal landed after the confirming read but before work began,
		// or the pool was shut down between the claim and here. Record the
		// outcome through the same uniform path as an interrupted execution:
		// the transition is guarded, so a user cancellation that already won
		// stays CANCELLED, while a job interrupted by shutdown does not stay
		// stranded in RUNNING (SPEC 35 step 4).
		p.log.Info("job cancelled before execution started", jobAttrs(job)...)
		p.record(job, nil, err)
		return
	}

	p.log.Info("job started", jobAttrs(job)...)
	result, err := jobs.Execute(execCtx, job.Type, job.Payload)
	p.record(job, result, err)
}

// confirmRunning re-reads the persisted row and reports whether the job is
// still RUNNING, i.e. whether this worker may begin meaningful execution.
//
// This is the second half of the SPEC 26 argument on execute: it catches a
// cancellation that won the database transition before this execution was
// registered and could therefore not be signalled. It is a read taken *after*
// the guarded claim already succeeded and it can only ever suppress work —
// never authorize a write — so it does not reintroduce a read-then-write race.
//
// A read failure is treated as "do not run": the row stays RUNNING and startup
// recovery will resolve it, which is safer than executing a job whose state
// this worker could not verify.
func (p *Pool) confirmRunning(job *jobs.Job) bool {
	ctx, cancel := context.WithTimeout(context.Background(), transitionTimeout)
	defer cancel()

	current, err := p.store.Get(ctx, job.ID)
	if err != nil {
		p.log.Error("job state could not be confirmed before execution",
			append(jobAttrs(job), "error", err)...)
		return false
	}
	if current.Status != jobs.StatusRunning {
		if current.Status == jobs.StatusCancelled {
			p.log.Info("job cancelled before execution started", jobAttrs(job)...)
		} else {
			p.log.Warn("job left RUNNING state before execution started",
				append(jobAttrs(job), "status", string(current.Status))...)
		}
		return false
	}
	return true
}

// record attempts the job's terminal transition. Both transitions are guarded
// on the job still being RUNNING, so a cancellation that already won is never
// overwritten (SPEC 22, 23, 25); losing the race is a normal outcome, not an
// error.
func (p *Pool) record(job *jobs.Job, result json.RawMessage, execErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), transitionTimeout)
	defer cancel()

	if execErr == nil {
		updated, outcome, err := p.store.Complete(ctx, job.ID, result)
		switch {
		case err != nil:
			p.log.Error("job completion could not be persisted",
				append(jobAttrs(job), "error", err)...)
		case outcome == store.OutcomeWon:
			p.log.Info("job completed", jobAttrs(job)...)
		default:
			p.log.Info("job completion skipped; another transition won",
				append(jobAttrs(job), "status", finalStatus(updated))...)
		}
		return
	}

	jobErr := executionFailure(execErr)
	updated, outcome, err := p.store.Fail(ctx, job.ID, jobErr)
	switch {
	case err != nil:
		p.log.Error("job failure could not be persisted",
			append(jobAttrs(job), "error", err)...)
	case outcome == store.OutcomeWon:
		p.log.Info("job failed", append(jobAttrs(job), "error_code", jobErr.Code)...)
	default:
		p.log.Info("job failure skipped; another transition won",
			append(jobAttrs(job), "status", finalStatus(updated))...)
	}
}

// executionFailure maps an executor error onto the error document to persist.
//
// A cancellation is not a job failure, and the worker deliberately does not
// try to work out *why* it was cancelled. It uniformly attempts RUNNING ->
// FAILED with SERVER_SHUTDOWN: if a user cancellation caused it, the row is
// already CANCELLED and that guarded update is a harmless no-op, so only a job
// genuinely interrupted by shutdown ends up recording SERVER_SHUTDOWN
// (SPEC 35).
func executionFailure(err error) *jobs.JobError {
	var jobErr *jobs.JobError
	if errors.As(err, &jobErr) {
		return jobErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return jobs.ServerShutdown()
	}
	return &jobs.JobError{Code: codeExecutionError, Message: err.Error()}
}

// pause waits one poll interval, returning early once claiming has stopped so
// that shutdown never waits on a sleeping worker.
func (p *Pool) pause() {
	timer := time.NewTimer(p.pollInterval)
	defer timer.Stop()

	select {
	case <-p.claimCtx.Done():
	case <-timer.C:
	}
}

// jobAttrs are the SPEC 36 attributes every job log entry carries. Payloads
// are never logged.
func jobAttrs(job *jobs.Job) []any {
	return []any{
		"job_id", job.ID,
		"job_type", string(job.Type),
		"attempt_count", job.AttemptCount,
	}
}

// finalStatus renders the status of the snapshot returned by a lost
// transition, tolerating a nil snapshot (the job was deleted, which the
// service itself never does).
func finalStatus(job *jobs.Job) string {
	if job == nil {
		return "unknown"
	}
	return string(job.Status)
}
