package jobs

import (
	"bytes"
	"encoding/json"
	"sort"
	"testing"
	"time"
)

// specFieldOrder is the exact SPEC 11 field set, in order.
var specFieldOrder = []string{
	"id",
	"type",
	"status",
	"payload",
	"result",
	"error",
	"attempt_count",
	"created_at",
	"queued_at",
	"started_at",
	"finished_at",
	"updated_at",
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed.UTC()
}

// TestJobMarshalsSpecFieldSet checks that a freshly queued job produces exactly
// the twelve SPEC 11 fields with JSON null for every absent value.
func TestJobMarshalsSpecFieldSet(t *testing.T) {
	created := mustTime(t, "2026-01-01T12:00:00Z")
	job := Job{
		ID:           "3f6d6a1c-1f2a-4a5b-8c7d-9e0f1a2b3c4d",
		Type:         TypeHash,
		Status:       StatusQueued,
		Payload:      json.RawMessage(`{"text":"hello world"}`),
		AttemptCount: 0,
		CreatedAt:    created,
		QueuedAt:     created,
		UpdatedAt:    created,
	}

	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"id":"3f6d6a1c-1f2a-4a5b-8c7d-9e0f1a2b3c4d",` +
		`"type":"hash",` +
		`"status":"QUEUED",` +
		`"payload":{"text":"hello world"},` +
		`"result":null,` +
		`"error":null,` +
		`"attempt_count":0,` +
		`"created_at":"2026-01-01T12:00:00.000000000Z",` +
		`"queued_at":"2026-01-01T12:00:00.000000000Z",` +
		`"started_at":null,` +
		`"finished_at":null,` +
		`"updated_at":"2026-01-01T12:00:00.000000000Z"}`
	if string(raw) != want {
		t.Fatalf("marshalled job:\n got %s\nwant %s", raw, want)
	}

	assertExactFields(t, raw)
}

func TestJobMarshalsCompletedAndCancelledShapes(t *testing.T) {
	created := mustTime(t, "2026-01-01T12:00:00Z")
	started := mustTime(t, "2026-01-01T12:00:01.5Z")
	finished := mustTime(t, "2026-01-01T12:00:02.25Z")

	cases := []struct {
		name       string
		job        Job
		wantResult string
		wantError  string
		wantStart  string
		wantFinish string
	}{
		{
			name: "completed carries a result and no error",
			job: Job{
				ID: "id-1", Type: TypeHash, Status: StatusCompleted,
				Payload:    json.RawMessage(`{"text":""}`),
				Result:     json.RawMessage(`{"sha256":"abc"}`),
				CreatedAt:  created,
				QueuedAt:   created,
				StartedAt:  &started,
				FinishedAt: &finished,
				UpdatedAt:  finished,
			},
			wantResult: `{"sha256":"abc"}`,
			wantError:  `null`,
			wantStart:  `"2026-01-01T12:00:01.500000000Z"`,
			wantFinish: `"2026-01-01T12:00:02.250000000Z"`,
		},
		{
			name: "failed carries an error and no result",
			job: Job{
				ID: "id-2", Type: TypeFail, Status: StatusFailed,
				Payload:    json.RawMessage(`{}`),
				Err:        IntentionalFailure().JSON(),
				CreatedAt:  created,
				QueuedAt:   created,
				StartedAt:  &started,
				FinishedAt: &finished,
				UpdatedAt:  finished,
			},
			wantResult: `null`,
			wantError:  `{"code":"INTENTIONAL_FAILURE","message":"job failed intentionally"}`,
			wantStart:  `"2026-01-01T12:00:01.500000000Z"`,
			wantFinish: `"2026-01-01T12:00:02.250000000Z"`,
		},
		{
			name: "cancelled before running has null result, error and started_at",
			job: Job{
				ID: "id-3", Type: TypeDelay, Status: StatusCancelled,
				Payload:    json.RawMessage(`{"milliseconds":5000}`),
				CreatedAt:  created,
				QueuedAt:   created,
				FinishedAt: &finished,
				UpdatedAt:  finished,
			},
			wantResult: `null`,
			wantError:  `null`,
			wantStart:  `null`,
			wantFinish: `"2026-01-01T12:00:02.250000000Z"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.job)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			assertExactFields(t, raw)

			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			for field, want := range map[string]string{
				"result":      tc.wantResult,
				"error":       tc.wantError,
				"started_at":  tc.wantStart,
				"finished_at": tc.wantFinish,
			} {
				if got := string(fields[field]); got != want {
					t.Errorf("%s = %s, want %s", field, got, want)
				}
			}
		})
	}
}

// assertExactFields checks the marshalled object against the SPEC 11 field set:
// no field missing, no field extra, and the documented order preserved.
func assertExactFields(t *testing.T, raw []byte) {
	t.Helper()

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := make([]string, 0, len(fields))
	for name := range fields {
		got = append(got, name)
	}
	sort.Strings(got)

	want := append([]string(nil), specFieldOrder...)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("field set = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("field set = %v, want %v", got, want)
		}
	}

	// Field order in the encoded document must match SPEC 11.
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening brace
		t.Fatalf("read opening token: %v", err)
	}
	for _, want := range specFieldOrder {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("read key %q: %v", want, err)
		}
		key, ok := tok.(string)
		if !ok || key != want {
			t.Fatalf("key = %v, want %q", tok, want)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatalf("read value for %q: %v", want, err)
		}
	}
}

func TestNewJobSetsCreationState(t *testing.T) {
	now := mustTime(t, "2026-01-01T12:00:00Z")
	job, err := NewJob(TypeDelay, json.RawMessage(`{"milliseconds":5000}`), now)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if job.Status != StatusQueued {
		t.Errorf("status = %s, want QUEUED", job.Status)
	}
	if job.AttemptCount != 0 {
		t.Errorf("attempt_count = %d, want 0", job.AttemptCount)
	}
	if !job.CreatedAt.Equal(now) || !job.QueuedAt.Equal(now) || !job.UpdatedAt.Equal(now) {
		t.Errorf("created/queued/updated = %v/%v/%v, want all %v",
			job.CreatedAt, job.QueuedAt, job.UpdatedAt, now)
	}
	if job.StartedAt != nil || job.FinishedAt != nil {
		t.Errorf("started_at = %v, finished_at = %v, want both nil", job.StartedAt, job.FinishedAt)
	}
	if job.Result != nil || job.Err != nil {
		t.Errorf("result = %s, error = %s, want both nil", job.Result, job.Err)
	}
	if !uuidV4Pattern.MatchString(job.ID) {
		t.Errorf("id = %q, want a UUIDv4", job.ID)
	}
}

// TestTimestampLayoutIsFixedWidthAndSortable is the reason the layout is not
// RFC3339Nano: timestamps are persisted as TEXT and ordered as strings, so
// lexical order must equal chronological order.
func TestTimestampLayoutIsFixedWidthAndSortable(t *testing.T) {
	base := mustTime(t, "2026-01-01T12:00:00Z")
	ordered := []time.Time{
		base,
		base.Add(1 * time.Nanosecond),
		base.Add(100 * time.Millisecond),
		base.Add(150 * time.Millisecond),
		base.Add(1 * time.Second),
		base.Add(2 * time.Hour),
	}

	var previous string
	width := len(FormatTime(base))
	for _, ts := range ordered {
		got := FormatTime(ts)
		if len(got) != width {
			t.Fatalf("FormatTime(%v) = %q, width %d, want %d", ts, got, len(got), width)
		}
		if previous != "" && !(previous < got) {
			t.Fatalf("string order broken: %q is not before %q", previous, got)
		}
		previous = got
	}

	if got := FormatTime(base); got != "2026-01-01T12:00:00.000000000Z" {
		t.Fatalf("FormatTime = %q, want zero-padded fraction and Z suffix", got)
	}
}

func TestFormatTimeConvertsToUTC(t *testing.T) {
	zone := time.FixedZone("UTC+2", 2*60*60)
	local := time.Date(2026, 1, 1, 14, 0, 0, 0, zone)
	if got := FormatTime(local); got != "2026-01-01T12:00:00.000000000Z" {
		t.Fatalf("FormatTime = %q, want the UTC rendering", got)
	}
}

func TestParseTimeRoundTrip(t *testing.T) {
	original := mustTime(t, "2026-01-01T12:00:00.123456789Z")
	parsed, err := ParseTime(FormatTime(original))
	if err != nil {
		t.Fatalf("ParseTime: %v", err)
	}
	if !parsed.Equal(original) {
		t.Fatalf("ParseTime = %v, want %v", parsed, original)
	}
	if _, err := ParseTime("not a timestamp"); err == nil {
		t.Fatal("ParseTime(invalid) = nil error, want error")
	}
}
