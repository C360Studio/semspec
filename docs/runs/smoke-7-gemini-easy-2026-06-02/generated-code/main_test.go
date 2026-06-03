package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestHealthHandler_GET(t *testing.T) {

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthHandler)

	handler.ServeHTTP(rr, req)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Check the Content-Type is JSON
	expectedContentType := "application/json"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("handler returned wrong content type: got %v want %v",
			contentType, expectedContentType)
	}

	// Verify JSON structure
	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	// Check for "status" == "ok"
	if status, ok := response["status"]; !ok || status != "ok" {
		t.Errorf("handler returned unexpected JSON status: got %v want 'ok'", status)
	}

	// Check for "uptime" is an integer
	if uptime, ok := response["uptime"]; !ok {
		t.Errorf("handler missing 'uptime' in JSON response")
	} else {
		uptimeFloat, ok := uptime.(float64)
		if !ok {
			t.Errorf("handler returned 'uptime' that is not a number: got %T", uptime)
		} else if uptimeFloat != float64(int(uptimeFloat)) {
			t.Errorf("handler returned 'uptime' that is not an integer: got %v", uptimeFloat)
		}
	}

	// Check for "version" is the Go runtime version
	if version, ok := response["version"]; !ok {
		t.Errorf("handler missing 'version' in JSON response")
	} else {
		versionStr, ok := version.(string)
		if !ok {
			t.Errorf("handler returned 'version' that is not a string: got %T", version)
		} else if versionStr != runtime.Version() {
			t.Errorf("handler returned wrong 'version': got %v want %v", versionStr, runtime.Version())
		}
	}
}

func TestHealthHandler_POST(t *testing.T) {

	req, err := http.NewRequest("POST", "/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthHandler)

	handler.ServeHTTP(rr, req)

	// Check the status code
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code for POST: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

