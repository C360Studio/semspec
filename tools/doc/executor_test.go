package doc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// newTestExecutor creates an Executor configured for testing with a mock server URL.
func newTestExecutor(serverURL, sourcesDir string) *Executor {
	return &Executor{
		gatewayURL: serverURL,
		sourcesDir: sourcesDir,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func TestListTools(t *testing.T) {
	executor := NewExecutor("/tmp/sources")
	tools := executor.ListTools()

	if len(tools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expected := []string{"doc_import", "doc_list", "doc_search", "doc_get"}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestDocImport(t *testing.T) {
	// Create mock server for HTTP gateway
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sources/ingest" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status": "accepted", "id": "doc.test.abc123"}`))
	}))
	defer server.Close()

	executor := newTestExecutor(server.URL, "/tmp/sources")

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name: "import with path",
			args: map[string]any{
				"path": "docs/test.md",
			},
			wantErr: false,
		},
		{
			name: "import with project_id",
			args: map[string]any{
				"path":       "docs/sop.md",
				"project_id": "semspec.local.project.my-project",
			},
			wantErr: false,
		},
		{
			name:    "import without path",
			args:    map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := agentic.ToolCall{
				ID:        "test-call",
				Name:      "doc_import",
				Arguments: tt.args,
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}
			}
		})
	}
}

func TestDocList(t *testing.T) {
	// Create mock GraphQL server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		response := map[string]any{
			"data": map[string]any{
				"entities": []any{
					map[string]any{
						"id": "doc.test.abc123",
						"triples": []any{
							map[string]any{"predicate": "source.name", "object": "Test Doc"},
							map[string]any{"predicate": "source.doc.category", "object": "sop"},
							map[string]any{"predicate": "source.status", "object": "ready"},
						},
					},
					map[string]any{
						"id": "doc.another.def456",
						"triples": []any{
							map[string]any{"predicate": "source.name", "object": "Another Doc"},
							map[string]any{"predicate": "source.doc.category", "object": "reference"},
							map[string]any{"predicate": "source.status", "object": "ready"},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	executor := newTestExecutor(server.URL, "/tmp/sources")

	tests := []struct {
		name         string
		args         map[string]any
		wantErr      bool
		wantMinCount int
	}{
		{
			name:         "list all documents",
			args:         map[string]any{},
			wantErr:      false,
			wantMinCount: 2,
		},
		{
			name: "list with limit",
			args: map[string]any{
				"limit": float64(10),
			},
			wantErr:      false,
			wantMinCount: 2,
		},
		{
			name: "list by category",
			args: map[string]any{
				"category": "sop",
			},
			wantErr:      false,
			wantMinCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := agentic.ToolCall{
				ID:        "test-call",
				Name:      "doc_list",
				Arguments: tt.args,
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}

				var docs []map[string]any
				if err := json.Unmarshal([]byte(result.Content), &docs); err != nil {
					t.Fatalf("failed to parse result: %v", err)
				}

				if len(docs) < tt.wantMinCount {
					t.Errorf("expected at least %d documents, got %d", tt.wantMinCount, len(docs))
				}
			}
		})
	}
}

func TestDocSearch(t *testing.T) {
	// Create mock GraphQL server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		response := map[string]any{
			"data": map[string]any{
				"entities": []any{
					map[string]any{
						"id": "doc.error-handling.abc123",
						"triples": []any{
							map[string]any{"predicate": "source.name", "object": "Error Handling SOP"},
							map[string]any{"predicate": "source.doc.category", "object": "sop"},
							map[string]any{"predicate": "source.status", "object": "ready"},
							map[string]any{"predicate": "source.doc.summary", "object": "Guidelines for error handling in Go applications"},
							map[string]any{"predicate": "source.doc.domain", "object": []any{"error-handling", "go"}},
						},
					},
					map[string]any{
						"id": "doc.logging.def456",
						"triples": []any{
							map[string]any{"predicate": "source.name", "object": "Logging Best Practices"},
							map[string]any{"predicate": "source.doc.category", "object": "reference"},
							map[string]any{"predicate": "source.status", "object": "ready"},
							map[string]any{"predicate": "source.doc.summary", "object": "Standard logging patterns for services"},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	executor := newTestExecutor(server.URL, "/tmp/sources")

	tests := []struct {
		name         string
		args         map[string]any
		wantErr      bool
		wantMinCount int
	}{
		{
			name: "search by query",
			args: map[string]any{
				"query": "error",
			},
			wantErr:      false,
			wantMinCount: 1,
		},
		{
			name: "search by query case insensitive",
			args: map[string]any{
				"query": "ERROR",
			},
			wantErr:      false,
			wantMinCount: 1,
		},
		{
			name: "search with domain filter",
			args: map[string]any{
				"query":  "error",
				"domain": []any{"error-handling"},
			},
			wantErr:      false,
			wantMinCount: 1,
		},
		{
			name:    "search without query",
			args:    map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := agentic.ToolCall{
				ID:        "test-call",
				Name:      "doc_search",
				Arguments: tt.args,
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}

				var docs []map[string]any
				if err := json.Unmarshal([]byte(result.Content), &docs); err != nil {
					t.Fatalf("failed to parse result: %v", err)
				}

				if len(docs) < tt.wantMinCount {
					t.Errorf("expected at least %d documents, got %d", tt.wantMinCount, len(docs))
				}
			}
		})
	}
}

func TestDocGet(t *testing.T) {
	// Create mock GraphQL server
	entityRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		entityRequests++
		var response map[string]any

		if entityRequests == 1 {
			// First request: get document entity
			response = map[string]any{
				"data": map[string]any{
					"entity": map[string]any{
						"id": "doc.test.abc123",
						"triples": []any{
							map[string]any{"predicate": "source.name", "object": "Test Doc"},
							map[string]any{"predicate": "source.doc.category", "object": "sop"},
							map[string]any{"predicate": "source.doc.chunk_count", "object": float64(2)},
						},
					},
				},
			}
		} else {
			// Second request: get chunks
			response = map[string]any{
				"data": map[string]any{
					"entities": []any{
						map[string]any{
							"id": "doc.test.abc123.chunk.1",
							"triples": []any{
								map[string]any{"predicate": "code.structure.belongs", "object": "doc.test.abc123"},
								map[string]any{"predicate": "source.doc.content", "object": "First chunk content"},
								map[string]any{"predicate": "source.doc.chunk_index", "object": float64(1)},
							},
						},
						map[string]any{
							"id": "doc.test.abc123.chunk.2",
							"triples": []any{
								map[string]any{"predicate": "code.structure.belongs", "object": "doc.test.abc123"},
								map[string]any{"predicate": "source.doc.content", "object": "Second chunk content"},
								map[string]any{"predicate": "source.doc.chunk_index", "object": float64(2)},
							},
						},
					},
				},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	executor := newTestExecutor(server.URL, "/tmp/sources")

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name: "get existing document",
			args: map[string]any{
				"entity_id": "doc.test.abc123",
			},
			wantErr: false,
		},
		{
			name:    "get without entity_id",
			args:    map[string]any{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityRequests = 0 // Reset counter

			call := agentic.ToolCall{
				ID:        "test-call",
				Name:      "doc_get",
				Arguments: tt.args,
			}

			result, _ := executor.Execute(context.Background(), call)

			if tt.wantErr {
				if result.Error == "" {
					t.Error("expected error, got none")
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}

				var doc map[string]any
				if err := json.Unmarshal([]byte(result.Content), &doc); err != nil {
					t.Fatalf("failed to parse result: %v", err)
				}

				if _, ok := doc["document"]; !ok {
					t.Error("response missing 'document' field")
				}
				if _, ok := doc["chunks"]; !ok {
					t.Error("response missing 'chunks' field")
				}
			}
		})
	}
}

func TestDocGetNotFound(t *testing.T) {
	// Create mock GraphQL server that returns null entity
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"data": map[string]any{
				"entity": nil,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	executor := newTestExecutor(server.URL, "/tmp/sources")

	call := agentic.ToolCall{
		ID:        "test-call",
		Name:      "doc_get",
		Arguments: map[string]any{"entity_id": "doc.nonexistent.xyz"},
	}

	result, _ := executor.Execute(context.Background(), call)

	if result.Error == "" {
		t.Error("expected error for non-existent document")
	}
	if result.Error != "document not found: doc.nonexistent.xyz" {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

func TestUnknownTool(t *testing.T) {
	executor := NewExecutor("/tmp/sources")

	call := agentic.ToolCall{
		ID:        "test-call",
		Name:      "unknown_tool",
		Arguments: map[string]any{},
	}

	result, err := executor.Execute(context.Background(), call)

	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if result.Error == "" {
		t.Error("expected error message for unknown tool")
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		name   string
		obj    any
		query  string
		expect bool
	}{
		{
			name:   "string match",
			obj:    "Hello World",
			query:  "hello",
			expect: true,
		},
		{
			name:   "string no match",
			obj:    "Hello World",
			query:  "foo",
			expect: false,
		},
		{
			name:   "array match",
			obj:    []any{"foo", "bar", "baz"},
			query:  "bar",
			expect: true,
		},
		{
			name:   "array no match",
			obj:    []any{"foo", "bar", "baz"},
			query:  "qux",
			expect: false,
		},
		{
			name:   "nil object",
			obj:    nil,
			query:  "test",
			expect: false,
		},
		{
			name:   "empty query",
			obj:    "test",
			query:  "",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsIgnoreCase(tt.obj, tt.query)
			if got != tt.expect {
				t.Errorf("containsIgnoreCase(%v, %q) = %v, want %v", tt.obj, tt.query, got, tt.expect)
			}
		})
	}
}
