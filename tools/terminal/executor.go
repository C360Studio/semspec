// Package terminal provides terminal tools that signal loop completion.
// Terminal tools return ToolResult with StopLoop=true, which causes the
// semstreams agentic loop to exit immediately. The Content becomes
// the LoopCompletedEvent.Result.
//
// submit_work arguments ARE the structured output — the fields described
// in the agent's output format instructions are passed directly as arguments.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// Executor handles the submit_work terminal tool.
type Executor struct{}

// NewExecutor creates a terminal tool executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// ListTools returns the terminal tool definitions.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "submit_work",
			Description: "Submit your completed work. The arguments to this function ARE your output — include the fields described in your output format instructions.",
			Parameters: map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
		// ask_question is handled by tools/question/executor.go (non-terminal tool).
	}
}

// Execute handles terminal tool calls.
func (e *Executor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "submit_work":
		return e.submitWork(call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown terminal tool: %s", call.Name),
		}, nil
	}
}

// submitWork signals task completion. The call arguments ARE the structured output —
// agents pass their deliverable fields directly (no summary/deliverable wrapper).
// The JSON content becomes the LoopCompletedEvent.Result for downstream parsers.
//
// When deliverable_type metadata is present, arguments are validated against the
// role-specific schema. Validation errors return StopLoop=false so the LLM can
// fix and retry within the same loop iteration.
func (e *Executor) submitWork(call agentic.ToolCall) (agentic.ToolResult, error) {
	if len(call.Arguments) == 0 {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "arguments are empty — include the fields from your output format instructions",
		}, nil
	}

	deliverableType, _ := call.Metadata["deliverable_type"].(string)
	if validator := GetDeliverableValidator(deliverableType); validator != nil {
		if err := validator(call.Arguments); err != nil {
			slog.Warn("submit_work validation failed",
				"deliverable_type", deliverableType,
				"error", err.Error(),
				"call_id", call.ID,
				"keys", deliverableKeys(call.Arguments))
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("validation failed: %s", err.Error()),
			}, nil
		}
	}

	data, _ := json.Marshal(call.Arguments)
	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(data),
		StopLoop: true,
	}, nil
}

// deliverableKeys returns a diagnostic string showing each key and its value type.
// Example: "goal:[]interface{}(2), context:string, scope:map[string]interface{}"
func deliverableKeys(d map[string]any) string {
	parts := make([]string, 0, len(d))
	for k, v := range d {
		switch val := v.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s:string(%d)", k, len(val)))
		case []any:
			parts = append(parts, fmt.Sprintf("%s:[]any(%d)", k, len(val)))
		case map[string]any:
			parts = append(parts, fmt.Sprintf("%s:map(%d)", k, len(val)))
		case float64:
			parts = append(parts, fmt.Sprintf("%s:number", k))
		case bool:
			parts = append(parts, fmt.Sprintf("%s:bool", k))
		case nil:
			parts = append(parts, fmt.Sprintf("%s:null", k))
		default:
			parts = append(parts, fmt.Sprintf("%s:%T", k, v))
		}
	}
	return strings.Join(parts, ", ")
}
