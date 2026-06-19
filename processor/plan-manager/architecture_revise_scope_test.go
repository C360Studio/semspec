package planmanager

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// These tests lock the architecture_revise scope-resolution boundary that #238
// is about: a recovery decision that names affected requirements must reset/
// regenerate ONLY that requirement+story closure, and a whole-phase ("all")
// reset must require explicit phase-level contract-change evidence — never a
// silent fallback. The audit (docs/audit/e2e-flow-accuracy-and-coverage.md)
// found this boundary untested even though the logic is correct; a regression
// here is exactly how a single-req revise regenerates the whole plan layer.
//
// planDecisionResetScope is a pure function (plan + proposal -> scope), so these
// drive it directly rather than through the full accept path.

func reqScopePlan() *workflow.Plan {
	return &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "contract", Title: "External API contract"},
			{ID: "unrelated", Title: "Independent feature"},
		},
		Stories: []workflow.Story{
			{ID: "story.contract", RequirementIDs: []string{"contract"}},
			{ID: "story.unrelated", RequirementIDs: []string{"unrelated"}},
		},
	}
}

func sliceHas(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestPlanDecisionResetScope_ScopedWinsOverWholePhaseEvidence is the load-bearing
// #238 guard: when a decision names affected requirements, the closure scope must
// win even if the contract impact ALSO carries whole-phase ("phase:*") evidence.
// A populated affected list must never escalate to "all" — that is the exact
// shape that regenerated all requirements' stories/scenarios in a paid run.
func TestPlanDecisionResetScope_ScopedWinsOverWholePhaseEvidence(t *testing.T) {
	plan := reqScopePlan()
	proposal := &workflow.PlanDecision{
		Kind:           workflow.PlanDecisionKindArchitectureRevise,
		AffectedReqIDs: []string{"contract"},
		ContractImpact: &workflow.ContractImpact{
			Kind:        workflow.ContractImpactChange,
			AffectedIDs: []string{"phase:*"}, // whole-phase evidence ALSO present
		},
	}

	scope, reqIDs, err := planDecisionResetScope(plan, proposal)
	if err != nil {
		t.Fatalf("planDecisionResetScope: %v", err)
	}
	if scope != "requirements" {
		t.Fatalf("scope = %q, want \"requirements\" — a named affected requirement must NOT escalate to a whole-phase reset even when phase:* evidence is present (#238)", scope)
	}
	if !sliceHas(reqIDs, "contract") {
		t.Errorf("reset reqIDs = %v, want to include \"contract\"", reqIDs)
	}
	if sliceHas(reqIDs, "unrelated") {
		t.Errorf("reset reqIDs = %v, must NOT include the unrelated requirement", reqIDs)
	}
}

// TestPlanDecisionResetScope_WholePhaseRequiresExplicitEvidence pins the two
// whole-phase guards: with no affected requirements, "all" is returned ONLY when
// the contract impact is a change carrying explicit phase-level evidence;
// otherwise the reset is refused (error) rather than silently wiping everything.
func TestPlanDecisionResetScope_WholePhaseRequiresExplicitEvidence(t *testing.T) {
	tests := []struct {
		name      string
		impact    *workflow.ContractImpact
		wantScope string
		wantErr   bool
	}{
		{
			name:      "empty affected + phase:* change -> all (the only legit whole-phase)",
			impact:    &workflow.ContractImpact{Kind: workflow.ContractImpactChange, AffectedIDs: []string{"phase:*"}},
			wantScope: "all",
		},
		{
			name:      "empty affected + phase:architecture change -> all",
			impact:    &workflow.ContractImpact{Kind: workflow.ContractImpactChange, AffectedIDs: []string{"phase:architecture"}},
			wantScope: "all",
		},
		{
			name:    "empty affected + change but NO phase evidence -> refused",
			impact:  &workflow.ContractImpact{Kind: workflow.ContractImpactChange, AffectedIDs: []string{"contract"}},
			wantErr: true,
		},
		{
			name:    "empty affected + phase:* but Kind=preserve -> refused (whole-phase needs change)",
			impact:  &workflow.ContractImpact{Kind: workflow.ContractImpactPreserve, AffectedIDs: []string{"phase:*"}},
			wantErr: true,
		},
		{
			name:    "empty affected + nil contract impact -> refused",
			impact:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := reqScopePlan()
			proposal := &workflow.PlanDecision{
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				AffectedReqIDs: nil, // no scoped requirements
				ContractImpact: tt.impact,
			}
			scope, reqIDs, err := planDecisionResetScope(plan, proposal)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("planDecisionResetScope scope=%q reqIDs=%v, want error — whole-phase reset must require explicit phase evidence", scope, reqIDs)
				}
				return
			}
			if err != nil {
				t.Fatalf("planDecisionResetScope: %v", err)
			}
			if scope != tt.wantScope {
				t.Fatalf("scope = %q, want %q", scope, tt.wantScope)
			}
			if scope == "all" && len(reqIDs) != 0 {
				t.Errorf("whole-phase reset reqIDs = %v, want nil", reqIDs)
			}
		})
	}
}
