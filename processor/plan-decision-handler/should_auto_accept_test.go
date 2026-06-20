package changeproposalhandler

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func recoveryImpact(kind workflow.ContractImpactKind) *workflow.ContractImpact {
	return &workflow.ContractImpact{Kind: kind, Summary: "test impact"}
}

// TestShouldAutoAcceptRecovery_WidenedForStoryReprepare pins the Train C
// step 4 widening: recovery-agent-emitted story_reprepare proposals are
// auto-acceptable alongside safe requirement_change proposals, while
// contract-changing actions stay human-gated.
func TestShouldAutoAcceptRecovery_WidenedForStoryReprepare(t *testing.T) {
	cases := []struct {
		name    string
		dec     *workflow.PlanDecision
		wantOK  bool
		comment string
	}{
		{
			name: "scope_incomplete from plan-manager is auto-acceptable",
			dec: &workflow.PlanDecision{
				ProposedBy:     "plan-manager",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindScopeIncomplete,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
			},
			wantOK:  true,
			comment: "Level-0 completeness recovery preserves declared scope and may retry automatically",
		},
		{
			name: "scope_incomplete from recovery-agent is human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindScopeIncomplete,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
			},
			wantOK:  false,
			comment: "scope_incomplete is owned by the deterministic plan-manager gate",
		},
		{
			name: "scope_incomplete contract change is human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "plan-manager",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindScopeIncomplete,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactChange),
			},
			wantOK:  false,
			comment: "changing declared scope needs review",
		},
		{
			name: "story_reprepare is auto-acceptable",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindStoryReprepare,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactRefine),
			},
			wantOK:  true,
			comment: "Train C step 4: widened from requirement_change-only",
		},
		{
			name: "requirement_change still auto-acceptable",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindRequirementChange,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
			},
			wantOK:  true,
			comment: "pre-existing path unchanged",
		},
		{
			name: "architecture_revise (refine impact) auto-accepts in full-auto",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactRefine),
			},
			wantOK:  true,
			comment: "#211: full-auto auto-accepts a scoped, recovery-agent architecture_revise",
		},
		{
			name: "narrow_scope requirement_change stays human-gated even if malformed as preserve",
			dec: &workflow.PlanDecision{
				Title:          "Recovery: narrow_scope",
				Rationale:      "Recommended action: narrow_scope\n\nDiagnosis:\nRemove accepted scope.",
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindRequirementChange,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
			},
			wantOK:  false,
			comment: "scope-narrowing is contract-changing even though it maps to requirement_change",
		},
		{
			name: "architecture_revise (change impact) auto-accepts in full-auto",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactChange),
			},
			wantOK:  true,
			comment: "#211: architecture_revise is inherently contract-changing; full-auto accepts it (cap-bounded), no human gate",
		},
		{
			name: "architecture_revise without scope stays gated (no target)",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				ContractImpact: recoveryImpact(workflow.ContractImpactChange),
			},
			wantOK:  false,
			comment: "#211: cap-bounded auto-accept still needs AffectedReqIDs to target; an unscoped whole-phase revise is not auto-accepted",
		},
		{
			name: "architecture_revise from a non-recovery-agent proposer stays gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "qa-reviewer",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactChange),
			},
			wantOK:  false,
			comment: "the auto-accept filter is recovery-agent-only by design",
		},
		{
			name: "missing contract impact stays human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindRequirementChange,
				AffectedReqIDs: []string{"req.demo.1"},
			},
			wantOK:  false,
			comment: "legacy or malformed recovery decisions need review",
		},
		{
			name: "execution_exhausted stays human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindExecutionExhausted,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
			},
			wantOK:  false,
			comment: "terminal kind requires human ack",
		},
		{
			name: "non-recovery-agent proposer stays human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "qa-reviewer",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindStoryReprepare,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactRefine),
			},
			wantOK:  false,
			comment: "filter is recovery-agent-only by design",
		},
		{
			name: "non-proposed status stays human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusAccepted,
				Kind:           workflow.PlanDecisionKindStoryReprepare,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactRefine),
			},
			wantOK:  false,
			comment: "already-decided proposals aren't re-accepted",
		},
		{
			name: "empty AffectedReqIDs stays human-gated (story_reprepare)",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindStoryReprepare,
				ContractImpact: recoveryImpact(workflow.ContractImpactRefine),
			},
			wantOK:  false,
			comment: "no scope to target — needs human triage",
		},
		{
			name:   "nil proposal stays human-gated",
			dec:    nil,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldAutoAcceptRecovery(tc.dec)
			if got != tc.wantOK {
				t.Errorf("shouldAutoAcceptRecovery = %v, want %v (%s)", got, tc.wantOK, tc.comment)
			}
		})
	}
}

func TestCountAcceptedScopeIncompleteRecoveries(t *testing.T) {
	decisions := []workflow.PlanDecision{
		{ID: "a", Kind: workflow.PlanDecisionKindScopeIncomplete, Status: workflow.PlanDecisionStatusAccepted},
		{ID: "b", Kind: workflow.PlanDecisionKindScopeIncomplete, Status: workflow.PlanDecisionStatusProposed},
		{ID: "c", Kind: workflow.PlanDecisionKindRequirementChange, Status: workflow.PlanDecisionStatusAccepted},
		{ID: "d", Kind: workflow.PlanDecisionKindScopeIncomplete, Status: workflow.PlanDecisionStatusRejected},
		{ID: "e", Kind: workflow.PlanDecisionKindScopeIncomplete, Status: workflow.PlanDecisionStatusAccepted},
	}
	if got := countAcceptedScopeIncompleteRecoveries(decisions); got != 2 {
		t.Fatalf("countAcceptedScopeIncompleteRecoveries = %d, want 2", got)
	}
}
