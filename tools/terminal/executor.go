// Package terminal provides terminal tools that signal loop completion.
// Both tools return ToolResult with StopLoop=true, which causes the
// semstreams agentic loop to exit immediately. The Content becomes
// the LoopCompletedEvent.Result.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// Executor handles terminal tools (submit_work, submit_review).
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
		{
			Name:        "submit_review",
			Description: "Submit your code review verdict. Call this when you have finished reviewing.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"verdict": map[string]any{
						"type":        "string",
						"enum":        []string{"approved", "rejected"},
						"description": "Review verdict: approved or rejected",
					},
					"rejection_type": map[string]any{
						"type":        "string",
						"enum":        []string{"fixable", "misscoped", "architectural", "too_big"},
						"description": "Classification of rejection (required when verdict is rejected)",
					},
					"feedback": map[string]any{
						"type":        "string",
						"description": "Specific, actionable feedback with line numbers",
					},
					"confidence": map[string]any{
						"type":        "number",
						"description": "Confidence score 0.0-1.0. Below 0.7 triggers human review",
					},
				},
				"required": []string{"verdict", "feedback"},
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
	case "submit_review":
		return e.submitReview(call)
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

	if looksLikeQuestion(summary) {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "Your submission looks like a question, not completed work. Use ask_question instead of submit_work when you need clarification.",
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

// submitReview signals review completion. The verdict JSON becomes the
// LoopCompletedEvent.Result, which parseCodeReviewResult in execution-manager
// parses directly. Following the semdragon review_sub_quest pattern: a dedicated
// tool for review verdicts ensures the structured result flows cleanly through
// the event pipeline without wrapping.
func (e *Executor) submitReview(call agentic.ToolCall) (agentic.ToolResult, error) {
	verdict, _ := call.Arguments["verdict"].(string)
	if verdict == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  `verdict is required — must be "approved" or "rejected"`,
		}, nil
	}
	if verdict != "approved" && verdict != "rejected" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf(`verdict must be "approved" or "rejected", got %q`, verdict),
		}, nil
	}

	feedback, _ := call.Arguments["feedback"].(string)
	if feedback == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "feedback is required — provide specific, actionable details",
		}, nil
	}

	result := map[string]any{
		"verdict":  verdict,
		"feedback": feedback,
	}

	if rejType, ok := call.Arguments["rejection_type"].(string); ok && rejType != "" {
		result["rejection_type"] = rejType
	}
	if confidence, ok := call.Arguments["confidence"].(float64); ok {
		result["confidence"] = confidence
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
