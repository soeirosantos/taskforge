package api

import (
	"net/http"

	"taskforge/internal/jobs"
	"taskforge/internal/store"
)

// cancelJob implements POST /jobs/{id}/cancel (SPEC 24, 25, 26).
//
// The store's Cancel is the single guarded atomic transition; this handler
// never reads the job first to decide whether to cancel it — that would be a
// check-then-act race on top of an already-atomic primitive. It only
// classifies the Outcome the store already computed, and — per the SPEC 25/26
// ordering contract — signals the cancellation registry only *after* the
// store confirms this call won the QUEUED|RUNNING -> CANCELLED transition,
// never before and never on a lost race.
func (h *Handler) cancelJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	j, outcome, err := h.store.Cancel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "failed to cancel job")
		return
	}

	switch outcome {
	case store.OutcomeWon:
		// This call won the DB transition first; only now is it safe to
		// signal a running execution (SPEC 26). A false return means the
		// job was QUEUED, not RUNNING, which is not an error.
		if h.registry != nil {
			h.registry.Cancel(id)
		}
		writeJSON(w, http.StatusOK, j)

	case store.OutcomeLost:
		// The store reports OutcomeLost both for an already-CANCELLED job
		// (idempotent 200, SPEC 24) and for COMPLETED/FAILED (409). The
		// returned snapshot is what tells them apart; this read is safe
		// because it only classifies a result already decided by the
		// guarded update, not because it precedes one.
		if j.Status == jobs.StatusCancelled {
			writeJSON(w, http.StatusOK, j)
			return
		}
		writeError(w, http.StatusConflict, CodeInvalidStateTransition, "job cannot be cancelled from its current state")

	case store.OutcomeNotFound:
		writeError(w, http.StatusNotFound, CodeJobNotFound, "job not found")

	default:
		writeError(w, http.StatusInternalServerError, CodeInternalError, "unexpected cancel outcome")
	}
}
