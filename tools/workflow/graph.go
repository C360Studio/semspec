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
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/vocabulary/observability"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/recoveryhint"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/metric"
)

const maxGraphResponseBytes = 100 * 1024 // 100KB

// entityIDArgPattern matches `id: "..."` or `start: "..."` arguments in
// a GraphQL query — the two argument names that take a full entity ID
// per the schema (entity(id:), traverse(start:)). entitiesByPrefix(prefix:)
// is intentionally NOT matched: prefixes are partial by design.
//
// Variable bindings like `id: $foo` (no quotes) are also intentionally
// not matched — runtime-bound values cannot be validated statically.
var entityIDArgPattern = regexp.MustCompile(`\b(id|start)\s*:\s*"([^"]+)"`)

// minEntityIDSegments is the smallest segment count for a valid entity
// ID per the schema. Plans/requirements/scenarios/executions/source-doc/
// source-code all use {org}.{platform}.{kind}.{...} shape with 4 or more
// dot-separated segments. Below this is presumed truncated.
const minEntityIDSegments = 4

// recoveryPrefixCap caps the prefix length used for fuzzy-match
// recovery on a "not found" graph_query failure. We drop the last
// segment of the failing ID (the part the model got wrong) and cap
// at 4 segments — the universal-stable head per the schema docs
// (org.platform.kind.subkind covers wf/exec/source variants).
const recoveryPrefixCap = 4

// recoverySuggestionLimit caps how many candidates we surface in the
// RETRY HINT. Five is enough to give the agent options without
// drowning the response.
const recoverySuggestionLimit = 5

// recoveryByPrefixLimit caps the entitiesByPrefix lookup we run during
// recovery. 20 is wide enough to find a near-match but tight enough
// that a Levenshtein top-5 ranking stays meaningful.
const recoveryByPrefixLimit = 20

// recoveryInFlightKey is a context.Context key set on the recursive
// entitiesByPrefix call inside the recovery branch. Prevents a
// recovery-on-recovery cascade if the prefix lookup itself returns
// "not found" (e.g. malformed or empty prefix). Typed empty struct
// is the idiomatic Go pattern.
type recoveryInFlightKey struct{}

// graphRecoveryCounter tracks per-fire recovery outcomes at /metrics.
// Labeled by outcome (suggested | not_suggested). Pre-warmed at
// init so testutil reads return 0 for unfired children.
var graphRecoveryCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "semspec_graph_recovery_total",
		Help: "Total fires of graph_query recovery hints (ADR-035 audit D.8 follow-up). Labeled by outcome: suggested (we found candidates and injected directive RETRY HINTs) or not_suggested (recovery attempted but no candidates available).",
	},
	[]string{"outcome"},
)

func init() {
	graphRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeSuggested)
	graphRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeNotSuggested)
}

// RegisterMetrics registers the graph_query recovery counter with the
// given metrics registry so per-fire telemetry surfaces at /metrics.
// Call once during process startup. Idempotent. Nil-safe.
func RegisterMetrics(reg *metric.MetricsRegistry) error {
	if reg == nil {
		return nil
	}
	if err := reg.RegisterCounterVec("graph_query", "recovery_total", graphRecoveryCounter); err != nil {
		return fmt.Errorf("register graph_query recovery counter: %w", err)
	}
	return nil
}

// GraphExecutor implements graph query tools for workflow context.
type GraphExecutor struct {
	registry     *graph.SourceRegistry
	querier      graph.Querier            // federated querier (nil = graph not configured)
	tripleWriter *graphutil.TripleWriter  // for ADR-035 tool.recovery.incident emit; nil-safe
}

// WithTripleWriter installs a triple writer so tool-recovery
// incidents reach the SKG. When unset, recovery still injects RETRY
// HINTs into the agent-facing error and increments Prom counters,
// just without per-call SKG attribution.
func (e *GraphExecutor) WithTripleWriter(tw *graphutil.TripleWriter) *GraphExecutor {
	e.tripleWriter = tw
	return e
}

// NewGraphExecutor creates a new graph executor.
// Uses the global SourceRegistry for all URL resolution and federated queries.
func NewGraphExecutor() *GraphExecutor {
	reg := graph.GlobalSources()
	if reg == nil {
		return &GraphExecutor{}
	}
	return &GraphExecutor{
		registry: reg,
		querier:  graph.NewFederatedGraphGatherer(reg, nil),
	}
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
			Name: "graph_query",
			Description: "GraphQL query against the knowledge graph (NOT SPARQL, NOT Cypher — GraphQL).\n" +
				"Use {field {sub-field}} brace syntax. Example query string:\n" +
				"  { entity(id: \"semspec.semsource.code.workspace.file.main-go\") { triples { predicate object } } }\n" +
				"Call with introspect:true first to discover available top-level queries (entity, search, etc.) and the schema.\n" +
				"For natural-language questions, use graph_search instead.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "GraphQL query string (e.g. `{ entity(id: \"...\") { triples { predicate object } } }`). Required unless introspect is true. SPARQL/Cypher syntax will fail.",
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

// graphSummary returns a knowledge graph overview from the registry.
func (e *GraphExecutor) graphSummary(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if e.registry == nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "graph not configured",
		}, nil
	}

	text := e.registry.FormatSummaryForPrompt(ctx)
	if text == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "no graph data available (semsource may still be indexing)",
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: text,
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

// validateEntityIDsInQuery scans a GraphQL query for `id:` and `start:`
// arguments and returns an error citing the first malformed value. Returns
// nil when no entity-ID arguments are present (caller continues normally —
// entitiesByPrefix and globalSearch queries don't take full IDs).
//
// ADR-035 audit site D.8: pre-flight rejection here forecloses the
// truncation wedge where models call `entity(id: "semspec.semsou")` (cut
// from a longer ID) and the gateway returns "not found:" with no hint,
// causing the model to loop on the same broken ID for many iterations.
func validateEntityIDsInQuery(query string) error {
	matches := entityIDArgPattern.FindAllStringSubmatch(query, -1)
	for _, m := range matches {
		argName, value := m[1], m[2]
		if err := validateEntityIDShape(value); err != nil {
			return fmt.Errorf("argument %q has invalid entity ID %q: %w", argName, value, err)
		}
	}
	return nil
}

// validateEntityIDShape checks that an entity ID has the expected
// dot-separated structure. Returns nil when the ID looks well-formed.
func validateEntityIDShape(id string) error {
	segments := strings.Split(id, ".")
	if len(segments) < minEntityIDSegments {
		return fmt.Errorf("entity IDs are dot-separated with at least %d segments (e.g. \"semspec.local.wf.plan.plan.abc123\"); got %d segment(s)", minEntityIDSegments, len(segments))
	}
	for i, seg := range segments {
		if seg == "" {
			return fmt.Errorf("segment %d is empty (no leading/trailing dots, no double dots allowed)", i)
		}
	}
	return nil
}

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

	// ADR-035 CP-2 (audit site D.8): pre-flight entity-ID shape validation.
	// Catches truncated IDs before they reach the gateway so the model gets
	// a directive error instead of an opaque "not found:" the gateway emits
	// for malformed lookups.
	if err := validateEntityIDsInQuery(query); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("invalid entity ID in query: %v. Expected format: dot-separated segments like \"semspec.local.wf.plan.plan.abc123\". Use entitiesByPrefix(prefix:) for partial-prefix lookups or graph_search(query:) for natural-language queries.", err),
		}, nil
	}

	result, err := e.executeGraphQL(ctx, query, nil)
	if err != nil {
		// ADR-035 D.8 follow-up: when the gateway returns "not found"
		// for an entity lookup, augment the agent-facing error with
		// directive candidate hints derived from a fuzzy-match against
		// entitiesByPrefix. Symmetric to ADR-035's named-quirks list:
		// when the system silently compensates for an agent's
		// near-miss, be loud about the help (Prom counter + SKG
		// triple). Best-effort — recovery itself never fails the
		// outer call.
		augmented := e.tryRecoverNotFound(ctx, call, query, err)
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  augmented,
		}, nil
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(output),
	}, nil
}

// tryRecoverNotFound augments a "not found" gateway error with
// directive candidate hints when feasible, and emits per-fire
// telemetry. Returns the agent-facing error string — either the
// augmented version (with RETRY HINT) or the original wrapped error
// when recovery isn't applicable.
//
// Recovery short-circuits on:
//   - errors that don't look like "not found" (we can only help the
//     entity-lookup case today; expand pattern when other recoverable
//     errors emerge)
//   - recursive call inside the recovery branch (ctx flag set)
//   - no entity_id extractable from the query
//   - prefix lookup itself fails
//
// On every reachable invocation we increment the Prom counter with
// the appropriate outcome label so operators see fire rates even
// when the SKG triple write fails.
func (e *GraphExecutor) tryRecoverNotFound(ctx context.Context, call agentic.ToolCall, query string, originalErr error) string {
	wrappedOriginal := fmt.Sprintf("graph query failed: %v", originalErr)

	// Short-circuit on recursive recovery — the prefix lookup we run
	// internally must NOT trigger another recovery cascade.
	if v, _ := ctx.Value(recoveryInFlightKey{}).(bool); v {
		return wrappedOriginal
	}

	// Only handle "not found" today. Other recoverable error shapes
	// (e.g. "syntax error", "unknown field") need their own ranking
	// strategies — extend this match when a real fixture demands it.
	errMsg := originalErr.Error()
	if !strings.Contains(errMsg, "not found:") {
		return wrappedOriginal
	}

	// Extract entity_id from the original query.
	matches := entityIDArgPattern.FindStringSubmatch(query)
	if len(matches) < 3 {
		return wrappedOriginal
	}
	originalID := matches[2]

	// Build the prefix: drop the last segment (the part the model got
	// wrong), cap at recoveryPrefixCap. Falls through to no-recovery
	// if the prefix would be too short to be meaningful.
	prefix := buildRecoveryPrefix(originalID)
	if prefix == "" {
		return e.recordNoSuggestions(ctx, call, query, wrappedOriginal)
	}

	// Recursive lookup with the ctx guard set.
	recCtx := context.WithValue(ctx, recoveryInFlightKey{}, true)
	prefixQuery := fmt.Sprintf(`{ entitiesByPrefix(prefix: %q, limit: %d) { id } }`, prefix, recoveryByPrefixLimit)
	prefixResult, prefixErr := e.executeGraphQL(recCtx, prefixQuery, nil)
	if prefixErr != nil {
		return e.recordNoSuggestions(ctx, call, query, wrappedOriginal)
	}

	// Pull candidate IDs out of the typed response shape.
	candidates := extractEntityIDs(prefixResult)
	if len(candidates) == 0 {
		return e.recordNoSuggestions(ctx, call, query, wrappedOriginal)
	}

	suggestions := recoveryhint.Suggest(originalID, candidates, recoverySuggestionLimit)
	if len(suggestions) == 0 {
		return e.recordNoSuggestions(ctx, call, query, wrappedOriginal)
	}

	// Build the directive hint. Match the existing
	// validateEntityIDsInQuery error voice — the directive ending
	// stops the "did you mean" → "let me try yet another guess"
	// pattern observed in the v11 wedge.
	hint := fmt.Sprintf(
		`%s. RETRY HINT: entity not found "%s". Closest matches: %s. Try entity(id: %q) if that's the one you meant.`,
		wrappedOriginal,
		originalID,
		strings.Join(suggestions, ", "),
		suggestions[0],
	)

	graphRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeSuggested).Inc()
	e.emitRecoveryIncident(ctx, call, query, observability.ToolRecoveryOutcomeSuggested, suggestions)
	return hint
}

// recordNoSuggestions handles the not_suggested branch: bump the
// counter, emit the incident with no candidates, return the original
// error unchanged. Centralized so every short-circuit path produces
// uniform telemetry.
func (e *GraphExecutor) recordNoSuggestions(ctx context.Context, call agentic.ToolCall, query, wrappedOriginal string) string {
	graphRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeNotSuggested).Inc()
	e.emitRecoveryIncident(ctx, call, query, observability.ToolRecoveryOutcomeNotSuggested, nil)
	return wrappedOriginal
}

// emitRecoveryIncident writes the tool.recovery.incident triple set
// via recoveryhint.Emit. Best-effort — emit failures are logged at
// the Prom counter level only (the counter already reflects the
// fire); we don't propagate triple-write errors to the agent.
func (e *GraphExecutor) emitRecoveryIncident(ctx context.Context, call agentic.ToolCall, query, outcome string, candidates []string) {
	if e.tripleWriter == nil {
		return
	}
	rc := recoveryhint.RecoveryContext{
		CallID:   call.LoopID,
		Role:     stringFromMetadata(call.Metadata, "role"),
		Model:    stringFromMetadata(call.Metadata, "model"),
		ToolName: "graph_query",
	}
	re := recoveryhint.RecoveryEvent{
		Outcome:       outcome,
		OriginalQuery: query,
		Candidates:    candidates,
	}
	_, _ = recoveryhint.Emit(ctx, e.tripleWriter, rc, re)
}

// buildRecoveryPrefix returns the dot-separated prefix used to fuzzy-
// match candidates for a failing entity ID. Drops the last segment
// (the part the model most likely got wrong) and caps at
// recoveryPrefixCap segments to keep the candidate set tight enough
// for top-N ranking. Returns "" when the input doesn't have enough
// segments to produce a meaningful prefix.
func buildRecoveryPrefix(id string) string {
	segs := strings.Split(id, ".")
	if len(segs) < 3 {
		// Need at least 3 segments to drop one and still have a
		// non-trivial prefix. Anything shorter is malformed enough
		// that the validateEntityIDsInQuery pre-flight should have
		// caught it; this is a defensive guard.
		return ""
	}
	prefixLen := len(segs) - 1
	if prefixLen > recoveryPrefixCap {
		prefixLen = recoveryPrefixCap
	}
	return strings.Join(segs[:prefixLen], ".") + "."
}

// extractEntityIDs pulls the `id` field from each entity in a
// `{ entitiesByPrefix { id } }` GraphQL response. Returns nil on
// shape mismatch — callers treat that as the not_suggested branch.
func extractEntityIDs(data map[string]any) []string {
	prefix, ok := data["entitiesByPrefix"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(prefix))
	for _, ent := range prefix {
		obj, ok := ent.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := obj["id"].(string); ok && id != "" {
			out = append(out, id)
		}
	}
	return out
}

// stringFromMetadata fetches a string from the tool-call metadata,
// returning "" on absence or wrong type. Centralized so every callsite
// handles missing metadata uniformly.
func stringFromMetadata(md map[string]any, key string) string {
	if md == nil {
		return ""
	}
	v, _ := md[key].(string)
	return v
}

// executeGraphQL executes a GraphQL query against the graph gateway.
func (e *GraphExecutor) executeGraphQL(ctx context.Context, query string, variables map[string]any) (map[string]any, error) {
	graphqlURL := ""
	if e.registry != nil {
		graphqlURL = e.registry.LocalGraphQLURL()
	}
	if graphqlURL == "" {
		return nil, fmt.Errorf("graph gateway not configured")
	}

	reqBody := map[string]any{"query": query}
	if variables != nil {
		reqBody["variables"] = variables
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// 60s caps the agent's wait for graph_search/graph_query results. The
	// older 10s value was tuned when graph-gateway was a thin GraphQL
	// shim; today globalSearch dispatches LLM-driven query_classification
	// (T3 fallback) + answer_synthesis whose combined budget is in the
	// 30–60s tier per semstreams' capability spec sheets. With those
	// capabilities bound to a concurrent seminstruct backend the typical
	// path is well under 30s; this ceiling exists for the worst case.
	// TODO: lift to GraphExecutor config so per-deployment tuning doesn't
	// require a code change. Track alongside a per-tool-timeout story
	// (no semstreams precedent yet).
	client := &http.Client{Timeout: 60 * time.Second}
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
