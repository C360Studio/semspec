package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewContractPacketCapturesRootBriefAndScope(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	scope := Scope{
		Include:    []string{"src/existing.go"},
		Exclude:    []string{"vendor/"},
		DoNotTouch: []string{"secrets.yaml"},
		Create:     []string{"src/new.go"},
	}
	constraints := []string{"extend the existing module", "do not create standalone build roots"}

	packet := NewContractPacket("demo-plan", "Implement the sponsor request", scope, constraints, now)

	if packet.ID != PlanContractID("demo-plan") {
		t.Fatalf("ID = %q, want %q", packet.ID, PlanContractID("demo-plan"))
	}
	if packet.Version != 1 {
		t.Fatalf("Version = %d, want 1", packet.Version)
	}
	if packet.Brief != "Implement the sponsor request" {
		t.Fatalf("Brief = %q", packet.Brief)
	}
	if len(packet.SourceRefs) != 1 || packet.SourceRefs[0].Kind != "user_brief" {
		t.Fatalf("SourceRefs = %#v, want user_brief", packet.SourceRefs)
	}
	if got := packet.Scope.Create[0]; got != "src/new.go" {
		t.Fatalf("Scope.Create[0] = %q", got)
	}
	if got := packet.Constraints[1]; got != constraints[1] {
		t.Fatalf("Constraints[1] = %q", got)
	}

	scope.Create[0] = "mutated.go"
	constraints[1] = "mutated"
	if packet.Scope.Create[0] != "src/new.go" {
		t.Fatalf("packet scope aliased caller slice: %v", packet.Scope.Create)
	}
	if packet.Constraints[1] != "do not create standalone build roots" {
		t.Fatalf("packet constraints aliased caller slice: %v", packet.Constraints)
	}
}

func TestContractPacketJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	packet := &ContractPacket{
		ID:          PlanContractID("demo"),
		Version:     1,
		Brief:       "Keep the brownfield topology.",
		Constraints: []string{"no standalone project"},
		TopologyFacts: []TopologyFact{{
			Kind:     "build_root",
			Path:     "build.gradle",
			Evidence: []string{"build.gradle"},
		}},
		Amendments: []ContractAmendment{{
			ID:             "amendment-1",
			PlanDecisionID: "plan-decision.demo.1",
			Impact: ContractImpact{
				Kind:    ContractImpactRefine,
				Summary: "Narrow one scenario after QA evidence.",
			},
			CreatedAt: now,
		}},
		ValidationFindings: []ContractValidationFinding{{
			Severity:  "error",
			Category:  "topology",
			Message:   "standalone settings.gradle conflicts with baseline",
			CreatedAt: now,
		}},
		CreatedAt: now,
	}

	data, err := json.Marshal(packet)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ContractPacket
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != packet.ID {
		t.Fatalf("ID = %q, want %q", decoded.ID, packet.ID)
	}
	if decoded.TopologyFacts[0].Kind != "build_root" {
		t.Fatalf("TopologyFacts = %#v", decoded.TopologyFacts)
	}
	if decoded.Amendments[0].Impact.Kind != ContractImpactRefine {
		t.Fatalf("Amendment impact = %#v", decoded.Amendments[0].Impact)
	}
}

func TestContractImpactKindIsValid(t *testing.T) {
	tests := map[ContractImpactKind]bool{
		ContractImpactPreserve: true,
		ContractImpactRefine:   true,
		ContractImpactChange:   true,
		"":                     false,
		"replace":              false,
	}
	for kind, want := range tests {
		if got := kind.IsValid(); got != want {
			t.Errorf("ContractImpactKind(%q).IsValid() = %v, want %v", kind, got, want)
		}
	}
}

func TestEnsureContractPacketDoesNotOverwriteExisting(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	plan := &Plan{Slug: "demo"}
	plan.EnsureContractPacket("first brief", now)
	first := plan.Contract

	plan.EnsureContractPacket("second brief", now.Add(time.Hour))
	if plan.Contract != first {
		t.Fatal("EnsureContractPacket replaced an existing contract")
	}
	if plan.Contract.Brief != "first brief" {
		t.Fatalf("Brief = %q, want first brief", plan.Contract.Brief)
	}
}
