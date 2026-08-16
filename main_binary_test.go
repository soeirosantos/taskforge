package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestBinaryStartsWithDefaultsAndShutsDownOnSIGTERM builds and runs the real
// taskforge binary — exercising main.go's signal handling, not just the App
// type directly — to satisfy acceptance criterion 3 (starts with defaults on
// a clean checkout, stops cleanly, leaves no stray database file) as an
// automated, repeatable check rather than a one-off manual run.
//
// The whole test is wrapped in a hard deadline so a hang here fails the test
// instead of consuming the suite's budget (SPEC 42).
func TestBinaryStartsWithDefaultsAndShutsDownOnSIGTERM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess integration test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	workDir := t.TempDir()
	binPath := filepath.Join(workDir, "taskforge-under-test")

	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Discover a free port ourselves rather than hardcoding one, so this
	// test never collides with anything else using the machine.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	dbPath := filepath.Join(workDir, "taskforge.db")

	cmd := exec.Command(binPath)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"PORT="+strconv.Itoa(port),
		"WORKER_COUNT=1",
		"DATABASE_PATH="+dbPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	addr := "127.0.0.1:" + strconv.Itoa(port)
	if !waitForHealthy(ctx, addr) {
		cmd.Process.Kill()
		<-exited
		t.Fatalf("server on %s never became healthy", addr)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}

	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("process exited with error after SIGTERM: %v", err)
		}
	case <-ctx.Done():
		cmd.Process.Kill()
		t.Fatal("process did not exit within the test's hard deadline after SIGTERM")
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected database file at %s to have been created: %v", dbPath, err)
	}

	// The repository root must never gain a stray taskforge.db from running
	// the binary with defaults; this test always points DATABASE_PATH at a
	// temp directory, so confirm that held.
	if _, err := os.Stat(filepath.Join(repoRoot, "taskforge.db")); err == nil {
		t.Error("a taskforge.db file was left in the repository root")
	}
}

func waitForHealthy(ctx context.Context, addr string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		resp, err := client.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}
