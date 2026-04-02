// Package terminal provides terminal tools that signal loop completion.
// Terminal tools return ToolResult with StopLoop=true, which causes the
// semstreams agentic loop to exit immediately. The Content becomes
// the LoopCompletedEvent.Result.
//
// submit_work supports an optional "deliverable" field for structured output.
// When present, the deliverable is validated against a role-specific schema
// (determined by the "deliverable_type" task metadata). Validation errors
// return StopLoop=false, giving the LLM a chance to fix and retry within
// the same loop iteration.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"
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
			Description: "Submit your completed work. Call this when you have finished the task. For structured deliverables (plans, requirements, scenarios), include a 'deliverable' object matching the schema described in your instructions.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{
						"type":        "string",
						"description": "Brief summary of what was accomplished",
					},
					"deliverable": map[string]any{
						"type":        "object",
						"description": "Structured work product matching the deliverable schema in your instructions. When present, this is validated and becomes the loop result.",
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
//
// When a "deliverable" argument is present, it is validated against the
// role-specific schema (looked up via deliverable_type in task metadata).
// Validation errors return StopLoop=false so the LLM can fix and retry.
// On success, the deliverable JSON becomes the loop result directly.
//
// Without a deliverable, the legacy summary+files_modified behavior applies.
func (e *Executor) submitWork(call agentic.ToolCall) (agentic.ToolResult, error) {
	summary, _ := call.Arguments["summary"].(string)
	if summary == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "summary is required — describe what you accomplished",
		}, nil
	}

	if looksLikeQuestion(summary) {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "Your submission looks like a question, not completed work. Use ask_question instead of submit_work when you need clarification.",
		}, nil
	}

	// Structured deliverable path: validate and return deliverable as result.
	if deliverable, ok := call.Arguments["deliverable"].(map[string]any); ok {
		deliverableType, _ := call.Metadata["deliverable_type"].(string)
		if validator := GetDeliverableValidator(deliverableType); validator != nil {
			if err := validator(deliverable); err != nil {
				return agentic.ToolResult{
					CallID: call.ID,
					Error:  fmt.Sprintf("deliverable validation failed: %s", err.Error()),
				}, nil // StopLoop=false — LLM retries
			}
		}
		data, _ := json.Marshal(deliverable)
		return agentic.ToolResult{
			CallID:   call.ID,
			Content:  string(data),
			StopLoop: true,
		}, nil
	}

	// Legacy path: summary + files_modified wrapper.
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

// looksLikeQuestion detects when an agent submits a question instead of work.
// Borrowed from semdragon's anti-pattern guard — prevents wasted review cycles
// when agents misuse submit_work for clarification requests.
func looksLikeQuestion(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Check for common question phrases at the start.
	questionPrefixes := []string{
		"could you", "can you", "should i", "how do i", "how should",
		"what should", "where should", "i need clarification",
		"i'm not sure", "i have a question", "please clarify",
	}
	for _, prefix := range questionPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	// Short single-line text ending with question mark is likely a question.
	if len(lower) < 200 && !strings.Contains(lower, "\n") && strings.HasSuffix(lower, "?") {
		return true
	}

	// High ratio of question-mark lines (>50%) in multi-line text.
	lines := strings.Split(text, "\n")
	questionLines := 0
	nonEmpty := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		nonEmpty++
		if strings.HasSuffix(trimmed, "?") {
			questionLines++
		}
	}
	if nonEmpty > 1 && float64(questionLines)/float64(nonEmpty) > 0.5 {
		return true
	}

	return false
}
