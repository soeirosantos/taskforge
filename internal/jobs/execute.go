package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// hashResult is the successful result of a "hash" job (SPEC 7).
type hashResult struct {
	SHA256 string `json:"sha256"`
}

// delayResult is the successful result of a "delay" job (SPEC 8).
type delayResult struct {
	DelayedMilliseconds int64 `json:"delayed_milliseconds"`
}

// Execute runs a job of the given type against its payload.
//
// It returns either a result document to persist, or an error. Two kinds of
// error are possible and the caller must distinguish them:
//
//   - a *JobError, which is the job's own failure and is persisted as the job's
//     "error" field;
//   - a context error (context.Canceled or context.DeadlineExceeded), which
//     means execution was interrupted; the caller decides whether that was a
//     user cancellation or a server shutdown and records the state accordingly.
//
// Execution is cooperative: it checks cancellation before starting any work and
// abandons a delay as soon as the context is done.
func Execute(ctx context.Context, t Type, payload json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch t {
	case TypeHash:
		return executeHash(payload)
	case TypeDelay:
		return executeDelay(ctx, payload)
	case TypeFail:
		return executeFail(payload)
	default:
		return nil, fmt.Errorf("unsupported job type %q", string(t))
	}
}

// executeHash computes SHA-256 over the UTF-8 bytes of the payload text and
// returns it as lowercase hexadecimal.
func executeHash(payload json.RawMessage) (json.RawMessage, error) {
	text, err := decodeHashPayload(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(text))
	return marshalResult(hashResult{SHA256: hex.EncodeToString(sum[:])})
}

// executeDelay waits for the requested duration, returning early with the
// context error if execution is cancelled. It never sleeps uninterruptibly: the
// wait is a select over the timer and ctx.Done() so cancellation is observed
// immediately rather than after the full delay.
func executeDelay(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	ms, err := decodeDelayPayload(payload)
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return marshalResult(delayResult{DelayedMilliseconds: ms})
	}
}

// executeFail validates the payload and then always fails (SPEC 9).
func executeFail(payload json.RawMessage) (json.RawMessage, error) {
	if err := decodeFailPayload(payload); err != nil {
		return nil, err
	}
	return nil, IntentionalFailure()
}

// marshalResult encodes a result document. The result structs contain only
// strings and integers, so encoding cannot fail.
func marshalResult(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal job result: %w", err)
	}
	return raw, nil
}
