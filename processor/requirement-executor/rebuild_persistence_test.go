package requirementexecutor

import (
	"context"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// TestRebuildExecFromKV_CommitSHARoundTrips pins the load-bearing
// contract behind go-reviewer Pass-1 finding C4: after a process restart,
// the requirement-executor rebuilds its in-memory state from
// EXECUTION_STATES KV. NodeResults restored from KV MUST carry their
// CommitSHA values so the RequireCommitObservation gate sees the merge
// commit observation that produced them.
//
// Pre-fix, workflow.NodeResult had no CommitSHA field. The executor set
// CommitSHA on its in-memory shadow type but dropped it at the wire
// boundary when building the workflow.NodeResult for execution-manager's
// KV. After a restart, rebuildExecFromKV reconstructed every NodeResult
// with an empty CommitSHA, and the gate at handleApprovedClaimMismatchLocked
// false-failed every requirement that had completed nodes claiming
// FilesModified.
func TestRebuildExecFromKV_CommitSHARoundTrips(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		NodeResults: []workflow.NodeResult{
			{NodeID: "node.A", FilesModified: []string{"src/a.go"}, CommitSHA: "abc123"},
			{NodeID: "node.B", FilesModified: []string{"src/b.go"}, CommitSHA: "def456"},
		},
	}

	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	if len(exec.NodeResults) != 2 {
		t.Fatalf("NodeResults = %d, want 2", len(exec.NodeResults))
	}
	if exec.NodeResults[0].CommitSHA != "abc123" {
		t.Errorf("NodeResults[0].CommitSHA = %q, want abc123 (rebuilt from KV)", exec.NodeResults[0].CommitSHA)
	}
	if exec.NodeResults[1].CommitSHA != "def456" {
		t.Errorf("NodeResults[1].CommitSHA = %q, want def456 (rebuilt from KV)", exec.NodeResults[1].CommitSHA)
	}
}

// TestRebuildExecFromKV_StoryCursorRoundTrips pins go-reviewer Pass-1
// findings C1 + C2: after restart, the per-Story cursor (SortedStoryIDs +
// CurrentStoryIdx) MUST round-trip through KV. Pre-fix, no sendReqPhase
// call sent these fields, so rebuildExecFromKV always reconstructed
// SortedStoryIDs as nil and CurrentStoryIdx as 0. A 3-Story requirement
// mid-Story-2 at restart would silently truncate to a single Story:
//
//	rebuilt cursor: SortedStoryIDs=[], CurrentStoryIdx=0
//	handleApprovedVerdictLocked: 0+1 < 0 → false → markCompletedLocked
//	Stories 2 and 3 silently never run; requirement claims success.
func TestRebuildExecFromKV_StoryCursorRoundTrips(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:        "entity-1",
		Slug:            "demo",
		RequirementID:   "req.demo.1",
		SortedStoryIDs:  []string{"story.demo.1.1", "story.demo.1.2", "story.demo.1.3"},
		CurrentStoryIdx: 1,
	}

	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	if len(exec.SortedStoryIDs) != 3 {
		t.Errorf("SortedStoryIDs = %v (len=%d), want 3 entries", exec.SortedStoryIDs, len(exec.SortedStoryIDs))
	}
	if exec.CurrentStoryIdx != 1 {
		t.Errorf("CurrentStoryIdx = %d, want 1 (rebuilt from KV)", exec.CurrentStoryIdx)
	}
}

// TestRebuildExecFromKV_EmptyCommitSHAPreservesBackCompat documents that
// the new CommitSHA field round-trips empty strings unchanged. KV records
// written before this PR have no commit_sha field on their NodeResults;
// after restart they restore as CommitSHA="", which the
// RequireCommitObservation gate already treated as "unobserved." This is
// the pre-existing semantic — not a regression introduced by the new field.
//
// In practice the gate's behavior for ALREADY-EMPTY-IN-KV records is
// unchanged. The new field only PERSISTS values that were previously
// dropped. Operators with old KV state will still see the gate fire on
// pre-PR completions; the fix is forward-looking.
func TestRebuildExecFromKV_EmptyCommitSHAPreservesBackCompat(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		NodeResults: []workflow.NodeResult{
			{NodeID: "node.A", FilesModified: []string{"src/a.go"}}, // legacy: no CommitSHA
		},
	}

	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	if len(exec.NodeResults) != 1 {
		t.Fatalf("NodeResults = %d, want 1", len(exec.NodeResults))
	}
	if exec.NodeResults[0].CommitSHA != "" {
		t.Errorf("NodeResults[0].CommitSHA = %q, want empty (legacy KV record had no CommitSHA)", exec.NodeResults[0].CommitSHA)
	}
}

// TestHandleApprovedClaimMismatch_DoesNotFalseFailRestoredNodes is the
// headline regression test for go-reviewer Pass-1 finding C4. Pre-fix,
// every restored NodeResult carried an empty CommitSHA (because the wire
// shape didn't have the field), so the gate at handleApprovedClaimMismatchLocked
// false-failed every requirement that completed nodes before a restart.
//
// Post-fix, restored NodeResults retain their CommitSHA values, the gate
// finds no unobserved nodes, and the requirement proceeds normally.
func TestHandleApprovedClaimMismatch_DoesNotFalseFailRestoredNodes(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		NodeResults: []workflow.NodeResult{
			{NodeID: "node.A", FilesModified: []string{"src/a.go"}, CommitSHA: "abc123"},
		},
	}
	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	fired := c.handleApprovedClaimMismatchLocked(context.Background(), exec)

	if fired {
		t.Errorf("claim/observation gate fired on restored NodeResult with CommitSHA — pre-fix shape would false-fail here; CommitSHA round-trip closes Pass-1 C4")
	}
}

// TestRebuildExecFromKV_VisitedNodesScopedToCurrentStory pins go-reviewer
// Pass-1 finding C3. Pre-fix, rebuildExecFromKV pre-populated VisitedNodes
// from the entire cross-Story NodeResults accumulator. When a 2+ Story
// requirement was mid-Story-2 at restart:
//
//	NodeResults restored = [story1.A, story1.B] (from Story 1)
//	SortedNodeIDs restored = [story2.C, story2.D, story2.E] (current Story 2 DAG)
//	VisitedNodes built from NodeResults = {story1.A, story1.B} (2 entries)
//
// First Story-2 node completes → VisitedNodes = {story1.A, story1.B, story2.C} (3)
// Check: len(VisitedNodes) >= len(SortedNodeIDs) → 3 >= 3 → TRUE
// Reviewer fires early, Story 2's D and E nodes never run.
//
// Post-fix: VisitedNodes is scoped to the current Story's SortedNodeIDs,
// so the check correctly waits for all 3 Story-2 nodes.
func TestRebuildExecFromKV_VisitedNodesScopedToCurrentStory(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		// Story 1's nodes already completed; recorded as NodeResults.
		NodeResults: []workflow.NodeResult{
			{NodeID: "story1.A", FilesModified: []string{"src/a.go"}, CommitSHA: "aa"},
			{NodeID: "story1.B", FilesModified: []string{"src/b.go"}, CommitSHA: "bb"},
		},
		// Current Story 2 DAG persisted: 3 nodes, none visited yet.
		SortedNodeIDs:   []string{"story2.C", "story2.D", "story2.E"},
		SortedStoryIDs:  []string{"story.demo.1.1", "story.demo.1.2"},
		CurrentStoryIdx: 1,
	}

	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	// NodeResults preserves the cross-Story accumulator (the claim/observation
	// gate needs both Stories' results).
	if len(exec.NodeResults) != 2 {
		t.Errorf("NodeResults = %d, want 2 (cross-Story accumulator preserved)", len(exec.NodeResults))
	}

	// VisitedNodes is the headline assertion: only current Story's nodes
	// count. Story 1's completed nodes must NOT pre-populate it.
	if len(exec.VisitedNodes) != 0 {
		t.Errorf("VisitedNodes = %v (len=%d), want empty (Story 1 nodes do not belong to Story 2's dispatch surface)", exec.VisitedNodes, len(exec.VisitedNodes))
	}
	if exec.VisitedNodes["story1.A"] {
		t.Errorf("VisitedNodes wrongly contains story1.A — pre-fix C3 shape would trip reviewer-fires-early")
	}
}

// TestRebuildExecFromKV_VisitedNodesLegacyFallbackWhenNoDAG pins the
// back-compat behavior: when SortedNodeIDs is empty (legacy KV record
// from before the DAG was persisted, or pre-Sarah plan), VisitedNodes
// falls back to the pre-C3 semantic of populating from every NodeResult.
// Important so legacy KV state still rebuilds correctly.
func TestRebuildExecFromKV_VisitedNodesLegacyFallbackWhenNoDAG(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		NodeResults: []workflow.NodeResult{
			{NodeID: "node.X", FilesModified: []string{"src/x.go"}, CommitSHA: "xx"},
		},
		// SortedNodeIDs intentionally empty — legacy KV state.
	}

	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	if !exec.VisitedNodes["node.X"] {
		t.Errorf("legacy fallback: VisitedNodes should contain node.X when SortedNodeIDs is empty (pre-C3 behavior preserved)")
	}
}

// TestHandleApprovedClaimMismatch_FiresWhenObservationGenuinelyMissing
// pins the positive direction of the gate: a NodeResult that claims
// FilesModified but truly has no CommitSHA (the bug-9 pattern this gate
// was built for) still fails the requirement. The C4 fix narrows
// false-positives without disabling the gate.
func TestHandleApprovedClaimMismatch_FiresWhenObservationGenuinelyMissing(t *testing.T) {
	c := newTestComponent(t)
	persisted := &workflow.RequirementExecution{
		EntityID:      "entity-1",
		Slug:          "demo",
		RequirementID: "req.demo.1",
		NodeResults: []workflow.NodeResult{
			{NodeID: "node.A", FilesModified: []string{"src/a.go"}}, // empty CommitSHA — bug-9 pattern
		},
	}
	exec := c.rebuildExecFromKV("req.demo.req.demo.1", persisted)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	fired := c.handleApprovedClaimMismatchLocked(context.Background(), exec)

	if !fired {
		t.Errorf("claim/observation gate should fire when CommitSHA is genuinely empty on a node that claimed FilesModified — gate must still catch bug-9 patterns")
	}
}
