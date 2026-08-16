package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Type is a supported job type (SPEC 6). Type names are case-sensitive.
type Type string

const (
	TypeHash  Type = "hash"
	TypeDelay Type = "delay"
	TypeFail  Type = "fail"
)

// Valid reports whether t is one of the three supported job types.
func (t Type) Valid() bool {
	switch t {
	case TypeHash, TypeDelay, TypeFail:
		return true
	default:
		return false
	}
}

// ParseType converts a request value into a job type.
func ParseType(s string) (Type, error) {
	t := Type(s)
	if !t.Valid() {
		return "", fmt.Errorf("unsupported job type %q", s)
	}
	return t, nil
}

// Delay bounds, in milliseconds (SPEC 8).
const (
	MinDelayMilliseconds = 100
	MaxDelayMilliseconds = 30000
)

// hashPayload is the "hash" job payload. The field is a pointer because the
// zero value of a string is itself valid input: an empty "text" is accepted but
// an absent (or null) "text" is not, and only a pointer distinguishes the two.
type hashPayload struct {
	Text *string `json:"text"`
}

// delayPayload is the "delay" job payload. The field is a pointer for the same
// reason as hashPayload.Text, and a plain int64 (rather than json.Number or
// float64) so that the decoder itself rejects 100.5, 1e3, 5000.0 and "5000".
type delayPayload struct {
	Milliseconds *int64 `json:"milliseconds"`
}

// ValidatePayload checks a job payload against the rules for its type. It is
// the single entry point used when accepting a job; the executors re-decode the
// same payload when they run.
func ValidatePayload(t Type, raw json.RawMessage) error {
	switch t {
	case TypeHash:
		_, err := decodeHashPayload(raw)
		return err
	case TypeDelay:
		_, err := decodeDelayPayload(raw)
		return err
	case TypeFail:
		return decodeFailPayload(raw)
	default:
		return fmt.Errorf("unsupported job type %q", string(t))
	}
}

// decodeHashPayload validates a "hash" payload and returns its text (SPEC 7).
func decodeHashPayload(raw json.RawMessage) (string, error) {
	var p hashPayload
	if err := decodeObject(raw, &p); err != nil {
		return "", err
	}
	if p.Text == nil {
		return "", errors.New(`payload field "text" is required`)
	}
	return *p.Text, nil
}

// decodeDelayPayload validates a "delay" payload and returns its duration in
// milliseconds (SPEC 8).
func decodeDelayPayload(raw json.RawMessage) (int64, error) {
	var p delayPayload
	if err := decodeObject(raw, &p); err != nil {
		return 0, err
	}
	if p.Milliseconds == nil {
		return 0, errors.New(`payload field "milliseconds" is required`)
	}
	ms := *p.Milliseconds
	if ms < MinDelayMilliseconds || ms > MaxDelayMilliseconds {
		return 0, fmt.Errorf("payload field \"milliseconds\" must be between %d and %d, got %d",
			MinDelayMilliseconds, MaxDelayMilliseconds, ms)
	}
	return ms, nil
}

// decodeFailPayload validates a "fail" payload, which must be an empty JSON
// object (SPEC 9).
func decodeFailPayload(raw json.RawMessage) error {
	var p struct{}
	return decodeObject(raw, &p)
}

// decodeObject strictly decodes a payload into dst. The payload must be a
// single JSON object with no unknown fields, no trailing data, and values of
// the declared types.
func decodeObject(raw json.RawMessage, dst any) error {
	// A JSON null, array, string or number decodes into some struct targets
	// without complaint, so reject anything that is not an object up front.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil || probe == nil {
		return errors.New("payload must be a JSON object")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("payload must contain exactly one JSON object")
	}
	return nil
}
