package jobs

import "fmt"

// Status is a job state (SPEC 12).
type Status string

const (
	StatusQueued    Status = "QUEUED"
	StatusRunning   Status = "RUNNING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

// AllStatuses lists every legal job state.
var AllStatuses = []Status{
	StatusQueued,
	StatusRunning,
	StatusCompleted,
	StatusFailed,
	StatusCancelled,
}

// Valid reports whether s is one of the five job states.
func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s can never be left (SPEC 12, SPEC 15).
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// validTransitions is the complete SPEC 12 transition table. Any pair absent
// from it is illegal, which is what makes COMPLETED and CANCELLED terminal: they
// have no outgoing edges at all.
var validTransitions = map[Status]map[Status]bool{
	StatusQueued: {
		StatusRunning:   true,
		StatusCancelled: true,
	},
	StatusRunning: {
		StatusCompleted: true,
		StatusFailed:    true,
		StatusCancelled: true,
	},
	StatusFailed: {
		StatusQueued: true,
	},
}

// CanTransition reports whether from -> to is a legal state transition. A
// transition to the same state is never legal.
func CanTransition(from, to Status) bool {
	return validTransitions[from][to]
}

// ValidateTransition returns nil when from -> to is legal, and a descriptive
// error otherwise.
func ValidateTransition(from, to Status) error {
	if !from.Valid() {
		return fmt.Errorf("invalid source status %q", string(from))
	}
	if !to.Valid() {
		return fmt.Errorf("invalid target status %q", string(to))
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid transition %s -> %s", string(from), string(to))
	}
	return nil
}
