package gatherers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPing_Success(t *testing.T) {
	// Server returns valid response with null entity (not found, but pipeline works)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"entity":null}}`)
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	err := g.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping should succeed when server returns null entity, got: %v", err)
	}
}

func TestPing_NotFound(t *testing.T) {
	// Server returns a GraphQL error with "not found" — pipeline IS working
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":null,"errors":[{"message":"entity not found: __readiness_probe__"}]}`)
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	err := g.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping should succeed on 'not found' error (pipeline is working), got: %v", err)
	}
}

func TestPing_Timeout(t *testing.T) {
	// Server delays beyond the 3s probe timeout
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"entity":null}}`)
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	err := g.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping should fail when server times out")
	}
}

func TestPing_ConnectionRefused(t *testing.T) {
	// Use a URL that nothing is listening on
	g := NewGraphGatherer("http://127.0.0.1:1")
	err := g.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping should fail when connection is refused")
	}
}

func TestPing_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "upstream unavailable")
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	err := g.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping should fail on 502 response")
	}
}

func TestWaitForReady_ImmediateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"entity":null}}`)
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	start := time.Now()
	err := g.WaitForReady(context.Background(), 5*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("WaitForReady should succeed immediately, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("WaitForReady should return quickly on immediate success, took: %v", elapsed)
	}
}

func TestWaitForReady_EventualSuccess(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			// First 2 attempts fail with connection-like error
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, "upstream unavailable")
			return
		}
		// Third attempt succeeds
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"entity":null}}`)
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	err := g.WaitForReady(context.Background(), 10*time.Second)

	if err != nil {
		t.Fatalf("WaitForReady should eventually succeed, got: %v", err)
	}

	finalAttempts := attempts.Load()
	if finalAttempts < 3 {
		t.Fatalf("Expected at least 3 attempts, got %d", finalAttempts)
	}
}

func TestWaitForReady_BudgetExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "upstream unavailable")
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	start := time.Now()
	err := g.WaitForReady(context.Background(), 1*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WaitForReady should fail when budget is exhausted")
	}
	// Should take approximately 1s (the budget)
	if elapsed < 900*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("Expected ~1s elapsed for 1s budget, got: %v", elapsed)
	}
}

func TestWaitForReady_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "upstream unavailable")
	}))
	defer srv.Close()

	g := NewGraphGatherer(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := g.WaitForReady(ctx, 10*time.Second)
	if err == nil {
		t.Fatal("WaitForReady should fail when context is cancelled")
	}
}
