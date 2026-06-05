package changeproposalhandler

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestCountAcceptedArchitectureRevises is the monotonic loop bound for the
// architecture_revise auto-accept cap (go-reviewer C1). Only accepted
// architecture_revise decisions count; proposed ones, other kinds, and
// rejected ones do not.
func TestCountAcceptedArchitectureRevises(t *testing.T) {
	decisions := []workflow.PlanDecision{
		{ID: "a", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusAccepted},
		{ID: "b", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusProposed}, // not yet accepted
		{ID: "c", Kind: workflow.PlanDecisionKindStoryReprepare, Status: workflow.PlanDecisionStatusAccepted},     // wrong kind
		{ID: "d", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusRejected}, // rejected
		{ID: "e", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusAccepted},
	}

	if got := countAcceptedArchitectureRevises(decisions); got != 2 {
		t.Errorf("countAcceptedArchitectureRevises = %d, want 2 (only accepted architecture_revise)", got)
	}
	if got := countAcceptedArchitectureRevises(nil); got != 0 {
		t.Errorf("countAcceptedArchitectureRevises(nil) = %d, want 0", got)
	}
}

// TestArchitectureReviseCap_Gate documents the cap semantics the watch loop
// enforces: once MaxAutoArchitectureRevises accepted architecture_revise
// decisions exist on a plan, a further proposed one is NOT auto-accepted (it
// stays for human review). With the default cap of 1, the first auto-accepts
// and the second is gated.
func TestArchitectureReviseCap_Gate(t *testing.T) {
	const reviseCap = 1

	// One already accepted; a new proposed one arrives. count(1) >= reviseCap(1) → gate.
	withOneAccepted := []workflow.PlanDecision{
		{ID: "first", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusAccepted},
		{ID: "second", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusProposed},
	}
	if got := countAcceptedArchitectureRevises(withOneAccepted); got < reviseCap {
		t.Fatalf("precondition: expected >= %d accepted, got %d", reviseCap, got)
	}

	// None accepted yet; the first proposed one is below cap → auto-acceptable.
	noneAccepted := []workflow.PlanDecision{
		{ID: "first", Kind: workflow.PlanDecisionKindArchitectureRevise, Status: workflow.PlanDecisionStatusProposed},
	}
	if got := countAcceptedArchitectureRevises(noneAccepted); got >= reviseCap {
		t.Errorf("expected first architecture_revise below cap; got count %d >= reviseCap %d", got, reviseCap)
	}
}
