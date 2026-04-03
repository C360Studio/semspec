package workflow

import (
	"context"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// Register registers graph tools (graph_summary, graph_search, graph_query)
// as separate executors to avoid duplicate tool definitions with Gemini.
func Register() {
	graphExec := NewGraphExecutor()

	// Register each tool individually. The shared GraphExecutor handles all
	// three, but RegisterTool calls ListTools() which returns all definitions.
	// Wrapping each registration ensures only one definition per name.
	_ = agentictools.RegisterTool("graph_summary", singleGraphTool(graphExec, "graph_summary"))
	_ = agentictools.RegisterTool("graph_search", singleGraphTool(graphExec, "graph_search"))
	_ = agentictools.RegisterTool("graph_query", singleGraphTool(graphExec, "graph_query"))
}

// singleGraphTool wraps a GraphExecutor so ListTools() returns only the
// definition matching the given name. Prevents Gemini's "Duplicate function
// declaration" error when the same executor handles multiple tools.
func singleGraphTool(exec *GraphExecutor, name string) agentictools.ToolExecutor {
	return &filteredGraphTool{exec: exec, name: name}
}

type filteredGraphTool struct {
	exec *GraphExecutor
	name string
}

func (f *filteredGraphTool) ListTools() []agentic.ToolDefinition {
	var filtered []agentic.ToolDefinition
	for _, t := range f.exec.ListTools() {
		if t.Name == f.name {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (f *filteredGraphTool) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return f.exec.Execute(ctx, call)
}
