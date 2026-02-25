package llm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func TestCallPayload_Schema(t *testing.T) {
	payload := &CallPayload{}
	schema := payload.Schema()

	if schema.Domain != "llm" {
		t.Errorf("expected domain 'llm', got %q", schema.Domain)
	}
	if schema.Category != "call" {
		t.Errorf("expected category 'call', got %q", schema.Category)
	}
	if schema.Version != "v1" {
		t.Errorf("expected version 'v1', got %q", schema.Version)
	}
}

func TestCallPayload_EntityID(t *testing.T) {
	payload := &CallPayload{
		ID: "test-entity-id",
	}
	if got := payload.EntityID(); got != "test-entity-id" {
		t.Errorf("EntityID() = %q, want %q", got, "test-entity-id")
	}
}

func TestCallPayload_Triples(t *testing.T) {
	triples := []message.Triple{
		{Subject: "s1", Predicate: "p1", Object: "o1"},
		{Subject: "s2", Predicate: "p2", Object: "o2"},
	}
	payload := &CallPayload{
		ID:         "test-id",
		TripleData: triples,
	}

	got := payload.Triples()
	if len(got) != 2 {
		t.Errorf("expected 2 triples, got %d", len(got))
	}
}

func TestCallPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		payload *CallPayload
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid payload",
			payload: &CallPayload{
				ID:         "test-id",
				TripleData: []message.Triple{{Subject: "s", Predicate: "p", Object: "o"}},
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			payload: &CallPayload{
				ID:         "",
				TripleData: []message.Triple{{Subject: "s", Predicate: "p", Object: "o"}},
			},
			wantErr: true,
			errMsg:  "entity ID is required",
		},
		{
			name: "empty triples",
			payload: &CallPayload{
				ID:         "test-id",
				TripleData: []message.Triple{},
			},
			wantErr: true,
			errMsg:  "at least one triple is required",
		},
		{
			name: "nil triples",
			payload: &CallPayload{
				ID:         "test-id",
				TripleData: nil,
			},
			wantErr: true,
			errMsg:  "at least one triple is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCallPayload_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip
	payload := &CallPayload{
		ID: "test-id",
		TripleData: []message.Triple{
			{Subject: "s1", Predicate: "p1", Object: "o1"},
		},
		UpdatedAt: now,
	}

	// Marshal
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Unmarshal
	var unmarshaled CallPayload
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify
	if unmarshaled.ID != payload.ID {
		t.Errorf("ID mismatch: got %q, want %q", unmarshaled.ID, payload.ID)
	}
	if len(unmarshaled.TripleData) != 1 {
		t.Errorf("TripleData length mismatch: got %d, want 1", len(unmarshaled.TripleData))
	}
	if !unmarshaled.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt mismatch: got %v, want %v", unmarshaled.UpdatedAt, now)
	}
}

func TestLLMCallType(t *testing.T) {
	if LLMCallType.Domain != "llm" {
		t.Errorf("LLMCallType.Domain = %q, want 'llm'", LLMCallType.Domain)
	}
	if LLMCallType.Category != "call" {
		t.Errorf("LLMCallType.Category = %q, want 'call'", LLMCallType.Category)
	}
	if LLMCallType.Version != "v1" {
		t.Errorf("LLMCallType.Version = %q, want 'v1'", LLMCallType.Version)
	}
}
