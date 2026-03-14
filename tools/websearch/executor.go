package websearch

import (
	"context"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

const (
	toolName           = "web_search"
	defaultMaxResults  = 5
)

// Executor implements the web_search agentic tool.
type Executor struct {
	provider SearchProvider
}

// NewExecutor creates a new websearch executor backed by the given provider.
func NewExecutor(provider SearchProvider) *Executor {
	return &Executor{provider: provider}
}

// Execute dispatches tool calls to the appropriate handler.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case toolName:
		return e.webSearch(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions exposed by this executor.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        toolName,
			Description: "Search the web for documentation, API references, libraries, or technical solutions. Returns titles, URLs, and descriptions for matching results.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query — be specific for best results",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"description": "Maximum results to return (default 5, max 10)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// webSearch executes the web_search tool call.
func (e *Executor) webSearch(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	query, ok := call.Arguments["query"].(string)
	if !ok || query == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required",
		}, nil
	}

	maxResults := defaultMaxResults
	if v, ok := call.Arguments["max_results"].(float64); ok && v > 0 {
		maxResults = int(v)
	}

	results, err := e.provider.Search(ctx, query, maxResults)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("search failed: %v", err),
		}, nil
	}

	if len(results) == 0 {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: "No results found.",
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: formatResults(results),
	}, nil
}

// formatResults renders results as a numbered markdown list.
func formatResults(results []SearchResult) string {
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, r.Title)
		fmt.Fprintf(&sb, "    %s\n", r.URL)
		if r.Description != "" {
			fmt.Fprintf(&sb, "    %s\n", r.Description)
		}
		if i < len(results)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
