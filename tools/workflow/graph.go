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
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"

	"github.com/c360studio/semspec/graph"
	codeAst "github.com/c360studio/semspec/processor/ast"
	semspecVocab "github.com/c360studio/semspec/vocabulary/semspec"
)

const maxGraphResponseBytes = 100 * 1024 // 100KB

// GraphExecutor implements graph query tools for workflow context.
type GraphExecutor struct {
	gatewayURL string
	querier    graph.Querier // federated querier (nil = use gatewayURL directly)
}

// NewGraphExecutor creates a new graph executor.
// Uses the global GraphRegistry for federated queries when available.
func NewGraphExecutor() *GraphExecutor {
	e := &GraphExecutor{
		gatewayURL: getGatewayURL(),
	}

	// Wire federated querier if global registry is available.
	if reg := graph.GlobalRegistry(); reg != nil {
		e.querier = graph.NewFederatedGraphGatherer(reg, nil)
	}

	return e
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
	case "graph_summary":
		return e.graphSummary(ctx, call)
	case "graph_search":
		return e.graphSearch(ctx, call)
	case "graph_query":
		return e.queryGraph(ctx, call)
	// graph_codebase, graph_entity, graph_traverse removed — agents use
	// graph_search for discovery and graph_query for specific lookups.
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
			Name:        "graph_summary",
			Description: "What's in the knowledge graph. Call ONCE first to see entity types, domains, and counts.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include_predicates": map[string]any{
						"type":        "boolean",
						"description": "Include predicate schemas in the response (default: true)",
					},
				},
			},
		},
		{
			Name:        "graph_search",
			Description: "Search the knowledge graph. Returns a synthesized answer about your question. Use for any question about the codebase, architecture, or project.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language question or keyword search (e.g., 'how does authentication work' or 'error handling patterns')",
					},
					"level": map[string]any{
						"type":        "integer",
						"description": "Community level 0-3. Higher levels give broader answers (default: 1)",
					},
					"max_communities": map[string]any{
						"type":        "integer",
						"description": "Maximum communities to search (default: 10)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "graph_query",
			Description: "Raw GraphQL query for specific lookups. Use entitiesByPredicate(predicate), entity(id), or entitiesByPrefix(prefix). For general questions, use graph_search instead.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "GraphQL query string. Example: { entity(id: \"my.entity.id\") { id triples { predicate object } } }",
					},
				},
				"required": []string{"query"},
			},
		},
		// graph_codebase, graph_entity, graph_traverse removed — agents use
		// graph_search for discovery and graph_query for specific lookups.
	}
}

// graphSummary returns a knowledge graph overview from all connected semsource instances.
func (e *GraphExecutor) graphSummary(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	includePredicates := true
	if v, ok := call.Arguments["include_predicates"].(bool); ok {
		includePredicates = v
	}

	// Use federated querier when available (normal production path).
	if e.querier != nil {
		summaries, err := e.querier.GraphSummary(ctx)
		if err != nil {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("graph summary failed: %v", err),
			}, nil
		}

		if !includePredicates {
			for i := range summaries {
				summaries[i].Predicates = nil
			}
		}

		output, _ := json.MarshalIndent(summaries, "", "  ")
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: string(output),
		}, nil
	}

	// Fallback: direct HTTP to semsource when no registry is wired.
	semsourceURL := os.Getenv("SEMSOURCE_URL")
	if semsourceURL == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "graph summary unavailable: no semsource configured",
		}, nil
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, semsourceURL+"/source-manifest/summary", nil)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("create request: %v", err)}, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("request failed: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("semsource returned %d", resp.StatusCode),
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return agentic.ToolResult{CallID: call.ID, Error: fmt.Sprintf("read response: %v", err)}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(body),
	}, nil
}

// graphSearch executes a natural language search via globalSearch and returns
// the synthesized answer first, then entity digests.
func (e *GraphExecutor) graphSearch(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	query, ok := call.Arguments["query"].(string)
	if !ok || query == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required",
		}, nil
	}

	level := 1
	if v, ok := call.Arguments["level"].(float64); ok {
		level = int(v)
	}
	maxCommunities := 10
	if v, ok := call.Arguments["max_communities"].(float64); ok {
		maxCommunities = int(v)
	}

	gql := `query($query: String!, $level: Int, $maxCommunities: Int) {
		globalSearch(query: $query, level: $level, maxCommunities: $maxCommunities) {
			answer
			answer_model
			entity_digests { id type label relevance }
			community_summaries {
				communityId summary keywords level relevance
				member_count
				entities { id type label relevance }
			}
			count
		}
	}`

	vars := map[string]any{
		"query":          query,
		"level":          level,
		"maxCommunities": maxCommunities,
	}

	result, err := e.executeGraphQL(ctx, gql, vars)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("graph search failed: %v", err),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: formatSearchResult(result),
	}, nil
}

// formatSearchResult formats a globalSearch response for LLM consumption.
// Priority: answer > entity_digests > community_summaries > raw count.
func formatSearchResult(data map[string]any) string {
	search, ok := data["globalSearch"].(map[string]any)
	if !ok {
		return "No results found."
	}

	var sb strings.Builder

	// 1. Answer — the synthesized knowledge summary
	if answer, ok := search["answer"].(string); ok && answer != "" {
		sb.WriteString(answer)
		if model, ok := search["answer_model"].(string); ok && model != "" {
			sb.WriteString(fmt.Sprintf("\n\n(synthesized by %s)", model))
		}
	}

	// 2. Entity digests — lightweight context for matched entities
	if digests, ok := search["entity_digests"].([]any); ok && len(digests) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n---\nMatched entities:\n")
		} else {
			sb.WriteString("Matched entities:\n")
		}
		for _, d := range digests {
			if digest, ok := d.(map[string]any); ok {
				label, _ := digest["label"].(string)
				etype, _ := digest["type"].(string)
				id, _ := digest["id"].(string)
				if label != "" {
					sb.WriteString(fmt.Sprintf("- %s [%s] %s\n", label, etype, id))
				} else {
					sb.WriteString(fmt.Sprintf("- [%s] %s\n", etype, id))
				}
			}
		}
	}

	// 3. Community summaries — clustered knowledge (only when no answer and no digests)
	if communities, ok := search["community_summaries"].([]any); ok && len(communities) > 0 && sb.Len() == 0 {
		sb.WriteString("Knowledge clusters:\n")
		for _, c := range communities {
			if comm, ok := c.(map[string]any); ok {
				summary, _ := comm["summary"].(string)
				if summary != "" {
					sb.WriteString(fmt.Sprintf("\n%s\n", summary))
				}
				// Show representative entities
				if entities, ok := comm["entities"].([]any); ok {
					for _, e := range entities {
						if ent, ok := e.(map[string]any); ok {
							label, _ := ent["label"].(string)
							etype, _ := ent["type"].(string)
							if label != "" {
								sb.WriteString(fmt.Sprintf("  - %s [%s]\n", label, etype))
							}
						}
					}
				}
			}
		}
	}

	// Fallback: count only
	if sb.Len() == 0 {
		if count, ok := search["count"].(float64); ok {
			return fmt.Sprintf("Found %d entities but no summary available. Use graph_query for specific lookups.", int(count))
		}
		return "No results found."
	}

	return sb.String()
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
