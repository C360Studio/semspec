package storypreparer

import (
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestBuildPromptContext_PopulatesStoryRecoveryHints pins the Train C
// step 5 contract: plan.Stories carrying Story.RecoveryHint values
// (written by plan-manager at story_reprepare PlanDecision accept time)
// flow into the prompt context as StoryRecoveryHint entries. Sarah's
// re-prep prompt then surfaces those diagnoses so she re-authors the
// affected Stories with the wedge context in scope.
func TestBuildPromptContext_PopulatesStoryRecoveryHints(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "demo",
		Title: "demo",
		Stories: []workflow.Story{
			{ID: "story.demo.1.1", RequirementID: "req.demo.1", Title: "untouched"},
			{ID: "story.demo.1.2", RequirementID: "req.demo.1", Title: "flagged", RecoveryHint: "missed src/x.go"},
			{ID: "story.demo.1.3", RequirementID: "req.demo.1", Title: "untouched"},
		},
	}

	ctx := buildPromptContext(plan, "")

	if len(ctx.StoryRecoveryHints) != 1 {
		t.Fatalf("StoryRecoveryHints len = %d, want 1 (only Story 2 has a hint)", len(ctx.StoryRecoveryHints))
	}
	if ctx.StoryRecoveryHints[0].StoryID != "story.demo.1.2" {
		t.Errorf("StoryRecoveryHints[0].StoryID = %q, want story.demo.1.2", ctx.StoryRecoveryHints[0].StoryID)
	}
	if ctx.StoryRecoveryHints[0].Hint != "missed src/x.go" {
		t.Errorf("StoryRecoveryHints[0].Hint = %q, want %q", ctx.StoryRecoveryHints[0].Hint, "missed src/x.go")
	}
}

// TestBuildPromptContext_NoHintsForForwardFlow pins the back-compat path:
// on the architecture_generated → preparing_stories forward flow, no
// Story has a RecoveryHint set (Sarah is preparing for the first time),
// so StoryRecoveryHints comes out empty. The prompt render's hints block
// won't fire and Sarah's prompt looks like the pre-Train-C shape.
func TestBuildPromptContext_NoHintsForForwardFlow(t *testing.T) {
	plan := &workflow.Plan{
		Slug:  "demo",
		Title: "demo",
		// No Stories yet — typical forward-flow state at preparing_stories
		// claim time. Even if Stories were present, none would have
		// RecoveryHint set on the forward flow.
	}

	ctx := buildPromptContext(plan, "")

	if len(ctx.StoryRecoveryHints) != 0 {
		t.Errorf("StoryRecoveryHints = %v, want empty (forward flow has no recovery context)", ctx.StoryRecoveryHints)
	}
}

// TestClaimedSlugDedup_RoundTripsAndConsumes pins the in-process dedup
// that prevents the watcher from double-dispatching when its own
// ClaimPlanStatus echoes back as a preparing_stories KV event.
func TestClaimedSlugDedup_RoundTripsAndConsumes(t *testing.T) {
	c := &Component{claimedSlugs: make(map[string]bool)}

	// First-time mark + consume returns true (skip the echo).
	c.markClaimedSlug("demo")
	if !c.consumeClaimedSlug("demo") {
		t.Errorf("first consume after mark returned false; want true (skip self-echo)")
	}

	// Second consume returns false (no pending claim → external trigger
	// like plan-manager's back-transition).
	if c.consumeClaimedSlug("demo") {
		t.Errorf("second consume returned true; want false (no pending claim)")
	}

	// Unmarked slug always returns false.
	if c.consumeClaimedSlug("other") {
		t.Errorf("consume on unmarked slug returned true; want false")
	}
}

// TestClaimedSlugDedup_PerSlugIndependent pins that the dedup is
// slug-scoped — marking slug A doesn't affect slug B.
func TestClaimedSlugDedup_PerSlugIndependent(t *testing.T) {
	c := &Component{claimedSlugs: make(map[string]bool)}

	c.markClaimedSlug("a")
	c.markClaimedSlug("b")

	if !c.consumeClaimedSlug("a") {
		t.Errorf("consume(a) after mark(a) returned false; want true")
	}
	if !c.consumeClaimedSlug("b") {
		t.Errorf("consume(b) after mark(b) returned false; want true (independent of a)")
	}
}
