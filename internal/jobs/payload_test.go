package jobs

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestValidatePayload(t *testing.T) {
	cases := []struct {
		name    string
		typ     Type
		payload string
		wantErr bool
	}{
		// SPEC 7 - hash.
		{"hash spec example", TypeHash, `{"text":"hello world"}`, false},
		{"hash empty string", TypeHash, `{"text":""}`, false},
		{"hash unicode", TypeHash, `{"text":"héllo ✅"}`, false},
		{"hash missing field", TypeHash, `{}`, true},
		{"hash null text", TypeHash, `{"text":null}`, true},
		{"hash unknown field", TypeHash, `{"text":"a","extra":1}`, true},
		{"hash only unknown field", TypeHash, `{"txt":"a"}`, true},
		{"hash wrong type number", TypeHash, `{"text":5}`, true},
		{"hash wrong type object", TypeHash, `{"text":{"a":1}}`, true},
		{"hash wrong type array", TypeHash, `{"text":["a"]}`, true},
		{"hash payload null", TypeHash, `null`, true},
		{"hash payload array", TypeHash, `[]`, true},
		{"hash payload string", TypeHash, `"text"`, true},
		{"hash payload empty", TypeHash, ``, true},
		{"hash trailing data", TypeHash, `{"text":"a"} {"text":"b"}`, true},

		// SPEC 8 - delay.
		{"delay spec example", TypeDelay, `{"milliseconds":5000}`, false},
		{"delay lower bound", TypeDelay, `{"milliseconds":100}`, false},
		{"delay upper bound", TypeDelay, `{"milliseconds":30000}`, false},
		{"delay below range", TypeDelay, `{"milliseconds":99}`, true},
		{"delay above range", TypeDelay, `{"milliseconds":30001}`, true},
		{"delay zero", TypeDelay, `{"milliseconds":0}`, true},
		{"delay negative", TypeDelay, `{"milliseconds":-100}`, true},
		{"delay missing field", TypeDelay, `{}`, true},
		{"delay null value", TypeDelay, `{"milliseconds":null}`, true},
		{"delay unknown field", TypeDelay, `{"milliseconds":100,"ms":1}`, true},
		{"delay misnamed field", TypeDelay, `{"ms":5000}`, true},
		{"delay fractional", TypeDelay, `{"milliseconds":100.5}`, true},
		{"delay exponent form", TypeDelay, `{"milliseconds":1e3}`, true},
		{"delay integral float", TypeDelay, `{"milliseconds":5000.0}`, true},
		{"delay string value", TypeDelay, `{"milliseconds":"5000"}`, true},
		{"delay bool value", TypeDelay, `{"milliseconds":true}`, true},
		{"delay payload null", TypeDelay, `null`, true},

		// SPEC 9 - fail.
		{"fail spec example", TypeFail, `{}`, false},
		{"fail with whitespace", TypeFail, "{\n}\n", false},
		{"fail unknown field", TypeFail, `{"reason":"x"}`, true},
		{"fail payload null", TypeFail, `null`, true},
		{"fail payload array", TypeFail, `[]`, true},

		// Unsupported type.
		{"unknown type", Type("sleep"), `{}`, true},
		{"case sensitive type", Type("HASH"), `{"text":"a"}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePayload(tc.typ, json.RawMessage(tc.payload))
			if tc.wantErr && err == nil {
				t.Fatalf("ValidatePayload(%s, %s) = nil, want error", tc.typ, tc.payload)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidatePayload(%s, %s) = %v, want nil", tc.typ, tc.payload, err)
			}
		})
	}
}

func TestDecodeDelayPayloadValue(t *testing.T) {
	ms, err := decodeDelayPayload(json.RawMessage(`{"milliseconds":5000}`))
	if err != nil {
		t.Fatalf("decodeDelayPayload: %v", err)
	}
	if ms != 5000 {
		t.Fatalf("milliseconds = %d, want 5000", ms)
	}
}

func TestDecodeHashPayloadValue(t *testing.T) {
	text, err := decodeHashPayload(json.RawMessage(`{"text":"hello world"}`))
	if err != nil {
		t.Fatalf("decodeHashPayload: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q, want %q", text, "hello world")
	}
}

func TestParseType(t *testing.T) {
	for _, name := range []string{"hash", "delay", "fail"} {
		got, err := ParseType(name)
		if err != nil {
			t.Fatalf("ParseType(%q) = %v, want nil", name, err)
		}
		if string(got) != name {
			t.Fatalf("ParseType(%q) = %q", name, got)
		}
	}
	for _, name := range []string{"", "Hash", "HASH", "sleep", " hash"} {
		if _, err := ParseType(name); err == nil {
			t.Fatalf("ParseType(%q) = nil error, want error", name)
		}
	}
}

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIDIsUniqueUUIDv4(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id, err := NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if !uuidV4Pattern.MatchString(id) {
			t.Fatalf("NewID() = %q, not a canonical lowercase UUIDv4", id)
		}
		if seen[id] {
			t.Fatalf("NewID() returned duplicate %q", id)
		}
		seen[id] = true
	}
}
