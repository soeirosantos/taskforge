package api

import (
	"net/http"

	"taskforge/internal/store"
)

// retryJob implements POST /jobs/{id}/retry (SPEC 27, 28).
//
// Retry is not idempotent: the store's guarded FAILED -> QUEUED update lets
// exactly one concurrent caller win, and this handler does no check-then-act
// of its own — it only classifies the Outcome the store already computed.
func (h *Handler) retryJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	j, outcome, err := h.store.Retry(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "failed to retry job")
		return
	}

	switch outcome {
	case store.OutcomeWon:
		writeJSON(w, http.StatusOK, j)

	case store.OutcomeAttemptLimitReached:
		writeError(w, http.StatusConflict, CodeAttemptLimitReached, "attempt limit reached")

	case store.OutcomeLost:
		writeError(w, http.StatusConflict, CodeInvalidStateTransition, "job cannot be retried from its current state")

	case store.OutcomeNotFound:
		writeError(w, http.StatusNotFound, CodeJobNotFound, "job not found")

	default:
		writeError(w, http.StatusInternalServerError, CodeInternalError, "unexpected retry outcome")
	}
}
