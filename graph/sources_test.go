package graph

import (
	"testing"
	"time"
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
		{Name: "workspace", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource", EntityPrefix: "semspec.semsource."},
	}, nil)

	// Mark semsource as ready for test.
	reg.sources[1].ready.Store(true)

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
		{Name: "ws", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource"},
	}, nil)

	reg.sources[1].ready.Store(true)

	sources := reg.SourcesForQuery("search", "", "")
	if len(sources) != 2 {
		t.Errorf("search fanout: expected 2 ready sources, got %d", len(sources))
	}
}

func TestSourcesForQuery_SummaryOnlySemsource(t *testing.T) {
	reg := NewSourceRegistry([]Source{
		{Name: "local", GraphQLURL: "http://local/graphql", Type: "local"},
		{Name: "ws", GraphQLURL: "http://ws/graphql", StatusURL: "http://ws/status", Type: "semsource"},
	}, nil)

	reg.sources[1].ready.Store(true)

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

func sourceNames(sources []*Source) []string {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	return names
}
