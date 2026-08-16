package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"taskforge/internal/jobs"
)

// createJobRequest is the top-level shape of a POST /jobs body (SPEC 10).
// Payload is decoded as raw JSON here and validated separately by
// jobs.ValidatePayload once the job type is known.
type createJobRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// createJob implements POST /jobs (SPEC 17).
func (h *Handler) createJob(w http.ResponseWriter, r *http.Request) {
	if !hasJSONContentType(r) {
		writeError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
			"Content-Type must be application/json")
		return
	}

	req, err := decodeCreateJobRequest(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidJSON, err.Error())
		return
	}

	jobType, err := jobs.ParseType(req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidJobType, err.Error())
		return
	}

	if err := jobs.ValidatePayload(jobType, req.Payload); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidPayload, err.Error())
		return
	}

	// Validation is complete; only now does the job become visible to
	// workers (SPEC 17).
	j, err := jobs.NewJob(jobType, req.Payload, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "failed to create job")
		return
	}

	if err := h.store.Create(r.Context(), j); err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternalError, "failed to persist job")
		return
	}

	writeJSON(w, http.StatusCreated, j)
}

// hasJSONContentType reports whether the request's Content-Type is
// application/json, tolerating parameters such as "; charset=utf-8"
// (SPEC 10). It is parsed with mime.ParseMediaType rather than compared as a
// string so that parameters don't cause a false rejection.
func hasJSONContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

// decodeCreateJobRequest strictly decodes a POST /jobs body: exactly one
// JSON object, no unknown top-level fields, no trailing non-whitespace data
// (SPEC 10).
func decodeCreateJobRequest(body io.Reader) (createJobRequest, error) {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	var req createJobRequest
	if err := dec.Decode(&req); err != nil {
		return createJobRequest{}, errors.New("request body must be a single JSON object matching the job schema")
	}

	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return createJobRequest{}, errors.New("request body must contain exactly one JSON object")
	}

	return req, nil
}
