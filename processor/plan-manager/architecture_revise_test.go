package planmanager

import (
	"context"
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

func TestApplyArchitectureRevise_ScopedResetUsesAffectedRequirementClosure(t *testing.T) {
	c := setupTestComponent(t)
	c.execBucket = resetKVStub{
		keys: []string{
			"req.demo.bootstrap",
			"task.demo.node-bootstrap",
			"req.demo.unrelated",
			"task.demo.node-unrelated",
			"req.demo.contract",
			"task.demo.node-contract",
			"req.demo.consumer",
			"task.demo.node-consumer",
		},
		values: map[string][]byte{
			"task.demo.node-bootstrap": []byte(`{"requirement_id":"bootstrap"}`),
			"task.demo.node-unrelated": []byte(`{"requirement_id":"unrelated"}`),
			"task.demo.node-contract":  []byte(`{"requirement_id":"contract"}`),
			"task.demo.node-consumer":  []byte(`{"requirement_id":"consumer"}`),
		},
	}
	var reset []string
	c.reqResetSender = func(_ context.Context, key string) error {
		reset = append(reset, key)
		return nil
	}

	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Requirements: []workflow.Requirement{
			{ID: "bootstrap", Title: "Project bootstrap"},
			{ID: "unrelated", Title: "Independent feature"},
			{ID: "contract", Title: "External dependency API contract", DependsOn: []string{"bootstrap"}},
			{ID: "consumer", Title: "Consumer integration", DependsOn: []string{"contract"}},
		},
		Architecture: &workflow.ArchitectureDocument{DataFlow: "prior design"},
		Stories:      []workflow.Story{{ID: "story.contract", RequirementIDs: []string{"contract"}}},
		Scenarios: []workflow.Scenario{
			{ID: "scen.contract", RequirementID: "contract"},
			{ID: "scen.consumer", RequirementID: "consumer"},
			{ID: "scen.unrelated", RequirementID: "unrelated"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:             "plan-decision.demo.recovery.contract",
		Kind:           workflow.PlanDecisionKindArchitectureRevise,
		Rationale:      "Add a verified external dependency and API contract instead of hand-rolled protocol behavior.",
		AffectedReqIDs: []string{"contract"},
	}

	if err := c.applyArchitectureRevise(context.Background(), plan, proposal); err != nil {
		t.Fatalf("applyArchitectureRevise: %v", err)
	}

	assertResetKeys(t, reset, []string{
		"req.demo.contract",
		"task.demo.node-contract",
		"req.demo.consumer",
		"task.demo.node-consumer",
	})
	assertNoResetKeys(t, reset, []string{
		"req.demo.bootstrap",
		"task.demo.node-bootstrap",
		"req.demo.unrelated",
		"task.demo.node-unrelated",
	})
	if plan.Status != workflow.StatusRequirementsGenerated {
		t.Fatalf("plan.Status = %s, want requirements_generated", plan.Status)
	}
}

func TestPlanDecisionResetScope_UnscopedFallsBackToAll(t *testing.T) {
	scope, reqIDs := planDecisionResetScope(&workflow.Plan{
		Requirements: []workflow.Requirement{{ID: "contract"}},
	}, &workflow.PlanDecision{Kind: workflow.PlanDecisionKindArchitectureRevise})

	if scope != "all" {
		t.Fatalf("scope = %q, want all", scope)
	}
	if reqIDs != nil {
		t.Fatalf("reqIDs = %v, want nil", reqIDs)
	}
}

func assertResetKeys(t *testing.T, got []string, want []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(got))
	for _, key := range got {
		seen[key] = struct{}{}
	}
	for _, key := range want {
		if _, ok := seen[key]; !ok {
			t.Fatalf("reset keys = %v, missing %q", got, key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("reset keys = %v, want exactly %v", got, want)
	}
}

func assertNoResetKeys(t *testing.T, got []string, forbidden []string) {
	t.Helper()
	seen := make(map[string]struct{}, len(got))
	for _, key := range got {
		seen[key] = struct{}{}
	}
	for _, key := range forbidden {
		if _, ok := seen[key]; ok {
			t.Fatalf("reset keys = %v, should not include %q", got, key)
		}
	}
}
