package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkflowTriggerPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		payload WorkflowTriggerPayload
		wantErr string
	}{
		{
			name:    "missing workflow_id",
			payload: WorkflowTriggerPayload{Slug: "test"},
			wantErr: "workflow_id",
		},
		{
			name:    "missing slug",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow"},
			wantErr: "slug",
		},
		{
			name: "valid payload",
			payload: WorkflowTriggerPayload{
				WorkflowID: "test-workflow",
				Slug:       "test-feature",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				// Verify it's a ValidationError
				if _, ok := err.(*ValidationError); !ok {
					t.Errorf("expected *ValidationError, got %T", err)
				}
			}
		})
	}
}

func TestWorkflowTriggerPayload_JSON(t *testing.T) {
	payload := WorkflowTriggerPayload{
		WorkflowID:  "test-workflow",
		Role:        "writer",
		Model:       "qwen",
		Prompt:      "Generate a plan",
		UserID:      "user-123",
		ChannelType: "cli",
		ChannelID:   "session-456",
		RequestID:   "req-789",
		Slug:        "test-feature",
		Title:       "Test Feature",
		Description: "A test feature",
		Auto:        true,
	}

	// Marshal
	data, err := json.Marshal(&payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify workflow_id is in JSON
	if !strings.Contains(string(data), `"workflow_id":"test-workflow"`) {
		t.Errorf("JSON does not contain workflow_id: %s", data)
	}

	// Verify slug is in JSON
	if !strings.Contains(string(data), `"slug":"test-feature"`) {
		t.Errorf("JSON does not contain slug: %s", data)
	}

	// Unmarshal
	var decoded WorkflowTriggerPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.WorkflowID != payload.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", decoded.WorkflowID, payload.WorkflowID)
	}
	if decoded.Slug != payload.Slug {
		t.Errorf("Slug = %q, want %q", decoded.Slug, payload.Slug)
	}
	if decoded.Auto != payload.Auto {
		t.Errorf("Auto = %v, want %v", decoded.Auto, payload.Auto)
	}
	if decoded.Model != payload.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, payload.Model)
	}
}

func TestWorkflowTriggerPayload_UnmarshalNestedData(t *testing.T) {
	// Test that we can unmarshal the old nested format for backward compat
	oldFormat := `{
		"workflow_id": "plan-review-loop",
		"role": "planner",
		"request_id": "req-123",
		"data": {
			"slug": "add-feature",
			"title": "Add Feature",
			"description": "Add a new feature",
			"trace_id": "trace-456"
		}
	}`

	var payload TriggerPayload
	if err := json.Unmarshal([]byte(oldFormat), &payload); err != nil {
		t.Fatalf("failed to unmarshal old format: %v", err)
	}

	if payload.WorkflowID != "plan-review-loop" {
		t.Errorf("WorkflowID = %q, want %q", payload.WorkflowID, "plan-review-loop")
	}
	if payload.Slug != "add-feature" {
		t.Errorf("Slug = %q, want %q (extracted from nested data)", payload.Slug, "add-feature")
	}
	if payload.Title != "Add Feature" {
		t.Errorf("Title = %q, want %q (extracted from nested data)", payload.Title, "Add Feature")
	}
	if payload.TraceID != "trace-456" {
		t.Errorf("TraceID = %q, want %q (extracted from nested data)", payload.TraceID, "trace-456")
	}
}

// sampleTrigger returns a TriggerPayload with fields populated so tests
// can assert nothing was dropped. Includes fields used across all workflows
// (plan-review-loop, task-review-loop, task-execution-loop).
func sampleTrigger() TriggerPayload {
	return TriggerPayload{
		WorkflowID:    "plan-review-loop",
		Role:          "planner",
		Model:         "qwen",
		Prompt:        "Add a goodbye endpoint",
		RequestID:     "req-123",
		TraceID:       "trace-abc",
		Slug:          "add-goodbye-endpoint",
		Title:         "Add goodbye endpoint",
		Description:   "Add a goodbye endpoint that returns a farewell message",
		ProjectID:     "proj-42",
		ScopePatterns: []string{"src/**/*.go"},
		// Data blob includes task-execution-loop fields not on the struct.
		Data: json.RawMessage(`{"task_id":"task.add-goodbye-endpoint.1"}`),
	}
}

// TestTriggerPayload_TraceIDSurvivesFlattening verifies that trace_id in
// TriggerPayload survives the semstreams buildMergedPayload() flattening.
func TestTriggerPayload_TraceIDSurvivesFlattening(t *testing.T) {
	trigger := sampleTrigger()
	merged := simulateMergedPayload(t, &trigger)

	traceID, ok := merged["trace_id"]
	if !ok {
		t.Fatal("trace_id not in merged payload — lost during workflow interpolation")
	}
	if traceID != "trace-abc" {
		t.Errorf("trace_id = %q, want %q", traceID, "trace-abc")
	}
}

// TestTriggerPayload_AllFieldsFlatten ensures all semspec-specific fields
// are accessible at the top level of the merged payload.
func TestTriggerPayload_AllFieldsFlatten(t *testing.T) {
	trigger := sampleTrigger()
	merged := simulateMergedPayload(t, &trigger)

	requiredFields := map[string]string{
		"slug":        "add-goodbye-endpoint",
		"title":       "Add goodbye endpoint",
		"description": "Add a goodbye endpoint that returns a farewell message",
		"project_id":  "proj-42",
		"trace_id":    "trace-abc",
	}

	for field, want := range requiredFields {
		got, ok := merged[field]
		if !ok {
			t.Errorf("field %q missing from merged payload", field)
			continue
		}
		if fmt, ok := got.(string); ok && fmt != want {
			t.Errorf("merged[%q] = %q, want %q", field, fmt, want)
		}
	}
}

func TestMarshalTriggerData(t *testing.T) {
	data := MarshalTriggerData(
		"test-slug",
		"Test Title",
		"Test Description",
		"trace-123",
		"proj-456",
		[]string{"src/**/*.go"},
		true,
	)

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["slug"] != "test-slug" {
		t.Errorf("slug = %q, want %q", parsed["slug"], "test-slug")
	}
	if parsed["title"] != "Test Title" {
		t.Errorf("title = %q, want %q", parsed["title"], "Test Title")
	}
	if parsed["trace_id"] != "trace-123" {
		t.Errorf("trace_id = %q, want %q", parsed["trace_id"], "trace-123")
	}
	if parsed["auto"] != true {
		t.Errorf("auto = %v, want true", parsed["auto"])
	}
}

func TestNewSemstreamsTrigger(t *testing.T) {
	trigger := NewSemstreamsTrigger(
		"plan-review-loop",
		"planner",
		"Test prompt",
		"req-123",
		"test-slug",
		"Test Title",
		"Test Description",
		"trace-456",
		"proj-789",
		[]string{"**/*.go"},
		false,
	)

	if trigger.WorkflowID != "plan-review-loop" {
		t.Errorf("WorkflowID = %q, want %q", trigger.WorkflowID, "plan-review-loop")
	}
	if trigger.Role != "planner" {
		t.Errorf("Role = %q, want %q", trigger.Role, "planner")
	}
	if trigger.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want %q", trigger.RequestID, "req-123")
	}

	// Verify Data blob contains correct fields
	var data map[string]any
	if err := json.Unmarshal(trigger.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal Data: %v", err)
	}
	if data["slug"] != "test-slug" {
		t.Errorf("Data.slug = %q, want %q", data["slug"], "test-slug")
	}
	if data["trace_id"] != "trace-456" {
		t.Errorf("Data.trace_id = %q, want %q", data["trace_id"], "trace-456")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// simulateMergedPayload replicates the semstreams workflow-processor
// buildMergedPayload() behavior: Data blob is parsed first (base layer),
// then struct fields are overlaid.
func simulateMergedPayload(t *testing.T, trigger *TriggerPayload) map[string]any {
	t.Helper()
	result := make(map[string]any)

	// Step 1: Parse Data blob (base layer) - this is where custom fields come from
	if len(trigger.Data) > 0 {
		if err := json.Unmarshal(trigger.Data, &result); err != nil {
			// If Data is not JSON, try marshaling the semspec fields directly
			result["_data"] = string(trigger.Data)
		}
	}

	// For flattened TriggerPayload, add the semspec fields directly
	if trigger.Slug != "" {
		result["slug"] = trigger.Slug
	}
	if trigger.Title != "" {
		result["title"] = trigger.Title
	}
	if trigger.Description != "" {
		result["description"] = trigger.Description
	}
	if trigger.ProjectID != "" {
		result["project_id"] = trigger.ProjectID
	}
	if trigger.TraceID != "" {
		result["trace_id"] = trigger.TraceID
	}
	if trigger.Auto {
		result["auto"] = trigger.Auto
	}
	if len(trigger.ScopePatterns) > 0 {
		result["scope_patterns"] = trigger.ScopePatterns
	}

	// Step 2: Overlay ONLY the fields that semstreams TriggerPayload knows.
	// This is the authoritative list from semstreams execution.go.
	if trigger.WorkflowID != "" {
		result["workflow_id"] = trigger.WorkflowID
	}
	if trigger.Role != "" {
		result["role"] = trigger.Role
	}
	if trigger.Model != "" {
		result["model"] = trigger.Model
	}
	if trigger.Prompt != "" {
		result["prompt"] = trigger.Prompt
	}
	if trigger.UserID != "" {
		result["user_id"] = trigger.UserID
	}
	if trigger.ChannelType != "" {
		result["channel_type"] = trigger.ChannelType
	}
	if trigger.ChannelID != "" {
		result["channel_id"] = trigger.ChannelID
	}
	if trigger.RequestID != "" {
		result["request_id"] = trigger.RequestID
	}

	return result
}


