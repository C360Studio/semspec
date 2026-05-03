package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

func makeCall(id, name string, args map[string]any) agentic.ToolCall {
	return agentic.ToolCall{ID: id, Name: name, Arguments: args}
}

func TestGraphSummary_NilRegistry_ReturnsError(t *testing.T) {
	executor := &GraphExecutor{} // no registry

	call := makeCall("c1", "graph_summary", map[string]any{})
	result, err := executor.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("expected error when registry is nil")
	}
}

func TestGraphExecutor_RegistrationKeysMatchListTools(t *testing.T) {
	exec := &GraphExecutor{}
	for _, tool := range exec.ListTools() {
		call := makeCall("test", tool.Name, map[string]any{})
		_, err := exec.Execute(context.Background(), call)
		if err != nil && strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("ListTools() advertises %q but Execute() doesn't handle it", tool.Name)
		}
	}
}

// TestGraphQueryDescription_TeachesGraphQLNotSPARQL pins the SKG
// discoverability fix from 2026-05-03 v7: agents were sending
// "SELECT ?entity WHERE { ?entity a Type }" SPARQL queries to
// graph_query because the description said "GraphQL" without an
// example. The dotted-namespace entity IDs look like RDF subjects;
// example-anchoring matters more than naming the language.
func TestGraphQueryDescription_TeachesGraphQLNotSPARQL(t *testing.T) {
	exec := &GraphExecutor{}
	var graphQuery *agentic.ToolDefinition
	for i, tool := range exec.ListTools() {
		if tool.Name == "graph_query" {
			graphQuery = &exec.ListTools()[i]
			break
		}
	}
	if graphQuery == nil {
		t.Fatal("graph_query not found in ListTools()")
	}
	mustContain := []string{
		"NOT SPARQL",
		"NOT Cypher",
		"entity(id:",
		"introspect:true",
	}
	for _, s := range mustContain {
		if !strings.Contains(graphQuery.Description, s) {
			t.Errorf("graph_query description must contain %q so the agent doesn't send SPARQL/Cypher", s)
		}
	}
}
