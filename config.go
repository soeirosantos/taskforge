package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Defaults and bounds for the runtime configuration (SPEC 4).
const (
	defaultPort         = 8080
	minPort             = 1
	maxPort             = 65535
	defaultWorkerCount  = 4
	minWorkerCount      = 1
	maxWorkerCount      = 64
	defaultDatabasePath = "./taskforge.db"

	// shutdownTimeout bounds the entire graceful shutdown sequence (SPEC 4,
	// SPEC 35). It has no environment variable in version 1.
	shutdownTimeout = 10 * time.Second
)

// Config holds the application's complete runtime configuration (SPEC 4).
// It is immutable once loaded.
type Config struct {
	Port         int
	WorkerCount  int
	DatabasePath string
}

// LoadConfig reads and validates configuration from the process environment.
// Any invalid value causes a non-zero-exit-worthy error with a clear,
// specific message; LoadConfig itself never exits or logs.
func LoadConfig() (Config, error) {
	port, err := loadPort()
	if err != nil {
		return Config{}, err
	}

	workerCount, err := loadWorkerCount()
	if err != nil {
		return Config{}, err
	}

	databasePath, err := loadDatabasePath()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:         port,
		WorkerCount:  workerCount,
		DatabasePath: databasePath,
	}, nil
}

// loadPort reads PORT, defaulting to defaultPort when unset. A value that is
// set but not a valid integer in [minPort, maxPort] is invalid (SPEC 4),
// including a set-but-empty string, since "" is not a valid integer either.
func loadPort() (int, error) {
	raw, ok := os.LookupEnv("PORT")
	if !ok {
		return defaultPort, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("PORT must be an integer between %d and %d, got %q", minPort, maxPort, raw)
	}
	if n < minPort || n > maxPort {
		return 0, fmt.Errorf("PORT must be between %d and %d, got %d", minPort, maxPort, n)
	}
	return n, nil
}

// loadWorkerCount reads WORKER_COUNT, defaulting to defaultWorkerCount when
// unset. A value that is set but not a valid integer in [minWorkerCount,
// maxWorkerCount] is invalid (SPEC 4).
func loadWorkerCount() (int, error) {
	raw, ok := os.LookupEnv("WORKER_COUNT")
	if !ok {
		return defaultWorkerCount, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("WORKER_COUNT must be an integer between %d and %d, got %q", minWorkerCount, maxWorkerCount, raw)
	}
	if n < minWorkerCount || n > maxWorkerCount {
		return 0, fmt.Errorf("WORKER_COUNT must be between %d and %d, got %d", minWorkerCount, maxWorkerCount, n)
	}
	return n, nil
}

// loadDatabasePath reads DATABASE_PATH, defaulting to defaultDatabasePath
// when unset. An explicitly supplied empty value is invalid (SPEC 4); this
// is why os.LookupEnv is used instead of os.Getenv, which cannot distinguish
// "unset" from "set to empty".
func loadDatabasePath() (string, error) {
	raw, ok := os.LookupEnv("DATABASE_PATH")
	if !ok {
		return defaultDatabasePath, nil
	}
	if raw == "" {
		return "", fmt.Errorf("DATABASE_PATH must not be empty when set")
	}
	return raw, nil
}
