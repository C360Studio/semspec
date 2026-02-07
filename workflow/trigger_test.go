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
			payload: WorkflowTriggerPayload{Slug: "test", Description: "desc"},
			wantErr: "workflow_id",
		},
		{
			name:    "missing slug",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow", Description: "desc"},
			wantErr: "slug",
		},
		{
			name:    "missing description",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow", Slug: "test"},
			wantErr: "description",
		},
		{
			name: "valid payload",
			payload: WorkflowTriggerPayload{
				WorkflowID:  DocumentGenerationWorkflowID,
				Slug:        "test-feature",
				Description: "Test feature description",
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
		WorkflowID:  DocumentGenerationWorkflowID,
		Slug:        "test-feature",
		Title:       "Test Feature",
		Description: "A test feature",
		Prompt:      "Generate a proposal",
		Model:       "claude-sonnet",
		Auto:        true,
		UserID:      "user-123",
		ChannelType: "cli",
		ChannelID:   "session-456",
	}

	// Marshal
	data, err := json.Marshal(&payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Verify workflow_id is in JSON
	if !strings.Contains(string(data), `"workflow_id":"document-generation"`) {
		t.Errorf("JSON does not contain workflow_id: %s", data)
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
}
