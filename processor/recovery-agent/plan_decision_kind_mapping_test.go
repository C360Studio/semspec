package recoveryagent

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// TestRecoveryActionToPlanDecisionKind pins the mapping that determines
// which PlanDecisionKind a given RecoveryActionKind escalates into. The
// kind drives the downstream cascade shape:
//
//   - requirement_change → cascade dirty-marks Scenarios for affected reqs;
//     executor re-runs the same Story DAG.
//   - story_reprepare → cascade dirty-marks Stories + Scenarios; plan-manager
//     drives stories_generated → preparing_stories so Sarah re-runs.
//   - execution_exhausted → terminal; plan-manager auto-archives when subject
//     reaches a non-failed terminal state.
//
// Train C step 1: split story_reprepare out of requirement_change so the
// cascade can actually do something different for Story-shaped wedges.
// Pre-fix, story_reprepare returned requirement_change and the wedge
// silently degraded into a scenarios-only cascade that didn't re-run Sarah.
func TestRecoveryActionToPlanDecisionKind(t *testing.T) {
	// MAINTENANCE: when adding a RecoveryActionKind constant in
	// workflow/payloads/recovery.go, ALSO update the switch in
	// recoveryActionToPlanDecisionKind AND this table. The defensive
	// default falls to execution_exhausted (terminal), so an unrecognized
	// action will route to "terminal" without test failure — silent.
	cases := map[payloads.RecoveryActionKind]workflow.PlanDecisionKind{
		payloads.RecoveryActionRefinePrompt:      workflow.PlanDecisionKindRequirementChange,
		payloads.RecoveryActionNarrowScope:       workflow.PlanDecisionKindRequirementChange,
		payloads.RecoveryActionSplitReq:          workflow.PlanDecisionKindRequirementChange,
		payloads.RecoveryActionStoryReprepare:    workflow.PlanDecisionKindStoryReprepare,
		payloads.RecoveryActionEscalateHuman:     workflow.PlanDecisionKindExecutionExhausted,
		payloads.RecoveryActionMarkUnrecoverable: workflow.PlanDecisionKindExecutionExhausted,
	}
	for action, want := range cases {
		got := recoveryActionToPlanDecisionKind(action)
		if got != want {
			t.Errorf("recoveryActionToPlanDecisionKind(%q) = %q, want %q", action, got, want)
		}
	}

	// Defensive: unknown action falls to execution_exhausted (terminal — safer
	// than running an unintended cascade).
	got := recoveryActionToPlanDecisionKind("definitely-not-a-real-action")
	if got != workflow.PlanDecisionKindExecutionExhausted {
		t.Errorf("unknown action: got %q, want execution_exhausted (defensive fallback)", got)
	}
}
