package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHealthHandlerGET covers SPEC.md section 7, points 1-3:
// GET /health returns 200, Content-Type: application/json, and a JSON body
// that decodes to {"status":"ok"}.
func TestHealthHandlerGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(rec, req)

	res := rec.Result()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusOK)
	}

	contentType := res.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want prefix %q", contentType, "application/json")
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("body.Status = %q, want %q", body.Status, "ok")
	}
}

// TestHealthHandlerPOST covers SPEC.md section 7, point 4:
// POST /health returns 405.
func TestHealthHandlerPOST(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(rec, req)

	res := rec.Result()

	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestUnknownRouteReturns404 covers SPEC.md section 7, point 5: a request to
// an undefined route returns 404. This exercises the real mux/routing
// behavior (not just the handler function directly).
func TestUnknownRouteReturns404(t *testing.T) {
	mux := newMux()

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	res := rec.Result()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusNotFound)
	}
}
