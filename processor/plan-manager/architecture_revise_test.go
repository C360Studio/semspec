package planmanager

import (
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestReviseArchitectureState_HappyPath pins the pure state mutation an
// accepted architecture_revise PlanDecision performs from the implementing
// status: capture the prior architecture, wipe Architecture + Stories +
// Scenarios, route the diagnosis into ReviewFormattedFindings, and drive the
// back-transition to requirements_generated. This is the seam that the
// EXECUTION_STATES reset (NATS I/O) wraps in applyArchitectureRevise.
func TestReviseArchitectureState_HappyPath(t *testing.T) {
	plan := &workflow.Plan{
		Slug:   "mavlink-hard",
		Status: workflow.StatusImplementing,
		Architecture: &workflow.ArchitectureDocument{
			DataFlow: "sensor -> driver -> mavsdk",
		},
		Stories:   []workflow.Story{{ID: "story-1"}},
		Scenarios: []workflow.Scenario{{ID: "scenario-1"}},
	}
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.mavlink-hard.recovery.abcd1234",
		Kind:      workflow.PlanDecisionKindArchitectureRevise,
		Rationale: "Winston pinned the 3.x mavsdk API but the driver needs 2.x; every dev cycle re-hallucinates coords.",
	}

	transitioned, from := reviseArchitectureState(plan, proposal)

	if !transitioned {
		t.Fatalf("expected back-transition to be applied, got transitioned=false (from=%q)", from)
	}
	if from != workflow.StatusImplementing {
		t.Errorf("from status: got %q, want implementing", from)
	}
	if plan.Status != workflow.StatusRequirementsGenerated {
		t.Errorf("plan.Status: got %q, want requirements_generated", plan.Status)
	}
	if plan.Architecture != nil {
		t.Error("Architecture should be wiped")
	}
	if plan.Stories != nil {
		t.Error("Stories should be wiped")
	}
	if plan.Scenarios != nil {
		t.Error("Scenarios should be wiped")
	}
	if !strings.Contains(plan.PreviousArchitectureJSON, "mavsdk") {
		t.Errorf("PreviousArchitectureJSON should capture the prior architecture, got %q", plan.PreviousArchitectureJSON)
	}
	if plan.ReviewFormattedFindings != proposal.Rationale {
		t.Errorf("ReviewFormattedFindings: got %q, want the diagnosis", plan.ReviewFormattedFindings)
	}
}

// TestReviseArchitectureState_OutOfWindow verifies that a plan which has
// already moved past implementing (e.g. reached complete while the accept
// landed late) is left ENTIRELY untouched: no transition, AND no entity wipe.
// Wiping a terminal plan's architecture/stories/scenarios would corrupt it
// (go-reviewer M2). The wipe must be gated behind the transition check.
func TestReviseArchitectureState_OutOfWindow(t *testing.T) {
	plan := &workflow.Plan{
		Slug:         "mavlink-hard",
		Status:       workflow.StatusComplete,
		Architecture: &workflow.ArchitectureDocument{DataFlow: "x"},
		Stories:      []workflow.Story{{ID: "story-1"}},
		Scenarios:    []workflow.Scenario{{ID: "scenario-1"}},
	}
	proposal := &workflow.PlanDecision{
		ID:        "plan-decision.mavlink-hard.recovery.abcd1234",
		Kind:      workflow.PlanDecisionKindArchitectureRevise,
		Rationale: "diagnosis",
	}

	transitioned, from := reviseArchitectureState(plan, proposal)

	if transitioned {
		t.Errorf("expected transitioned=false from %q, got true", from)
	}
	if plan.Status != workflow.StatusComplete {
		t.Errorf("plan.Status should be unchanged, got %q", plan.Status)
	}
	if plan.Architecture == nil {
		t.Error("Architecture must NOT be wiped on an out-of-window accept (M2)")
	}
	if plan.Stories == nil || plan.Scenarios == nil {
		t.Error("Stories/Scenarios must NOT be wiped on an out-of-window accept (M2)")
	}
	if plan.ReviewFormattedFindings != "" {
		t.Error("ReviewFormattedFindings must NOT be set on an out-of-window accept (M2)")
	}
}

// TestReviseArchitectureState_NoPriorArchitecture confirms PreviousArchitectureJSON
// stays empty (no stale leftover) when the plan has no architecture to capture,
// and the transition + wipe still proceed.
func TestReviseArchitectureState_NoPriorArchitecture(t *testing.T) {
	plan := &workflow.Plan{
		Slug:                     "mavlink-hard",
		Status:                   workflow.StatusImplementing,
		PreviousArchitectureJSON: "stale-leftover",
	}
	proposal := &workflow.PlanDecision{Kind: workflow.PlanDecisionKindArchitectureRevise}

	transitioned, _ := reviseArchitectureState(plan, proposal)

	if !transitioned {
		t.Error("expected transition to requirements_generated")
	}
	if plan.PreviousArchitectureJSON != "" {
		t.Errorf("PreviousArchitectureJSON should be cleared when no architecture exists, got %q", plan.PreviousArchitectureJSON)
	}
}
