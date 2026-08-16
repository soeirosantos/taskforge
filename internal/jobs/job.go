// Package jobs holds the TaskForge job domain: job types, states, payload
// validation, the persisted job representation, and the three job executors.
//
// The package is self-contained: it does not know about storage or HTTP.
package jobs

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

// MaxAttempts is the maximum number of execution attempts a job may accumulate
// (SPEC 14). A job that has already been attempted this many times may not be
// claimed or retried again.
const MaxAttempts = 3

// TimestampLayout is the fixed-width textual form used for every timestamp the
// service emits or persists.
//
// It is deliberately not time.RFC3339Nano. RFC3339Nano removes trailing zeros
// from the fractional second, so "…:00.1Z" and "…:00.15Z" have different
// widths and sort lexicographically in the wrong order ("…1Z" > "…15Z" because
// 'Z' > '5'). Timestamps are persisted as TEXT and ordered with plain string
// comparison, so a fixed nine-digit fraction is what makes lexical order and
// chronological order the same thing. Always formatted in UTC, where the
// "Z07:00" element renders as a single "Z".
const TimestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

// FormatTime renders t in TimestampLayout, in UTC.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}

// ParseTime parses a timestamp written by FormatTime, returning a UTC time.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(TimestampLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Job is the canonical in-memory representation of a job. Its JSON encoding is
// the API representation required by SPEC 11.
type Job struct {
	ID     string
	Type   Type
	Status Status

	// Payload is the validated job payload as stored. Result and Err hold the
	// raw JSON documents persisted for a finished job; both are nil (encoded as
	// JSON null) unless the job is COMPLETED or FAILED respectively.
	Payload json.RawMessage
	Result  json.RawMessage
	Err     json.RawMessage

	AttemptCount int

	CreatedAt  time.Time
	QueuedAt   time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	UpdatedAt  time.Time
}

// jobWire is the exact JSON object shape of SPEC 11: twelve fields, in this
// order, with JSON null for absent values.
type jobWire struct {
	ID           string          `json:"id"`
	Type         Type            `json:"type"`
	Status       Status          `json:"status"`
	Payload      json.RawMessage `json:"payload"`
	Result       json.RawMessage `json:"result"`
	Err          json.RawMessage `json:"error"`
	AttemptCount int             `json:"attempt_count"`
	CreatedAt    string          `json:"created_at"`
	QueuedAt     string          `json:"queued_at"`
	StartedAt    *string         `json:"started_at"`
	FinishedAt   *string         `json:"finished_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// MarshalJSON encodes the job in the SPEC 11 representation. A nil
// json.RawMessage already encodes as null, and absent timestamps are encoded as
// null through a nil *string.
func (j Job) MarshalJSON() ([]byte, error) {
	return json.Marshal(jobWire{
		ID:           j.ID,
		Type:         j.Type,
		Status:       j.Status,
		Payload:      j.Payload,
		Result:       j.Result,
		Err:          j.Err,
		AttemptCount: j.AttemptCount,
		CreatedAt:    FormatTime(j.CreatedAt),
		QueuedAt:     FormatTime(j.QueuedAt),
		StartedAt:    formatTimePtr(j.StartedAt),
		FinishedAt:   formatTimePtr(j.FinishedAt),
		UpdatedAt:    FormatTime(j.UpdatedAt),
	})
}

// formatTimePtr renders an optional timestamp, preserving absence as nil so it
// encodes as JSON null.
func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := FormatTime(*t)
	return &s
}

// NewJob builds a freshly created, QUEUED job with the creation timestamps of
// SPEC 13. The payload must already have been validated with ValidatePayload.
func NewJob(t Type, payload json.RawMessage, now time.Time) (*Job, error) {
	id, err := NewID()
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	return &Job{
		ID:           id,
		Type:         t,
		Status:       StatusQueued,
		Payload:      payload,
		AttemptCount: 0,
		CreatedAt:    now,
		QueuedAt:     now,
		UpdatedAt:    now,
	}, nil
}

// NewID returns a random RFC 4122 version 4 UUID in canonical text form.
//
// Generating this directly keeps the project free of a dependency that would
// only be used for sixteen random bytes (SPEC 47). Nothing in the service ever
// parses an id: an unknown or malformed id is simply a lookup miss.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
