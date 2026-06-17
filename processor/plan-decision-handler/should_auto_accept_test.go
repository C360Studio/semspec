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
// now auto-acceptable alongside requirement_change. Pre-fix the filter
// only matched requirement_change → story_reprepare proposals sat in
// `proposed` forever waiting for a human click, defeating the whole
// autonomous-recovery shape.
func TestShouldAutoAcceptRecovery_WidenedForStoryReprepare(t *testing.T) {
	cases := []struct {
		name    string
		dec     *workflow.PlanDecision
		wantOK  bool
		comment string
	}{
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
			name: "architecture_revise is auto-acceptable",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactRefine),
			},
			wantOK:  true,
			comment: "heaviest recovery kind; user chose auto-accept",
		},
		{
			name: "contract-changing recovery stays human-gated",
			dec: &workflow.PlanDecision{
				ProposedBy:     "recovery-agent",
				Status:         workflow.PlanDecisionStatusProposed,
				Kind:           workflow.PlanDecisionKindArchitectureRevise,
				AffectedReqIDs: []string{"req.demo.1"},
				ContractImpact: recoveryImpact(workflow.ContractImpactChange),
			},
			wantOK:  false,
			comment: "auto-accept may not silently amend the root contract",
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
