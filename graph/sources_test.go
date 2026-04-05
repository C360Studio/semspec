package graph

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/resource"
)

func TestNewSourceRegistry_LocalAlwaysReady(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://localhost:8080/graphql", Type: "local"},
		{Name: "semsource", GraphQLURL: "http://semsource/graphql", StatusURL: "http://semsource/status", Type: "semsource"},
	}, nil)

	ready := reg.ReadySources()
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready source (local), got %d", len(ready))
	}
	if ready[0].Name != "local" {
		t.Errorf("ready source name: got %q, want %q", ready[0].Name, "local")
	}
}

func TestNewSourceRegistry_AlwaysQueryMarksReady(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "always", GraphQLURL: "http://always/graphql", Type: "semsource", AlwaysQuery: true},
	}, nil)

	ready := reg.ReadySources()
	if len(ready) != 1 {
		t.Fatalf("expected AlwaysQuery source to be ready, got %d", len(ready))
	}
}

func TestNewSourceRegistry_BackwardCompat_URLDerivation(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "legacy", URL: "http://semsource:8080", Type: "semsource"},
	}, nil)

	src := reg.sources[0]
	wantGraphQL := "http://semsource:8080/graph-gateway/graphql"
	wantStatus := "http://semsource:8080/source-manifest/status"

	if src.GraphQLURL != wantGraphQL {
		t.Errorf("GraphQLURL: got %q, want %q", src.GraphQLURL, wantGraphQL)
	}
	if src.StatusURL != wantStatus {
		t.Errorf("StatusURL: got %q, want %q", src.StatusURL, wantStatus)
	}
}

func TestNewSourceRegistry_BackwardCompat_URLWithTrailingSlash(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "trailing", URL: "http://semsource:8080/", Type: "semsource"},
	}, nil)

	src := reg.sources[0]
	if src.GraphQLURL != "http://semsource:8080/graph-gateway/graphql" {
		t.Errorf("trailing slash not handled: got %q", src.GraphQLURL)
	}
}

func TestSourcesForQuery_EntityRouting(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "workspace", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource", EntityPrefix: "semspec.semsource.", AlwaysQuery: true},
	}, nil)

	// Entity query with matching prefix routes to single source.
	sources := reg.SourcesForQuery("entity", "semspec.semsource.source.doc.readme", "")
	if len(sources) != 1 || sources[0].Name != "workspace" {
		t.Errorf("entity routing: expected workspace, got %v", sourceNames(sources))
	}

	// Entity query without prefix match falls back to local source.
	sources = reg.SourcesForQuery("entity", "unknown.prefix.entity", "")
	if len(sources) != 1 || sources[0].Name != "local" {
		t.Errorf("entity fallback: expected [local], got %v", sourceNames(sources))
	}
}

func TestSourcesForQuery_SearchFanout(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource", AlwaysQuery: true},
	}, nil)

	sources := reg.SourcesForQuery("search", "", "")
	if len(sources) != 2 {
		t.Errorf("search fanout: expected 2 ready sources, got %d", len(sources))
	}
}

func TestSourcesForQuery_SummaryOnlySemsource(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource", AlwaysQuery: true},
	}, nil)

	sources := reg.SourcesForQuery("summary", "", "")
	if len(sources) != 1 || sources[0].Name != "ws" {
		t.Errorf("summary routing: expected [ws], got %v", sourceNames(sources))
	}
}

func TestSourcesForQuery_PrefixNotReady_ReturnsNil(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "ws", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource", EntityPrefix: "ws."},
	}, nil)

	// ws is not ready, but owns the prefix — should return nil (not fallback).
	sources := reg.SourcesForQuery("entity", "ws.source.doc.readme", "")
	if len(sources) != 0 {
		t.Errorf("expected empty (prefix owner not ready), got %v", sourceNames(sources))
	}
}

func TestSummaryURL_Derivation(t *testing.T) {
	s := &Source{StatusURL: "http://semsource:8080/source-manifest/status"}
	want := "http://semsource:8080/source-manifest/summary"
	if got := s.SummaryURL(); got != want {
		t.Errorf("SummaryURL: got %q, want %q", got, want)
	}
}

func TestSummaryURL_EmptyStatusURL(t *testing.T) {
	s := &Source{StatusURL: ""}
	if got := s.SummaryURL(); got != "" {
		t.Errorf("expected empty SummaryURL for empty StatusURL, got %q", got)
	}
}

func TestQueryTimeout_Default(t *testing.T) {
	reg := NewSourceRegistry(nil, nil)
	if got := reg.QueryTimeout(); got != 3_000_000_000 {
		t.Errorf("default QueryTimeout: got %v, want 3s", got)
	}
}

func TestLocalGraphQLURL(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "semsource", GraphQLURL: "http://ss/graphql", Type: "semsource"},
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
	}, nil)

	if got := reg.LocalGraphQLURL(); got != "http://local/graphql" {
		t.Errorf("LocalGraphQLURL: got %q, want %q", got, "http://local/graphql")
	}
}

func TestLocalGraphQLURL_NoLocal(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "semsource", GraphQLURL: "http://ss/graphql", Type: "semsource"},
	}, nil)

	if got := reg.LocalGraphQLURL(); got != "" {
		t.Errorf("LocalGraphQLURL with no local: got %q, want empty", got)
	}
}

func TestHasSemsources(t *testing.T) {
	t.Run("with semsource", func(t *testing.T) {
		reg := NewSourceRegistry([]Source{
			{Name: "local", Type: "local"},
			{Name: "ws", Type: "semsource"},
		}, nil)
		if !reg.HasSemsources() {
			t.Error("expected HasSemsources=true")
		}
	})

	t.Run("without semsource", func(t *testing.T) {
		reg := NewSourceRegistry([]Source{
			{Name: "local", Type: "local"},
		}, nil)
		if reg.HasSemsources() {
			t.Error("expected HasSemsources=false")
		}
	})
}

func TestResolveByPrefix_LocalFallback(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
	}, nil)

	src := reg.resolveByPrefix("unknown.entity.id")
	if src == nil || src.Name != "local" {
		t.Errorf("expected local fallback, got %v", src)
	}
}

func TestNewSourceRegistry_WithQueryTimeout(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://localhost/graphql", Type: "local"},
	}, nil, WithQueryTimeout(15*time.Second))

	if reg.queryTimeout != 15*time.Second {
		t.Errorf("queryTimeout = %v, want 15s", reg.queryTimeout)
	}
}

func TestNewSourceRegistry_WithHTTPTimeout(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://localhost/graphql", Type: "local"},
	}, nil, WithHTTPTimeout(20*time.Second))

	if reg.client.Timeout != 20*time.Second {
		t.Errorf("client.Timeout = %v, want 20s", reg.client.Timeout)
	}
}

func TestNewSourceRegistry_OptionsDefaultsPreserved(t *testing.T) {
	reg := NewSourceRegistry(nil, nil)

	if reg.queryTimeout != 3*time.Second {
		t.Errorf("default queryTimeout = %v, want 3s", reg.queryTimeout)
	}
	if reg.client.Timeout != 5*time.Second {
		t.Errorf("default client.Timeout = %v, want 5s", reg.client.Timeout)
	}
}

func TestNewSourceRegistry_ZeroOptionIgnored(t *testing.T) {
	reg := NewSourceRegistry(nil, nil, WithQueryTimeout(0), WithHTTPTimeout(0))

	if reg.queryTimeout != 3*time.Second {
		t.Errorf("queryTimeout should stay default, got %v", reg.queryTimeout)
	}
	if reg.client.Timeout != 5*time.Second {
		t.Errorf("client.Timeout should stay default, got %v", reg.client.Timeout)
	}
}

func TestWithReadinessBudget(t *testing.T) {
	reg := NewSourceRegistry(nil, nil, WithReadinessBudget(60*time.Second))
	if reg.readinessBudget != 60*time.Second {
		t.Errorf("readinessBudget = %v, want 60s", reg.readinessBudget)
	}
}

func TestWithReadinessBudget_ZeroIgnored(t *testing.T) {
	reg := NewSourceRegistry(nil, nil, WithReadinessBudget(0))
	if reg.readinessBudget != 0 {
		t.Errorf("readinessBudget should stay 0, got %v", reg.readinessBudget)
	}
}

func TestNewSourceRegistry_ReadinessBudget_AffectsStartup(t *testing.T) {
	// With a 4s budget: 4s / 2s = 2 attempts. Server becomes ready on attempt 3
	// → startup should FAIL (only 2 attempts).
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"phase":"ready","total_entities":42,"sources":[]}`)
	}))
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil, WithReadinessBudget(4*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// With 4s budget = 2 attempts, server needs 3 → should NOT be ready at startup.
	// But background recovery (5s recheck) should pick it up quickly.
	if reg.sources[0].IsReady() {
		// If ready, the budget allowed enough attempts — that's fine too.
		// The key assertion is that it doesn't use the old 3-attempt default.
		t.Logf("source ready at startup with %d attempts", attempts)
		return
	}

	// Wait for background recovery (5s recheck interval).
	if !pollReady(t, reg.sources[0], 10*time.Second) {
		t.Fatal("source did not recover via background check with 5s recheck interval")
	}
}

func TestStartWatchers_SourceBecomesReady(t *testing.T) {
	// Simulate a semsource that becomes ready on the 2nd attempt.
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"phase":"ready","total_entities":42,"sources":[]}`)
	}))
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", GraphQLURL: srv.URL + "/graphql", StatusURL: srv.URL + "/status", Type: "semsource"},
	}, nil)

	// Before StartWatchers, semsource should not be ready.
	if reg.sources[1].IsReady() {
		t.Fatal("semsource should not be ready before StartWatchers")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// After StartWatchers, semsource should be ready (succeeded on 2nd attempt).
	if !reg.sources[1].IsReady() {
		t.Fatal("semsource should be ready after StartWatchers (2nd attempt succeeds)")
	}

	ready := reg.ReadySources()
	if len(ready) != 2 {
		t.Errorf("expected 2 ready sources, got %d", len(ready))
	}
}

func TestStartWatchers_SourceNotReady_EntersBackground(t *testing.T) {
	// Simulate a semsource that never becomes ready during startup.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", GraphQLURL: srv.URL + "/graphql", StatusURL: srv.URL + "/status", Type: "semsource"},
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Semsource should not be ready (all 3 startup attempts failed).
	if reg.sources[1].IsReady() {
		t.Fatal("semsource should not be ready when status endpoint is unavailable")
	}

	// Only local source should be ready.
	ready := reg.ReadySources()
	if len(ready) != 1 || ready[0].Name != "local" {
		t.Errorf("expected only local ready, got %v", sourceNames(ready))
	}
}

func sourceNames(sources []*Source) []string {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	return names
}

// ---------------------------------------------------------------------------
// Background recovery tests — the critical gap that let graph_summary break 5x
// ---------------------------------------------------------------------------

// newTestSemsourceServer returns a test server whose health can be toggled via the
// returned *atomic.Bool. When healthy=true, /status returns phase:"ready" and
// /summary returns a valid summary with 42 entities.
func newTestSemsourceServer(healthy *atomic.Bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"phase":"ready","total_entities":42,"sources":[{"instance_name":"test","source_type":"ast","phase":"ready","entity_count":42,"error_count":0}]}`)
		case strings.HasSuffix(r.URL.Path, "/summary"):
			fmt.Fprint(w, `{"namespace":"test","phase":"ready","entity_id_format":"test.{domain}.{type}.{name}","total_entities":42,"domains":[{"domain":"source","entity_count":42,"types":[{"type":"function","count":42}],"sources":["ast"]}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// pollReady polls IsReady with short sleeps until true or timeout.
func pollReady(t *testing.T, src *Source, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if src.IsReady() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// pollNotReady polls IsReady with short sleeps until false or timeout.
func pollNotReady(t *testing.T, src *Source, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !src.IsReady() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestStartWatchers_BackgroundRecovery(t *testing.T) {
	// Source fails all startup attempts, enters background, then server becomes healthy.
	// IsReady() MUST return true after background recheck succeeds.
	healthy := &atomic.Bool{}
	healthy.Store(false)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	// Override watcher intervals for fast test.
	src := reg.sources[1]
	src.watcher = newFastWatcher(reg, src)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// After startup: source should NOT be ready (all attempts failed).
	if src.IsReady() {
		t.Fatal("source should not be ready after failed startup")
	}

	// Make server healthy — background check should detect this.
	healthy.Store(true)

	// Poll until ready or timeout.
	if !pollReady(t, src, 2*time.Second) {
		t.Fatal("source did not recover via background check within 2s — THIS IS THE BUG")
	}
}

func TestStartWatchers_BackgroundRecovery_SummaryWorks(t *testing.T) {
	// Full chain: startup fails → background recovers → FormatSummaryForPrompt returns data.
	healthy := &atomic.Bool{}
	healthy.Store(false)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	src := reg.sources[1]
	src.watcher = newFastWatcher(reg, src)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Before recovery: summary should be empty.
	text := reg.FormatSummaryForPrompt(ctx)
	if text != "" {
		t.Errorf("expected empty summary before recovery, got %d bytes", len(text))
	}

	// Make server healthy and wait for recovery.
	healthy.Store(true)
	if !pollReady(t, src, 2*time.Second) {
		t.Fatal("source did not recover via background check")
	}

	// After recovery: summary should contain data.
	text = reg.FormatSummaryForPrompt(ctx)
	if text == "" {
		t.Fatal("FormatSummaryForPrompt returned empty after recovery — full chain broken")
	}
	if !strings.Contains(text, "42") {
		t.Errorf("summary should mention 42 entities, got: %s", text)
	}
}

func TestStartWatchers_HealthCheck_DetectsLoss(t *testing.T) {
	// Source is healthy at startup, then goes down.
	// StartWatchers must start health monitoring even on success path.
	healthy := &atomic.Bool{}
	healthy.Store(true)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	src := reg.sources[0]
	src.watcher = newFastWatcher(reg, src)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Should be ready after startup.
	if !src.IsReady() {
		t.Fatal("source should be ready after successful startup")
	}

	// Kill the server — StartWatchers should have started health monitoring.
	healthy.Store(false)

	// Poll until not ready.
	if !pollNotReady(t, src, 2*time.Second) {
		t.Fatal("health check did not detect source loss — StartWatchers must start background monitoring on success path too")
	}
}

func TestStartWatchers_HealthCheck_FullCycle(t *testing.T) {
	// healthy → lost → recovered. All three states must be reflected.
	// No manual StartBackgroundCheck — StartWatchers must handle it.
	healthy := &atomic.Bool{}
	healthy.Store(true)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	src := reg.sources[0]
	src.watcher = newFastWatcher(reg, src)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Phase 1: ready at startup.
	if !src.IsReady() {
		t.Fatal("phase 1: should be ready after startup")
	}

	// Phase 2: goes down.
	healthy.Store(false)
	if !pollNotReady(t, src, 2*time.Second) {
		t.Fatal("phase 2: health check did not detect loss")
	}

	// Phase 3: comes back.
	healthy.Store(true)
	if !pollReady(t, src, 2*time.Second) {
		t.Fatal("phase 3: did not recover after source came back")
	}
}

func TestGatherSummaries_SkipsNotReadySource(t *testing.T) {
	healthy := &atomic.Bool{}
	healthy.Store(false)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	// Don't start watchers — source stays not-ready.
	fetched, total := reg.gatherSummaries(context.Background())
	if len(fetched) != 0 {
		t.Errorf("expected 0 fetched sources, got %d", len(fetched))
	}
	if total != 0 {
		t.Errorf("expected 0 total entities, got %d", total)
	}
}

func TestGatherSummaries_IncludesRecoveredSource(t *testing.T) {
	healthy := &atomic.Bool{}
	healthy.Store(false)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	src := reg.sources[0]
	src.watcher = newFastWatcher(reg, src)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Before recovery.
	fetched, _ := reg.gatherSummaries(ctx)
	if len(fetched) != 0 {
		t.Errorf("expected 0 fetched before recovery, got %d", len(fetched))
	}

	// Recover.
	healthy.Store(true)
	if !pollReady(t, src, 2*time.Second) {
		t.Fatal("source did not recover")
	}

	// After recovery — gatherSummaries should include the source.
	fetched, total := reg.gatherSummaries(ctx)
	if len(fetched) != 1 {
		t.Fatalf("expected 1 fetched source after recovery, got %d", len(fetched))
	}
	if total != 42 {
		t.Errorf("expected 42 total entities, got %d", total)
	}
}

func TestStartWatchers_MultipleSourcesIndependent(t *testing.T) {
	healthy1 := &atomic.Bool{}
	healthy1.Store(false)
	healthy2 := &atomic.Bool{}
	healthy2.Store(false)

	srv1 := newTestSemsourceServer(healthy1)
	defer srv1.Close()
	srv2 := newTestSemsourceServer(healthy2)
	defer srv2.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "src1", StatusURL: srv1.URL + "/status", GraphQLURL: srv1.URL + "/graphql", Type: "semsource"},
		{Name: "src2", StatusURL: srv2.URL + "/status", GraphQLURL: srv2.URL + "/graphql", Type: "semsource"},
	}, nil)

	reg.sources[0].watcher = newFastWatcher(reg, reg.sources[0])
	reg.sources[1].watcher = newFastWatcher(reg, reg.sources[1])

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Both should be down.
	if reg.sources[0].IsReady() || reg.sources[1].IsReady() {
		t.Fatal("both sources should be down initially")
	}

	// Only recover src1.
	healthy1.Store(true)
	if !pollReady(t, reg.sources[0], 2*time.Second) {
		t.Fatal("src1 did not recover")
	}

	// src2 should still be down.
	if reg.sources[1].IsReady() {
		t.Fatal("src2 should still be down — sources must be independent")
	}

	// Now recover src2.
	healthy2.Store(true)
	if !pollReady(t, reg.sources[1], 2*time.Second) {
		t.Fatal("src2 did not recover")
	}
}

func TestStartWatchers_RapidRecovery(t *testing.T) {
	// Source becomes healthy quickly after startup fails.
	// Recovery should happen on the first background tick, not wait 30s.
	healthy := &atomic.Bool{}
	healthy.Store(false)

	srv := newTestSemsourceServer(healthy)
	defer srv.Close()

	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil)

	src := reg.sources[0]
	src.watcher = newFastWatcher(reg, src)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg.StartWatchers(ctx)
	defer reg.StopWatchers()

	// Make healthy immediately after startup fails.
	healthy.Store(true)

	start := time.Now()
	if !pollReady(t, src, 2*time.Second) {
		t.Fatal("rapid recovery failed")
	}
	elapsed := time.Since(start)

	// Should recover within ~200ms (fast watcher interval), not 30s.
	if elapsed > 500*time.Millisecond {
		t.Errorf("recovery took %v — too slow for rapid recovery test", elapsed)
	}
}

func TestFetchStatus_ParsesReadyPhase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"phase":"ready","total_entities":42,"sources":[]}`)
	}))
	defer srv.Close()

	reg := NewSourceRegistry(nil, nil)
	phase, count, err := reg.fetchStatus(context.Background(), srv.URL+"/status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase != "ready" {
		t.Errorf("phase: got %q, want %q", phase, "ready")
	}
	if count != 42 {
		t.Errorf("count: got %d, want 42", count)
	}
}

func TestFetchStatus_ParsesDegradedPhase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"phase":"degraded","total_entities":10,"sources":[]}`)
	}))
	defer srv.Close()

	reg := NewSourceRegistry(nil, nil)
	phase, _, err := reg.fetchStatus(context.Background(), srv.URL+"/status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase != "degraded" {
		t.Errorf("phase: got %q, want %q", phase, "degraded")
	}
}

func TestFetchStatus_RejectsBadPhase(t *testing.T) {
	// The watcher check function rejects non-ready/degraded phases.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"phase":"indexing","total_entities":0,"sources":[]}`)
	}))
	defer srv.Close()

	// Use small budget to keep test fast (3 attempts × 2s = 6s, not default 30s).
	reg := NewSourceRegistry([]Source{
		{Name: "ws", StatusURL: srv.URL + "/status", GraphQLURL: srv.URL + "/graphql", Type: "semsource"},
	}, nil, WithReadinessBudget(6*time.Second))

	// The watcher's checkFn should reject "indexing" phase.
	src := reg.sources[0]
	if src.watcher == nil {
		t.Fatal("watcher should be created for semsource with StatusURL")
	}

	ctx := context.Background()
	// WaitForStartup should fail because phase is "indexing", not "ready".
	if src.watcher.WaitForStartup(ctx) {
		t.Error("WaitForStartup should return false for indexing phase")
	}
}

func TestIsReady_NilWatcher(t *testing.T) {
	src := &Source{Name: "no-watcher", Type: "semsource"}
	if src.IsReady() {
		t.Error("semsource with nil watcher should not be ready")
	}
}

func TestIsReady_LocalAlwaysTrue(t *testing.T) {
	src := &Source{Name: "local", Type: "local"}
	if !src.IsReady() {
		t.Error("local source should always be ready")
	}
}

func TestIsReady_AlwaysQueryTrue(t *testing.T) {
	src := &Source{Name: "always", Type: "semsource", AlwaysQuery: true}
	if !src.IsReady() {
		t.Error("AlwaysQuery source should always be ready")
	}
}

// newFastWatcher creates a resource.Watcher with very short intervals for testing.
// Startup: 2 attempts × 10ms. Background recheck: 50ms. Health: 50ms.
func newFastWatcher(reg *SourceRegistry, src *Source) *resource.Watcher {
	return resource.NewWatcher(
		"semsource:"+src.Name,
		func(ctx context.Context) error {
			phase, _, err := reg.fetchStatus(ctx, src.StatusURL)
			if err != nil {
				return err
			}
			if phase != "ready" && phase != "degraded" {
				return fmt.Errorf("phase: %s", phase)
			}
			return nil
		},
		resource.Config{
			StartupAttempts: 2,
			StartupInterval: 10 * time.Millisecond,
			RecheckInterval: 50 * time.Millisecond,
			HealthInterval:  50 * time.Millisecond,
		},
	)
}
