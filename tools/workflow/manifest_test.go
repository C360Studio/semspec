package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// predicatesResponse builds a well-formed GraphQL predicates response body.
func predicatesResponse(t *testing.T, predicates []struct {
	Predicate   string
	EntityCount int
}, total int) []byte {
	t.Helper()

	type predEntry struct {
		Predicate   string `json:"predicate"`
		EntityCount int    `json:"entityCount"`
	}
	type responseShape struct {
		Data struct {
			Predicates struct {
				Predicates []predEntry `json:"predicates"`
				Total      int         `json:"total"`
			} `json:"predicates"`
		} `json:"data"`
	}

	var entries []predEntry
	for _, p := range predicates {
		entries = append(entries, predEntry{Predicate: p.Predicate, EntityCount: p.EntityCount})
	}

	var resp responseShape
	resp.Data.Predicates.Predicates = entries
	resp.Data.Predicates.Total = total

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("predicatesResponse: marshal: %v", err)
	}
	return b
}

// newTestClient creates a ManifestClient pointed at srv.URL with no logger.
func newTestClient(srv *httptest.Server) *ManifestClient {
	c := NewManifestClient(srv.URL, nil)
	// Use a shorter HTTP timeout so tests don't stall.
	c.httpClient = &http.Client{Timeout: 2 * time.Second}
	return c
}

// ---------------------------------------------------------------------------
// Fetch: successful response
// ---------------------------------------------------------------------------

func TestManifestClient_Fetch_Success(t *testing.T) {
	preds := []struct {
		Predicate   string
		EntityCount int
	}{
		{"code.function.main.Run", 120},
		{"code.type.main.Server", 38},
		{"code.interface.io.Reader", 5},
		{"source.doc.architecture", 12},
		{"source.doc.api", 8},
		{"semspec.plan.status", 3},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(predicatesResponse(t, preds, len(preds)))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	manifest := client.Fetch(context.Background())

	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}

	// Family grouping: "code" should aggregate all three code predicates.
	wantCodeEntities := 120 + 38 + 5
	if got := manifest.PredicateFamilies["code"]; got != wantCodeEntities {
		t.Errorf("code family entity count: got %d, want %d", got, wantCodeEntities)
	}

	// "source" family should aggregate both source predicates.
	wantSourceEntities := 12 + 8
	if got := manifest.PredicateFamilies["source"]; got != wantSourceEntities {
		t.Errorf("source family entity count: got %d, want %d", got, wantSourceEntities)
	}

	// "semspec" family.
	if got := manifest.PredicateFamilies["semspec"]; got != 3 {
		t.Errorf("semspec family entity count: got %d, want 3", got)
	}

	// Category extraction: two-level key "code.function".
	if got := manifest.PredicateCategories["code.function"]; got != 120 {
		t.Errorf("code.function category: got %d, want 120", got)
	}
	if got := manifest.PredicateCategories["code.type"]; got != 38 {
		t.Errorf("code.type category: got %d, want 38", got)
	}

	// TotalPredicates counts non-excluded predicates.
	if manifest.TotalPredicates != len(preds) {
		t.Errorf("TotalPredicates: got %d, want %d", manifest.TotalPredicates, len(preds))
	}
}

// ---------------------------------------------------------------------------
// Fetch: excluded predicates are filtered
// ---------------------------------------------------------------------------

func TestManifestClient_Fetch_ExcludedPredicatesFiltered(t *testing.T) {
	preds := []struct {
		Predicate   string
		EntityCount int
	}{
		{"source.doc.architecture", 10},
		{"source.doc.chunk_index", 999},   // excluded
		{"source.doc.chunk_count", 999},   // excluded
		{"source.doc.etag", 999},          // excluded
		{"source.doc.content_hash", 999},  // excluded
		{"source.doc.error", 999},         // excluded
		{"source.doc.raw_content", 999},   // excluded
		{"code.function.main.Run", 50},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(predicatesResponse(t, preds, len(preds)))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	manifest := client.Fetch(context.Background())

	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}

	// Only non-excluded predicates counted.
	if manifest.TotalPredicates != 2 {
		t.Errorf("TotalPredicates after exclusion: got %d, want 2", manifest.TotalPredicates)
	}

	// Excluded entity counts must not bleed into families.
	if got := manifest.PredicateFamilies["source"]; got != 10 {
		t.Errorf("source family should only include non-excluded predicates: got %d, want 10", got)
	}
	if got := manifest.PredicateFamilies["code"]; got != 50 {
		t.Errorf("code family: got %d, want 50", got)
	}
}

// ---------------------------------------------------------------------------
// Fetch: cache returns fresh data without re-querying
// ---------------------------------------------------------------------------

func TestManifestClient_Fetch_CacheHit(t *testing.T) {
	var callCount atomic.Int32

	preds := []struct {
		Predicate   string
		EntityCount int
	}{
		{"code.function.pkg.Foo", 7},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(predicatesResponse(t, preds, len(preds)))
	}))
	defer srv.Close()

	client := newTestClient(srv)

	first := client.Fetch(context.Background())
	if first == nil {
		t.Fatal("first fetch returned nil")
	}

	second := client.Fetch(context.Background())
	if second == nil {
		t.Fatal("second fetch returned nil")
	}

	// Both calls should return the same pointer (cached).
	if first != second {
		t.Error("expected second Fetch to return cached pointer")
	}

	// Only one HTTP call should have been made.
	if n := callCount.Load(); n != 1 {
		t.Errorf("HTTP call count: got %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Fetch: stale cache returned on fetch failure
// ---------------------------------------------------------------------------

func TestManifestClient_Fetch_StaleOnFailure(t *testing.T) {
	var failNext atomic.Bool

	preds := []struct {
		Predicate   string
		EntityCount int
	}{
		{"code.function.pkg.Bar", 42},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failNext.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(predicatesResponse(t, preds, len(preds)))
	}))
	defer srv.Close()

	client := newTestClient(srv)

	// First fetch succeeds — populates cache.
	first := client.Fetch(context.Background())
	if first == nil {
		t.Fatal("initial fetch returned nil")
	}

	// Expire the cache manually.
	client.mu.Lock()
	client.cachedAt = time.Now().Add(-manifestCacheTTL - time.Second)
	client.mu.Unlock()

	// Subsequent fetch will fail; expect stale data back.
	failNext.Store(true)
	second := client.Fetch(context.Background())
	if second == nil {
		t.Fatal("expected stale manifest on fetch failure, got nil")
	}

	if second.PredicateFamilies["code"] != 42 {
		t.Errorf("stale manifest family count: got %d, want 42", second.PredicateFamilies["code"])
	}
}

// ---------------------------------------------------------------------------
// HasKnowledge
// ---------------------------------------------------------------------------

func TestGraphManifest_HasKnowledge(t *testing.T) {
	tests := []struct {
		name     string
		manifest *GraphManifest
		want     bool
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			want:     false,
		},
		{
			name:     "empty families",
			manifest: &GraphManifest{PredicateFamilies: map[string]int{}},
			want:     false,
		},
		{
			name: "all-zero counts",
			manifest: &GraphManifest{
				PredicateFamilies: map[string]int{"code": 0, "source": 0},
			},
			want: false,
		},
		{
			name: "at least one non-zero family",
			manifest: &GraphManifest{
				PredicateFamilies: map[string]int{"code": 5, "source": 0},
			},
			want: true,
		},
		{
			name: "all families non-zero",
			manifest: &GraphManifest{
				PredicateFamilies: map[string]int{"code": 100, "source": 20},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.manifest.HasKnowledge(); got != tt.want {
				t.Errorf("HasKnowledge() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FormatForPrompt
// ---------------------------------------------------------------------------

func TestGraphManifest_FormatForPrompt(t *testing.T) {
	t.Run("nil manifest returns empty string", func(t *testing.T) {
		var m *GraphManifest
		if got := m.FormatForPrompt(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("empty families returns empty string", func(t *testing.T) {
		m := &GraphManifest{PredicateFamilies: map[string]int{}}
		if got := m.FormatForPrompt(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("non-empty manifest produces expected sections", func(t *testing.T) {
		m := &GraphManifest{
			PredicateFamilies: map[string]int{
				"code":   163,
				"source": 20,
			},
			PredicateCategories: map[string]int{
				"code.function":  120,
				"code.type":      38,
				"code.interface": 5,
				"source.doc":     20,
			},
			TotalPredicates: 4,
		}

		output := m.FormatForPrompt()

		requiredSubstrings := []string{
			"--- Knowledge Graph Contents ---",
			"2 predicate families indexed",
			"183 entities",
			"code (163)",
			"source (20)",
			"workflow_get_codebase_summary",
			"workflow_query_graph",
			"entitiesByPredicate",
		}

		for _, sub := range requiredSubstrings {
			if !strings.Contains(output, sub) {
				t.Errorf("FormatForPrompt output missing %q\nGot:\n%s", sub, output)
			}
		}

		// Families must appear in sorted order.
		codePos := strings.Index(output, "code (163)")
		sourcePos := strings.Index(output, "source (20)")
		if codePos == -1 || sourcePos == -1 {
			t.Fatal("expected both families in output")
		}
		if codePos > sourcePos {
			t.Error("families are not sorted alphabetically (code should precede source)")
		}
	})

	t.Run("family with no matching categories still renders", func(t *testing.T) {
		m := &GraphManifest{
			PredicateFamilies:   map[string]int{"semspec": 7},
			PredicateCategories: map[string]int{}, // no categories for semspec
			TotalPredicates:     1,
		}

		output := m.FormatForPrompt()
		if !strings.Contains(output, "semspec (7)") {
			t.Errorf("expected 'semspec (7)' in output, got:\n%s", output)
		}
	})
}

// ---------------------------------------------------------------------------
// categoriesForFamily
// ---------------------------------------------------------------------------

func TestCategoriesForFamily(t *testing.T) {
	categories := map[string]int{
		"code.function":  120,
		"code.type":      38,
		"code.interface": 5,
		"source.doc":     20,
		"semspec.plan":   3,
	}

	t.Run("returns sorted category labels for matching family", func(t *testing.T) {
		got := categoriesForFamily(categories, "code")
		// Expect sorted: "function (120)", "interface (5)", "type (38)"
		want := []string{"function (120)", "interface (5)", "type (38)"}
		if len(got) != len(want) {
			t.Fatalf("length mismatch: got %v, want %v", got, want)
		}
		for i, w := range want {
			if got[i] != w {
				t.Errorf("[%d]: got %q, want %q", i, got[i], w)
			}
		}
	})

	t.Run("returns empty slice for unknown family", func(t *testing.T) {
		got := categoriesForFamily(categories, "unknown")
		if len(got) != 0 {
			t.Errorf("expected empty slice, got %v", got)
		}
	})

	t.Run("does not include unrelated families as subcategories", func(t *testing.T) {
		got := categoriesForFamily(categories, "source")
		if len(got) != 1 || got[0] != "doc (20)" {
			t.Errorf("unexpected categories for source: %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// NewManifestClient: nil for empty URL
// ---------------------------------------------------------------------------

func TestNewManifestClient_EmptyURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if c := NewManifestClient(tt.url, nil); c != nil {
				t.Errorf("expected nil for URL %q, got non-nil client", tt.url)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Fetch: GraphQL error response is handled gracefully
// ---------------------------------------------------------------------------

func TestManifestClient_Fetch_GraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errors":[{"message":"unknown field 'predicates'"}]}`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	manifest := client.Fetch(context.Background())

	// Error response should return nil (no stale to fall back to).
	if manifest != nil {
		t.Errorf("expected nil manifest on GraphQL error, got %+v", manifest)
	}
}
