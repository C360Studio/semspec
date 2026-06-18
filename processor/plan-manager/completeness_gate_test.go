package planmanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestUndeliveredScopeFiles(t *testing.T) {
	tests := []struct {
		name      string
		declared  []string
		delivered []string
		want      []string
	}{
		{
			name:      "all declared delivered",
			declared:  []string{"a.go", "b.go"},
			delivered: []string{"a.go", "b.go", "extra.go"},
			want:      nil,
		},
		{
			name:      "some missing, order preserved over declared",
			declared:  []string{"a.go", "b.go", "c.go"},
			delivered: []string{"a.go"},
			want:      []string{"b.go", "c.go"},
		},
		{
			name:      "empty declared → nothing missing",
			declared:  nil,
			delivered: []string{"a.go"},
			want:      nil,
		},
		{
			name:      "nothing delivered → all declared missing (zero-work assembly, C1)",
			declared:  []string{"a.go", "b.go"},
			delivered: nil,
			want:      []string{"a.go", "b.go"},
		},
		{
			name:      "path normalization: ./x matches x",
			declared:  []string{"./src/A.java", "src/B.java"},
			delivered: []string{"src/A.java", "./src/B.java"},
			want:      nil,
		},
		{
			name:      "blank entries ignored on both sides",
			declared:  []string{"a.go", "", "  "},
			delivered: []string{"a.go", ""},
			want:      nil,
		},
		{
			name:      "extra delivered files do not count as missing",
			declared:  []string{"a.go"},
			delivered: []string{"a.go", "b.go", "c.go"},
			want:      nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := undeliveredScopeFiles(tc.declared, tc.delivered)
			if len(got) != len(tc.want) {
				t.Fatalf("undeliveredScopeFiles() = %v (len %d), want %v (len %d)", got, len(got), tc.want, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("missing[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestUndeliveredScopeFiles_RunShape reproduces the 2026-06-16 hybrid-gpt5 run:
// the plan declared ~30 files in scope.create but delivered only 8. The gate
// must surface the 22-file gap (which the run shipped silently to QA).
func TestUndeliveredScopeFiles_RunShape(t *testing.T) {
	declared := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		declared = append(declared, fmt.Sprintf("src/main/java/org/sensorhub/impl/sensor/mavsdk/File%02d.java", i))
	}
	delivered := append([]string(nil), declared[:8]...) // only the first 8 were created
	delivered = append(delivered, "build.gradle", "COVERAGE.md")

	missing := undeliveredScopeFiles(declared, delivered)
	if len(missing) != 22 {
		t.Fatalf("missing count = %d, want 22 (30 declared − 8 delivered)", len(missing))
	}
	// The first undelivered file is File08 (0-7 delivered).
	if missing[0] != "src/main/java/org/sensorhub/impl/sensor/mavsdk/File08.java" {
		t.Errorf("missing[0] = %q, want File08", missing[0])
	}
}

func TestFailPlanOnIncompleteScopeCreatesRecoverableDecisionWithContractImpact(t *testing.T) {
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("scope-gap"),
		Slug:   "scope-gap",
		Title:  "scope-gap",
		Status: workflow.StatusImplementing,
		Scope:  workflow.Scope{Create: []string{"src/required.go", "src/missing.go"}},
		Requirements: []workflow.Requirement{
			{ID: "req.scope-gap.1"},
			{ID: "req.scope-gap.2"},
		},
	}
	if err := c.plans.save(context.Background(), plan); err != nil {
		t.Fatalf("save: %v", err)
	}

	c.failPlanOnIncompleteScope(context.Background(), plan, []string{"src/missing.go"})

	got, ok := c.plans.get(plan.Slug)
	if !ok {
		t.Fatal("plan missing after completeness gate")
	}
	if got.EffectiveStatus() != workflow.StatusRejected {
		t.Fatalf("status = %s, want rejected", got.EffectiveStatus())
	}
	if len(got.PlanDecisions) != 1 {
		t.Fatalf("PlanDecisions len = %d, want 1", len(got.PlanDecisions))
	}
	decision := got.PlanDecisions[0]
	if decision.Kind != workflow.PlanDecisionKindScopeIncomplete {
		t.Fatalf("decision.Kind = %s, want scope_incomplete", decision.Kind)
	}
	if decision.ProposedBy != "plan-manager" {
		t.Fatalf("decision.ProposedBy = %q, want plan-manager", decision.ProposedBy)
	}
	if decision.ContractImpact == nil {
		t.Fatal("decision.ContractImpact is nil")
	}
	if decision.ContractImpact.Kind != workflow.ContractImpactPreserve {
		t.Fatalf("ContractImpact.Kind = %s, want preserve", decision.ContractImpact.Kind)
	}
	if len(decision.ContractImpact.AffectedIDs) != 1 ||
		decision.ContractImpact.AffectedIDs[0] != "contract.scope.create:src/missing.go" {
		t.Fatalf("ContractImpact.AffectedIDs = %v, want missing file contract id", decision.ContractImpact.AffectedIDs)
	}
	if len(decision.AffectedReqIDs) != 2 {
		t.Fatalf("AffectedReqIDs = %v, want all requirements", decision.AffectedReqIDs)
	}
}
