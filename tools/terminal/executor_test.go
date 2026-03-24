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
