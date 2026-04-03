package terminal

import (
	"context"
	"encoding/json"
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
	if parsed["summary"] != "Implemented auth middleware" {
		t.Errorf("summary = %v", parsed["summary"])
	}
	files, ok := parsed["files_modified"].([]any)
	if !ok || len(files) != 2 {
		t.Errorf("files_modified = %v, want 2 entries", parsed["files_modified"])
	}
}

func TestSubmitWork_RequiresArguments(t *testing.T) {
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
		t.Error("expected error for empty arguments")
	}
}

// ask_question is no longer a terminal tool — it moved to tools/question/executor.go
// and does NOT set StopLoop=true.

func TestSubmitWork_ReviewDeliverable(t *testing.T) {
	e := NewExecutor()
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-rev",
		Name: "submit_work",
		Arguments: map[string]any{
			"verdict":  "approved",
			"feedback": "Implementation correctly handles all acceptance criteria.",
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
			"feedback": "looks good",
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
			"verdict":  "rejected",
			"feedback": "needs fixes",
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
