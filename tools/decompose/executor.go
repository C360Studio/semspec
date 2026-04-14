package decompose

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
)

const toolName = "decompose_task"

// Executor implements agentic.ToolExecutor for the decompose_task tool.
// It is a passthrough executor: the LLM provides the DAG structure in its
// tool call arguments and the executor validates it, then returns the
// validated DAG as JSON. No DAG execution happens here — the parent agent
// decides what to do next (spawn nodes individually or trigger the DAG
// execution workflow).
//
// All public methods are safe for concurrent use — the struct holds no
// mutable state.
type Executor struct{}

// NewExecutor constructs a decompose_task Executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// Execute validates the DAG provided by the LLM in the tool call arguments
// and returns the validated DAG as JSON in ToolResult.Content.
//
// Argument validation errors are surfaced as non-nil ToolResult.Error strings
// rather than Go errors. Go errors are reserved for infrastructure failures
// that the agentic-tools dispatcher should treat as fatal — none arise in
// this passthrough implementation.
func (e *Executor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	goal, ok := stringArg(call.Arguments, "goal")
	if !ok || goal == "" {
		return errorResult(call, `missing required argument "goal"`), nil
	}

	rawNodes, ok := call.Arguments["nodes"]
	if !ok {
		return errorResult(call, `missing required argument "nodes"`), nil
	}

	dag, err := parseNodes(rawNodes)
	if err != nil {
		return errorResult(call, fmt.Sprintf("invalid nodes argument: %s", err)), nil
	}

	if err := dag.Validate(); err != nil {
		return errorResult(call, fmt.Sprintf("invalid dag: %s", err)), nil
	}

	// Wrap in the response envelope the spec defines.
	response := map[string]any{
		"goal": goal,
		"dag":  dag,
	}

	return jsonResult(call, response)
}

// ListTools returns the single tool definition for decompose_task.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name: toolName,
		Description: `Decompose a complex goal into a DAG of subtasks. You MUST provide at least one node.

CRITICAL: Each node's prompt MUST include CONCRETE FILE PATHS (e.g., 'Create pkg/auth/middleware.go' not 'Create auth middleware'). Agents share a workspace — without explicit paths they waste iterations exploring. The file_scope array MUST list every file the node may modify.

Example call:
{"goal":"Add health endpoint","nodes":[{"id":"health-handler","prompt":"Create cmd/server/health.go with a HealthHandler that returns JSON {\"status\":\"ok\"}. Register it on GET /health in cmd/server/main.go.","role":"developer","file_scope":["cmd/server/health.go","cmd/server/main.go"],"depends_on":[]},{"id":"health-test","prompt":"Create cmd/server/health_test.go with table-driven tests for the health endpoint: verify 200 status, JSON content-type, and response body.","role":"developer","file_scope":["cmd/server/health_test.go"],"depends_on":["health-handler"]}]}`,
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"goal", "nodes"},
			"properties": map[string]any{
				"goal": map[string]any{
					"type":        "string",
					"description": "High-level goal to decompose",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Additional context for the decomposition",
				},
				"nodes": map[string]any{
					"type":        "array",
					"description": "Subtask nodes forming the DAG",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"id", "prompt", "role", "file_scope"},
						"properties": map[string]any{
							"id": map[string]any{
								"type":        "string",
								"description": "Unique node identifier",
							},
							"prompt": map[string]any{
								"type":        "string",
								"description": "Task prompt with CONCRETE FILE PATHS. Include exact paths to create/modify (e.g., 'Implement JWT validation in pkg/auth/jwt.go'). Vague prompts without paths waste agent iterations.",
							},
							"role": map[string]any{
								"type":        "string",
								"description": "Agent role for the subtask",
							},
							"depends_on": map[string]any{
								"type":        "array",
								"description": "IDs of nodes that must complete before this one",
								"items":       map[string]any{"type": "string"},
							},
							"file_scope": map[string]any{
								"type":        "array",
								"description": "Files or glob patterns this task is allowed to modify (e.g. 'src/auth/*.go', 'pkg/utils/hash.go')",
								"items":       map[string]any{"type": "string"},
							},
							"scenario_ids": map[string]any{
								"type":        "array",
								"description": "IDs of the acceptance criteria scenarios this node addresses. Used to route retry feedback when specific scenarios fail.",
								"items":       map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}}
}

// -- helpers --

// parseNodes converts the raw "nodes" argument (a []any from JSON
// unmarshalling into map[string]any) into a TaskDAG.
// Each element must be a map[string]any with at least "id", "prompt", "role",
// and "file_scope".
func parseNodes(raw any) (TaskDAG, error) {
	slice, ok := raw.([]any)
	if !ok {
		return TaskDAG{}, fmt.Errorf("nodes must be an array, got %T", raw)
	}
	if len(slice) == 0 {
		return TaskDAG{}, fmt.Errorf("nodes array must not be empty")
	}

	nodes := make([]TaskNode, 0, len(slice))
	for i, item := range slice {
		m, ok := item.(map[string]any)
		if !ok {
			return TaskDAG{}, fmt.Errorf("nodes[%d] must be an object, got %T", i, item)
		}

		id, ok := stringField(m, "id")
		if !ok || id == "" {
			return TaskDAG{}, fmt.Errorf("nodes[%d]: missing required field \"id\"", i)
		}
		prompt, ok := stringField(m, "prompt")
		if !ok || prompt == "" {
			return TaskDAG{}, fmt.Errorf("nodes[%d]: missing required field \"prompt\"", i)
		}
		role, ok := stringField(m, "role")
		if !ok || role == "" {
			return TaskDAG{}, fmt.Errorf("nodes[%d]: missing required field \"role\"", i)
		}

		dependsOn, err := stringSliceField(m, "depends_on", i)
		if err != nil {
			return TaskDAG{}, err
		}

		fileScope, err := stringSliceField(m, "file_scope", i)
		if err != nil {
			return TaskDAG{}, err
		}

		scenarioIDs, err := stringSliceField(m, "scenario_ids", i)
		if err != nil {
			return TaskDAG{}, err
		}

		nodes = append(nodes, TaskNode{
			ID:          id,
			Prompt:      prompt,
			Role:        role,
			DependsOn:   dependsOn,
			FileScope:   fileScope,
			ScenarioIDs: scenarioIDs,
		})
	}

	return TaskDAG{Nodes: nodes}, nil
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
		CallID:   call.ID,
		Content:  string(data),
		LoopID:   call.LoopID,
		TraceID:  call.TraceID,
		StopLoop: true,
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

// stringSliceField extracts an optional string array from a node field map.
// Returns nil when the key is absent. Returns an error when the value is
// present but not a []string.
func stringSliceField(m map[string]any, key string, nodeIdx int) ([]string, error) {
	raw, exists := m[key]
	if !exists || raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("nodes[%d]: %s must be an array, got %T", nodeIdx, key, raw)
	}
	result := make([]string, 0, len(arr))
	for j, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("nodes[%d].%s[%d] must be a string, got %T", nodeIdx, key, j, item)
		}
		result = append(result, s)
	}
	return result, nil
}

// stringField extracts a string value from an object field map by key.
// Returns ("", false) when the key is absent or the value is not a string.
func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
