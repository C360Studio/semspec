package recoveryagent

import (
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestBuildRecoveryPlanDecision_PrefersAffectedRequirementIDsOverSingleID is
// the autonomy contract from the recovery-agent side: when plan-manager
// populates RecoveryRequested.AffectedRequirementIDs (QA verdict wedges),
// the emitted PlanDecision's AffectedReqIDs threads through the list
// verbatim. plan-decision-handler/recovery_autoaccept.go gates auto-accept
// on len(dec.AffectedReqIDs) > 0; without this propagation the QA-rejection
// retry loop sits waiting for a human click forever.
func TestBuildRecoveryPlanDecision_PrefersAffectedRequirementIDsOverSingleID(t *testing.T) {
	req := &payloads.RecoveryRequested{
		RecoveryID:             "12345678-aaaa-bbbb-cccc-dddddddddddd",
		Slug:                   "qa-rejection-multi-req",
		Layer:                  payloads.RecoveryLayerPhaseLocal,
		EscalationReason:       "QA verdict needs_changes at level integration",
		AffectedRequirementIDs: []string{"req-1", "req-2", "req-3"},
		// RequirementID intentionally left empty — the QA wedge is plan-scoped.
	}

	dec := buildRecoveryPlanDecision(req, nil, payloads.RecoveryActionRefinePrompt, "diagnosis text", true, time.Now())

	if len(dec.AffectedReqIDs) != 3 {
		t.Fatalf("AffectedReqIDs length = %d, want 3 (from AffectedRequirementIDs); got %v",
			len(dec.AffectedReqIDs), dec.AffectedReqIDs)
	}
	wantSet := map[string]bool{"req-1": false, "req-2": false, "req-3": false}
	for _, id := range dec.AffectedReqIDs {
		if _, ok := wantSet[id]; !ok {
			t.Errorf("unexpected ID in AffectedReqIDs: %q", id)
			continue
		}
		wantSet[id] = true
	}
	for id, seen := range wantSet {
		if !seen {
			t.Errorf("missing expected ID: %q", id)
		}
	}
	if dec.ProposedBy != componentName {
		t.Errorf("ProposedBy = %q, want %q", dec.ProposedBy, componentName)
	}
	if dec.Status != workflow.PlanDecisionStatusProposed {
		t.Errorf("Status = %q, want proposed", dec.Status)
	}
}

// TestBuildRecoveryPlanDecision_FallsBackToSingleRequirementID covers the
// execution-manager iteration-exhaustion path: a single TDD task wedged
// against one requirement, RequirementID is set on the payload,
// AffectedRequirementIDs is empty. The emitted PlanDecision should target
// just that one requirement so auto-accept retries the right scope.
func TestBuildRecoveryPlanDecision_FallsBackToSingleRequirementID(t *testing.T) {
	req := &payloads.RecoveryRequested{
		RecoveryID:       "12345678-eeee-ffff-0000-111111111111",
		Slug:             "iteration-exhaustion-single-req",
		Layer:            payloads.RecoveryLayerPhaseLocal,
		EscalationReason: "fixable rejections exceeded TDD cycle budget",
		RequirementID:    "req-only",
		// AffectedRequirementIDs intentionally empty.
	}

	dec := buildRecoveryPlanDecision(req, nil, payloads.RecoveryActionRefinePrompt, "diagnosis", true, time.Now())

	if len(dec.AffectedReqIDs) != 1 || dec.AffectedReqIDs[0] != "req-only" {
		t.Errorf("AffectedReqIDs = %v, want [req-only] (single-ID fallback)", dec.AffectedReqIDs)
	}
}

// TestBuildRecoveryPlanDecision_BothEmptyLeavesAffectedReqIDsNil covers the
// plan-review revision-cap wedge: the plan itself is wrong, neither a
// specific requirement nor a plan-scoped retry list applies. AffectedReqIDs
// is nil so the auto-accept watcher's `len > 0` filter rejects auto-accept
// and the PlanDecision waits for human review. That's the correct outcome
// — silently retrying the wrong plan would just re-wedge.
func TestBuildRecoveryPlanDecision_BothEmptyLeavesAffectedReqIDsNil(t *testing.T) {
	req := &payloads.RecoveryRequested{
		RecoveryID:       "12345678-1111-2222-3333-444444444444",
		Slug:             "plan-review-cap-no-reqs",
		Layer:            payloads.RecoveryLayerPhaseLocal,
		EscalationReason: "plan review revision cap reached",
	}

	dec := buildRecoveryPlanDecision(req, nil, payloads.RecoveryActionRefinePrompt, "diagnosis", false, time.Now())

	if len(dec.AffectedReqIDs) != 0 {
		t.Errorf("AffectedReqIDs = %v, want nil — auto-accept should not fire on plan-level wedges", dec.AffectedReqIDs)
	}
}
