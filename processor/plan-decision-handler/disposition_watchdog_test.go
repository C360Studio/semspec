package changeproposalhandler

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// allPlanDecisionKinds is the authoritative enumeration of PlanDecisionKind.
// It MUST stay in sync with PlanDecisionKind.IsValid() in workflow/types.go.
// Adding a new PlanDecisionKind without adding it here fails
// TestPlanDecisionKind_AllKindsEnumerated; adding it here without a declared
// disposition fails TestRecoveryDisposition_Watchdog. That is the forcing
// function for #221 invariant 1.
var allPlanDecisionKinds = []workflow.PlanDecisionKind{
	workflow.PlanDecisionKindRequirementChange,
	workflow.PlanDecisionKindExecutionExhausted,
	workflow.PlanDecisionKindStoryReprepare,
	workflow.PlanDecisionKindArchitectureRevise,
	workflow.PlanDecisionKindAssemblyConflict,
	workflow.PlanDecisionKindScopeIncomplete,
}

// disposition classifies the full-auto handling of a PlanDecisionKind.
type disposition int

const (
	// autoAcceptable: a well-formed recovery proposal of this kind can be
	// auto-accepted by shouldAutoAcceptRecovery (subject to per-kind caps).
	autoAcceptable disposition = iota
	// humanGatedOrTerminal: this kind is NEVER auto-accepted. It is either a
	// terminal record (execution_exhausted, assembly_conflict) or contract-
	// changing and requires an operator decision (architecture_revise). It must
	// surface as a visible proposed/terminal state, never silently auto-resolve.
	humanGatedOrTerminal
)

// kindDisposition is the #221 INV1 contract: every PlanDecisionKind has a
// deterministic full-auto disposition, so none can sit in proposed-limbo with
// no owner. proposer is the proposer under which an autoAcceptable kind fires —
// the auto-accept filter is proposer-scoped (recovery-agent for the recovery
// actions; plan-manager for the deterministic scope_incomplete gate).
var kindDisposition = map[workflow.PlanDecisionKind]struct {
	want     disposition
	proposer string
}{
	workflow.PlanDecisionKindRequirementChange:  {autoAcceptable, "recovery-agent"},
	workflow.PlanDecisionKindStoryReprepare:     {autoAcceptable, "recovery-agent"},
	workflow.PlanDecisionKindScopeIncomplete:    {autoAcceptable, "plan-manager"},
	workflow.PlanDecisionKindArchitectureRevise: {humanGatedOrTerminal, ""},
	workflow.PlanDecisionKindExecutionExhausted: {humanGatedOrTerminal, ""},
	workflow.PlanDecisionKindAssemblyConflict:   {humanGatedOrTerminal, ""},
}

// TestPlanDecisionKind_AllKindsEnumerated guards the enumeration used by the
// watchdog: every kind in allPlanDecisionKinds is IsValid(), and IsValid()
// actually rejects unknown kinds. If you add a PlanDecisionKind to IsValid(),
// add it to allPlanDecisionKinds AND kindDisposition — the watchdog then forces
// you to declare its full-auto disposition (#221 INV1).
func TestPlanDecisionKind_AllKindsEnumerated(t *testing.T) {
	for _, k := range allPlanDecisionKinds {
		if !k.IsValid() {
			t.Errorf("allPlanDecisionKinds contains %q which is not IsValid()", k)
		}
	}
	if workflow.PlanDecisionKind("bogus-kind").IsValid() {
		t.Error("IsValid() accepted a bogus kind; the enumeration guard is meaningless")
	}
}

// TestRecoveryDisposition_Watchdog is the #221 INV1 no-wedge invariant: in
// full-auto mode no proposed PlanDecision may wait forever without an owner.
// Every PlanDecisionKind must either auto-accept (a deterministic owner drives
// it) or be a visible human-gated/terminal decision. For each enumerated kind
// it asserts shouldAutoAcceptRecovery agrees with the declared disposition, so a
// regression that drops a kind into proposed-limbo fails deterministically
// instead of wedging a paid run.
func TestRecoveryDisposition_Watchdog(t *testing.T) {
	// Exhaustiveness: every enumerated kind has a declared disposition.
	for _, k := range allPlanDecisionKinds {
		if _, ok := kindDisposition[k]; !ok {
			t.Errorf("PlanDecisionKind %q has no declared full-auto disposition — "+
				"#221 INV1 requires every kind to auto-accept, human-gate, or terminate", k)
		}
	}

	for _, k := range allPlanDecisionKinds {
		d, ok := kindDisposition[k]
		if !ok {
			continue // already reported above
		}
		t.Run(string(k), func(t *testing.T) {
			switch d.want {
			case autoAcceptable:
				// A well-formed, non-contract-changing proposal of this kind from
				// its expected proposer MUST be auto-acceptable (it has an owner).
				dec := &workflow.PlanDecision{
					ProposedBy:     d.proposer,
					Status:         workflow.PlanDecisionStatusProposed,
					Kind:           k,
					AffectedReqIDs: []string{"req.demo.1"},
					ContractImpact: recoveryImpact(workflow.ContractImpactPreserve),
				}
				if !shouldAutoAcceptRecovery(dec) {
					t.Errorf("kind %q is declared autoAcceptable but shouldAutoAcceptRecovery=false "+
						"for a well-formed %s proposal — it has no auto owner and would sit proposed",
						k, d.proposer)
				}
			case humanGatedOrTerminal:
				// NO well-formed proposal of this kind may auto-accept, under any
				// proposer or contract impact — it must surface for a human/terminate.
				for _, proposer := range []string{"recovery-agent", "plan-manager", "qa-reviewer"} {
					for _, impact := range []workflow.ContractImpactKind{
						workflow.ContractImpactPreserve,
						workflow.ContractImpactRefine,
						workflow.ContractImpactChange,
					} {
						dec := &workflow.PlanDecision{
							ProposedBy:     proposer,
							Status:         workflow.PlanDecisionStatusProposed,
							Kind:           k,
							AffectedReqIDs: []string{"req.demo.1"},
							ContractImpact: recoveryImpact(impact),
						}
						if shouldAutoAcceptRecovery(dec) {
							t.Errorf("kind %q is declared human-gated/terminal but auto-accepted "+
								"(proposer=%s impact=%s) — would silently bypass the operator",
								k, proposer, impact)
						}
					}
				}
			}
		})
	}
}
