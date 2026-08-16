package store

import "errors"

// Sentinel errors returned by the store package. Callers should compare
// against these with errors.Is rather than matching on error text — error
// text may wrap driver-specific detail that the store does not guarantee to
// keep stable, and SPEC 30 forbids surfacing raw database errors to clients
// in the first place.
var (
	// ErrNotFound is returned by Get when no job exists with the given id.
	ErrNotFound = errors.New("store: job not found")

	// ErrUnavailable is returned by Ping when the database cannot be reached.
	ErrUnavailable = errors.New("store: unavailable")

	// ErrInvalidFilter is returned by List when a filter value is not a
	// recognized status or type. The store rejects rather than silently
	// ignoring invalid filters; translating this into an HTTP 400 is the
	// API layer's job.
	ErrInvalidFilter = errors.New("store: invalid filter")
)
