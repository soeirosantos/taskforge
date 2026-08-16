// Package api implements the TaskForge HTTP API: routing, the JSON error
// envelope, and the request handlers described in SPEC 16-31.
package api

import (
	"encoding/json"
	"net/http"
)

// Error codes required by SPEC 30. These are the complete, stable set; codes
// not used by the endpoints implemented in this package (INVALID_FILTER,
// INVALID_STATE_TRANSITION, ATTEMPT_LIMIT_REACHED) are still defined here so
// that later work (list, cancel, retry) can use them without redeclaring the
// set.
const (
	CodeInvalidJSON            = "INVALID_JSON"
	CodeInvalidJobType         = "INVALID_JOB_TYPE"
	CodeInvalidPayload         = "INVALID_PAYLOAD"
	CodeInvalidFilter          = "INVALID_FILTER"
	CodeUnsupportedMediaType   = "UNSUPPORTED_MEDIA_TYPE"
	CodeJobNotFound            = "JOB_NOT_FOUND"
	CodeInvalidStateTransition = "INVALID_STATE_TRANSITION"
	CodeAttemptLimitReached    = "ATTEMPT_LIMIT_REACHED"
	CodeMethodNotAllowed       = "METHOD_NOT_ALLOWED"
	CodeRouteNotFound          = "ROUTE_NOT_FOUND"
	CodePersistenceUnavailable = "PERSISTENCE_UNAVAILABLE"
	CodeInternalError          = "INTERNAL_ERROR"
)

// errorBody is the exact SPEC 30 error envelope.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError writes a SPEC 30 JSON error envelope with the given HTTP status,
// error code, and human-readable message. It always sets Content-Type, per
// SPEC 16.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeJSON marshals v as the response body with the given status code,
// setting Content-Type: application/json first (SPEC 16). A marshaling
// failure here would be a bug in this package, not a client error, so it is
// reported by falling back to a minimal internal-error body rather than
// panicking.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"failed to encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}
