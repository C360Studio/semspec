// Package review implements the review_scenario agentic tool. It allows a
// reviewer agent to submit a structured peer review of a scenario
// implementation, recording the verdict, ratings, and error categories against
// the persistent agent being reviewed.
package review

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/google/uuid"
)

const toolName = "review_scenario"

// GraphHelper defines the graph operations needed by the review tool.
// The interface is narrow — only operations required by this executor are
// included, keeping the dependency surface small and the mock simple.
type GraphHelper interface {
	RecordReview(ctx context.Context, review agentgraph.Review) error
	IncrementAgentErrorCounts(ctx context.Context, agentID string, categoryIDs []string) error
	UpdateAgentStats(ctx context.Context, agentID string, stats workflow.ReviewStats) error
	GetAgent(ctx context.Context, agentID string) (*workflow.Agent, error)
}

// Executor implements agentic.ToolExecutor for the review_scenario tool.
// It validates the review submitted by a reviewer agent, persists it to the
// graph, and updates the reviewed agent's running statistics.
//
// All public methods are safe for concurrent use — internal state is read-only
// after construction.
type Executor struct {
	graph    GraphHelper
	registry *workflow.ErrorCategoryRegistry
}

// NewExecutor constructs a review_scenario Executor with the given graph helper
// and error category registry. Both arguments must be non-nil.
func NewExecutor(graph GraphHelper, registry *workflow.ErrorCategoryRegistry) *Executor {
	return &Executor{graph: graph, registry: registry}
}

// Execute validates the review call arguments, persists the review, and
// updates the reviewed agent's statistics.
//
// Argument validation errors and business rule violations are surfaced as
// non-nil ToolResult.Error strings rather than Go errors. Go errors are
// reserved for infrastructure failures that the agentic-tools dispatcher
// should treat as fatal.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	scenarioID, ok := stringArg(call.Arguments, "scenario_id")
	if !ok || scenarioID == "" {
		return errorResult(call, `missing required argument "scenario_id"`), nil
	}

	agentID, ok := stringArg(call.Arguments, "agent_id")
	if !ok || agentID == "" {
		return errorResult(call, `missing required argument "agent_id"`), nil
	}

	verdictRaw, ok := stringArg(call.Arguments, "verdict")
	if !ok || verdictRaw == "" {
		return errorResult(call, `missing required argument "verdict"`), nil
	}

	q1, ok := intArg(call.Arguments, "q1_correctness")
	if !ok {
		return errorResult(call, `missing required argument "q1_correctness"`), nil
	}

	q2, ok := intArg(call.Arguments, "q2_quality")
	if !ok {
		return errorResult(call, `missing required argument "q2_quality"`), nil
	}

	q3, ok := intArg(call.Arguments, "q3_completeness")
	if !ok {
		return errorResult(call, `missing required argument "q3_completeness"`), nil
	}

	// explanation is optional at the argument level; business rules inside
	// Review.Validate() enforce when it is required.
	explanation, _ := stringArg(call.Arguments, "explanation")

	var errors []agentgraph.ReviewErrorRef
	if rawErrors, exists := call.Arguments["errors"]; exists && rawErrors != nil {
		var err error
		errors, err = parseErrors(rawErrors)
		if err != nil {
			return errorResult(call, fmt.Sprintf("invalid errors argument: %s", err)), nil
		}
	}

	review := agentgraph.Review{
		ID:             uuid.New().String(),
		ScenarioID:     scenarioID,
		AgentID:        agentID,
		Verdict:        agentgraph.ReviewVerdict(verdictRaw),
		Q1Correctness:  q1,
		Q2Quality:      q2,
		Q3Completeness: q3,
		Errors:         errors,
		Explanation:    explanation,
		Timestamp:      time.Now(),
	}

	if err := review.Validate(e.registry); err != nil {
		return errorResult(call, fmt.Sprintf("invalid review: %s", err)), nil
	}

	agent, err := e.graph.GetAgent(ctx, agentID)
	if err != nil {
		return errorResult(call, fmt.Sprintf("load agent %q: %s", agentID, err)), nil
	}

	if err := e.graph.RecordReview(ctx, review); err != nil {
		return errorResult(call, fmt.Sprintf("record review: %s", err)), nil
	}

	// Update agent review statistics. Best-effort: a stats failure must not
	// prevent the review from being recorded.
	agent.ReviewStats.UpdateStats(q1, q2, q3)
	if err := e.graph.UpdateAgentStats(ctx, agentID, agent.ReviewStats); err != nil {
		// Log-level concern; the review is already persisted so we proceed.
		_ = err
	}

	// Increment per-category error counts on rejection. Best-effort.
	var categoryIDs []string
	if review.Verdict == agentgraph.VerdictRejected {
		for _, ref := range review.Errors {
			categoryIDs = append(categoryIDs, ref.CategoryID)
		}
		if len(categoryIDs) > 0 {
			if err := e.graph.IncrementAgentErrorCounts(ctx, agentID, categoryIDs); err != nil {
				// Best-effort: the review is already committed.
				_ = err
			}
		}
	}

	response := map[string]any{
		"verdict":     string(review.Verdict),
		"avg_rating":  review.Average(),
		"scenario_id": review.ScenarioID,
		"agent_id":    review.AgentID,
		"errors":      categoryIDsOrNil(categoryIDs),
	}

	return jsonResult(call, response)
}

// ListTools returns the single tool definition for review_scenario.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: toolName,
		Description: "Submit a structured peer review of a scenario implementation. " +
			"Rate the work on three dimensions (1–5 each): correctness (are acceptance criteria met?), " +
			"quality (do patterns and SOPs align?), completeness (edge cases, tests, docs). " +
			"Rating anchors: 3 = meets expectations, 5 = exceptional (rare). " +
			"Anti-inflation: all-5s accepted reviews require a written explanation. " +
			"Rejected reviews must cite at least one error category and explain the verdict " +
			"when the average drops below 3.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"scenario_id", "agent_id", "verdict", "q1_correctness", "q2_quality", "q3_completeness"},
			"properties": map[string]any{
				"scenario_id": map[string]any{
					"type":        "string",
					"description": "ID of the scenario whose implementation is under review",
				},
				"agent_id": map[string]any{
					"type":        "string",
					"description": "ID of the persistent agent whose work is being reviewed",
				},
				"verdict": map[string]any{
					"type":        "string",
					"enum":        []string{"accepted", "rejected"},
					"description": "Overall review outcome",
				},
				"q1_correctness": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     5,
					"description": "Correctness rating: are all acceptance criteria met? (1–5, 3 = meets expectations)",
				},
				"q2_quality": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     5,
					"description": "Quality rating: do implementation patterns and SOPs align? (1–5, 3 = meets expectations)",
				},
				"q3_completeness": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     5,
					"description": "Completeness rating: edge cases, tests, and documentation covered? (1–5, 3 = meets expectations)",
				},
				"errors": map[string]any{
					"type":        "array",
					"description": "Error categories observed during the review. Required on rejection.",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"category_id"},
						"properties": map[string]any{
							"category_id": map[string]any{
								"type":        "string",
								"description": "Registered error category ID (e.g. missing_tests, wrong_pattern)",
							},
							"related_entity_ids": map[string]any{
								"type":        "array",
								"description": "Optional graph entity IDs related to this error (SOPs, files, etc.)",
								"items":       map[string]any{"type": "string"},
							},
						},
					},
				},
				"explanation": map[string]any{
					"type": "string",
					"description": "Human-readable context for the verdict. " +
						"Required when: (a) rejected with average below 3, or (b) accepted with all-5s ratings.",
				},
			},
		},
	}}
}

// -- helpers --

// intArg extracts an integer from the arguments map, handling both float64
// (produced by JSON unmarshalling into map[string]any) and direct int types.
// Returns (0, false) when the key is absent or the value cannot be converted.
func intArg(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

// parseErrors converts the raw "errors" argument (a []any from JSON
// unmarshalling into map[string]any) into a slice of ReviewErrorRef.
// Each element must be a map[string]any with a required "category_id" string
// and an optional "related_entity_ids" string array.
func parseErrors(raw any) ([]agentgraph.ReviewErrorRef, error) {
	slice, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("errors must be an array, got %T", raw)
	}

	refs := make([]agentgraph.ReviewErrorRef, 0, len(slice))
	for i, item := range slice {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("errors[%d] must be an object, got %T", i, item)
		}

		catID, ok := m["category_id"].(string)
		if !ok || catID == "" {
			return nil, fmt.Errorf("errors[%d]: missing required field \"category_id\"", i)
		}

		var relatedIDs []string
		if rawRelated, exists := m["related_entity_ids"]; exists && rawRelated != nil {
			related, ok := rawRelated.([]any)
			if !ok {
				return nil, fmt.Errorf("errors[%d]: related_entity_ids must be an array, got %T", i, rawRelated)
			}
			for j, entry := range related {
				s, ok := entry.(string)
				if !ok {
					return nil, fmt.Errorf("errors[%d].related_entity_ids[%d] must be a string, got %T", i, j, entry)
				}
				relatedIDs = append(relatedIDs, s)
			}
		}

		refs = append(refs, agentgraph.ReviewErrorRef{
			CategoryID:       catID,
			RelatedEntityIDs: relatedIDs,
		})
	}

	return refs, nil
}

// categoryIDsOrNil returns nil when the slice is empty so the JSON output
// serialises as null rather than []. Callers can distinguish "no errors" from
// "error list present but empty".
func categoryIDsOrNil(ids []string) any {
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// jsonResult marshals v to JSON and returns a successful ToolResult.
// A marshalling failure is returned as an error ToolResult rather than a Go
// error, because the failure indicates a programming error in the executor
// (unexpected type) rather than an infrastructure error.
func jsonResult(call agentic.ToolCall, v any) (agentic.ToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return errorResult(call, fmt.Sprintf("failed to marshal result: %s", err)), nil
	}
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(data),
		LoopID:  call.LoopID,
		TraceID: call.TraceID,
	}, nil
}

// errorResult returns a ToolResult carrying an error message with no Go error.
// The distinction matters: Go errors from Execute signal infrastructure
// failures to the agentic-tools dispatcher; ToolResult.Error is forwarded to
// the LLM as structured feedback.
func errorResult(call agentic.ToolCall, msg string) agentic.ToolResult {
	return agentic.ToolResult{
		CallID:  call.ID,
		Error:   msg,
		LoopID:  call.LoopID,
		TraceID: call.TraceID,
	}
}

// stringArg extracts a string value from the top-level arguments map by key.
// Returns ("", false) when the key is absent or the value is not a string.
func stringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
