// Package terminal provides terminal tools that signal loop completion.
// Both tools return ToolResult with StopLoop=true, which causes the
// semstreams agentic loop to exit immediately. The Content becomes
// the LoopCompletedEvent.Result.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
)

// Executor handles terminal tools (submit_work).
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
			Description: "Submit your completed work. Call this when you have finished the task.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{
						"type":        "string",
						"description": "Brief summary of what was accomplished",
					},
					"files_modified": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "List of files created or modified",
					},
				},
				"required": []string{"summary"},
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

// submitWork signals task completion. The JSON content becomes the
// LoopCompletedEvent.Result, which downstream orchestrators parse.
func (e *Executor) submitWork(call agentic.ToolCall) (agentic.ToolResult, error) {
	summary, _ := call.Arguments["summary"].(string)
	if summary == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "summary is required — describe what you accomplished",
		}, nil
	}

	result := map[string]any{
		"type":    "work_product",
		"summary": summary,
	}

	if files, ok := call.Arguments["files_modified"].([]any); ok && len(files) > 0 {
		var fileStrs []string
		for _, f := range files {
			if s, ok := f.(string); ok {
				fileStrs = append(fileStrs, s)
			}
		}
		result["files_modified"] = fileStrs
	}

	data, _ := json.Marshal(result)
	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  string(data),
		StopLoop: true,
	}, nil
}

