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
		PlanID:    workflow.PlanEntityID("test-plan"),
		Title:     id,
		Status:    workflow.RequirementStatusActive,
		DependsOn: deps,
	}
}

// completed builds a completedReqIDs set from the given IDs. Used in place
// of the prior scenario-status fixtures — completion is now signaled via
// EXECUTION_STATES.stage=="completed" in production, surfaced as a set here.
func completed(ids ...string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// requirementComplete
// ---------------------------------------------------------------------------

func TestRequirementComplete_InCompletedSet(t *testing.T) {
	if !requirementComplete("r1", completed("r1", "r2")) {
		t.Error("requirementComplete() = false, want true when reqID is in the completed set")
	}
}

func TestRequirementComplete_NotInCompletedSet(t *testing.T) {
	if requirementComplete("r1", completed("r2")) {
		t.Error("requirementComplete() = true, want false when reqID not in completed set")
	}
}

func TestRequirementComplete_EmptySet(t *testing.T) {
	if requirementComplete("r1", map[string]bool{}) {
		t.Error("requirementComplete() = true, want false against empty completed set")
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

	got := filterReadyRequirements(reqs, completed())
	if len(got) != 2 {
		t.Errorf("filterReadyRequirements() returned %d requirements, want 2", len(got))
	}
}

func TestFilterReadyRequirements_SkipsAlreadyComplete(t *testing.T) {
	reqs := []workflow.Requirement{makeReq("r1")}

	got := filterReadyRequirements(reqs, completed("r1"))
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

	got := filterReadyRequirements(reqs, completed())
	gotIDs := requirementIDs(got)
	for _, id := range gotIDs {
		if id == "r2" {
			t.Error("filterReadyRequirements() included r2, but r2 should be blocked by incomplete r1")
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

	got := filterReadyRequirements(reqs, completed("r1"))
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

	got := filterReadyRequirements(reqs, completed())
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
// filterReadyRequirements — incomplete sibling does not block unrelated
// ---------------------------------------------------------------------------

func TestFilterReadyRequirements_IncompleteDoesNotBlockUnrelated(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2", "r1"),
		makeReq("r3"),
	}

	got := filterReadyRequirements(reqs, completed())
	gotIDs := requirementIDs(got)

	for _, id := range gotIDs {
		if id == "r2" {
			t.Error("filterReadyRequirements() dispatched r2, but r2 should be blocked by incomplete r1")
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

	t.Run("D blocked when B complete but C still pending", func(t *testing.T) {
		got := filterReadyRequirements(reqs, completed("A", "B"))
		gotIDs := requirementIDs(got)

		for _, id := range gotIDs {
			if id == "D" {
				t.Error("filterReadyRequirements() dispatched D, but D should be blocked until both B and C are complete")
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

	t.Run("D dispatched when both B and C are complete", func(t *testing.T) {
		got := filterReadyRequirements(reqs, completed("A", "B", "C"))
		if len(got) != 1 || got[0].ID != "D" {
			t.Errorf("filterReadyRequirements() = %v, want [D] once both B and C are complete", requirementIDs(got))
		}
	})

	t.Run("D blocked when both B and C are still pending", func(t *testing.T) {
		got := filterReadyRequirements(reqs, completed("A"))
		gotIDs := requirementIDs(got)

		for _, id := range gotIDs {
			if id == "D" {
				t.Error("filterReadyRequirements() dispatched D, but D should be blocked until both B and C are complete")
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
// filterReadyRequirements — chain dispatch (the @hard regression case)
// ---------------------------------------------------------------------------

// TestFilterReadyRequirements_ChainAdvancesOneAtATime simulates the @hard
// scenario: 4 requirements with serial deps 1→2→3→4, each becoming
// complete in turn. Verifies that exactly one new requirement unblocks per
// completion, which is the behavior plan-manager's re-fire of
// scenario.orchestrate.<slug> relies on.
func TestFilterReadyRequirements_ChainAdvancesOneAtATime(t *testing.T) {
	reqs := []workflow.Requirement{
		makeReq("r1"),
		makeReq("r2", "r1"),
		makeReq("r3", "r2"),
		makeReq("r4", "r3"),
	}

	cases := []struct {
		name string
		done []string
		want []string
	}{
		{"nothing complete → only r1 ready", nil, []string{"r1"}},
		{"r1 complete → only r2 ready", []string{"r1"}, []string{"r2"}},
		{"r1+r2 complete → only r3 ready", []string{"r1", "r2"}, []string{"r3"}},
		{"r1+r2+r3 complete → only r4 ready", []string{"r1", "r2", "r3"}, []string{"r4"}},
		{"all complete → nothing ready", []string{"r1", "r2", "r3", "r4"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requirementIDs(filterReadyRequirements(reqs, completed(tc.done...)))
			if len(got) != len(tc.want) {
				t.Fatalf("filterReadyRequirements() = %v, want %v", got, tc.want)
			}
			for i, id := range got {
				if id != tc.want[i] {
					t.Errorf("filterReadyRequirements()[%d] = %q, want %q", i, id, tc.want[i])
				}
			}
		})
	}
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
