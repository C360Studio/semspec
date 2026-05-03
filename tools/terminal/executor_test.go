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
	// Empty arguments
	result, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-2",
		Name:      "submit_work",
		Arguments: map[string]any{},
	})
	if result.StopLoop {
		t.Error("should not stop loop on empty arguments")
	}
	if result.Error == "" {
		t.Error("expected error for empty arguments")
	}

	// Nil arguments
	result, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-2b",
		Name:      "submit_work",
		Arguments: nil,
	})
	if result.StopLoop {
		t.Error("should not stop loop on nil arguments")
	}
	if result.Error == "" {
		t.Error("expected error for nil arguments")
	}
}

func TestSubmitWork_EmptyArgsIncludesHint(t *testing.T) {
	e := NewExecutor()

	tests := []struct {
		name            string
		deliverableType string
		wantContains    string
	}{
		{"plan", "plan", `"goal"`},
		{"review", "review", `"verdict"`},
		{"requirements", "requirements", `"requirements"`},
		{"scenarios", "scenarios", `"scenarios"`},
		{"architecture", "architecture", `"actors"`},
		{"developer", "", `"summary"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := e.Execute(context.Background(), agentic.ToolCall{
				ID:        "call-hint",
				Name:      "submit_work",
				Arguments: map[string]any{},
				Metadata:  map[string]any{"deliverable_type": tt.deliverableType},
			})
			if !strings.Contains(result.Error, tt.wantContains) {
				t.Errorf("error %q should contain %q", result.Error, tt.wantContains)
			}
		})
	}
}

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

	// Rejected without rejection_type — validator auto-fills "fixable"
	// (defense-in-depth for the bucket-#4 wedge caught 2026-05-03 v4)
	// rather than rejecting the submission. Loop should now stop and the
	// submitted content should reflect the auto-fill.
	args := map[string]any{
		"verdict":  "rejected",
		"feedback": "needs fixes",
	}
	result, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:        "call-rev-bad2",
		Name:      "submit_work",
		Arguments: args,
		Metadata:  map[string]any{"deliverable_type": "review"},
	})
	if !result.StopLoop {
		t.Error("rejected w/o rejection_type should auto-fill and stop the loop, not error")
	}
	if result.Error != "" {
		t.Errorf("expected no validation error after auto-fill, got: %s", result.Error)
	}
	if got, _ := args["rejection_type"].(string); got != "fixable" {
		t.Errorf("rejection_type after auto-fill = %q, want \"fixable\"", got)
	}

	// Rejected with INVALID rejection_type — still rejects (auto-fill
	// only kicks in when the field is absent; an explicit-but-bad value
	// is a model error to surface).
	result, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-rev-bad3",
		Name: "submit_work",
		Arguments: map[string]any{
			"verdict":        "rejected",
			"feedback":       "needs fixes",
			"rejection_type": "wrong",
		},
		Metadata: map[string]any{"deliverable_type": "review"},
	})
	if result.StopLoop {
		t.Error("should not stop loop on invalid rejection_type")
	}
	if result.Error == "" {
		t.Error("expected validation error for invalid rejection_type")
	}
}

func TestSubmitWork_PlanDeliverable(t *testing.T) {
	e := NewExecutor()
	result, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-plan",
		Name: "submit_work",
		Arguments: map[string]any{
			"goal":    "Add /goodbye endpoint",
			"context": "Flask API with /hello endpoint needs a parallel /goodbye",
		},
		Metadata: map[string]any{"deliverable_type": "plan"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StopLoop {
		t.Error("plan deliverable must set StopLoop=true")
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content), &parsed); err != nil {
		t.Fatalf("result content is not valid JSON: %v", err)
	}
	if parsed["goal"] != "Add /goodbye endpoint" {
		t.Errorf("goal = %v", parsed["goal"])
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
