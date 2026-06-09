package workflow

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// commitResponse builds a graph-gateway entitiesByPredicate response in the
// real wire shape: {"data":{"entitiesByPredicate":{"entities":[<id>...]}}}.
// semsource builds the commit entity ID with the SHORT 7-char sha, so the ID
// suffix is ".commit.<sha7>" regardless of the full SHA the gate is handed.
func commitResponse(sha string) []byte {
	return []byte(fmt.Sprintf(
		`{"data":{"entitiesByPredicate":{"entities":["acme.semsource.git.repo.commit.%s"]}}}`,
		sha[:7]))
}

// emptyResponse builds a response with no matching entities (object shape).
func emptyResponse() []byte {
	return []byte(`{"data":{"entitiesByPredicate":{"entities":[]}}}`)
}

// bareArrayResponse builds the legacy/introspection [String] shape:
// {"data":{"entitiesByPredicate":[<id>...]}} — exercised by the parser fallback.
func bareArrayResponse(sha string) []byte {
	return []byte(fmt.Sprintf(
		`{"data":{"entitiesByPredicate":["acme.semsource.git.repo.commit.%s"]}}`,
		sha[:7]))
}

func newTestGate(srv *httptest.Server) *IndexingGate {
	g := NewIndexingGate(srv.URL, nil)
	g.httpClient = &http.Client{Timeout: 2 * time.Second}
	return g
}

// ---------------------------------------------------------------------------
// NewIndexingGate
// ---------------------------------------------------------------------------

func TestNewIndexingGate_EmptyURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if g := NewIndexingGate(tt.url, nil); g != nil {
				t.Errorf("expected nil for URL %q, got non-nil gate", tt.url)
			}
		})
	}
}

func TestNewIndexingGate_ValidURL(t *testing.T) {
	const url = "http://localhost:8080/graph-gateway"
	g := NewIndexingGate(url, nil)
	if g == nil {
		t.Fatal("expected non-nil gate for valid URL")
	}
	if g.graphGatewayURL != url {
		t.Errorf("gatewayURL = %q, want %q", g.graphGatewayURL, url)
	}
}

// ---------------------------------------------------------------------------
// AwaitCommitIndexed: nil receiver
// ---------------------------------------------------------------------------

func TestAwaitCommitIndexed_NilGate(t *testing.T) {
	var g *IndexingGate
	err := g.AwaitCommitIndexed(context.Background(), "abc123", time.Second)
	if err != nil {
		t.Errorf("nil gate should return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AwaitCommitIndexed: found immediately
// ---------------------------------------------------------------------------

func TestAwaitCommitIndexed_FoundImmediately(t *testing.T) {
	commitSHA := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(commitResponse(commitSHA))
	}))
	defer srv.Close()

	gate := newTestGate(srv)
	err := gate.AwaitCommitIndexed(context.Background(), commitSHA, 5*time.Second)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AwaitCommitIndexed: found after retries
// ---------------------------------------------------------------------------

func TestAwaitCommitIndexed_FoundAfterRetries(t *testing.T) {
	commitSHA := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			w.Write(emptyResponse())
		} else {
			w.Write(commitResponse(commitSHA))
		}
	}))
	defer srv.Close()

	gate := newTestGate(srv)
	err := gate.AwaitCommitIndexed(context.Background(), commitSHA, 10*time.Second)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if n := callCount.Load(); n < 3 {
		t.Errorf("expected at least 3 calls, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// AwaitCommitIndexed: timeout
// ---------------------------------------------------------------------------

func TestAwaitCommitIndexed_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyResponse())
	}))
	defer srv.Close()

	gate := newTestGate(srv)
	err := gate.AwaitCommitIndexed(context.Background(), "abc123", 2*time.Second)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// ---------------------------------------------------------------------------
// AwaitCommitIndexed: cancelled context
// ---------------------------------------------------------------------------

func TestAwaitCommitIndexed_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyResponse())
	}))
	defer srv.Close()

	gate := newTestGate(srv)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	err := gate.AwaitCommitIndexed(ctx, "abc123", 30*time.Second)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if err != context.Canceled {
		t.Logf("error was %v (acceptable for timeout-style errors)", err)
	}
}

// ---------------------------------------------------------------------------
// AwaitCommitIndexed: server error (non-200) retries
// ---------------------------------------------------------------------------

func TestAwaitCommitIndexed_ServerErrorThenSuccess(t *testing.T) {
	commitSHA := "cafebabecafebabecafebabecafebabecafebabe"
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(commitResponse(commitSHA))
	}))
	defer srv.Close()

	gate := newTestGate(srv)
	err := gate.AwaitCommitIndexed(context.Background(), commitSHA, 10*time.Second)
	if err != nil {
		t.Fatalf("expected nil error after recovery, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// containsCommitSHA unit tests
// ---------------------------------------------------------------------------

func TestContainsCommitSHA_Found(t *testing.T) {
	sha := "abc123def456abc123def456abc123def456abc1"
	body := commitResponse(sha)
	if !containsCommitSHA(body, sha) {
		t.Error("expected true for matching SHA")
	}
}

func TestContainsCommitSHA_NotFound(t *testing.T) {
	body := commitResponse("abc123def456abc123def456abc123def456abc1")
	if containsCommitSHA(body, "different_sha_entirely") {
		t.Error("expected false for non-matching SHA")
	}
}

func TestContainsCommitSHA_EmptyResponse(t *testing.T) {
	if containsCommitSHA(emptyResponse(), "anything") {
		t.Error("expected false for empty response")
	}
}

// The live graph-gateway returns {"entities":null} (not []) when nothing
// matches the value filter — captured from a real probe.
func TestContainsCommitSHA_NullEntities(t *testing.T) {
	body := []byte(`{"data":{"entitiesByPredicate":{"entities":null}}}`)
	if containsCommitSHA(body, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef") {
		t.Error("expected false for null entities (live empty-match shape)")
	}
}

func TestContainsCommitSHA_MalformedJSON(t *testing.T) {
	if containsCommitSHA([]byte(`{bad json`), "anything") {
		t.Error("expected false for malformed JSON")
	}
}

// The gate is handed a FULL 40-char SHA but the entity ID embeds only the
// short 7-char sha; the match must normalize.
func TestContainsCommitSHA_FullSHAMatchesShortID(t *testing.T) {
	full := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	if !containsCommitSHA(commitResponse(full), full) {
		t.Error("expected full SHA to match the short-sha entity ID suffix")
	}
}

// A different commit's full SHA must not match (different 7-char prefix).
func TestContainsCommitSHA_DifferentCommitNotFound(t *testing.T) {
	indexed := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	other := "fedcba9876543210fedcba9876543210fedcba98"
	if containsCommitSHA(commitResponse(indexed), other) {
		t.Error("expected a non-indexed commit to not match")
	}
}

// The parser must also handle the bare [String] array shape.
func TestContainsCommitSHA_BareArrayShape(t *testing.T) {
	sha := "abc123def456abc123def456abc123def456abc1"
	if !containsCommitSHA(bareArrayResponse(sha), sha) {
		t.Error("expected true for bare-array entitiesByPredicate shape")
	}
	if containsCommitSHA(bareArrayResponse(sha), "fedcba9876543210fedcba9876543210fedcba98") {
		t.Error("expected false for non-matching SHA in bare-array shape")
	}
}
