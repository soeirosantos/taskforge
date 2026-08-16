package api

import (
	"net/http"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// listJobsResponse is the SPEC 19 response envelope: an object with a single
// "jobs" array, always present and never null (even when empty).
type listJobsResponse struct {
	Jobs []*jobs.Job `json:"jobs"`
}

// listJobs implements GET /jobs (SPEC 19).
//
// status and type are optional, independent query filters. An unrecognized
// value for either is a 400 INVALID_FILTER; an unrecognized query parameter
// *name* is simply ignored (SPEC 19 constrains values, not the query string
// as a whole). Ordering (created_at DESC, id ASC) is enforced by the store.
func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var filter store.ListFilter

	if raw := q.Get("status"); raw != "" {
		s := jobs.Status(raw)
		if !s.Valid() {
			writeError(w, http.StatusBadRequest, CodeInvalidFilter, "invalid status filter value")
			return
		}
		filter.Status = &s
	}

	if raw := q.Get("type"); raw != "" {
		t, err := jobs.ParseType(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, CodeInvalidFilter, "invalid type filter value")
			return
		}
		filter.Type = &t
	}

	result, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "failed to list jobs")
		return
	}

	// Belt-and-braces: the store already returns a non-nil, zero-length
	// slice for no matches, but the response contract (SPEC 19: "[]", never
	// "null") is asserted here too since it's this handler's guarantee to
	// keep.
	if result == nil {
		result = []*jobs.Job{}
	}

	writeJSON(w, http.StatusOK, listJobsResponse{Jobs: result})
}
