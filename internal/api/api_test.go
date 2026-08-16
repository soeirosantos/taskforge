package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"taskforge/internal/api"
	"taskforge/internal/store"
	"taskforge/internal/worker"
)

// newTestHandler opens a fresh SQLite-backed store in a temp directory and
// returns the routed API handler backed by it, per SPEC 38 (no manually
// started server; a real store on a t.TempDir() file). A fresh
// *worker.Registry is wired in exactly as main (T7) would; no worker pool is
// started, which is fine for these tests since running jobs are set up
// directly against the store (store.Claim) rather than through a pool.
func newTestHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	h, s, _ := newTestHandlerWithRegistry(t)
	return h, s
}

// newTestHandlerWithRegistry is newTestHandler for the tests that also need to
// observe the cancellation registry — the SPEC 26 ordering contract is about
// what the handler does to the registry, and only a test holding the same
// *worker.Registry main would pass in can see it.
func newTestHandlerWithRegistry(t *testing.T) (http.Handler, *store.Store, *worker.Registry) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "taskforge.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	reg := worker.NewRegistry()
	return api.New(s, reg).Routes(), s, reg
}

// decodeErrorBody parses the SPEC 30 error envelope from an httptest
// response body.
func decodeErrorBody(t *testing.T, body []byte) struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
} {
	t.Helper()
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, body)
	}
	return e
}

func requireJSONContentType(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func doRequest(h http.Handler, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// --- Create ---------------------------------------------------------------

func TestCreateJob_ValidHash(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json; charset=utf-8",
		[]byte(`{"type":"hash","payload":{"text":"hello"}}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)

	var got struct {
		ID           string `json:"id"`
		Type         string `json:"type"`
		Status       string `json:"status"`
		AttemptCount int    `json:"attempt_count"`
		Result       any    `json:"result"`
		Error        any    `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID == "" {
		t.Error("id is empty")
	}
	if got.Type != "hash" {
		t.Errorf("type = %q, want hash", got.Type)
	}
	if got.Status != "QUEUED" {
		t.Errorf("status = %q, want QUEUED", got.Status)
	}
	if got.AttemptCount != 0 {
		t.Errorf("attempt_count = %d, want 0", got.AttemptCount)
	}
	if got.Result != nil {
		t.Errorf("result = %v, want null", got.Result)
	}
	if got.Error != nil {
		t.Errorf("error = %v, want null", got.Error)
	}
}

func TestCreateJob_ValidDelay(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"delay","payload":{"milliseconds":500}}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Type         string `json:"type"`
		Status       string `json:"status"`
		AttemptCount int    `json:"attempt_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Type != "delay" || got.Status != "QUEUED" || got.AttemptCount != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestCreateJob_ValidFail(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"fail","payload":{}}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Type         string `json:"type"`
		Status       string `json:"status"`
		AttemptCount int    `json:"attempt_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Type != "fail" || got.Status != "QUEUED" || got.AttemptCount != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestCreateJob_MalformedJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"hash","payload":{`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_JSON" {
		t.Errorf("code = %q, want INVALID_JSON", e.Error.Code)
	}
}

func TestCreateJob_TrailingData(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"hash","payload":{"text":"a"}}{}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_JSON" {
		t.Errorf("code = %q, want INVALID_JSON", e.Error.Code)
	}
}

func TestCreateJob_UnknownTopLevelField(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"hash","payload":{"text":"a"},"extra":true}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_JSON" {
		t.Errorf("code = %q, want INVALID_JSON", e.Error.Code)
	}
}

func TestCreateJob_UnknownPayloadField(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"hash","payload":{"text":"a","extra":1}}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_PAYLOAD" {
		t.Errorf("code = %q, want INVALID_PAYLOAD", e.Error.Code)
	}
}

func TestCreateJob_UnknownJobType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"bogus","payload":{}}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_JOB_TYPE" {
		t.Errorf("code = %q, want INVALID_JOB_TYPE", e.Error.Code)
	}
}

func TestCreateJob_MissingJobType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"payload":{}}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_JOB_TYPE" {
		t.Errorf("code = %q, want INVALID_JOB_TYPE", e.Error.Code)
	}
}

func TestCreateJob_InvalidDelayDuration(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"delay","payload":{"milliseconds":99999}}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "INVALID_PAYLOAD" {
		t.Errorf("code = %q, want INVALID_PAYLOAD", e.Error.Code)
	}
}

func TestCreateJob_UnsupportedMediaType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "text/plain",
		[]byte(`{"type":"hash","payload":{"text":"a"}}`))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("code = %q, want UNSUPPORTED_MEDIA_TYPE", e.Error.Code)
	}
}

func TestCreateJob_MissingContentType(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPost, "/jobs", "",
		[]byte(`{"type":"hash","payload":{"text":"a"}}`))

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("code = %q, want UNSUPPORTED_MEDIA_TYPE", e.Error.Code)
	}
}

// --- Get --------------------------------------------------------------

func TestGetJob_Existing(t *testing.T) {
	h, _ := newTestHandler(t)
	createRec := doRequest(h, http.MethodPost, "/jobs", "application/json",
		[]byte(`{"type":"hash","payload":{"text":"a"}}`))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	rec := doRequest(h, http.MethodGet, "/jobs/"+created.ID, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)

	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id = %q, want %q", got.ID, created.ID)
	}
}

func TestGetJob_UnknownID(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs/00000000-0000-4000-8000-000000000000", "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "JOB_NOT_FOUND" {
		t.Errorf("code = %q, want JOB_NOT_FOUND", e.Error.Code)
	}
}

func TestGetJob_MalformedID(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs/not-a-uuid", "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "JOB_NOT_FOUND" {
		t.Errorf("code = %q, want JOB_NOT_FOUND", e.Error.Code)
	}
}

// --- Health -------------------------------------------------------------

func TestHealth_Available(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/health", "", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %s, want {\"status\":\"ok\"}", rec.Body.String())
	}
}

func TestHealth_Unavailable(t *testing.T) {
	h, s := newTestHandler(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	rec := doRequest(h, http.MethodGet, "/health", "", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)

	e := decodeErrorBody(t, rec.Body.Bytes())
	if e.Error.Code != "PERSISTENCE_UNAVAILABLE" {
		t.Errorf("code = %q, want PERSISTENCE_UNAVAILABLE", e.Error.Code)
	}

	lower := bytes.ToLower(rec.Body.Bytes())
	for _, leak := range []string{"sqlite", ".db", "sql:", "no such"} {
		if bytes.Contains(lower, []byte(leak)) {
			t.Errorf("response body leaks database detail (%q): %s", leak, rec.Body.String())
		}
	}
}

// --- Routing --------------------------------------------------------------

func TestRouting_UnknownRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/nope", "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "ROUTE_NOT_FOUND" {
		t.Errorf("code = %q, want ROUTE_NOT_FOUND", e.Error.Code)
	}
}

func TestRouting_JobsTrailingSlashIsUnknownRoute(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs/", "", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "ROUTE_NOT_FOUND" {
		t.Errorf("code = %q, want ROUTE_NOT_FOUND", e.Error.Code)
	}
}

// TestRouting_DeleteJobsMethodNotAllowed proves DELETE /jobs returns 405, not
// the 404 it would silently fall back to if ServeMux's catch-all "/" pattern
// masked the built-in method mismatch (see Routes' doc comment).
func TestRouting_DeleteJobsMethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodDelete, "/jobs", "", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("Allow header is empty, want it to name supported methods")
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("code = %q, want METHOD_NOT_ALLOWED", e.Error.Code)
	}
}

func TestRouting_PutJobByIDMethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodPut, "/jobs/abc", "", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	requireJSONContentType(t, rec)
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("Allow header is empty, want it to name supported methods")
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("code = %q, want METHOD_NOT_ALLOWED", e.Error.Code)
	}
}

// TestRouting_GetJobsCancelMethodNotAllowed proves /jobs/{id}/cancel is now a
// known route (POST only): an unsupported method on it is a 405, not the 404
// it would have been before this task registered the route.
func TestRouting_GetJobsCancelMethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodGet, "/jobs/abc/cancel", "", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	if allow := rec.Header().Get("Allow"); allow == "" {
		t.Error("Allow header is empty, want it to name supported methods")
	}
	if e := decodeErrorBody(t, rec.Body.Bytes()); e.Error.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("code = %q, want METHOD_NOT_ALLOWED", e.Error.Code)
	}
}

// TestRouting_JobsAllowsGetAndPost asserts the SPEC-31 Allow header on /jobs
// now names both supported methods, now that GET (list) exists alongside
// POST (create).
func TestRouting_JobsAllowsGetAndPost(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(h, http.MethodDelete, "/jobs", "", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	allow := rec.Header().Get("Allow")
	if !strings.Contains(allow, http.MethodGet) || !strings.Contains(allow, http.MethodPost) {
		t.Errorf("Allow = %q, want it to name both GET and POST", allow)
	}
}
