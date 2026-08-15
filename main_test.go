package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthGetReturns200 verifies GET /health returns HTTP 200.
func TestHealthGetReturns200(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestHealthContentTypeIsJSON verifies the Content-Type media type is
// application/json, ignoring any charset parameter.
func TestHealthContentTypeIsJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("failed to parse Content-Type %q: %v", ct, err)
	}
	if mediaType != "application/json" {
		t.Errorf("media type = %q, want %q", mediaType, "application/json")
	}
}

// TestHealthBodyStatusOK verifies the decoded JSON body has status == "ok".
func TestHealthBodyStatusOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal body %q: %v", rec.Body.String(), err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
}

// TestHealthPostReturns405 verifies POST /health returns HTTP 405.
func TestHealthPostReturns405(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestUnknownRouteReturns404 verifies an undefined route returns HTTP 404.
func TestUnknownRouteReturns404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
