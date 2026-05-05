package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchJetStream_HitsExpectedURL(t *testing.T) {
	const fakeBody = `{"streams":[{"name":"AGENT","messages":42}],"now":"2026-05-05T15:00:00Z"}`
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.RequestURI()
		_, _ = w.Write([]byte(fakeBody))
	}))
	defer srv.Close()

	snap, err := FetchJetStream(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchJetStream: %v", err)
	}
	if snap == nil {
		t.Fatal("snapshot is nil")
	}
	if !strings.Contains(capturedPath, "/jsz") || !strings.Contains(capturedPath, "streams=true") || !strings.Contains(capturedPath, "consumers=true") {
		t.Errorf("path = %q, want /jsz with streams+consumers", capturedPath)
	}
	if snap.Status != 200 {
		t.Errorf("status = %d, want 200", snap.Status)
	}
	if string(snap.JSZ) != fakeBody {
		t.Errorf("body mismatch: got %q want %q", string(snap.JSZ), fakeBody)
	}
	if !strings.HasPrefix(snap.URL, srv.URL+"/jsz") {
		t.Errorf("snap.URL = %q, want prefix %q/jsz", snap.URL, srv.URL)
	}
}

func TestFetchJetStream_PreservesNon200(t *testing.T) {
	// 404 from a NATS instance with monitoring disabled — bundle should
	// still record the snapshot so operators see "I tried, here's what
	// I got" instead of silent absence.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("monitoring disabled"))
	}))
	defer srv.Close()

	snap, err := FetchJetStream(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("non-200 must not error: %v", err)
	}
	if snap.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", snap.Status)
	}
	if string(snap.JSZ) != "monitoring disabled" {
		t.Errorf("body preserved on non-200 expected, got %q", string(snap.JSZ))
	}
}

func TestFetchJetStream_EmptyURLReturnsError(t *testing.T) {
	if _, err := FetchJetStream(context.Background(), http.DefaultClient, ""); err == nil {
		t.Error("FetchJetStream with empty URL should error (bundler treats as opt-out, not silent skip)")
	}
}

func TestFetchJetStream_NetworkErrorReturned(t *testing.T) {
	// Closed server simulates "monitoring port unreachable" — should
	// return error so the bundler records a CaptureError.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	if _, err := FetchJetStream(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("FetchJetStream against closed server should return network error")
	}
}
