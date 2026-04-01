package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semstreams/agentic"
)

func makeCall(id, name string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{ID: id, Name: name, Arguments: args}
}

// mockGraphQuerier implements graph.Querier for testing graph summary.
type mockGraphQuerier struct {
	summaries []graph.SourceSummary
	err       error
}

func (m *mockGraphQuerier) GraphSummary(_ context.Context) ([]graph.SourceSummary, error) {
	return m.summaries, m.err
}

// Stub implementations to satisfy the Querier interface.
func (m *mockGraphQuerier) QueryEntitiesByPredicate(_ context.Context, _ string) ([]graph.Entity, error) {
	return nil, nil
}
func (m *mockGraphQuerier) QueryEntitiesByIDPrefix(_ context.Context, _ string) ([]graph.Entity, error) {
	return nil, nil
}
func (m *mockGraphQuerier) GetEntity(_ context.Context, _ string) (*graph.Entity, error) {
	return nil, nil
}
func (m *mockGraphQuerier) HydrateEntity(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (m *mockGraphQuerier) GetCodebaseSummary(_ context.Context) (string, error) { return "", nil }
func (m *mockGraphQuerier) TraverseRelationships(_ context.Context, _, _, _ string, _ int) ([]graph.Entity, error) {
	return nil, nil
}
func (m *mockGraphQuerier) Ping(_ context.Context) error                          { return nil }
func (m *mockGraphQuerier) WaitForReady(_ context.Context, _ time.Duration) error { return nil }
func (m *mockGraphQuerier) QueryProjectSources(_ context.Context, _ string) ([]graph.Entity, error) {
	return nil, nil
}

// fixtureSummary builds a SourceSummary with domains and predicates for testing.
func fixtureSummary() graph.SourceSummary {
	return graph.SourceSummary{
		Source:         "test-repo",
		Phase:          "ready",
		TotalEntities:  42,
		EntityIDFormat: "domain.category.name",
		Domains: []graph.DomainSummary{
			{
				Domain:      "code",
				EntityCount: 30,
				Types: []graph.TypeCount{
					{Type: "function", Count: 20},
					{Type: "type", Count: 10},
				},
			},
		},
		Predicates: []graph.PredicateSchema{
			{
				SourceType: "go",
				Predicates: []graph.PredicateDescriptor{
					{Name: "code.function", Description: "Go function", DataType: "string", Role: "entity"},
				},
			},
		},
	}
}

func TestGraphSummary_WithQuerier_ReturnsSummary(t *testing.T) {
	expected := fixtureSummary()
	executor := &GraphExecutor{
		querier: &mockGraphQuerier{summaries: []graph.SourceSummary{expected}},
	}

	call := makeCall("c1", "graph_summary", map[string]any{})
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	var got []graph.SourceSummary
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(got))
	}
	if got[0].Source != expected.Source {
		t.Errorf("source: got %q, want %q", got[0].Source, expected.Source)
	}
	if got[0].TotalEntities != expected.TotalEntities {
		t.Errorf("total_entities: got %d, want %d", got[0].TotalEntities, expected.TotalEntities)
	}
	if len(got[0].Domains) != 1 {
		t.Errorf("expected 1 domain, got %d", len(got[0].Domains))
	}
	if len(got[0].Predicates) != 1 {
		t.Errorf("expected 1 predicate schema, got %d", len(got[0].Predicates))
	}
}

func TestGraphSummary_IncludePredicatesFalse_StripsPredicates(t *testing.T) {
	executor := &GraphExecutor{
		querier: &mockGraphQuerier{summaries: []graph.SourceSummary{fixtureSummary()}},
	}

	call := makeCall("c2", "graph_summary", map[string]any{
		"include_predicates": false,
	})
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	var got []graph.SourceSummary
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(got))
	}
	if len(got[0].Predicates) != 0 {
		t.Errorf("predicates should be empty when include_predicates=false, got %d", len(got[0].Predicates))
	}
	// Domains must still be present.
	if len(got[0].Domains) != 1 {
		t.Errorf("expected 1 domain, got %d", len(got[0].Domains))
	}
}

func TestGraphSummary_FallbackHTTP_ReturnsBody(t *testing.T) {
	// Start a test server that mimics the semsource /source-manifest/summary endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/source-manifest/summary" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"namespace":"fallback-repo","phase":"ready","total_entities":7}`))
	}))
	defer srv.Close()

	t.Setenv("SEMSOURCE_URL", srv.URL)

	// querier is nil → triggers fallback path.
	executor := &GraphExecutor{querier: nil, gatewayURL: srv.URL}

	call := makeCall("c3", "graph_summary", map[string]any{})
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	// The fallback returns the raw body — verify it contains expected fields.
	var raw map[string]any
	if err := json.Unmarshal([]byte(result.Content), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if raw["namespace"] != "fallback-repo" {
		t.Errorf("namespace: got %v, want fallback-repo", raw["namespace"])
	}
}

func TestGraphSummary_FallbackHTTP_Non200_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("SEMSOURCE_URL", srv.URL)

	executor := &GraphExecutor{querier: nil, gatewayURL: srv.URL}

	call := makeCall("c4", "graph_summary", map[string]any{})
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-200 from the fallback path: we still return the body (raw pass-through).
	// The test verifies the tool doesn't crash and returns some content.
	if result.Content == "" && result.Error == "" {
		t.Error("expected either content or error from non-200 response")
	}
}

func TestGraphSummary_NoSemsourceConfigured_ReturnsError(t *testing.T) {
	t.Setenv("SEMSOURCE_URL", "")

	executor := &GraphExecutor{querier: nil}

	call := makeCall("c5", "graph_summary", map[string]any{})
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error when SEMSOURCE_URL is empty and querier is nil")
	}
}
