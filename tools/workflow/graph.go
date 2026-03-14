// Package workflow provides workflow-specific tools for document generation.
// These tools support the LLM-driven workflow by providing graph-first
// context gathering, document management, and constitution validation.
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/c360studio/semstreams/agentic"

	codeAst "github.com/c360studio/semspec/processor/ast"
	semspecVocab "github.com/c360studio/semspec/vocabulary/semspec"
)

const maxGraphResponseBytes = 100 * 1024 // 100KB

// GraphExecutor implements graph query tools for workflow context.
type GraphExecutor struct {
	gatewayURL string
}

// NewGraphExecutor creates a new graph executor.
func NewGraphExecutor() *GraphExecutor {
	return &GraphExecutor{
		gatewayURL: getGatewayURL(),
	}
}

// getGatewayURL returns the graph gateway URL from environment or default.
func getGatewayURL() string {
	if url := os.Getenv("SEMSPEC_GRAPH_GATEWAY_URL"); url != "" {
		return url
	}
	return "http://localhost:8082"
}

// Execute executes a graph tool call.
func (e *GraphExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "workflow_query_graph":
		return e.queryGraph(ctx, call)
	case "workflow_get_codebase_summary":
		return e.getCodebaseSummary(ctx, call)
	case "workflow_get_entity":
		return e.getEntity(ctx, call)
	case "workflow_traverse_relationships":
		return e.traverseRelationships(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for graph operations.
func (e *GraphExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "workflow_query_graph",
			Description: "Query the semantic knowledge graph using GraphQL. Results are capped at 100KB — use specific predicates and limits for best results. The graph contains indexed code entities (functions, types, interfaces), their relationships (calls, implements, imports), and workflow entities (plans, specs).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "GraphQL query to execute against the graph. Example: { entitiesByPredicate(predicate: \"code.function\") } or { entity(id: \"my.entity.id\") { id triples { predicate object } } }",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "workflow_get_codebase_summary",
			Description: "Get a high-level summary of the codebase from the knowledge graph. Returns counts and samples of functions, types, interfaces, packages, and their relationships. Use this to understand the overall structure before diving into specifics.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include_samples": map[string]any{
						"type":        "boolean",
						"description": "Include sample entities for each category (default: true)",
					},
					"max_samples": map[string]any{
						"type":        "integer",
						"description": "Maximum number of sample entities per category (default: 5)",
					},
				},
			},
		},
		{
			Name:        "workflow_get_entity",
			Description: "Get a specific entity from the knowledge graph by ID. Returns all triples (predicate-object pairs) for the entity. Use this to get details about a specific function, type, or workflow entity.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{
						"type":        "string",
						"description": "The entity ID to retrieve (e.g., 'code.function.main.Run' or 'c360.semspec.workflow.plan.plan.add-auth')",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "workflow_traverse_relationships",
			Description: "Traverse relationships from a starting entity in the knowledge graph. Results capped at 100KB. Max depth 3. Use this to find related code (what calls a function, what implements an interface, what a type depends on).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"start_entity": map[string]any{
						"type":        "string",
						"description": "Entity ID to start traversal from",
					},
					"predicate": map[string]any{
						"type":        "string",
						"description": "Relationship predicate to follow (e.g., 'code.relationship.calls', 'code.relationship.implements')",
					},
					"direction": map[string]any{
						"type":        "string",
						"enum":        []string{"outbound", "inbound"},
						"description": "Direction to traverse: 'outbound' (what this entity points to) or 'inbound' (what points to this entity)",
					},
					"depth": map[string]any{
						"type":        "integer",
						"description": "Maximum traversal depth (default: 1, max: 3)",
					},
				},
				"required": []string{"start_entity"},
			},
		},
	}
}

// queryGraph executes a raw GraphQL query.
func (e *GraphExecutor) queryGraph(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	query, ok := call.Arguments["query"].(string)
	if !ok || query == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required",
		}, nil
	}

	result, err := e.executeGraphQL(ctx, query, nil)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("graph query failed: %v", err),
		}, nil
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// getCodebaseSummary returns a high-level summary of the codebase.
func (e *GraphExecutor) getCodebaseSummary(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	includeSamples := true
	if v, ok := call.Arguments["include_samples"].(bool); ok {
		includeSamples = v
	}

	maxSamples := 5
	if v, ok := call.Arguments["max_samples"].(float64); ok {
		maxSamples = int(v)
	}

	categories := []struct {
		key       string
		predicate string
	}{
		{"functions", "code.function"},
		{"types", "code.type"},
		{"interfaces", "code.interface"},
		{"packages", "code.package"},
		{"plans", "semspec.plan"},
	}

	summary := map[string]any{}

	for _, cat := range categories {
		ids, err := e.queryEntityIDs(ctx, cat.predicate, "", 0)
		if err != nil {
			continue
		}

		entry := map[string]any{"count": len(ids)}
		if includeSamples && len(ids) > 0 {
			sampleIDs := ids
			if len(sampleIDs) > maxSamples {
				sampleIDs = sampleIDs[:maxSamples]
			}
			samples := make([]map[string]string, 0, len(sampleIDs))
			for _, id := range sampleIDs {
				entity, err := e.getEntityByID(ctx, id)
				if err != nil {
					continue
				}
				samples = append(samples, e.extractSampleFields(entity))
			}
			entry["samples"] = samples
		}
		summary[cat.key] = entry
	}

	output, _ := json.MarshalIndent(summary, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// getEntity retrieves a specific entity by ID.
func (e *GraphExecutor) getEntity(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	entityID, ok := call.Arguments["entity_id"].(string)
	if !ok || entityID == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "entity_id argument is required",
		}, nil
	}

	entity, err := e.getEntityByID(ctx, entityID)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to get entity: %v", err),
		}, nil
	}

	output, _ := json.MarshalIndent(entity, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// traverseRelationships traverses relationships from a starting entity.
func (e *GraphExecutor) traverseRelationships(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	startEntity, ok := call.Arguments["start_entity"].(string)
	if !ok || startEntity == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "start_entity argument is required",
		}, nil
	}

	predicate, _ := call.Arguments["predicate"].(string)
	direction := "outbound"
	if d, ok := call.Arguments["direction"].(string); ok {
		direction = d
	}
	depth := 1
	if d, ok := call.Arguments["depth"].(float64); ok {
		depth = min(int(d), 3)
	}

	// Build the traverse query with parameterized variables.
	directionArg := "OUTBOUND"
	if direction == "inbound" {
		directionArg = "INBOUND"
	}

	vars := map[string]any{
		"start":     startEntity,
		"depth":     depth,
		"direction": directionArg,
	}

	var query string
	if predicate != "" {
		query = `query($start: String!, $depth: Int!, $direction: String!, $predicate: String!) {
			traverse(start: $start, depth: $depth, direction: $direction, predicate: $predicate) {
				nodes { id triples { predicate object } }
				edges { source target predicate }
			}
		}`
		vars["predicate"] = predicate
	} else {
		query = `query($start: String!, $depth: Int!, $direction: String!) {
			traverse(start: $start, depth: $depth, direction: $direction) {
				nodes { id triples { predicate object } }
				edges { source target predicate }
			}
		}`
	}

	result, err := e.executeGraphQL(ctx, query, vars)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("traversal failed: %v", err),
		}, nil
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// executeGraphQL executes a GraphQL query against the graph gateway.
func (e *GraphExecutor) executeGraphQL(ctx context.Context, query string, variables map[string]any) (map[string]any, error) {
	reqBody := map[string]any{"query": query}
	if variables != nil {
		reqBody["variables"] = variables
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.gatewayURL+"/graphql", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph gateway returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGraphResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxGraphResponseBytes {
		return nil, fmt.Errorf("response too large (%d bytes exceeds %d limit) — use more specific queries with predicates, entity IDs, or limits", len(body), maxGraphResponseBytes)
	}

	var result struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data, nil
}

// queryEntityIDs uses entitiesByPredicate to get entity IDs matching a predicate/value.
func (e *GraphExecutor) queryEntityIDs(ctx context.Context, predicate, value string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	query := `query($predicate: String!, $value: String, $limit: Int) {
		entitiesByPredicate(predicate: $predicate, value: $value, limit: $limit)
	}`
	vars := map[string]any{"predicate": predicate}
	if value != "" {
		vars["value"] = value
	}
	if limit > 0 {
		vars["limit"] = limit
	}

	data, err := e.executeGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	idsRaw, _ := data["entitiesByPredicate"].([]any)
	ids := make([]string, 0, len(idsRaw))
	for _, idRaw := range idsRaw {
		if id, ok := idRaw.(string); ok {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// getEntityByID retrieves a single entity by ID with parameterized query.
func (e *GraphExecutor) getEntityByID(ctx context.Context, id string) (map[string]any, error) {
	query := `query($id: String!) {
		entity(id: $id) {
			id
			triples { predicate object }
		}
	}`
	data, err := e.executeGraphQL(ctx, query, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	entity, ok := data["entity"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("entity not found: %s", id)
	}
	return entity, nil
}

// extractSampleFields extracts key predicates from an entity for summary display.
func (e *GraphExecutor) extractSampleFields(entityMap map[string]any) map[string]string {
	sample := map[string]string{}
	if id, ok := entityMap["id"].(string); ok {
		sample["id"] = id
	}
	if triples, ok := entityMap["triples"].([]any); ok {
		for _, t := range triples {
			triple, ok := t.(map[string]any)
			if !ok {
				continue
			}
			pred, _ := triple["predicate"].(string)
			obj := triple["object"]
			switch pred {
			case codeAst.DcTitle, codeAst.CodePath, codeAst.CodeType,
				semspecVocab.PlanTitle, semspecVocab.PredicatePlanStatus:
				if objStr, ok := obj.(string); ok {
					sample[pred] = objStr
				}
			}
		}
	}
	return sample
}
