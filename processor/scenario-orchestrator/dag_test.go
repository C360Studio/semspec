package scenarioorchestrator

import (
	"sort"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requirementIDs extracts IDs from a slice of Requirements for comparison.
func requirementIDs(reqs []workflow.Requirement) []string {
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.ID
	}
	sort.Strings(ids)
	return ids
}

// makeReq builds a Requirement with the given id and optional upstream deps.
func makeReq(id string, deps ...string) workflow.Requirement {
	return workflow.Requirement{
		ID:        id,
		PlanID:    "test-plan",
		Title:     id,
		Status:    workflow.RequirementStatusActive,
		DependsOn: deps,
	}
}

// makeScenario builds a Scenario owned by reqID with the given status.
func makeScenario(id, reqID string, status workflow.ScenarioStatus) workflow.Scenario {
	return workflow.Scenario{
		ID:            id,
		RequirementID: reqID,
		Status:        status,
	}
}

// ---------------------------------------------------------------------------
// requirementComplete
// ---------------------------------------------------------------------------

func TestRequirementComplete_AllPassing(t *testing.T) {
	reqScenarios := map[string][]workflow.Scenario{
		"r1": {
			makeScenario("s1", "r1", workflow.ScenarioStatusPassing),
			makeScenario("s2", "r1", workflow.ScenarioStatusPassing),
		},
	}
	if !requirementComplete("r1", reqScenarios) {
		t.Error("requirementComplete() = false, want true when all scenarios are passing")
	}
}

func TestRequirementComplete_AllSkipped(t *testing.T) {
	reqScenarios := map[string][]workflow.Scenario{
		"r1": {
			makeScenario("s1", "r1", workflow.ScenarioStatusSkipped),
		},
	}
	if !requirementComplete("r1", reqScenarios) {
		t.Error("requirementComplete() = false, want true when all scenarios are skipped")
	}
}

func TestRequirementComplete_MixedPassingAndSkipped(t *testing.T) {
	reqScenarios := map[string][]workflow.Scenario{
		"r1": {
			makeScenario("s1", "r1", workflow.ScenarioStatusPassing),
			makeScenario("s2", "r1", workflow.ScenarioStatusSkipped),
		},
	}
	if !requirementComplete("r1", reqScenarios) {
		t.Error("requirementComplete() = false, want true for mixed passing+skipped")
	}
}

func TestRequirementComplete_OneFailing(t *testing.T) {
	reqScenarios := map[string][]workflow.Scenario{
		"r1": {
			makeScenario("s1", "r1", workflow.ScenarioStatusPassing),
			makeScenario("s2", "r1", workflow.ScenarioStatusFailing),
		},
	}
	if requirementComplete("r1", reqScenarios) {
		t.Error("requirementComplete() = true, want false when a scenario is failing")
	}
}

func TestRequirementComplete_OnePending(t *testing.T) {
	reqScenarios := map[string][]workflow.Scenario{
		"r1": {
			makeScenario("s1", "r1", workflow.ScenarioStatusPending),
		},
	}
	if requirementComplete("r1", reqScenarios) {
		t.Error("requirementComplete() = true, want false when a scenario is pending")
	}
}

func TestRequirementComplete_NoScenarios(t *testing.T) {
	reqScenarios := map[string][]workflow.Scenario{
		"r1": {},
	}
	if requirementComplete("r1", reqScenarios) {
		t.Error("requirementComplete() = true, want false for requirement with no scenarios")
	}
}

func TestRequirementComplete_RequirementNotInMap(t *testing.T) {
	if requirementComplete("unknown", map[string][]workflow.Scenario{}) {
		t.Error("requirementComplete() = true, want false for unknown requirement ID")
	}
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — root requirements (no deps)
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_NoDependencies_AllDispatched(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2"),
	}
	allScenarios := []workflow.Scenario{
		makeScenario("s1", "r1", workflow.ScenarioStatusPending),
		makeScenario("s2", "r2", workflow.ScenarioStatusPending),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	if len(got) != 2 {
		t.Errorf("filterReadyRequirements() returned %d requirements, want 2", len(got))
	}
}

func TestFilterReadyRequirements_SkipsAlreadyComplete(t *testing.T) {
	reqs := []workflow.Requirement{makeReq("r1")}
	allScenarios := []workflow.Scenario{
		makeScenario("s1", "r1", workflow.ScenarioStatusPassing),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	if len(got) != 0 {
		t.Errorf("filterReadyRequirements() returned %d, want 0 (r1 already complete)", len(got))
	}
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — dependency blocking
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_DependentBlockedByIncompleteUpstream(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2", "r1"),
	}
	allScenarios := []workflow.Scenario{
		makeScenario("s1", "r1", workflow.ScenarioStatusFailing),
		makeScenario("s2", "r2", workflow.ScenarioStatusPending),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	gotIDs := requirementIDs(got)
	for _, id := range gotIDs {
		if id == "r2" {
			t.Error("filterReadyRequirements() included r2, but r2 should be blocked by failing r1")
		}
	}
	// r1 should still be dispatched (no deps, not complete).
	found := false
	for _, id := range gotIDs {
		if id == "r1" {
			found = true
		}
	}
	if !found {
		t.Error("filterReadyRequirements() did not dispatch r1; it has no deps and is not complete")
	}
}

func TestFilterReadyRequirements_DependentUnblockedWhenUpstreamComplete(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2", "r1"),
	}
	allScenarios := []workflow.Scenario{
		makeScenario("s1", "r1", workflow.ScenarioStatusPassing),
		makeScenario("s2", "r2", workflow.ScenarioStatusPending),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	if len(got) != 1 || got[0].ID != "r2" {
		t.Errorf("filterReadyRequirements() = %v, want [r2]", requirementIDs(got))
	}
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — independent requirements dispatch together
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_IndependentRequirementsDispatchedTogether(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2"),
	}
	allScenarios := []workflow.Scenario{
		makeScenario("s1", "r1", workflow.ScenarioStatusPending),
		makeScenario("s2", "r2", workflow.ScenarioStatusPending),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	gotIDs := requirementIDs(got)
	wantIDs := []string{"r1", "r2"}

	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("filterReadyRequirements() returned %v, want %v", gotIDs, wantIDs)
	}
	for i, id := range gotIDs {
		if id != wantIDs[i] {
			t.Errorf("gotIDs[%d] = %q, want %q", i, id, wantIDs[i])
		}
	}
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — failing sibling does not block unrelated
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_FailingDoesNotBlockUnrelated(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2", "r1"),
		makeReq("r3"),
	}
	allScenarios := []workflow.Scenario{
		makeScenario("s1", "r1", workflow.ScenarioStatusFailing),
		makeScenario("s2", "r2", workflow.ScenarioStatusPending),
		makeScenario("s3", "r3", workflow.ScenarioStatusPending),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	gotIDs := requirementIDs(got)

	for _, id := range gotIDs {
		if id == "r2" {
			t.Error("filterReadyRequirements() dispatched r2, but r2 should be blocked by failing r1")
		}
	}

	found := make(map[string]bool)
	for _, id := range gotIDs {
		found[id] = true
	}
	for _, expected := range []string{"r1", "r3"} {
		if !found[expected] {
			t.Errorf("filterReadyRequirements() did not dispatch %s, but it should be ready", expected)
		}
	}
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — diamond dependency
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_DiamondDependency(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("A"),
		makeReq("B", "A"),
		makeReq("C", "A"),
		makeReq("D", "B", "C"),
	}

	t.Run("D blocked when B passing but C still pending", func(t *testing.T) {
		allScenarios := []workflow.Scenario{
			makeScenario("sA", "A", workflow.ScenarioStatusPassing),
			makeScenario("sB", "B", workflow.ScenarioStatusPassing),
			makeScenario("sC", "C", workflow.ScenarioStatusPending),
			makeScenario("sD", "D", workflow.ScenarioStatusPending),
		}

		got := filterReadyRequirements(reqs, allScenarios)
		gotIDs := requirementIDs(got)

		for _, id := range gotIDs {
			if id == "D" {
				t.Error("filterReadyRequirements() dispatched D, but D should be blocked until both B and C pass")
			}
		}
		found := false
		for _, id := range gotIDs {
			if id == "C" {
				found = true
			}
		}
		if !found {
			t.Error("filterReadyRequirements() did not dispatch C, but C's dep (A) is complete")
		}
	})

	t.Run("D dispatched when both B and C pass", func(t *testing.T) {
		allScenarios := []workflow.Scenario{
			makeScenario("sA", "A", workflow.ScenarioStatusPassing),
			makeScenario("sB", "B", workflow.ScenarioStatusPassing),
			makeScenario("sC", "C", workflow.ScenarioStatusPassing),
			makeScenario("sD", "D", workflow.ScenarioStatusPending),
		}

		got := filterReadyRequirements(reqs, allScenarios)
		if len(got) != 1 || got[0].ID != "D" {
			t.Errorf("filterReadyRequirements() = %v, want [D] once both B and C are complete", requirementIDs(got))
		}
	})

	t.Run("D blocked when both B and C are still pending", func(t *testing.T) {
		allScenarios := []workflow.Scenario{
			makeScenario("sA", "A", workflow.ScenarioStatusPassing),
			makeScenario("sB", "B", workflow.ScenarioStatusPending),
			makeScenario("sC", "C", workflow.ScenarioStatusPending),
			makeScenario("sD", "D", workflow.ScenarioStatusPending),
		}

		got := filterReadyRequirements(reqs, allScenarios)
		gotIDs := requirementIDs(got)

		for _, id := range gotIDs {
			if id == "D" {
				t.Error("filterReadyRequirements() dispatched D, but D should be blocked until both B and C pass")
			}
		}
		found := make(map[string]bool)
		for _, id := range gotIDs {
			found[id] = true
		}
		for _, expected := range []string{"B", "C"} {
			if !found[expected] {
				t.Errorf("filterReadyRequirements() did not dispatch %s", expected)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — no requirements
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_NoRequirements_ReturnsNil(t *testing.T) {
	got := filterReadyRequirements(nil, nil)
	if got != nil {
		t.Errorf("filterReadyRequirements() with no requirements returned %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// filterReadyRequirements — multi-scenario requirement (partial completion)
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_MultiScenarioReq_OnePendingBlocksDependent(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2", "r1"),
	}
	allScenarios := []workflow.Scenario{
		makeScenario("s1a", "r1", workflow.ScenarioStatusPassing),
		makeScenario("s1b", "r1", workflow.ScenarioStatusPending),
		makeScenario("s2", "r2", workflow.ScenarioStatusPending),
	}

	got := filterReadyRequirements(reqs, allScenarios)
	gotIDs := requirementIDs(got)

	for _, id := range gotIDs {
		if id == "r2" {
			t.Error("filterReadyRequirements() dispatched r2 while r1 still has a pending scenario")
		}
	}
	found := false
	for _, id := range gotIDs {
		if id == "r1" {
			found = true
		}
	}
	if !found {
		t.Error("filterReadyRequirements() did not dispatch r1; r1 has no deps and should be ready")
	}
}

// ---------------------------------------------------------------------------
// depsComplete
// ---------------------------------------------------------------------------

func TestDepsComplete_NoDeps(t *testing.T) {
	req := makeReq("r1")
	if !depsComplete(req, map[string]bool{}) {
		t.Error("depsComplete() = false for requirement with no deps, want true")
	}
}

func TestDepsComplete_AllDepsComplete(t *testing.T) {
	req := makeReq("r3", "r1", "r2")
	complete := map[string]bool{"r1": true, "r2": true}
	if !depsComplete(req, complete) {
		t.Error("depsComplete() = false when all deps are complete, want true")
	}
}

func TestDepsComplete_OneDepIncomplete(t *testing.T) {
	req := makeReq("r3", "r1", "r2")
	complete := map[string]bool{"r1": true, "r2": false}
	if depsComplete(req, complete) {
		t.Error("depsComplete() = true when one dep is incomplete, want false")
	}
}

func TestDepsComplete_DepMissingFromMap(t *testing.T) {
	req := makeReq("r2", "r1")
	if depsComplete(req, map[string]bool{}) {
		t.Error("depsComplete() = true when dep is absent from completion map, want false")
	}
}
