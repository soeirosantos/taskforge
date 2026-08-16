package main

import (
	"strings"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with no env set: unexpected error: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want default %d", cfg.Port, defaultPort)
	}
	if cfg.WorkerCount != defaultWorkerCount {
		t.Errorf("WorkerCount = %d, want default %d", cfg.WorkerCount, defaultWorkerCount)
	}
	if cfg.DatabasePath != defaultDatabasePath {
		t.Errorf("DatabasePath = %q, want default %q", cfg.DatabasePath, defaultDatabasePath)
	}
}

func TestLoadConfigValidOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("WORKER_COUNT", "16")
	t.Setenv("DATABASE_PATH", "/tmp/custom.db")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with valid overrides: unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.WorkerCount != 16 {
		t.Errorf("WorkerCount = %d, want 16", cfg.WorkerCount)
	}
	if cfg.DatabasePath != "/tmp/custom.db" {
		t.Errorf("DatabasePath = %q, want /tmp/custom.db", cfg.DatabasePath)
	}
}

func TestLoadConfigBoundaryValuesAreValid(t *testing.T) {
	t.Setenv("PORT", "1")
	t.Setenv("WORKER_COUNT", "1")
	if _, err := LoadConfig(); err != nil {
		t.Errorf("PORT=1, WORKER_COUNT=1 should be valid, got error: %v", err)
	}

	t.Setenv("PORT", "65535")
	t.Setenv("WORKER_COUNT", "64")
	if _, err := LoadConfig(); err != nil {
		t.Errorf("PORT=65535, WORKER_COUNT=64 should be valid, got error: %v", err)
	}
}

func TestLoadConfigInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"port zero", map[string]string{"PORT": "0"}},
		{"port too large", map[string]string{"PORT": "70000"}},
		{"port not a number", map[string]string{"PORT": "abc"}},
		{"port negative", map[string]string{"PORT": "-1"}},
		{"port empty string", map[string]string{"PORT": ""}},
		{"worker count zero", map[string]string{"WORKER_COUNT": "0"}},
		{"worker count too large", map[string]string{"WORKER_COUNT": "65"}},
		{"worker count not a number", map[string]string{"WORKER_COUNT": "many"}},
		{"database path empty", map[string]string{"DATABASE_PATH": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("LoadConfig() with %v: expected error, got nil", tt.env)
			}
		})
	}
}

// TestLoadConfigUnsetDatabasePathUsesDefault pins down the SPEC 4 distinction
// this package must get right: DATABASE_PATH unset falls back to the
// default, while DATABASE_PATH="" explicitly set is invalid (covered above).
func TestLoadConfigUnsetDatabasePathUsesDefault(t *testing.T) {
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() with DATABASE_PATH unset: unexpected error: %v", err)
	}
	if cfg.DatabasePath != defaultDatabasePath {
		t.Errorf("DatabasePath = %q, want default %q", cfg.DatabasePath, defaultDatabasePath)
	}
}

func TestLoadConfigErrorMessagesAreClear(t *testing.T) {
	t.Setenv("PORT", "0")
	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Errorf("expected error mentioning PORT, got: %v", err)
	}
}
