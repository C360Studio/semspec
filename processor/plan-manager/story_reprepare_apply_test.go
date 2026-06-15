package planmanager

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// TestApplyRecoveryHint_StoryReprepare_NamedStoriesOnly is the headline
// Train C step 4 regression for the Story-scoped hint write: only the
// Stories in proposal.AffectedStoryIDs gain the RecoveryHint; sibling
// Stories on the same Requirement are untouched. Pre-fix the function
// only wrote Requirement.RecoveryHint regardless of Kind, so Sarah's
// re-prep prompt never saw the diagnosis.
func TestApplyRecoveryHint_StoryReprepare_NamedStoriesOnly(t *testing.T) {
	plan := &workflow.Plan{
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "untouched"},
			{ID: "story.demo.1.2", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "targeted"},
			{ID: "story.demo.1.3", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "untouched"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:               "plan-decision.demo.recovery.abc12345",
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		Rationale:        "Story 2's files_owned missed src/x.go; re-shard including the X module.",
		AffectedReqIDs:   []string{"req.demo.1"},
		AffectedStoryIDs: []string{"story.demo.1.2"},
	}

	applyRecoveryHint(plan, proposal)

	if got := plan.Stories[0].RecoveryHint; got != "" {
		t.Errorf("Stories[0].RecoveryHint = %q, want empty (sibling untouched)", got)
	}
	if got := plan.Stories[1].RecoveryHint; got != proposal.Rationale {
		t.Errorf("Stories[1].RecoveryHint = %q, want %q", got, proposal.Rationale)
	}
	if got := plan.Stories[2].RecoveryHint; got != "" {
		t.Errorf("Stories[2].RecoveryHint = %q, want empty (sibling untouched)", got)
	}
}

func TestApplyStoryReprepare_ImplementingRequeuesSarah(t *testing.T) {
	c := setupTestComponent(t)
	plan := &workflow.Plan{
		Slug:   "demo",
		Status: workflow.StatusImplementing,
		Requirements: []workflow.Requirement{
			{ID: "req.demo.1"},
			{ID: "req.demo.2"},
		},
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "driver"},
			{ID: "story.demo.2.1", RequirementIDs: []string{"req.demo.2"}, ComponentName: "bridge"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "scen.target", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
			{ID: "scen.sibling", RequirementID: "req.demo.2", StoryID: "story.demo.2.1"},
		},
	}
	proposal := &workflow.PlanDecision{
		ID:               "plan-decision.demo.recovery.story",
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		Rationale:        "Story missed the companion unit test.",
		AffectedReqIDs:   []string{"req.demo.1"},
		AffectedStoryIDs: []string{"story.demo.1.1"},
	}

	if err := c.applyStoryReprepare(context.Background(), plan, proposal, plan.Slug); err != nil {
		t.Fatalf("applyStoryReprepare: %v", err)
	}

	if plan.Status != workflow.StatusPreparingStories {
		t.Fatalf("plan.Status = %s, want preparing_stories", plan.Status)
	}
	if len(plan.Scenarios) != 1 || plan.Scenarios[0].ID != "scen.sibling" {
		t.Fatalf("scenarios after story_reprepare = %+v, want only sibling scenario", plan.Scenarios)
	}
}

func TestAffectedRequirementIDsForStoryReprepare_IncludesStoryCoveredReqs(t *testing.T) {
	plan := &workflow.Plan{
		Stories: []workflow.Story{
			{ID: "story.demo.1", RequirementIDs: []string{"req.demo.2", "req.demo.3"}},
		},
	}
	proposal := &workflow.PlanDecision{
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		AffectedReqIDs:   []string{"req.demo.1"},
		AffectedStoryIDs: []string{"story.demo.1"},
	}

	got := affectedRequirementIDsForStoryReprepare(plan, proposal)
	want := []string{"req.demo.1", "req.demo.2", "req.demo.3"}
	if len(got) != len(want) {
		t.Fatalf("affected reqs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("affected reqs = %v, want %v", got, want)
		}
	}
}

// TestApplyRecoveryHint_StoryReprepare_FallbackToReqScope pins the
// fallback path: when proposal.AffectedStoryIDs is empty,
// applyRecoveryHint writes onto every Story under proposal.AffectedReqIDs.
// Mirrors cascade.storiesForReprepare's fallback so the two functions
// agree on scope.
func TestApplyRecoveryHint_StoryReprepare_FallbackToReqScope(t *testing.T) {
	plan := &workflow.Plan{
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component"},
			{ID: "story.demo.1.2", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component"},
			{ID: "story.demo.2.1", RequirementIDs: []string{"req.demo.2"}, ComponentName: "placeholder-component"}, // different req
		},
	}
	proposal := &workflow.PlanDecision{
		Kind:           workflow.PlanDecisionKindStoryReprepare,
		Rationale:      "Re-shard req.demo.1 with the X module in scope.",
		AffectedReqIDs: []string{"req.demo.1"},
		// AffectedStoryIDs intentionally empty
	}

	applyRecoveryHint(plan, proposal)

	if got := plan.Stories[0].RecoveryHint; got != proposal.Rationale {
		t.Errorf("Stories[0].RecoveryHint = %q, want %q (in req scope)", got, proposal.Rationale)
	}
	if got := plan.Stories[1].RecoveryHint; got != proposal.Rationale {
		t.Errorf("Stories[1].RecoveryHint = %q, want %q (in req scope)", got, proposal.Rationale)
	}
	if got := plan.Stories[2].RecoveryHint; got != "" {
		t.Errorf("Stories[2].RecoveryHint = %q, want empty (different req)", got)
	}
}

// TestApplyRecoveryHint_StoryReprepare_FallbackHitsMultiCoverer pins the
// ADR-044 M:N invariant: a Story whose RequirementIDs covers any affected
// requirement (not just the first/primary one) must receive the hint on
// the fallback path. Pre-fix the fallback compared only PrimaryRequirementID,
// so a Story covering [R1, R2] when AffectedReqIDs=[R2] was silently skipped.
// HIGH 1 from ADR-044 commit-2 review.
func TestApplyRecoveryHint_StoryReprepare_FallbackHitsMultiCoverer(t *testing.T) {
	plan := &workflow.Plan{
		Stories: []workflow.Story{
			{ID: "story.cohesive.a", RequirementIDs: []string{"req.demo.1", "req.demo.2"}, ComponentName: "driver"},
			{ID: "story.other", RequirementIDs: []string{"req.demo.3"}, ComponentName: "other"},
		},
	}
	proposal := &workflow.PlanDecision{
		Kind:           workflow.PlanDecisionKindStoryReprepare,
		Rationale:      "Re-shard req.demo.2 with X module in scope.",
		AffectedReqIDs: []string{"req.demo.2"}, // matches story.cohesive.a's SECOND req
	}

	applyRecoveryHint(plan, proposal)

	if got := plan.Stories[0].RecoveryHint; got != proposal.Rationale {
		t.Errorf("Stories[0] (covers R1,R2) RecoveryHint = %q, want %q — M:N coverage missed", got, proposal.Rationale)
	}
	if got := plan.Stories[1].RecoveryHint; got != "" {
		t.Errorf("Stories[1] (covers R3) RecoveryHint = %q, want empty", got)
	}
}

// TestApplyRecoveryHint_RequirementChangePreservesBackCompat pins the
// pre-Train-C behavior: Kind=requirement_change still writes onto
// Requirement.RecoveryHint and does NOT touch Story.RecoveryHint.
func TestApplyRecoveryHint_RequirementChangePreservesBackCompat(t *testing.T) {
	plan := &workflow.Plan{
		Requirements: []workflow.Requirement{
			{ID: "req.demo.1"},
		},
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component"},
		},
	}
	proposal := &workflow.PlanDecision{
		Kind:           workflow.PlanDecisionKindRequirementChange,
		Rationale:      "Refine the prompt with explicit X reference.",
		AffectedReqIDs: []string{"req.demo.1"},
	}

	applyRecoveryHint(plan, proposal)

	if got := plan.Requirements[0].RecoveryHint; got != proposal.Rationale {
		t.Errorf("Requirements[0].RecoveryHint = %q, want %q (requirement_change writes here)", got, proposal.Rationale)
	}
	if got := plan.Stories[0].RecoveryHint; got != "" {
		t.Errorf("Stories[0].RecoveryHint = %q, want empty (requirement_change does NOT touch Stories)", got)
	}
}

// TestApplyRecoveryHint_StoryUpdatedAtSet pins that Story.UpdatedAt is
// bumped on hint write so downstream consumers (UI badges, graph
// observers) see the freshness.
func TestApplyRecoveryHint_StoryUpdatedAtSet(t *testing.T) {
	stale := time.Now().Add(-time.Hour)
	plan := &workflow.Plan{
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", UpdatedAt: stale},
		},
	}
	proposal := &workflow.PlanDecision{
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		Rationale:        "test",
		AffectedStoryIDs: []string{"story.demo.1.1"},
	}

	applyRecoveryHint(plan, proposal)

	if !plan.Stories[0].UpdatedAt.After(stale) {
		t.Errorf("Stories[0].UpdatedAt not bumped; was %v, still %v", stale, plan.Stories[0].UpdatedAt)
	}
}
