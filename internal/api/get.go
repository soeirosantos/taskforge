package api

import (
	"errors"
	"net/http"

	"taskforge/internal/store"
)

// getJob implements GET /jobs/{id} (SPEC 18).
//
// No UUID parsing happens anywhere: an id is just a lookup key, so a
// malformed id produces the same ErrNotFound as an unknown-but-well-formed
// one, and both map to 404 JOB_NOT_FOUND.
func (h *Handler) getJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	j, err := h.store.Get(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeJobNotFound, "job not found")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, CodeInternalError, "failed to retrieve job")
		return
	}

	writeJSON(w, http.StatusOK, j)
}
