package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkflowTaskPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		payload WorkflowTaskPayload
		wantErr string
	}{
		{
			name:    "missing task_id",
			payload: WorkflowTaskPayload{Role: "writer", WorkflowSlug: "test", WorkflowStep: "plan", Prompt: "test"},
			wantErr: "task_id",
		},
		{
			name:    "missing role",
			payload: WorkflowTaskPayload{TaskID: "123", WorkflowSlug: "test", WorkflowStep: "plan", Prompt: "test"},
			wantErr: "role",
		},
		{
			name:    "missing workflow_slug",
			payload: WorkflowTaskPayload{TaskID: "123", Role: "writer", WorkflowStep: "plan", Prompt: "test"},
			wantErr: "workflow_slug",
		},
		{
			name:    "missing workflow_step",
			payload: WorkflowTaskPayload{TaskID: "123", Role: "writer", WorkflowSlug: "test", Prompt: "test"},
			wantErr: "workflow_step",
		},
		{
			name:    "missing prompt",
			payload: WorkflowTaskPayload{TaskID: "123", Role: "writer", WorkflowSlug: "test", WorkflowStep: "plan"},
			wantErr: "prompt",
		},
		{
			name: "valid payload",
			payload: WorkflowTaskPayload{
				TaskID:       "task-123",
				WorkflowID:   "test-workflow",
				Role:         "writer",
				WorkflowSlug: "test-feature",
				WorkflowStep: "plan",
				Prompt:       "Generate a plan",
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

func TestWorkflowTaskPayload_JSON_WorkflowID(t *testing.T) {
	payload := WorkflowTaskPayload{
		TaskID:       "task-123",
		WorkflowID:   "test-workflow",
		Role:         "writer",
		WorkflowSlug: "test-feature",
		WorkflowStep: "plan",
		Prompt:       "Generate a plan",
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

	// Unmarshal
	var decoded WorkflowTaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.WorkflowID != payload.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", decoded.WorkflowID, payload.WorkflowID)
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{Field: "test_field", Message: "is required"}
	expected := "test_field: is required"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
