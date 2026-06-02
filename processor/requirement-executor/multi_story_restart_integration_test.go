package requirementexecutor

import (
	"context"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestMultiStoryRestart_RebuiltStatePassesAllPass1Invariants is the
// cross-cutting integration test for go-reviewer Pass-1 finding H3:
// "no test exercises advancement to a second Story with successful
// synthesis." We can't drive a full multi-Story happy path through
// unit-test mode (no NATS / no LLM dispatch), but we CAN simulate the
// load-bearing restart boundary: post-Story-1, mid-Story-2 KV state.
//
// The test pins the combined post-conditions of steps 1, 3, and 5 of
// the Train A fix stack:
//
//   - Step 1 (C1, C2): cursor (SortedStoryIDs + CurrentStoryIdx) round-trips.
//   - Step 1 (C4): NodeResult.CommitSHA round-trips on every restored entry.
//   - Step 3 (C3): VisitedNodes is scoped to the current Story's DAG (Story 2),
//     so Story 1's completed nodes do NOT pre-populate it and trigger an
//     early reviewer fire.
//   - Step 5 (H4): the post-recovery KV state mirrors the in-memory wipe
//     (validated separately at the execution-manager handler boundary; here
//     we pin the rebuild side of the round-trip).
//
// A pre-step-1 codebase would fail every assertion below. The bug was
// invisible in smoke 6 because every fixture had exactly one Story per
// Requirement — every Pass-1 finding is multi-Story-only.
func TestMultiStoryRestart_RebuiltStatePassesAllPass1Invariants(t *testing.T) {
	c := newTestComponent(t)

	// Post-Story-1 KV state:
	//   - Story 1 (story.demo.1.1) completed with 2 nodes; their NodeResults
	//     carry CommitSHA values.
	//   - Cursor advanced to Story 2 (CurrentStoryIdx=1); Story 2's DAG of
	//     3 nodes is persisted but none have run yet.
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		Stage:         "executing",

		// Cursor — Story 2 currently in flight.
		SortedStoryIDs:  []string{"story.demo.1.1", "story.demo.1.2"},
		CurrentStoryIdx: 1,

		// Story 2's DAG.
		SortedNodeIDs: []string{"story2.C", "story2.D", "story2.E"},

		// Cross-Story NodeResults accumulator carries Story 1's completed
		// nodes (with CommitSHA) plus zero entries for Story 2 yet.
		NodeResults: []workflow.NodeResult{
			{NodeID: "story1.A", FilesModified: []string{"src/a.go"}, CommitSHA: "aa"},
			{NodeID: "story1.B", FilesModified: []string{"src/b.go"}, CommitSHA: "bb"},
		},
	}

	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	// --- Step 1 / C1+C2: cursor round-tripped. ---
	if len(exec.SortedStoryIDs) != 2 {
		t.Errorf("SortedStoryIDs = %v (len=%d), want 2 (pre-fix C1 would have empty cursor here)", exec.SortedStoryIDs, len(exec.SortedStoryIDs))
	}
	if exec.CurrentStoryIdx != 1 {
		t.Errorf("CurrentStoryIdx = %d, want 1 (pre-fix C2 would have reset to 0)", exec.CurrentStoryIdx)
	}

	// --- Step 1 / C4: CommitSHA preserved on every restored NodeResult. ---
	if len(exec.NodeResults) != 2 {
		t.Fatalf("NodeResults = %d, want 2", len(exec.NodeResults))
	}
	for _, nr := range exec.NodeResults {
		if nr.CommitSHA == "" {
			t.Errorf("NodeResult %q has empty CommitSHA — pre-fix C4 would land here, false-failing the claim/observation gate", nr.NodeID)
		}
	}

	// --- Step 3 / C3: VisitedNodes scoped to Story 2's DAG. Story 1's
	// nodes (story1.A, story1.B) are present in NodeResults but MUST NOT
	// pre-populate VisitedNodes — pre-fix they would, and the reviewer
	// would fire after the first Story-2 node completed. ---
	if len(exec.VisitedNodes) != 0 {
		t.Errorf("VisitedNodes = %v (len=%d), want empty (pre-fix C3 would have {story1.A, story1.B} from cross-Story accumulator → reviewer fires after 1 Story-2 node)", exec.VisitedNodes, len(exec.VisitedNodes))
	}
	if exec.VisitedNodes["story1.A"] || exec.VisitedNodes["story1.B"] {
		t.Errorf("VisitedNodes wrongly carries Story 1 entries — pre-fix C3 shape")
	}

	// --- Combined / claim-observation gate must not false-fail on rebuild. ---
	// The gate iterates exec.NodeResults; with CommitSHA preserved (C4), no
	// node trips the "FilesModified non-empty AND CommitSHA empty" branch.
	exec.mu.Lock()
	fired := c.handleApprovedClaimMismatchLocked(context.Background(), exec)
	exec.mu.Unlock()
	if fired {
		t.Errorf("claim/observation gate false-fired on rebuilt multi-Story state — pre-fix C4 would land here, rejecting the requirement that already completed Story 1 successfully")
	}

	// --- Combined / Story-2 reviewer scope must isolate scenarios. ---
	// With SortedStoryIDs[CurrentStoryIdx] = story.demo.1.2, scopeScenariosToCurrentStory
	// returns only Story 2's scenarios. The reviewer prompt + context built
	// from this scope agree on the verdict surface (Pass-1 C5, closed in
	// step 2 — pinned in reviewer_scope_test.go).
	exec.Scenarios = []workflow.Scenario{
		{ID: "scn.s1.1", StoryID: "story.demo.1.1", Tags: []string{workflow.TierUnit}, Given: "g", When: "w", Then: []string{"t"}},
		{ID: "scn.s2.1", StoryID: "story.demo.1.2", Tags: []string{workflow.TierUnit}, Given: "g", When: "w", Then: []string{"t"}},
	}
	scoped := scopeScenariosToCurrentStory(exec)
	if len(scoped) != 1 {
		t.Fatalf("scoped len = %d, want 1 (Story 2 only)", len(scoped))
	}
	if scoped[0].ID != "scn.s2.1" {
		t.Errorf("scoped[0].ID = %q, want scn.s2.1", scoped[0].ID)
	}
}
