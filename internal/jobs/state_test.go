package jobs

import "testing"

// legalTransitions is written out independently of the implementation table so
// the test does not simply restate the code under test.
var legalTransitions = map[Status]map[Status]bool{
	StatusQueued:  {StatusRunning: true, StatusCancelled: true},
	StatusRunning: {StatusCompleted: true, StatusFailed: true, StatusCancelled: true},
	StatusFailed:  {StatusQueued: true},
}

// TestCanTransitionCoversEveryPair checks all 25 ordered pairs of states, so
// every legal transition is accepted and every illegal one rejected.
func TestCanTransitionCoversEveryPair(t *testing.T) {
	for _, from := range AllStatuses {
		for _, to := range AllStatuses {
			want := legalTransitions[from][to]
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
			err := ValidateTransition(from, to)
			if want && err != nil {
				t.Errorf("ValidateTransition(%s, %s) = %v, want nil", from, to, err)
			}
			if !want && err == nil {
				t.Errorf("ValidateTransition(%s, %s) = nil, want error", from, to)
			}
		}
	}
}

func TestTerminalStatesHaveNoOutgoingTransitions(t *testing.T) {
	for _, from := range []Status{StatusCompleted, StatusCancelled} {
		if !from.IsTerminal() {
			t.Errorf("%s.IsTerminal() = false, want true", from)
		}
		for _, to := range AllStatuses {
			if CanTransition(from, to) {
				t.Errorf("terminal %s must not transition to %s", from, to)
			}
		}
	}
	for _, from := range []Status{StatusQueued, StatusRunning, StatusFailed} {
		if from.IsTerminal() {
			t.Errorf("%s.IsTerminal() = true, want false", from)
		}
	}
}

func TestSelfTransitionsAreInvalid(t *testing.T) {
	for _, s := range AllStatuses {
		if CanTransition(s, s) {
			t.Errorf("CanTransition(%s, %s) = true, want false", s, s)
		}
	}
}

func TestValidateTransitionRejectsUnknownStatuses(t *testing.T) {
	cases := []struct {
		name     string
		from, to Status
	}{
		{"unknown source", Status("PENDING"), StatusRunning},
		{"unknown target", StatusQueued, Status("DONE")},
		{"empty source", Status(""), StatusQueued},
		{"lowercase", Status("queued"), StatusRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTransition(tc.from, tc.to); err == nil {
				t.Fatalf("ValidateTransition(%q, %q) = nil, want error", tc.from, tc.to)
			}
			if CanTransition(tc.from, tc.to) {
				t.Fatalf("CanTransition(%q, %q) = true, want false", tc.from, tc.to)
			}
		})
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range AllStatuses {
		if !s.Valid() {
			t.Errorf("%s.Valid() = false, want true", s)
		}
	}
	for _, s := range []Status{"", "queued", "PENDING", "RETRY"} {
		if s.Valid() {
			t.Errorf("%q.Valid() = true, want false", s)
		}
	}
}
