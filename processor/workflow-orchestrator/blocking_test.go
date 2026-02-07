package workfloworchestrator

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLoopStateIsBlocked(t *testing.T) {
	tests := []struct {
		name     string
		state    LoopState
		expected bool
	}{
		{
			name: "blocked by questions",
			state: LoopState{
				LoopID:    "loop-1",
				Status:    "complete",
				BlockedBy: []string{"q-123", "q-456"},
			},
			expected: true,
		},
		{
			name: "status is blocked",
			state: LoopState{
				LoopID: "loop-2",
				Status: "blocked",
			},
			expected: true,
		},
		{
			name: "both blocked status and questions",
			state: LoopState{
				LoopID:    "loop-3",
				Status:    "blocked",
				BlockedBy: []string{"q-789"},
			},
			expected: true,
		},
		{
			name: "not blocked - complete",
			state: LoopState{
				LoopID: "loop-4",
				Status: "complete",
			},
			expected: false,
		},
		{
			name: "not blocked - empty blocked by",
			state: LoopState{
				LoopID:    "loop-5",
				Status:    "complete",
				BlockedBy: []string{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.state.IsBlocked()
			if result != tt.expected {
				t.Errorf("IsBlocked() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBlockedLoopSerialization(t *testing.T) {
	blocked := BlockedLoop{
		LoopID:      "loop-abc123",
		QuestionIDs: []string{"q-def456", "q-ghi789"},
		State: LoopState{
			LoopID:       "loop-abc123",
			Role:         "design-writer",
			Status:       "blocked",
			WorkflowSlug: "add-auth",
			WorkflowStep: "design",
			Metadata: map[string]string{
				"title":       "Add Authentication",
				"description": "Add auth to the app",
			},
		},
		BlockedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	// Serialize
	data, err := json.Marshal(blocked)
	if err != nil {
		t.Fatalf("Failed to marshal BlockedLoop: %v", err)
	}

	// Deserialize
	var decoded BlockedLoop
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal BlockedLoop: %v", err)
	}

	// Verify fields
	if decoded.LoopID != blocked.LoopID {
		t.Errorf("LoopID = %v, want %v", decoded.LoopID, blocked.LoopID)
	}
	if len(decoded.QuestionIDs) != 2 {
		t.Errorf("QuestionIDs length = %v, want 2", len(decoded.QuestionIDs))
	}
	if decoded.State.Role != "design-writer" {
		t.Errorf("State.Role = %v, want design-writer", decoded.State.Role)
	}
	if decoded.State.Metadata["title"] != "Add Authentication" {
		t.Errorf("State.Metadata[title] = %v, want 'Add Authentication'", decoded.State.Metadata["title"])
	}
}

func TestLoopStateBlockingFields(t *testing.T) {
	state := LoopState{
		LoopID:        "loop-123",
		Role:          "spec-writer",
		Status:        "blocked",
		WorkflowSlug:  "feature-x",
		WorkflowStep:  "spec",
		BlockedBy:     []string{"q-001"},
		BlockedReason: "Waiting for API clarification",
		BlockedAt:     "2024-01-15T10:30:00Z",
		Metadata: map[string]string{
			"auto_continue": "true",
		},
	}

	// Serialize
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal LoopState: %v", err)
	}

	// Check JSON contains blocking fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, ok := raw["blocked_by"]; !ok {
		t.Error("JSON should contain blocked_by field")
	}
	if _, ok := raw["blocked_reason"]; !ok {
		t.Error("JSON should contain blocked_reason field")
	}
	if _, ok := raw["blocked_at"]; !ok {
		t.Error("JSON should contain blocked_at field")
	}

	// Deserialize back
	var decoded LoopState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal LoopState: %v", err)
	}

	if !decoded.IsBlocked() {
		t.Error("Decoded state should be blocked")
	}
	if decoded.BlockedReason != "Waiting for API clarification" {
		t.Errorf("BlockedReason = %v, want 'Waiting for API clarification'", decoded.BlockedReason)
	}
}

func TestLoopStateOmitEmptyBlockingFields(t *testing.T) {
	state := LoopState{
		LoopID: "loop-456",
		Role:   "proposal-writer",
		Status: "complete",
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	// Check that empty blocking fields are omitted
	if _, ok := raw["blocked_by"]; ok {
		t.Error("Empty blocked_by should be omitted")
	}
	if _, ok := raw["blocked_reason"]; ok {
		t.Error("Empty blocked_reason should be omitted")
	}
	if _, ok := raw["blocked_at"]; ok {
		t.Error("Empty blocked_at should be omitted")
	}
}
