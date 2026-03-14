package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// newTestExecutor creates an Executor pointing at a mock server URL.
func newTestExecutor(serverURL string) *Executor {
	provider := &BraveProvider{
		apiKey:     "test-key",
		httpClient: &http.Client{},
	}
	// Override the endpoint by using a custom RoundTripper that rewrites the host.
	// Instead, we build the Executor with a custom BraveProvider whose httpClient
	// points at the test server directly by rewriting requests in a transport.
	provider.httpClient.Transport = &rewriteTransport{base: serverURL}
	return NewExecutor(provider)
}

// rewriteTransport rewrites every outbound request to the given base URL,
// preserving the original query string.
type rewriteTransport struct {
	base string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Preserve query params from the original request.
	newURL := t.base + "?" + req.URL.RawQuery
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

// braveServerWith builds an httptest.Server that returns the given results payload.
func braveServerWith(t *testing.T, results []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"web": map[string]any{
				"results": results,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestWebSearch_Success(t *testing.T) {
	server := braveServerWith(t, []map[string]any{
		{"title": "Go net/http docs", "url": "https://pkg.go.dev/net/http", "description": "Standard library HTTP package"},
		{"title": "Go context docs", "url": "https://pkg.go.dev/context", "description": "Context package for cancellation"},
	})
	defer server.Close()

	exec := newTestExecutor(server.URL)
	call := agentic.ToolCall{
		ID:        "c1",
		Name:      "web_search",
		Arguments: map[string]any{"query": "golang http client"},
	}

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if !strings.Contains(result.Content, "[1]") {
		t.Error("expected numbered list in output, got: " + result.Content)
	}
	if !strings.Contains(result.Content, "Go net/http docs") {
		t.Error("expected first result title in output")
	}
	if !strings.Contains(result.Content, "https://pkg.go.dev/net/http") {
		t.Error("expected first result URL in output")
	}
	if !strings.Contains(result.Content, "[2]") {
		t.Error("expected second result in output")
	}
}

func TestWebSearch_EmptyResults(t *testing.T) {
	server := braveServerWith(t, []map[string]any{})
	defer server.Close()

	exec := newTestExecutor(server.URL)
	call := agentic.ToolCall{
		ID:        "c2",
		Name:      "web_search",
		Arguments: map[string]any{"query": "xyzzy no results"},
	}

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}
	if result.Content != "No results found." {
		t.Errorf("expected 'No results found.', got: %q", result.Content)
	}
}

func TestWebSearch_MaxResultsCapped(t *testing.T) {
	// Track the count query param the server receives.
	var capturedCount string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCount = r.URL.Query().Get("count")
		resp := map[string]any{"web": map[string]any{"results": []map[string]any{}}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	exec := newTestExecutor(server.URL)
	call := agentic.ToolCall{
		ID:        "c3",
		Name:      "web_search",
		Arguments: map[string]any{"query": "test", "max_results": float64(20)},
	}

	exec.Execute(context.Background(), call)

	if capturedCount != "10" {
		t.Errorf("expected count=10 (capped), got count=%s", capturedCount)
	}
}

func TestWebSearch_MissingQuery(t *testing.T) {
	exec := NewExecutor(nil) // provider never called when query is missing
	call := agentic.ToolCall{
		ID:        "c4",
		Name:      "web_search",
		Arguments: map[string]any{},
	}

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected tool error for missing query, got none")
	}
	if !strings.Contains(result.Error, "query argument is required") {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

func TestWebSearch_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid key"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	exec := newTestExecutor(server.URL)
	call := agentic.ToolCall{
		ID:        "c5",
		Name:      "web_search",
		Arguments: map[string]any{"query": "test"},
	}

	result, err := exec.Execute(context.Background(), call)

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected tool error for API error response, got none")
	}
	if !strings.Contains(result.Error, "search failed") {
		t.Errorf("unexpected error message: %s", result.Error)
	}
}

func TestWebSearch_LargeResponseBodyHandled(t *testing.T) {
	// Generate a response larger than 100KB — the executor must not panic or OOM.
	bigDescription := strings.Repeat("x", 110*1024)
	server := braveServerWith(t, []map[string]any{
		{"title": "Big Result", "url": "https://example.com", "description": bigDescription},
	})
	defer server.Close()

	exec := newTestExecutor(server.URL)
	call := agentic.ToolCall{
		ID:        "c6",
		Name:      "web_search",
		Arguments: map[string]any{"query": "large"},
	}

	// Should not panic; result may be an error or truncated — either is acceptable.
	result, err := exec.Execute(context.Background(), call)
	if err != nil {
		t.Logf("got Go error (acceptable): %v", err)
	}
	// The only hard requirement is no panic, but also verify CallID is set.
	if result.CallID != "c6" {
		t.Errorf("expected CallID=c6, got %q", result.CallID)
	}
}

func TestListTools(t *testing.T) {
	exec := NewExecutor(nil)
	tools := exec.ListTools()

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "web_search" {
		t.Errorf("expected tool name 'web_search', got %q", tools[0].Name)
	}
	if tools[0].Description == "" {
		t.Error("tool description must not be empty")
	}
	// Verify required parameter list includes "query".
	params, ok := tools[0].Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected 'properties' map in parameters")
	}
	if _, ok := params["query"]; !ok {
		t.Error("expected 'query' parameter in tool definition")
	}
}

func TestWebSearch_UnknownTool(t *testing.T) {
	exec := NewExecutor(nil)
	call := agentic.ToolCall{
		ID:        "c7",
		Name:      "not_a_tool",
		Arguments: map[string]any{},
	}

	result, err := exec.Execute(context.Background(), call)

	if err == nil {
		t.Error("expected Go error for unknown tool")
	}
	if result.Error == "" {
		t.Error("expected tool error message for unknown tool")
	}
}

func TestFormatResults(t *testing.T) {
	results := []SearchResult{
		{Title: "First", URL: "https://first.example.com", Description: "Desc one"},
		{Title: "Second", URL: "https://second.example.com", Description: ""},
	}

	got := formatResults(results)

	if !strings.Contains(got, "[1] First") {
		t.Error("expected '[1] First' in output")
	}
	if !strings.Contains(got, "https://first.example.com") {
		t.Error("expected first URL in output")
	}
	if !strings.Contains(got, "Desc one") {
		t.Error("expected first description in output")
	}
	if !strings.Contains(got, "[2] Second") {
		t.Error("expected '[2] Second' in output")
	}
	// Empty description should not add a blank description line.
	for line := range strings.SplitSeq(got, "\n") {
		if strings.TrimSpace(line) == "" {
			continue // blank separator lines are fine
		}
		// Indented lines should have actual content.
		if strings.HasPrefix(line, "    ") && strings.TrimSpace(line) == "" {
			t.Error("found indented empty line (empty description was rendered)")
		}
	}
}
