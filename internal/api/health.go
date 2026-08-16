package api

import "net/http"

// healthResponse is the SPEC 29 success body.
type healthResponse struct {
	Status string `json:"status"`
}

// health implements GET /health (SPEC 29). It never exposes SQLite error
// text, SQL, or the database path to the client — a Ping failure is reported
// only as PERSISTENCE_UNAVAILABLE.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, CodePersistenceUnavailable, "persistence is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
