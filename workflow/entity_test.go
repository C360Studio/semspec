package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/message"
)

func TestEntityID_SixParts(t *testing.T) {
	// All entity ID functions must produce exactly 6 dot-separated parts.
	tests := []struct {
		name string
		id   string
	}{
		{"plan", PlanEntityID("my-plan")},
		{"spec", SpecEntityID("my-plan")},
		{"tasks", TasksEntityID("my-plan")},
		{"task", TaskEntityID("my-plan", 1)},
		{"phase", PhaseEntityID("my-plan", 1)},
		{"phases", PhasesEntityID("my-plan")},
		{"approval", ApprovalEntityID("test-uuid-123")},
		{"question", QuestionEntityID("q-abc12345")},
		{"requirement", RequirementEntityID("requirement.my-plan.1")},
		{"scenario", ScenarioEntityID("scenario.my-plan.1")},
		{"proposal", ChangeProposalEntityID("change-proposal.my-plan.1")},
		{"dag-node", DAGNodeEntityID("exec-id-with.dots", "node-1")},
		{"project", ProjectEntityID("my-project")},
		{"project-config", ProjectConfigEntityID("checklist")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := strings.Split(tt.id, ".")
			if len(parts) != 6 {
				t.Errorf("%s: got %d parts, want 6: %q", tt.name, len(parts), tt.id)
			}
			// Instance (part 6) should be 16 hex chars.
			instance := parts[len(parts)-1]
			if len(instance) != 16 {
				t.Errorf("%s: instance %q is %d chars, want 16", tt.name, instance, len(instance))
			}
		})
	}
}

func TestEntityID_Deterministic(t *testing.T) {
	// Same inputs produce same hash.
	a := PlanEntityID("my-plan")
	b := PlanEntityID("my-plan")
	if a != b {
		t.Errorf("PlanEntityID not deterministic: %q != %q", a, b)
	}
}

func TestEntityID_DifferentInputs(t *testing.T) {
	a := PlanEntityID("plan-a")
	b := PlanEntityID("plan-b")
	if a == b {
		t.Errorf("different inputs should produce different IDs: %q == %q", a, b)
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

// ExtractSlugFromTaskID was removed — with hashed instance segments,
// slugs cannot be extracted from entity IDs. The slug is a triple on the entity.
