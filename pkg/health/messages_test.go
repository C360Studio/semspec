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

func TestFetchMessages_HappyPath(t *testing.T) {
	ts := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	wire := []messageEntry{
		{
			Sequence:    42,
			Timestamp:   mustJSON(t, ts),
			Subject:     "agent.response.foo",
			MessageType: "agentic.response.v1",
			TraceID:     "trace-1",
			RawData:     json.RawMessage(`{"finish_reason":"stop"}`),
		},
	}
	wantQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message-logger/entries" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		wantQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(wire)
	}))
	defer srv.Close()

	got, err := FetchMessages(context.Background(), srv.Client(), srv.URL, 10, "agent.*")
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Sequence != 42 || got[0].Subject != "agent.response.foo" {
		t.Errorf("unexpected message: %+v", got[0])
	}
	if !got[0].Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", got[0].Timestamp, ts)
	}
	if !strings.Contains(wantQuery, "limit=10") {
		t.Errorf("query missing limit: %q", wantQuery)
	}
	if !strings.Contains(wantQuery, "subject=agent.") {
		t.Errorf("query missing subject filter: %q", wantQuery)
	}
}

func TestFetchMessages_EmptySubjectOmitsParam(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	if _, err := FetchMessages(context.Background(), srv.Client(), srv.URL, 5, ""); err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if strings.Contains(got, "subject=") {
		t.Errorf("empty subject should omit the query param; got %q", got)
	}
}

func TestFetchMessages_DefaultLimit(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	if _, err := FetchMessages(context.Background(), srv.Client(), srv.URL, 0, ""); err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if !strings.Contains(got, "limit=5000") {
		t.Errorf("expected DefaultMessageLimit (5000); got %q", got)
	}
}

func TestFetchMessages_5xxSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := FetchMessages(context.Background(), srv.Client(), srv.URL, 1, "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500, got %v", err)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
