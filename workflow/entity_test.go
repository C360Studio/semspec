package workflow

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/message"
)

func TestApprovalEntityID(t *testing.T) {
	got := ApprovalEntityID("test-uuid-123")
	want := "semspec.local.wf.plan.approval.test-uuid-123"
	if got != want {
		t.Errorf("ApprovalEntityID(%q) = %q, want %q", "test-uuid-123", got, want)
	}
}

func TestQuestionEntityID(t *testing.T) {
	got := QuestionEntityID("q-abc12345")
	want := "semspec.local.wf.plan.question.q-abc12345"
	if got != want {
		t.Errorf("QuestionEntityID(%q) = %q, want %q", "q-abc12345", got, want)
	}
}

func TestEntityPayload_Schema(t *testing.T) {
	tests := []struct {
		name    string
		msgType message.Type
	}{
		{"plan", EntityType},
		{"phase", PhaseEntityType},
		{"approval", ApprovalEntityType},
		{"task", TaskEntityType},
		{"question", QuestionEntityType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewEntityPayload(tt.msgType, "test-id", []message.Triple{
				{Subject: "test-id", Predicate: "test.pred", Object: "val"},
			})
			got := p.Schema()
			if got != tt.msgType {
				t.Errorf("Schema() = %v, want %v", got, tt.msgType)
			}
		})
	}
}

func TestEntityPayload_JSONRoundTrip(t *testing.T) {
	p := NewEntityPayload(EntityType, "semspec.local.wf.plan.plan.test", []message.Triple{
		{Subject: "semspec.local.wf.plan.plan.test", Predicate: "semspec.plan.title", Object: "Test Plan"},
	})

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Verify JSON uses "id" field, not "entity_id"
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if _, ok := raw["id"]; !ok {
		t.Error("marshaled JSON missing 'id' field")
	}
	if _, ok := raw["entity_id"]; ok {
		t.Error("marshaled JSON should not contain 'entity_id' field")
	}

	// Verify round-trip
	var p2 EntityPayload
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if p2.ID != p.ID {
		t.Errorf("ID = %q, want %q", p2.ID, p.ID)
	}
	if len(p2.TripleData) != len(p.TripleData) {
		t.Errorf("TripleData len = %d, want %d", len(p2.TripleData), len(p.TripleData))
	}
}

func TestEntityPayload_Validate(t *testing.T) {
	tests := []struct {
		name    string
		payload *EntityPayload
		wantErr bool
	}{
		{
			name: "valid",
			payload: NewEntityPayload(EntityType, "test-id", []message.Triple{
				{Subject: "s", Predicate: "p", Object: "o"},
			}),
			wantErr: false,
		},
		{
			name:    "missing id",
			payload: NewEntityPayload(EntityType, "", []message.Triple{{Subject: "s", Predicate: "p", Object: "o"}}),
			wantErr: true,
		},
		{
			name:    "no triples",
			payload: NewEntityPayload(EntityType, "test-id", nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.payload.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractSlugFromTaskID(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		wantSlug string
	}{
		{
			name:     "valid single-word slug",
			taskID:   "semspec.local.wf.task.task.my-plan-1",
			wantSlug: "my-plan",
		},
		{
			name:     "valid multi-word slug",
			taskID:   "semspec.local.wf.task.task.add-auth-refresh-3",
			wantSlug: "add-auth-refresh",
		},
		{
			name:     "valid long slug with sequence 10",
			taskID:   "semspec.local.wf.task.task.add-a-goodbye-endpoint-that-returns-a-goodbye-mess-10",
			wantSlug: "add-a-goodbye-endpoint-that-returns-a-goodbye-mess",
		},
		{
			name:     "valid sequence 1",
			taskID:   "semspec.local.wf.task.task.simple-1",
			wantSlug: "simple",
		},
		{
			name:     "empty string",
			taskID:   "",
			wantSlug: "",
		},
		{
			name:     "wrong prefix",
			taskID:   "semspec.local.wf.plan.plan.my-plan",
			wantSlug: "",
		},
		{
			name:     "random string",
			taskID:   "random-string",
			wantSlug: "",
		},
		{
			name:     "prefix only",
			taskID:   "semspec.local.wf.task.task.",
			wantSlug: "",
		},
		{
			name:     "no sequence number",
			taskID:   "semspec.local.wf.task.task.my-plan",
			wantSlug: "",
		},
		{
			name:     "trailing hyphen no digits",
			taskID:   "semspec.local.wf.task.task.my-plan-",
			wantSlug: "",
		},
		{
			name:     "non-digit sequence",
			taskID:   "semspec.local.wf.task.task.my-plan-abc",
			wantSlug: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSlugFromTaskID(tt.taskID)
			if got != tt.wantSlug {
				t.Errorf("ExtractSlugFromTaskID(%q) = %q, want %q", tt.taskID, got, tt.wantSlug)
			}
		})
	}
}
