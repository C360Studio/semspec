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
			Description: "GraphQL query against the knowledge graph. Pass introspect:true to see the schema before writing queries. For general questions, use graph_search instead.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "GraphQL query string. Required unless introspect is true.",
					},
					"introspect": map[string]any{
						"type":        "boolean",
						"description": "Return the GraphQL schema instead of executing a query. Call once to discover available queries and types.",
					},
				},
			},
		},
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

// graphQLSchema is returned when introspect:true is passed to graph_query.
// It describes the available queries so agents can write targeted GraphQL.
const graphQLSchema = `# Knowledge Graph — GraphQL Schema

type Query {
  ## Single entity by full ID
  entity(id: String!): Entity

  ## Find entity IDs matching a predicate (optionally with a specific value)
  entitiesByPredicate(predicate: String!, value: String, limit: Int): [String!]!

  ## Find entities whose ID starts with a prefix
  entitiesByPrefix(prefix: String!, limit: Int): [Entity!]!

  ## Graph traversal from a starting entity
  traverse(start: String!, depth: Int!, direction: OUTBOUND | INBOUND, predicate: String): TraverseResult

  ## Natural-language search across community summaries (Graph RAG)
  globalSearch(query: String!, level: Int, max_communities: Int): GlobalSearchResult

  ## List all predicates with entity counts
  predicates: PredicatesSummary
}

type Entity {
  id: String!
  triples: [Triple!]!       # All predicate-object pairs for this entity
}

type Triple {
  predicate: String!         # e.g. "source.doc.file_path", "workflow.phase"
  object: Any                # String, number, or JSON
}

type TraverseResult {
  nodes: [Entity!]!
  edges: [Edge!]!
}

type Edge {
  source: String!
  target: String!
  predicate: String!
}

type GlobalSearchResult {
  answer: String!
  entity_digests: [EntityDigest!]!
  community_summaries: [CommunitySummary!]!
  count: Int!
}

## Common entity ID prefixes:
##   {org}.{platform}.wf.plan.plan.*          — Plans
##   {org}.{platform}.wf.plan.requirement.*   — Requirements
##   {org}.{platform}.wf.plan.scenario.*      — Scenarios
##   {org}.{platform}.exec.task.run.*         — Task executions
##   {org}.{platform}.exec.req.run.*          — Requirement executions
##   {org}.{platform}.wf.plan.question.*      — Questions
##   {org}.{platform}.source.doc.*            — Indexed documents
##   {org}.{platform}.source.code.*           — Indexed code entities

## Example queries:
##   { predicates { predicates { predicate entityCount } total } }
##   { entitiesByPrefix(prefix: "semspec.local.source.doc.") { id triples { predicate object } } }
##   { entity(id: "semspec.local.wf.plan.plan.abc123") { id triples { predicate object } } }
##   { traverse(start: "entity.id", depth: 2, direction: OUTBOUND) { nodes { id } edges { source target predicate } } }
##   { globalSearch(query: "authentication handler") { answer entity_digests { id type label relevance } } }
`

// queryGraph executes a raw GraphQL query or returns the schema for introspection.
func (e *GraphExecutor) queryGraph(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if introspect, _ := call.Arguments["introspect"].(bool); introspect {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: graphQLSchema,
		}, nil
	}

	query, ok := call.Arguments["query"].(string)
	if !ok || query == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "query argument is required (or pass introspect:true to see the schema)",
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
