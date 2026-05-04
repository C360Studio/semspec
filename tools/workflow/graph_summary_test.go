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

// Pin that the graph_query description does NOT carry literal fake-
// namespace entity IDs. The original `semspec.semsource.code.workspace.file.main-go`
// example contradicted the live Knowledge Graph manifest (which carries
// REAL IDs from the actual graph) — agents copied the broken example,
// hit "not found", iterated. Caught 2026-05-04 — see
// project_graph_query_truncated_id_wedge memory and the dense-A/B run
// that exposed the manifest-vs-tool-description conflict. The fix
// follows the semdragon pattern: drop literal examples from tool
// descriptions, let the manifest carry truth.
func TestGraphQueryDescription_NoLiteralFakeNamespaceExample(t *testing.T) {
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

	// Forbidden literals — the historical broken example and any
	// fake-but-plausible ID that doesn't match what the manifest
	// would emit. If a future edit drops back to a hardcoded
	// example, this fails LOUDLY.
	forbidden := []string{
		"semspec.semsource.code.workspace.file.main-go",
		"semspec.semsource.code.workspace.file.",
	}
	for _, s := range forbidden {
		if strings.Contains(graphQuery.Description, s) {
			t.Errorf("graph_query description contains forbidden literal %q — agents copy it; the description must point at the Knowledge Graph manifest for real IDs instead", s)
		}
	}

	// Required guidance — must point at the manifest as the source
	// of truth for entity IDs.
	mustContain := []string{
		"manifest",           // points at the Knowledge Graph manifest
		"<id-from-manifest>", // placeholder syntax in the SHAPE example
	}
	for _, s := range mustContain {
		if !strings.Contains(graphQuery.Description, s) {
			t.Errorf("graph_query description must contain %q so the agent knows where real IDs live", s)
		}
	}
}
