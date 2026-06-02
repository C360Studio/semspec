package planmanager

import (
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
			{ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "untouched"},
			{ID: "story.demo.1.2", RequirementID: "req.demo.1", Title: "targeted"},
			{ID: "story.demo.1.3", RequirementID: "req.demo.1", Title: "untouched"},
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

// TestApplyRecoveryHint_StoryReprepare_FallbackToReqScope pins the
// fallback path: when proposal.AffectedStoryIDs is empty,
// applyRecoveryHint writes onto every Story under proposal.AffectedReqIDs.
// Mirrors cascade.storiesForReprepare's fallback so the two functions
// agree on scope.
func TestApplyRecoveryHint_StoryReprepare_FallbackToReqScope(t *testing.T) {
	plan := &workflow.Plan{
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementID: "req.demo.1"},
			{ID: "story.demo.1.2", RequirementID: "req.demo.1"},
			{ID: "story.demo.2.1", RequirementID: "req.demo.2"}, // different req
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

// TestApplyRecoveryHint_RequirementChangePreservesBackCompat pins the
// pre-Train-C behavior: Kind=requirement_change still writes onto
// Requirement.RecoveryHint and does NOT touch Story.RecoveryHint.
func TestApplyRecoveryHint_RequirementChangePreservesBackCompat(t *testing.T) {
	plan := &workflow.Plan{
		Requirements: []workflow.Requirement{
			{ID: "req.demo.1"},
		},
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementID: "req.demo.1"},
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
			{ID: "story.demo.1.1", RequirementID: "req.demo.1", UpdatedAt: stale},
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
