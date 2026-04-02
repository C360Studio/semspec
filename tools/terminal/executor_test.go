package terminal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

func TestSubmitWork_StopsLoop(t *testing.T) {
	e := NewExecutor()
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-1",
		Name: "submit_work",
		Arguments: map[string]any{
			"summary":        "Implemented auth middleware",
			"files_modified": []any{"auth.go", "auth_test.go"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StopLoop {
		t.Error("submit_work must set StopLoop=true")
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("result content is not valid JSON: %v", err)
	}
	if parsed["type"] != "work_product" {
		t.Errorf("type = %v, want work_product", parsed["type"])
	}
	if parsed["summary"] != "Implemented auth middleware" {
		t.Errorf("summary = %v", parsed["summary"])
	}
	files, ok := parsed["files_modified"].([]any)
	if !ok || len(files) != 2 {
		t.Errorf("files_modified = %v, want 2 entries", parsed["files_modified"])
	}
}

func TestSubmitWork_RequiresSummary(t *testing.T) {
	e := NewExecutor()
	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-2",
		Name:      "submit_work",
		Arguments: map[string]any{},
	})
	if result.StopLoop {
		t.Error("should not stop loop on validation error")
	}
	if result.Error == "" {
		t.Error("expected error for missing summary")
	}
}

// ask_question is no longer a terminal tool — it moved to tools/question/executor.go
// and does NOT set StopLoop=true.

func TestSubmitWork_RejectsQuestions(t *testing.T) {
	e := NewExecutor()
	questions := []string{
		"How should I implement the auth middleware?",
		"Could you clarify the requirements for this task?",
		"I need clarification on the API contract",
		"Should I use JWT or session tokens?",
	}
	for _, q := range questions {
		result, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:   "call-q",
			Name: "submit_work",
			Arguments: map[string]any{
				"summary": q,
			},
		})
		if result.StopLoop {
			t.Errorf("should not stop loop for question: %s", q)
		}
		if result.Error == "" {
			t.Errorf("expected error for question submission: %s", q)
		}
	}
}

func TestSubmitWork_AcceptsLegitWork(t *testing.T) {
	e := NewExecutor()
	legit := []string{
		"Implemented auth middleware with JWT validation and rate limiting",
		"Added unit tests for the user service covering create, read, update, delete operations",
		"Refactored database connection pool to use context-aware timeouts. Updated 3 files.",
	}
	for _, s := range legit {
		result, _ := e.Execute(context.Background(), agentic.ToolCall{
			ID:   "call-l",
			Name: "submit_work",
			Arguments: map[string]any{
				"summary": s,
			},
		})
		if result.Error != "" {
			t.Errorf("unexpected error for legit work %q: %s", s, result.Error)
		}
		if !result.StopLoop {
			t.Errorf("expected StopLoop for legit work: %s", s)
		}
	}
}

func TestLooksLikeQuestion(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"How should I implement this?", true},
		{"Could you explain the API?", true},
		{"I need clarification on the scope", true},
		{"Implemented auth middleware with JWT validation", false},
		{"Added tests for user service", false},
		// Long text with question mark is OK (could be a summary mentioning a question)
		{strings.Repeat("x", 250) + "?", false},
		// Majority question-mark lines
		{"What about X?\nWhat about Y?\nWhat about Z?", true},
		// Mixed lines — minority questions
		{"Did X.\nDid Y.\nDid Z.\nWhat about W?", false},
	}
	for _, tt := range tests {
		got := looksLikeQuestion(tt.text)
		if got != tt.want {
			t.Errorf("looksLikeQuestion(%q) = %v, want %v", tt.text[:min(len(tt.text), 50)], got, tt.want)
		}
	}
}

func TestSubmitWork_ReviewDeliverable(t *testing.T) {
	e := NewExecutor()
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-rev",
		Name: "submit_work",
		Arguments: map[string]any{
			"summary": "Code review: approved",
			"deliverable": map[string]any{
				"verdict":  "approved",
				"feedback": "Implementation correctly handles all acceptance criteria.",
			},
		},
		Metadata: map[string]any{
			"deliverable_type": "review",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StopLoop {
		t.Error("review deliverable must set StopLoop=true")
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("result content is not valid JSON: %v", err)
	}
	if parsed["verdict"] != "approved" {
		t.Errorf("verdict = %v, want approved", parsed["verdict"])
	}
}

func TestSubmitWork_ReviewDeliverableValidation(t *testing.T) {
	e := NewExecutor()

	// Missing verdict
	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-rev-bad",
		Name: "submit_work",
		Arguments: map[string]any{
			"summary":     "Review",
			"deliverable": map[string]any{"feedback": "looks good"},
		},
		Metadata: map[string]any{"deliverable_type": "review"},
	})
	if result.StopLoop {
		t.Error("should not stop loop on validation error")
	}
	if result.Error == "" {
		t.Error("expected validation error for missing verdict")
	}

	// Rejected without rejection_type
	result, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-rev-bad2",
		Name: "submit_work",
		Arguments: map[string]any{
			"summary": "Review",
			"deliverable": map[string]any{
				"verdict":  "rejected",
				"feedback": "needs fixes",
			},
		},
		Metadata: map[string]any{"deliverable_type": "review"},
	})
	if result.StopLoop {
		t.Error("should not stop loop when rejection_type missing")
	}
	if result.Error == "" {
		t.Error("expected validation error for missing rejection_type")
	}
}

func TestUnknownTool(t *testing.T) {
	e := NewExecutor()
	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-5",
		Name: "unknown_tool",
	})
	if result.Error == "" {
		t.Error("expected error for unknown tool")
	}
}
