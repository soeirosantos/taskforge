package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"taskforge/internal/api"
	"taskforge/internal/store"
	"taskforge/internal/worker"
)

// recoveryTimeout bounds startup recovery (SPEC 34). Recovery runs before
// the HTTP listener accepts and before any worker claims, so a hang here
// would otherwise block startup forever.
const recoveryTimeout = 10 * time.Second

// App is the composition root: it owns the store, the shared worker
// registry, the worker pool, and the HTTP server, and sequences their
// startup and shutdown per SPEC 34 and SPEC 35.
//
// App is deliberately structured with separate construction (newApp), Start,
// and Shutdown steps — rather than a single blocking run-to-completion
// function — so that a test can start it and call Shutdown directly without
// sending OS signals (SPEC 42).
type App struct {
	cfg      Config
	logger   *slog.Logger
	store    *store.Store
	registry *worker.Registry
	pool     *worker.Pool
	server   *http.Server
	listener net.Listener
}

// newApp wires the application together:
//
//  1. open and initialize the database;
//  2. run startup recovery;
//  3. build (but do not start) the HTTP server and worker pool.
//
// Both share the single *worker.Registry, per SPEC 26.
//
// If listener is nil, one is opened on cfg.Port. Tests pass a listener
// pre-bound on port 0 (see net.Listen("tcp", "127.0.0.1:0")) so they never
// collide on a fixed port, and discover the actual address via App.Addr.
//
// On any failure, resources already acquired (the database, the listener)
// are released before returning the error.
func newApp(cfg Config, logger *slog.Logger, listener net.Listener) (a *App, err error) {
	if logger == nil {
		logger = slog.Default()
	}

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err != nil {
			st.Close()
		}
	}()

	// SPEC 34: recovery must complete before the HTTP server accepts
	// requests and before any worker claims. It runs here, before either is
	// constructed with a chance to start.
	recoverCtx, cancel := context.WithTimeout(context.Background(), recoveryTimeout)
	n, err := st.Recover(recoverCtx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("startup recovery: %w", err)
	}
	logger.Info("startup recovery completed", "jobs_recovered", n)

	if listener == nil {
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err != nil {
			return nil, fmt.Errorf("listen on port %d: %w", cfg.Port, err)
		}
	}
	defer func() {
		if err != nil {
			listener.Close()
		}
	}()

	registry := worker.NewRegistry()

	pool, err := worker.New(worker.Config{
		Store:    st,
		Registry: registry,
		Count:    cfg.WorkerCount,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("build worker pool: %w", err)
	}

	handler := api.New(st, registry)
	server := &http.Server{Handler: handler.Routes()}

	return &App{
		cfg:      cfg,
		logger:   logger,
		store:    st,
		registry: registry,
		pool:     pool,
		server:   server,
		listener: listener,
	}, nil
}

// Addr returns the address the HTTP listener is bound to.
func (a *App) Addr() string {
	return a.listener.Addr().String()
}

// Start begins serving HTTP requests and starts the worker pool (SPEC 34
// steps 4-5). Recovery has already completed in newApp, before this point,
// so neither the server nor the workers can observe a stale RUNNING row.
//
// It does not block. HTTP serve errors other than the expected
// http.ErrServerClosed (produced by Shutdown) are logged.
func (a *App) Start() {
	go func() {
		if err := a.server.Serve(a.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("http server error", "error", err)
		}
	}()
	a.pool.Start()
	a.logger.Info("server started", "addr", a.Addr())
}

// Shutdown runs the SPEC 35 graceful shutdown sequence, bounded by ctx:
//
//  1. stop workers from claiming new jobs;
//  2. stop accepting new HTTP requests;
//  3. signal cooperative cancellation to running executions;
//  4. (the pool's guarded update handles the RUNNING -> FAILED SERVER_SHUTDOWN
//     transition itself, for jobs where that transition is still available);
//  5. wait for workers to terminate;
//  6. close the database.
//
// If ctx expires before workers finish (step 5), Shutdown stops waiting and
// proceeds to close the database so the overall call still returns within
// the bound; SPEC 35 allows the process to terminate in that case, and any
// job still RUNNING will be handled by startup recovery on the next start.
func (a *App) Shutdown(ctx context.Context) error {
	a.logger.Info("server shutting down")

	// Step 1: stop claiming new jobs. In-flight jobs continue.
	a.pool.StopClaiming()

	// Step 2: stop accepting new HTTP requests, draining in-flight ones.
	var shutdownErr error
	if err := a.server.Shutdown(ctx); err != nil {
		a.logger.Error("http server shutdown error", "error", err)
		shutdownErr = fmt.Errorf("http server shutdown: %w", err)
	}

	// Step 3: signal cooperative cancellation to running executions. The
	// pool's own guarded transitions record SERVER_SHUTDOWN for whichever
	// jobs are still RUNNING at that point (step 4); an already-won terminal
	// state is left untouched.
	a.pool.CancelRunning()

	// Step 5: wait for workers, bounded by ctx so a stuck executor cannot
	// hold shutdown open past the deadline.
	done := make(chan struct{})
	go func() {
		a.pool.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		a.logger.Warn("shutdown timed out waiting for workers to terminate")
	}

	// Step 6: close the database.
	if err := a.store.Close(); err != nil {
		a.logger.Error("database close error", "error", err)
		if shutdownErr == nil {
			shutdownErr = fmt.Errorf("close database: %w", err)
		}
	}

	return shutdownErr
}
