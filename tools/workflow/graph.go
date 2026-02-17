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
)

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
			Description: "Query the semantic knowledge graph using GraphQL. Use this as the PRIMARY method to understand the codebase structure. The graph contains indexed code entities (functions, types, interfaces), their relationships (calls, implements, imports), and workflow entities (plans, specs).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "GraphQL query to execute against the graph. Example: { entities(filter: { predicatePrefix: \"code.function\" }) { id triples { predicate object } } }",
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
						"description": "The entity ID to retrieve (e.g., 'code.function.main.Run' or 'semspec.local.workflow.plan.plan.add-auth')",
					},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        "workflow_traverse_relationships",
			Description: "Traverse relationships from a starting entity in the knowledge graph. Use this to find related code (what calls a function, what implements an interface, what a type depends on).",
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

	result, err := e.executeGraphQL(ctx, query)
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

	// Query for counts and samples of each entity type
	summary := map[string]any{}

	// Get function entities
	functionsQuery := `{
		entities(filter: { predicatePrefix: "code.function" }) {
			id
			triples { predicate object }
		}
	}`
	functionsResult, err := e.executeGraphQL(ctx, functionsQuery)
	if err == nil {
		if entities, ok := functionsResult["entities"].([]any); ok {
			summary["functions"] = map[string]any{
				"count":   len(entities),
				"samples": e.extractSamples(entities, maxSamples, includeSamples),
			}
		}
	}

	// Get type entities
	typesQuery := `{
		entities(filter: { predicatePrefix: "code.type" }) {
			id
			triples { predicate object }
		}
	}`
	typesResult, err := e.executeGraphQL(ctx, typesQuery)
	if err == nil {
		if entities, ok := typesResult["entities"].([]any); ok {
			summary["types"] = map[string]any{
				"count":   len(entities),
				"samples": e.extractSamples(entities, maxSamples, includeSamples),
			}
		}
	}

	// Get interface entities
	interfacesQuery := `{
		entities(filter: { predicatePrefix: "code.interface" }) {
			id
			triples { predicate object }
		}
	}`
	interfacesResult, err := e.executeGraphQL(ctx, interfacesQuery)
	if err == nil {
		if entities, ok := interfacesResult["entities"].([]any); ok {
			summary["interfaces"] = map[string]any{
				"count":   len(entities),
				"samples": e.extractSamples(entities, maxSamples, includeSamples),
			}
		}
	}

	// Get package entities
	packagesQuery := `{
		entities(filter: { predicatePrefix: "code.package" }) {
			id
			triples { predicate object }
		}
	}`
	packagesResult, err := e.executeGraphQL(ctx, packagesQuery)
	if err == nil {
		if entities, ok := packagesResult["entities"].([]any); ok {
			summary["packages"] = map[string]any{
				"count":   len(entities),
				"samples": e.extractSamples(entities, maxSamples, includeSamples),
			}
		}
	}

	// Get plan entities
	plansQuery := `{
		entities(filter: { predicatePrefix: "semspec.plan" }) {
			id
			triples { predicate object }
		}
	}`
	plansResult, err := e.executeGraphQL(ctx, plansQuery)
	if err == nil {
		if entities, ok := plansResult["entities"].([]any); ok {
			summary["plans"] = map[string]any{
				"count":   len(entities),
				"samples": e.extractSamples(entities, maxSamples, includeSamples),
			}
		}
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

	query := fmt.Sprintf(`{
		entity(id: "%s") {
			id
			triples { predicate object }
		}
	}`, entityID)

	result, err := e.executeGraphQL(ctx, query)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to get entity: %v", err),
		}, nil
	}

	entity, ok := result["entity"]
	if !ok || entity == nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("entity not found: %s", entityID),
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
		depth = int(d)
		if depth > 3 {
			depth = 3
		}
	}

	// Build the traverse query
	directionArg := "OUTBOUND"
	if direction == "inbound" {
		directionArg = "INBOUND"
	}

	predicateFilter := ""
	if predicate != "" {
		predicateFilter = fmt.Sprintf(`, predicate: "%s"`, predicate)
	}

	query := fmt.Sprintf(`{
		traverse(start: "%s", depth: %d, direction: %s%s) {
			nodes {
				id
				triples { predicate object }
			}
			edges {
				source
				target
				predicate
			}
		}
	}`, startEntity, depth, directionArg, predicateFilter)

	result, err := e.executeGraphQL(ctx, query)
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
func (e *GraphExecutor) executeGraphQL(ctx context.Context, query string) (map[string]any, error) {
	reqBody := map[string]string{"query": query}
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

	var result struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data, nil
}

// extractSamples extracts sample entity summaries from a list.
func (e *GraphExecutor) extractSamples(entities []any, maxSamples int, includeSamples bool) []map[string]string {
	if !includeSamples {
		return nil
	}

	samples := make([]map[string]string, 0, maxSamples)
	for i, entity := range entities {
		if i >= maxSamples {
			break
		}

		entityMap, ok := entity.(map[string]any)
		if !ok {
			continue
		}

		sample := map[string]string{}
		if id, ok := entityMap["id"].(string); ok {
			sample["id"] = id
		}

		// Extract key predicates for summary
		if triples, ok := entityMap["triples"].([]any); ok {
			for _, t := range triples {
				triple, ok := t.(map[string]any)
				if !ok {
					continue
				}
				pred, _ := triple["predicate"].(string)
				obj := triple["object"]

				// Include important predicates in sample
				switch pred {
				case "code.artifact.name", "code.artifact.path", "code.artifact.type",
					"semspec.plan.title", "semspec.plan.status":
					if objStr, ok := obj.(string); ok {
						sample[pred] = objStr
					}
				}
			}
		}

		samples = append(samples, sample)
	}

	return samples
}
