package planmanager

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/cascade"
)

// TestStoryReprepare_EndToEnd_AcceptPathTransitionsPlanAndAppliesHints
// is the cross-cutting Train C regression. It walks the wire shape
// the recovery cascade depends on, asserting that:
//
//  1. applyRecoveryHint writes Story.RecoveryHint onto the affected
//     Stories (Train C step 4 — Story-level hint write).
//  2. The same applyRecoveryHint call leaves the rest of plan.Stories
//     intact (so Sarah's prompt context can iterate the full set in
//     step 5).
//  3. The accept-path's inline CanTransitionTo + plan.Status mutation
//     produces a SINGLE save at the trailing ps.save (Train C step 5 —
//     B1 fix; double-save would emit two preparing_stories watcher
//     events and double-dispatch Sarah).
//  4. The cascade dirty-marks only the named Stories + their scenarios
//     (Train C step 3 — story-shape cascade).
//
// Together, these pin the contract that an accepted story_reprepare
// PlanDecision drives the plan into preparing_stories with the
// affected Stories carrying RecoveryHint, sibling Stories untouched,
// and a per-(Req,Story) cascade scope ready for Sarah's re-emit.
//
// This is a pure-function integration: we don't drive the NATS path
// because that requires a test NATS server. The accept handler's
// in-memory mutation contract is the load-bearing piece — the wire
// glue is tested separately in the per-step commits.
func TestStoryReprepare_EndToEnd_AcceptPathTransitionsPlanAndAppliesHints(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	const slug = "demo-train-c-e2e"

	// Seed plan at stories_generated with 3 Stories under 1 Req + their
	// scenarios.
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusStoriesGenerated
	plan.Requirements = []workflow.Requirement{
		{ID: "req.demo.1", Title: "the requirement"},
	}
	plan.Stories = []workflow.Story{
		{ID: "story.demo.1.1", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "Sibling 1"},
		{ID: "story.demo.1.2", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "Targeted"},
		{ID: "story.demo.1.3", RequirementIDs: []string{"req.demo.1"}, ComponentName: "placeholder-component", Title: "Sibling 3"},
	}
	plan.Scenarios = []workflow.Scenario{
		{ID: "scen.demo.1.1", RequirementID: "req.demo.1", StoryID: "story.demo.1.1"},
		{ID: "scen.demo.1.2", RequirementID: "req.demo.1", StoryID: "story.demo.1.2"},
		{ID: "scen.demo.1.3", RequirementID: "req.demo.1", StoryID: "story.demo.1.3"},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	// Build a story_reprepare PlanDecision targeting only Story 2.
	proposal := workflow.PlanDecision{
		ID:               "plan-decision.demo.recovery.abcd1234",
		PlanID:           workflow.PlanEntityID(slug),
		Kind:             workflow.PlanDecisionKindStoryReprepare,
		Title:            "Recovery: story_reprepare for req.demo.1",
		Rationale:        "Story 2's files_owned missed src/x.go; re-shard to include the X module.",
		Status:           workflow.PlanDecisionStatusProposed,
		ProposedBy:       "recovery-agent",
		AffectedReqIDs:   []string{"req.demo.1"},
		AffectedStoryIDs: []string{"story.demo.1.2"},
		CreatedAt:        time.Now(),
	}
	plan.PlanDecisions = []workflow.PlanDecision{proposal}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("seed proposal save: %v", err)
	}

	// --- Phase 1: applyRecoveryHint writes Story.RecoveryHint onto
	// Story 2 only; siblings untouched. ---
	loaded, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan not in store after seed")
	}
	applyRecoveryHint(loaded, &proposal)
	if got := loaded.Stories[0].RecoveryHint; got != "" {
		t.Errorf("Story 1 RecoveryHint = %q, want empty (sibling untouched)", got)
	}
	if got := loaded.Stories[1].RecoveryHint; got != proposal.Rationale {
		t.Errorf("Story 2 RecoveryHint = %q, want %q", got, proposal.Rationale)
	}
	if got := loaded.Stories[2].RecoveryHint; got != "" {
		t.Errorf("Story 3 RecoveryHint = %q, want empty (sibling untouched)", got)
	}
	// Phase 1 invariant: ALL 3 Stories still in plan.Stories — sibling
	// Stories must survive so Sarah's prompt context can iterate them.
	if len(loaded.Stories) != 3 {
		t.Errorf("len(plan.Stories) = %d, want 3 (sibling Stories MUST survive)", len(loaded.Stories))
	}

	// --- Phase 2: inline transition check + direct mutation drives
	// plan.Status without an intermediate save. ---
	current := loaded.EffectiveStatus()
	if !current.CanTransitionTo(workflow.StatusPreparingStories) {
		t.Fatalf("transition stories_generated → preparing_stories rejected: current = %s", current)
	}
	loaded.Status = workflow.StatusPreparingStories

	// Single trailing save captures: proposal accepted + RecoveryHint
	// applied + plan.Status transitioned. One KV put → one watcher
	// event → Sarah dispatches ONCE. Pre-B1-fix the setPlanStatusCached
	// path would have saved twice here.
	if err := c.plans.save(ctx, loaded); err != nil {
		t.Fatalf("final save: %v", err)
	}

	// --- Phase 3: cascade.PlanDecision runs the story-shape cascade. ---
	cascadeResult, err := cascade.PlanDecision(&proposal, loaded.Stories, loaded.Scenarios)
	if err != nil {
		t.Fatalf("cascade: %v", err)
	}
	if len(cascadeResult.AffectedStoryIDs) != 1 || cascadeResult.AffectedStoryIDs[0] != "story.demo.1.2" {
		t.Errorf("cascade AffectedStoryIDs = %v, want [story.demo.1.2]", cascadeResult.AffectedStoryIDs)
	}
	if len(cascadeResult.AffectedScenarioIDs) != 1 || cascadeResult.AffectedScenarioIDs[0] != "scen.demo.1.2" {
		t.Errorf("cascade AffectedScenarioIDs = %v, want [scen.demo.1.2] (only Story 2's scenarios dirty)", cascadeResult.AffectedScenarioIDs)
	}

	// --- Phase 4: re-read the saved plan; assert the post-accept state
	// is durable and ready for Sarah's re-prep claim. ---
	final, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan disappeared after final save")
	}
	if final.EffectiveStatus() != workflow.StatusPreparingStories {
		t.Errorf("final plan.Status = %s, want preparing_stories (Sarah's watcher claim point)", final.EffectiveStatus())
	}
	if final.Stories[1].RecoveryHint != proposal.Rationale {
		t.Errorf("RecoveryHint did not survive save round-trip: %q", final.Stories[1].RecoveryHint)
	}
	if len(final.Stories) != 3 {
		t.Errorf("final len(Stories) = %d, want 3 (Sarah sees the full set on re-prep)", len(final.Stories))
	}
}
