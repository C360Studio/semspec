package workflow

import (
	"context"
	"strings"
	"testing"
)

// ADR-035 audit site D.8 — pre-flight entity-ID shape validation. The
// fix forecloses the project_graph_query_truncated_id_wedge_2026_05_03
// wedge where a model called `entity(id: "semspec.semsou")` (truncated)
// and the gateway returned "not found:" with no actionable hint, causing
// the model to loop on the same broken ID for many iterations.

func TestValidateEntityIDShape(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		// Valid: 4+ dot-separated segments, no empty segments.
		{"plan id (6 segs)", "semspec.local.wf.plan.plan.abc123", false},
		{"requirement id (6 segs)", "semspec.local.wf.plan.requirement.req-1", false},
		{"task-run id (6 segs)", "semspec.local.exec.task.run.task-1", false},
		{"source-doc id (5 segs)", "semspec.local.source.doc.readme", false},
		{"source-code id (4 segs floor)", "semspec.local.source.code", false},

		// Truncation — primary wedge shape (2 segments).
		{"truncated to 2 segments — wedge fixture", "semspec.semsou", true},
		{"truncated to 1 segment", "semspec", true},
		{"truncated to 3 segments", "semspec.local.wf", true},

		// Empty-segment cases.
		{"leading dot", ".semspec.local.wf.plan", true},
		{"trailing dot", "semspec.local.wf.plan.", true},
		{"double dot", "semspec.local..plan.plan.abc", true},
		{"all empty", "...", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEntityIDShape(tt.id)
			if tt.wantErr && err == nil {
				t.Errorf("validateEntityIDShape(%q) returned nil; expected error", tt.id)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateEntityIDShape(%q) = %v; expected nil", tt.id, err)
			}
		})
	}
}

func TestValidateEntityIDsInQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		// No entity-ID args present — pass through.
		{
			name:    "globalSearch query",
			query:   `{ globalSearch(query: "auth handler") { answer } }`,
			wantErr: false,
		},
		{
			name:    "entitiesByPrefix with short prefix",
			query:   `{ entitiesByPrefix(prefix: "semspec.") { id } }`,
			wantErr: false,
		},
		{
			name:    "predicates summary, no IDs",
			query:   `{ predicates { predicates { predicate entityCount } total } }`,
			wantErr: false,
		},
		{
			name:    "introspection-style empty",
			query:   `{ }`,
			wantErr: false,
		},

		// Valid entity() / traverse() arg.
		{
			name:    "entity with valid plan id",
			query:   `{ entity(id: "semspec.local.wf.plan.plan.abc123") { triples { predicate object } } }`,
			wantErr: false,
		},
		{
			// Pin the field-selection `id` non-match: the inner `{ id triples }`
			// is a return-position selector with no colon after it, so the
			// regex must NOT treat it as an entity-ID argument. If a future
			// regex tweak accidentally matched it, valid queries with bare
			// field-`id` selections would start failing.
			name:    "field-selection id is not validated",
			query:   `{ entity(id: "semspec.local.wf.plan.plan.x") { id triples { predicate object } } }`,
			wantErr: false,
		},
		{
			name:    "traverse with valid start id",
			query:   `{ traverse(start: "semspec.local.exec.task.run.t1", depth: 2, direction: OUTBOUND) { nodes { id } } }`,
			wantErr: false,
		},

		// Truncated wedge — the primary failure shape.
		{
			name:    "wedge fixture — truncated to 2 segments",
			query:   `{ entity(id: "semspec.semsou") { triples { predicate object } } }`,
			wantErr: true,
		},
		{
			name:    "truncated traverse start",
			query:   `{ traverse(start: "semspec.local", depth: 1) { nodes { id } } }`,
			wantErr: true,
		},

		// Whitespace variants.
		{
			name:    "no space after colon",
			query:   `{ entity(id:"semspec.bar") { id } }`,
			wantErr: true, // 2 segments — bad ID, validation should still find it
		},
		{
			name:    "space before colon",
			query:   `{ entity(id : "semspec.local.wf.plan.plan.abc") { id } }`,
			wantErr: false, // valid 6-segment ID, regex should still match
		},
		{
			name:    "newline before value",
			query:   "{ entity(id:\n\"semspec.local.wf.plan.plan.abc\") { id } }",
			wantErr: false,
		},

		// Mixed-quote case — GraphQL only allows double quotes; single
		// quotes are not standard GraphQL string literals. The regex
		// intentionally does not match them, so a `id: 'foo'` in a query
		// would not be validated. Pin that behavior.
		{
			name:    "single quotes — not matched by regex",
			query:   `{ entity(id: 'too.short') { id } }`,
			wantErr: false, // regex doesn't match single quotes
		},

		// Variable bindings — runtime-bound, not statically validatable.
		{
			name:    "variable binding — id: $foo",
			query:   `query($foo: String!) { entity(id: $foo) { id } }`,
			wantErr: false,
		},

		// Multiple IDs, one bad — first invalid is reported.
		{
			name:    "multi-arg query, one truncated",
			query:   `{ a: entity(id: "semspec.local.wf.plan.plan.x") { id } b: entity(id: "semspec.bar") { id } }`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEntityIDsInQuery(tt.query)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for query %q, got nil", tt.query)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for query %q: %v", tt.query, err)
			}
		})
	}
}

// Regression-pin the load-bearing hints in the queryGraph error message:
// the canonical example ID and the entitiesByPrefix redirect. Without
// these the model has no path forward; with them it has a sharp template
// and an alternate query type to try.
func TestQueryGraph_TruncatedID_RejectsWithHelpfulHint(t *testing.T) {
	exec := &GraphExecutor{} // no registry — validation runs before any gateway call

	call := makeCall("c-trunc", "graph_query", map[string]any{
		"query": `{ entity(id: "semspec.semsou") { triples { predicate object } } }`,
	})
	result, err := exec.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected ToolResult.Error to be populated for truncated entity ID")
	}

	// Load-bearing hint content — these strings teach the model how to fix
	// its query on the next iteration.
	mustContain := []string{
		"semspec.local.wf.plan.plan.abc123", // canonical example ID
		"entitiesByPrefix",                  // alternate query type for partial lookups
		"graph_search",                      // alternate query type for natural language
		"semspec.semsou",                    // the bad ID itself, so the model knows what was rejected
	}
	for _, want := range mustContain {
		if !strings.Contains(result.Error, want) {
			t.Errorf("ToolResult.Error missing required hint substring %q\nfull error: %s", want, result.Error)
		}
	}
}

// Pin the introspect path: it should bypass entity-ID validation entirely
// because the schema response does not depend on a query argument.
func TestQueryGraph_Introspect_BypassesValidation(t *testing.T) {
	exec := &GraphExecutor{} // no registry; introspect returns the static schema

	call := makeCall("c-intro", "graph_query", map[string]any{
		"introspect": true,
	})
	result, err := exec.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("introspect should not produce an error, got: %s", result.Error)
	}
	if !strings.Contains(result.Content, "type Query") {
		t.Errorf("introspect should return the GraphQL schema; got: %s", result.Content)
	}
}

// Pin the empty-query path: an empty query argument with introspect=false
// returns the existing "query argument is required" error, NOT a
// validation error. The validation only fires when a query is present.
func TestQueryGraph_EmptyQuery_ReturnsExistingError(t *testing.T) {
	exec := &GraphExecutor{}

	call := makeCall("c-empty", "graph_query", map[string]any{
		"query": "",
	})
	result, err := exec.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Error, "query argument is required") {
		t.Errorf("expected 'query argument is required' error, got: %s", result.Error)
	}
}

// ---------------------------------------------------------------------
// ADR-035 D.8 follow-up: graph_query recovery hints
// ---------------------------------------------------------------------

// buildRecoveryPrefix is the helper that decides which prefix to query
// against when fuzzy-matching a failed entity ID. Pin the cap and the
// drop-last-segment logic.
func TestBuildRecoveryPrefix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "6-segment ID — caps at 4, drops last",
			in:   "semspec.semsource.code.workspace.file.main-go",
			want: "semspec.semsource.code.workspace.",
		},
		{
			name: "5-segment ID — drops last (4 left, under cap)",
			in:   "semspec.local.source.doc.readme",
			want: "semspec.local.source.doc.",
		},
		{
			name: "4-segment ID — drops last (3 left)",
			in:   "semspec.local.wf.plan",
			want: "semspec.local.wf.",
		},
		{
			name: "3-segment ID — drops last (2 left, still meaningful)",
			in:   "semspec.local.wf",
			want: "semspec.local.",
		},
		{
			name: "2-segment ID — too short, returns empty",
			in:   "semspec.bar",
			want: "",
		},
		{
			name: "1-segment ID — too short",
			in:   "semspec",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRecoveryPrefix(tt.in)
			if got != tt.want {
				t.Errorf("buildRecoveryPrefix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// extractEntityIDs unwraps the entitiesByPrefix response shape. Pin
// the happy and the malformed-shape paths.
func TestExtractEntityIDs(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want []string
	}{
		{
			name: "valid response with 2 entities",
			in: map[string]any{
				"entitiesByPrefix": []any{
					map[string]any{"id": "semspec.local.wf.plan.plan.a"},
					map[string]any{"id": "semspec.local.wf.plan.plan.b"},
				},
			},
			want: []string{"semspec.local.wf.plan.plan.a", "semspec.local.wf.plan.plan.b"},
		},
		{
			name: "missing entitiesByPrefix key",
			in:   map[string]any{"foo": "bar"},
			want: nil,
		},
		{
			name: "wrong type for entitiesByPrefix",
			in:   map[string]any{"entitiesByPrefix": "not an array"},
			want: nil,
		},
		{
			name: "entity without id field skipped",
			in: map[string]any{
				"entitiesByPrefix": []any{
					map[string]any{"label": "no id here"},
					map[string]any{"id": "semspec.x.y.z"},
					map[string]any{"id": ""}, // empty id skipped
				},
			},
			want: []string{"semspec.x.y.z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEntityIDs(tt.in)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d (got %v)", len(got), len(tt.want), got)
				return
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

// stringFromMetadata helper — small but worth pinning since both
// recovery paths use it.
func TestStringFromMetadata(t *testing.T) {
	tests := []struct {
		name string
		md   map[string]any
		key  string
		want string
	}{
		{"nil map", nil, "role", ""},
		{"empty map", map[string]any{}, "role", ""},
		{"string value", map[string]any{"role": "developer"}, "role", "developer"},
		{"non-string value", map[string]any{"role": 42}, "role", ""},
		{"missing key", map[string]any{"other": "x"}, "role", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringFromMetadata(tt.md, tt.key)
			if got != tt.want {
				t.Errorf("stringFromMetadata(%v, %q) = %q, want %q", tt.md, tt.key, got, tt.want)
			}
		})
	}
}

// RegisterMetrics is nil-safe and idempotent.
func TestRegisterMetrics_NilSafe(t *testing.T) {
	if err := RegisterMetrics(nil); err != nil {
		t.Errorf("nil registry should be no-op, got error: %v", err)
	}
}
