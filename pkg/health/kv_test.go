package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchKVBucket_HappyPath(t *testing.T) {
	want := []KVEntry{
		{
			Key:      "plan-abc",
			Revision: 7,
			Created:  time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC),
			Value:    json.RawMessage(`{"status":"complete"}`),
		},
		{
			Key:      "plan-xyz",
			Revision: 3,
			Created:  time.Date(2026, 4, 30, 10, 1, 0, 0, time.UTC),
			Value:    json.RawMessage(`{"status":"drafting"}`),
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message-logger/kv/PLAN_STATES" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(kvEntriesResponse{Bucket: "PLAN_STATES", Entries: want})
	}))
	defer srv.Close()

	got, err := FetchKVBucket(context.Background(), srv.Client(), srv.URL, "PLAN_STATES")
	if err != nil {
		t.Fatalf("FetchKVBucket: %v", err)
	}
	if len(got) != 2 || got[0].Key != "plan-abc" || got[1].Revision != 3 {
		t.Errorf("got %+v", got)
	}
}

func TestFetchKVBucket_404IsEmptySection(t *testing.T) {
	// Adopters who haven't enabled e.g. ENTITY_STATES get a 404; the
	// bundle should record that as "no entries" rather than failing
	// the capture.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got, err := FetchKVBucket(context.Background(), srv.Client(), srv.URL, "ENTITY_STATES")
	if err != nil {
		t.Errorf("404 should not be an error, got %v", err)
	}
	if got != nil {
		t.Errorf("404 should yield nil entries, got %+v", got)
	}
}

func TestFetchKVBucket_5xxSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchKVBucket(context.Background(), srv.Client(), srv.URL, "PLAN_STATES")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 surfaced; got %v", err)
	}
}

func TestFetchKVBucket_BucketNameEscaping(t *testing.T) {
	// Belt-and-braces: confirm a bucket with characters that need
	// percent-encoding doesn't blow up the request URL.
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI preserves the on-the-wire form; URL.Path is
		// already decoded by the http layer.
		got = r.RequestURI
		_ = json.NewEncoder(w).Encode(kvEntriesResponse{})
	}))
	defer srv.Close()

	if _, err := FetchKVBucket(context.Background(), srv.Client(), srv.URL, "BUCKET WITH SPACES"); err != nil {
		t.Fatalf("FetchKVBucket: %v", err)
	}
	if !strings.Contains(got, "BUCKET%20WITH%20SPACES") {
		t.Errorf("bucket name not escaped: requestURI=%q", got)
	}
}
