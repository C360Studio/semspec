package workflow

import (
	"context"
	"errors"
	"log/slog"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/workflow/graphutil"
)

// Register registers graph tools (graph_summary, graph_search, graph_query)
// as separate executors to avoid duplicate tool definitions with Gemini.
//
// natsClient is optional — when provided, graph_query's recovery
// branch (ADR-035 audit D.8 follow-up) emits tool.recovery.incident
// triples to the SKG so per-call recovery attribution is queryable.
// When nil, recovery still injects RETRY HINTs into the agent error
// and increments Prom counters, just without SKG triples.
func Register(reg *agentictools.ExecutorRegistry, natsClient *natsclient.Client) error {
	graphExec := NewGraphExecutor()
	if natsClient != nil {
		graphExec = graphExec.WithTripleWriter(&graphutil.TripleWriter{
			NATSClient:    natsClient,
			Logger:        slog.Default(),
			ComponentName: "graph_query",
		})
	}

	// Register each tool individually. The shared GraphExecutor handles all
	// three, but ListTools() returns all definitions. Wrapping each
	// registration ensures only one definition per name.
	return errors.Join(
		reg.RegisterTool("graph_summary", singleGraphTool(graphExec, "graph_summary")),
		reg.RegisterTool("graph_search", singleGraphTool(graphExec, "graph_search")),
		reg.RegisterTool("graph_query", singleGraphTool(graphExec, "graph_query")),
	)
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
