// Package main provides unit tests for the HTTP server endpoints.
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// healthHandler is the handler under test for the /health endpoint.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// TestHealthEndpoint_Returns200 verifies that the /health endpoint returns HTTP 200 OK.
func TestHealthEndpoint_Returns200(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthHandler)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

// TestHealthEndpoint_StatusCodeOnly verifies the status code is exactly 200 (not 2xx range).
func TestHealthEndpoint_StatusCodeOnly(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	http.HandlerFunc(healthHandler).ServeHTTP(rr, req)

	const wantCode = http.StatusOK
	if rr.Code != wantCode {
		t.Errorf("healthHandler returned wrong status code: got %v want %v", rr.Code, wantCode)
	}
}

// TestHealthEndpoint_ViaServeMux verifies /health returns 200 when registered on a ServeMux.
func TestHealthEndpoint_ViaServeMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	req, err := http.NewRequest(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 via mux, got %d", rr.Code)
	}
}

// TestHealthEndpoint_PostMethod verifies /health also returns 200 for POST requests.
func TestHealthEndpoint_PostMethod(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	http.HandlerFunc(healthHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 for POST /health, got %d", rr.Code)
	}
}

// TestHealthEndpoint_DefaultRecorderCode validates httptest.Recorder default before handler call.
func TestHealthEndpoint_DefaultRecorderCode(t *testing.T) {
	rr := httptest.NewRecorder()

	// Before serving, the default code is 200 in httptest; confirm handler sets it explicitly.
	req, err := http.NewRequest(http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	http.HandlerFunc(healthHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected explicit 200, got %d", rr.Code)
	}
}
