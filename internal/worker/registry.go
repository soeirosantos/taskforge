// Package worker implements the bounded pool of goroutines that claim jobs
// from the store and execute them, together with the in-process registry that
// carries a cancellation signal to a job that is currently executing.
//
// The registry is owned by main and shared with the HTTP layer: the cancel
// endpoint wins the QUEUED|RUNNING -> CANCELLED transition in the database and
// then signals the registry, which is what makes a running job stop early.
// This package never imports the HTTP layer.
package worker

import (
	"context"
	"sync"
)

// Registry records, for every job currently being executed by this process,
// the CancelFunc of the context that execution runs under.
//
// # The SPEC 26 ordering contract
//
// SPEC 26 forbids any window in which the API has persisted CANCELLED and a
// worker afterwards begins or continues meaningful execution because it missed
// the signal. Closing that window is a contract between two callers, and both
// halves must be respected:
//
//  1. The canceller (the API's cancel endpoint, and graceful shutdown) signals
//     *only after* it has won the atomic database transition to CANCELLED.
//     Signalling first would let a worker claim the row afterwards and run it.
//  2. The worker registers the execution *before* it can begin meaningful
//     execution, and then confirms against the persisted row that the job is
//     still RUNNING. See the comment on Pool.execute for why the confirmation
//     step is what makes the register-then-claim ordering achievable at all,
//     and for the interleaving proof.
//
// A Registry is safe for concurrent use by any number of goroutines.
type Registry struct {
	mu      sync.Mutex
	running map[string]*execution
}

// execution is the registry's entry for one running job. It is referenced by
// pointer so that a release can tell "my own entry" from "an entry a later
// execution of the same job id installed", and only ever delete its own.
type execution struct {
	cancel context.CancelFunc
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{running: make(map[string]*execution)}
}

// Register derives a cancellable context for the execution of job id from
// parent and records it under that id. It returns the derived context, which
// execution must run under, and a release function.
//
// release cancels the derived context and removes the entry. It is idempotent
// and safe to defer: cancelling a context whose execution has already finished
// has no effect, and releasing an entry that a later execution has replaced
// leaves that later entry alone.
//
// Registering an id that is already present replaces the entry. That cannot
// happen for two live executions of the same job — a job is only claimable
// from QUEUED and a claim moves it to RUNNING — but the identity check keeps
// the map consistent if it ever did.
func (r *Registry) Register(parent context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	entry := &execution{cancel: cancel}

	r.mu.Lock()
	r.running[id] = entry
	r.mu.Unlock()

	release := func() {
		r.mu.Lock()
		if r.running[id] == entry {
			delete(r.running, id)
		}
		r.mu.Unlock()
		cancel()
	}
	return ctx, release
}

// Cancel signals the running execution of job id, reporting whether an entry
// was present.
//
// A false result is not an error and must not be reported as one: it simply
// means no execution of that job is registered in this process right now —
// the job may be QUEUED, already finished, or in the brief interval between
// its claim and its registration. Callers must have already won the database
// transition to CANCELLED before calling, which is what makes that last case
// safe (Pool.execute's confirmation step observes the persisted CANCELLED and
// never starts work).
func (r *Registry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.running[id]
	if !ok {
		return false
	}
	entry.cancel()
	return true
}

// CancelAll signals every registered execution. Graceful shutdown uses it to
// interrupt jobs that are still running (SPEC 35 step 3). Entries are left in
// place; each execution removes its own on the way out.
func (r *Registry) CancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range r.running {
		entry.cancel()
	}
}

// has reports whether an execution is registered for job id.
func (r *Registry) has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.running[id]
	return ok
}

// size reports how many executions are registered.
func (r *Registry) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.running)
}
