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
			payload: WorkflowTriggerPayload{Data: &WorkflowTriggerData{Slug: "test", Description: "desc"}},
			wantErr: "workflow_id",
		},
		{
			name:    "missing slug",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow", Data: &WorkflowTriggerData{Description: "desc"}},
			wantErr: "slug",
		},
		{
			name:    "missing data",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow"},
			wantErr: "slug",
		},
		{
			name:    "missing description",
			payload: WorkflowTriggerPayload{WorkflowID: "test-workflow", Data: &WorkflowTriggerData{Slug: "test"}},
			wantErr: "description",
		},
		{
			name: "valid payload",
			payload: WorkflowTriggerPayload{
				WorkflowID: DocumentGenerationWorkflowID,
				Data: &WorkflowTriggerData{
					Slug:        "test-feature",
					Description: "Test feature description",
				},
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
		Role:        "proposal-writer",
		Model:       "claude-sonnet",
		Prompt:      "Generate a proposal",
		UserID:      "user-123",
		ChannelType: "cli",
		ChannelID:   "session-456",
		RequestID:   "req-789",
		Data: &WorkflowTriggerData{
			Slug:        "test-feature",
			Title:       "Test Feature",
			Description: "A test feature",
			Auto:        true,
		},
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

	// Verify data.slug is in JSON
	if !strings.Contains(string(data), `"slug":"test-feature"`) {
		t.Errorf("JSON does not contain data.slug: %s", data)
	}

	// Unmarshal
	var decoded WorkflowTriggerPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.WorkflowID != payload.WorkflowID {
		t.Errorf("WorkflowID = %q, want %q", decoded.WorkflowID, payload.WorkflowID)
	}
	if decoded.Data == nil {
		t.Fatal("Data is nil after unmarshal")
	}
	if decoded.Data.Slug != payload.Data.Slug {
		t.Errorf("Data.Slug = %q, want %q", decoded.Data.Slug, payload.Data.Slug)
	}
	if decoded.Data.Auto != payload.Data.Auto {
		t.Errorf("Data.Auto = %v, want %v", decoded.Data.Auto, payload.Data.Auto)
	}
	if decoded.Model != payload.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, payload.Model)
	}
}
