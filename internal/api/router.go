package api

import (
	"net/http"

	"taskforge/internal/store"
	"taskforge/internal/worker"
)

// Handler holds the API's dependencies and exposes the routed HTTP handler.
// main (T7) constructs one with a *store.Store and the process's single
// *worker.Registry, so the cancel endpoint can signal a running job after it
// wins the store's atomic transition (SPEC 25, 26).
type Handler struct {
	store    *store.Store
	registry *worker.Registry
}

// New creates a Handler backed by the given store and cancellation registry.
func New(s *store.Store, reg *worker.Registry) *Handler {
	return &Handler{store: s, registry: reg}
}

// Routes builds the API's http.Handler (SPEC 16).
//
// Patterns are registered path-only ("/jobs", "/jobs/{id}", "/health"), and
// each handler switches on r.Method itself rather than relying on
// method-qualified patterns like "POST /jobs".
//
// This is deliberate, not incidental: net/http.ServeMux's built-in 405
// handling for method-qualified patterns is silently disabled as soon as a
// "/" catch-all is also registered. With both registered, an unsupported
// method (e.g. DELETE /jobs) falls through the method-qualified pattern,
// matches "/" instead, and returns 404 — silently violating SPEC 31's
// requirement of 405 with an Allow header. Registering path-only patterns
// and dispatching on method inside each handler avoids that trap entirely.
//
// Note also that "/jobs/{id}" requires a non-empty id segment, so it does
// not match "/jobs/" (trailing slash, empty id) — that path correctly falls
// through to the "/" catch-all and returns 404 ROUTE_NOT_FOUND.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/jobs", h.handleJobsCollection)
	mux.HandleFunc("/jobs/{id}", h.handleJobByID)
	mux.HandleFunc("/jobs/{id}/cancel", h.handleJobCancel)
	mux.HandleFunc("/jobs/{id}/retry", h.handleJobRetry)
	mux.HandleFunc("/", h.handleRouteNotFound)
	return mux
}

// handleRouteNotFound serves every path that no other pattern claimed
// (SPEC 31).
func (h *Handler) handleRouteNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, CodeRouteNotFound, "no such route")
}

// methodNotAllowed writes the SPEC 31 405 response, including an Allow
// header naming the route's currently supported methods.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed on this route")
}

// handleJobsCollection dispatches methods on /jobs: GET lists (SPEC 19), POST
// creates (SPEC 17).
func (h *Handler) handleJobsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listJobs(w, r)
	case http.MethodPost:
		h.createJob(w, r)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

// handleJobByID dispatches methods on /jobs/{id}. Only GET is implemented
// here; cancel and retry live on their own sub-paths (/jobs/{id}/cancel,
// /jobs/{id}/retry), not as methods on this route.
func (h *Handler) handleJobByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getJob(w, r)
	default:
		methodNotAllowed(w, http.MethodGet)
	}
}

// handleJobCancel dispatches methods on /jobs/{id}/cancel (SPEC 24).
func (h *Handler) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.cancelJob(w, r)
	default:
		methodNotAllowed(w, http.MethodPost)
	}
}

// handleJobRetry dispatches methods on /jobs/{id}/retry (SPEC 27).
func (h *Handler) handleJobRetry(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.retryJob(w, r)
	default:
		methodNotAllowed(w, http.MethodPost)
	}
}

// handleHealth dispatches methods on /health.
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.health(w, r)
	default:
		methodNotAllowed(w, http.MethodGet)
	}
}
