// Command taskforge runs the TaskForge job processing service: a REST API,
// a persistent SQLite job store, and a concurrent worker pool.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.Default()

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "taskforge: invalid configuration:", err)
		os.Exit(1)
	}

	app, err := newApp(cfg, logger, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "taskforge: startup failed:", err)
		os.Exit(1)
	}

	app.Start()

	// signal.NotifyContext is a thin wrapper around the deterministic
	// Start/Shutdown sequence above (SPEC 42): it exists only to translate
	// SIGINT/SIGTERM into context cancellation, so the sequence itself stays
	// testable without sending real OS signals.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := app.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown completed with errors", "error", err)
		os.Exit(1)
	}
}
