package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestExecuteHash(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "spec example",
			payload: `{"text":"hello world"}`,
			want:    "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:    "empty string is valid",
			payload: `{"text":""}`,
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			// Hashing is over the UTF-8 bytes, not the runes.
			name:    "utf-8 bytes are hashed",
			payload: `{"text":"héllo"}`,
			want:    "3c48591d8d098a4538f5e013dfcf406e948eac4d3277b10bf614e295d6068179",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Execute(context.Background(), TypeHash, json.RawMessage(tc.payload))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			var got hashResult
			if err := json.Unmarshal(result, &got); err != nil {
				t.Fatalf("unmarshal result %s: %v", result, err)
			}
			if got.SHA256 != tc.want {
				t.Fatalf("sha256 = %q, want %q", got.SHA256, tc.want)
			}
		})
	}
}

func TestExecuteHashResultIsLowercaseHex(t *testing.T) {
	result, err := Execute(context.Background(), TypeHash, json.RawMessage(`{"text":"hello world"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := `{"sha256":"b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"}`
	if string(result) != want {
		t.Fatalf("result = %s, want %s", result, want)
	}
}

func TestExecuteHashRejectsInvalidPayload(t *testing.T) {
	if _, err := Execute(context.Background(), TypeHash, json.RawMessage(`{}`)); err == nil {
		t.Fatal("Execute with missing text = nil error, want error")
	}
}

func TestExecuteDelayCompletes(t *testing.T) {
	start := time.Now()
	result, err := Execute(context.Background(), TypeDelay, json.RawMessage(`{"milliseconds":100}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("returned after %v, want at least 100ms", elapsed)
	}
	want := `{"delayed_milliseconds":100}`
	if string(result) != want {
		t.Fatalf("result = %s, want %s", result, want)
	}
}

// TestExecuteDelayObservesCancellation is the SPEC 8 promptness requirement: a
// 30-second delay cancelled 20ms in must abandon the wait immediately.
func TestExecuteDelayObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	time.AfterFunc(20*time.Millisecond, cancel)

	start := time.Now()
	result, err := Execute(ctx, TypeDelay, json.RawMessage(`{"milliseconds":30000}`))
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if result != nil {
		t.Fatalf("result = %s, want nil", result)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("returned after %v, want well under a second", elapsed)
	}
}

func TestExecuteReturnsImmediatelyWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tc := range []struct {
		typ     Type
		payload string
	}{
		{TypeHash, `{"text":"hello world"}`},
		{TypeDelay, `{"milliseconds":30000}`},
		{TypeFail, `{}`},
	} {
		t.Run(string(tc.typ), func(t *testing.T) {
			start := time.Now()
			_, err := Execute(ctx, tc.typ, json.RawMessage(tc.payload))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
				t.Fatalf("returned after %v, want immediately", elapsed)
			}
		})
	}
}

func TestExecuteFailAlwaysFails(t *testing.T) {
	for i := 0; i < 5; i++ {
		result, err := Execute(context.Background(), TypeFail, json.RawMessage(`{}`))
		if result != nil {
			t.Fatalf("result = %s, want nil", result)
		}
		var jobErr *JobError
		if !errors.As(err, &jobErr) {
			t.Fatalf("err = %v (%T), want *JobError", err, err)
		}
		if jobErr.Code != CodeIntentionalFailure {
			t.Fatalf("code = %q, want %q", jobErr.Code, CodeIntentionalFailure)
		}
		if jobErr.Message != "job failed intentionally" {
			t.Fatalf("message = %q, want %q", jobErr.Message, "job failed intentionally")
		}
		want := `{"code":"INTENTIONAL_FAILURE","message":"job failed intentionally"}`
		if got := string(jobErr.JSON()); got != want {
			t.Fatalf("persisted error = %s, want %s", got, want)
		}
	}
}

func TestExecuteRejectsUnknownType(t *testing.T) {
	if _, err := Execute(context.Background(), Type("sleep"), json.RawMessage(`{}`)); err == nil {
		t.Fatal("Execute with unknown type = nil error, want error")
	}
}

func TestJobErrorConstructors(t *testing.T) {
	cases := []struct {
		name string
		err  *JobError
		want string
	}{
		{"intentional", IntentionalFailure(), `{"code":"INTENTIONAL_FAILURE","message":"job failed intentionally"}`},
		{"interrupted", InterruptedExecution(), `{"code":"INTERRUPTED_EXECUTION","message":"job execution was interrupted by server termination"}`},
		{"shutdown", ServerShutdown(), `{"code":"SERVER_SHUTDOWN","message":"job execution was interrupted by server shutdown"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(tc.err.JSON()); got != tc.want {
				t.Fatalf("JSON() = %s, want %s", got, tc.want)
			}
		})
	}
}
