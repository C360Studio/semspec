package requirementexecutor

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// TestResetDependentBranchSubtree pins the P3 dependent-invalidation wiring:
// when an owner is reopened for QA-recovery, every requirement whose branch
// derives from it must have its stale work + reqbase branches deleted (so it
// re-derives from the rebuilt owner). The pure subtree math is covered by
// workflow.TestDependentBranchSubtree; here we pin that the executor enumerates
// the right dependents and targets the right branches (sendReqReset is a no-op
// in unit mode — nil natsClient — so this asserts the branch-delete wiring).
func TestResetDependentBranchSubtree(t *testing.T) {
	c := newTestComponent(t)
	stub := &stubSandbox{}
	c.sandbox = stub

	// a1 <- b1 <- c1 ; reopening a1 invalidates {b1, c1}, not the unrelated z1.
	plan := &workflow.Plan{
		Slug: "demo",
		Requirements: []workflow.Requirement{
			{ID: "a1"},
			{ID: "b1", DependsOn: []string{"a1"}},
			{ID: "c1", DependsOn: []string{"b1"}},
			{ID: "z1"},
		},
	}

	c.resetDependentBranchSubtree(context.Background(), "demo", "a1", plan)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	want := []string{
		"semspec/requirement-b1", "semspec/reqbase-b1",
		"semspec/requirement-c1", "semspec/reqbase-c1",
	}
	if len(stub.deletedBranchNames) != len(want) {
		t.Fatalf("deleted branches = %v, want %v", stub.deletedBranchNames, want)
	}
	for i, w := range want {
		if stub.deletedBranchNames[i] != w {
			t.Errorf("deletedBranchNames[%d] = %q, want %q", i, stub.deletedBranchNames[i], w)
		}
	}
	// z1 has no derivation edge to a1 — its branches must NOT be touched.
	for _, b := range stub.deletedBranchNames {
		if b == "semspec/requirement-z1" || b == "semspec/reqbase-z1" {
			t.Errorf("z1 branch %q deleted but z1 does not derive from a1", b)
		}
	}
}

// TestCoAffectedDependentSet pins finding [1]: when a single recovery event
// affects both a prerequisite and a requirement that derives from it, the
// dependent must be marked skip-direct-resume (the prereq's cascade re-derives
// it) — regardless of slice order — so it never rebuilds against the prereq's
// pre-rebuild base.
func TestCoAffectedDependentSet(t *testing.T) {
	reqs := []workflow.Requirement{
		{ID: "a1"},
		{ID: "b1", DependsOn: []string{"a1"}},
		{ID: "c1", DependsOn: []string{"b1"}},
	}

	// a1 + its dependents all affected, dependent-first order: b1, c1 skipped.
	skip := coAffectedDependentSet([]string{"c1", "b1", "a1"}, reqs, nil)
	if !skip["b1"] || !skip["c1"] {
		t.Errorf("b1 and c1 must be skipped (derive from a1); skip=%v", skip)
	}
	if skip["a1"] {
		t.Errorf("a1 is the root prerequisite and must NOT be skipped; skip=%v", skip)
	}

	// Only the prereq affected -> no dependents in the affected set -> none skipped.
	if s := coAffectedDependentSet([]string{"a1"}, reqs, nil); len(s) != 0 {
		t.Errorf("single affected req must skip nothing, got %v", s)
	}

	// Two unrelated reqs -> neither derives from the other -> none skipped.
	if s := coAffectedDependentSet([]string{"a1", "x1"}, []workflow.Requirement{{ID: "a1"}, {ID: "x1"}}, nil); len(s) != 0 {
		t.Errorf("unrelated affected reqs must skip nothing, got %v", s)
	}
}

// TestAbandonLiveExecForRequirement pins finding [5] / design §11.F: a
// dependent that is still mid-execution when its prerequisite reopens must be
// torn down (terminated + removed from activeExecs) so the fresh re-dispatch is
// not swallowed by the req_watcher EntityID duplicate guard.
func TestAbandonLiveExecForRequirement(t *testing.T) {
	c := newTestComponent(t)
	exec := &requirementExecution{
		EntityID:       workflow.EntityPrefix() + ".exec.req.run.demo-b1",
		Slug:           "demo",
		RequirementID:  "b1",
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
		storeKey:       workflow.RequirementExecutionKey("demo", "b1"),
	}
	c.activeExecs.Set(exec.EntityID, exec) //nolint:errcheck

	if !c.abandonLiveExecForRequirement("demo", "b1") {
		t.Fatal("expected to find and abandon the live b1 exec")
	}
	if !exec.terminated {
		t.Error("abandoned exec should be marked terminated")
	}
	if _, ok := c.activeExecs.Get(exec.EntityID); ok {
		t.Error("abandoned exec should be removed from activeExecs (cleanupExecutionLocked)")
	}
	// No live exec for the requirement -> false, no panic.
	if c.abandonLiveExecForRequirement("demo", "nonexistent") {
		t.Error("expected false when no live exec matches")
	}
}

// TestResumeTerminalForRecoveryLocked covers the new branch added 2026-05-28
// after the gemini mavlink-decode run #4 verified PR #29's
// publishRecoveryRequested wire fires correctly but the plan stayed at
// 'rejected' because the requirement_executions were at terminal stage
// 'completed' and findAwaitingByRequirement missed them.
//
// The test asserts: setting awaitingRecovery + recoveryReason then deferring
// to resumeFromRecoveryLocked produces the same end-state as the existing
// iteration-exhaustion resume path. We don't try to exercise the KV-loading
// portion here — that's covered by the integration path in the e2e harness.
// The unit-level claim this test pins is: "given a terminal-stage exec
// inserted into activeExecs, the QA-recovery branch correctly transitions
// it through the recovery flow."
func TestResumeTerminalForRecoveryLocked_TransitionsThroughRecoveryFlow(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-qa-recovery", "req-qa-recovery-1")
	// Terminal-stage prelude: in real life exec reached completed and was
	// removed from activeExecs by cleanupExecutionLocked. For this test we
	// just need the exec object — the caller-of-resumeTerminalForRecovery
	// re-inserts it into activeExecs after KV load (or in this case
	// directly).
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.resumeTerminalForRecoveryLocked(context.Background(), exec, "plan-decision.test.recovery.abc12345")
	exec.mu.Unlock()

	// After resumeFromRecoveryLocked runs, awaitingRecovery should be
	// cleared (it was set to true momentarily inside the function so the
	// resume's lifecycle bookkeeping treats the exec as recovering, then
	// the resume itself flips it back).
	if exec.awaitingRecovery {
		t.Error("exec.awaitingRecovery should be false after resume completes")
	}
	// ADR-043 PR 4g — exec.terminated is intentionally not asserted here.
	// Synthesis is sync and requires plan.Stories in PLAN_STATES; unit-test
	// mode has no NATS client so synthesis marks the exec failed before
	// returning. The bookkeeping assertions below still pin the resume
	// reset (the contract this test actually exercises); production paths
	// exercise successful re-execution via integration tests.
	if exec.recoveryRestarts != 1 {
		t.Errorf("exec.recoveryRestarts = %d, want 1 (incremented by the resume path)", exec.recoveryRestarts)
	}
	if exec.CurrentNodeIdx != -1 {
		t.Errorf("exec.CurrentNodeIdx = %d, want -1 (DAG reset by the resume path)", exec.CurrentNodeIdx)
	}
	if exec.DAG != nil {
		t.Errorf("exec.DAG = %v, want nil (DAG reset by the resume path)", exec.DAG)
	}
	if exec.ReviewVerdict != "" {
		t.Errorf("exec.ReviewVerdict = %q, want empty (resumption clears prior verdict)", exec.ReviewVerdict)
	}
}

// TestResumeTerminalForRecoveryLocked_RecordsReasonBeforeResume verifies
// the marking step (awaitingRecovery + recoveryReason) is applied by the
// wrapper BEFORE resumeFromRecoveryLocked clears awaitingRecovery on the
// way out. We intercept via the recoveryReason field's persistence across
// the resume: the existing resume function doesn't touch recoveryReason,
// so a value set by the wrapper survives the resume's state reset and is
// observable on the exec afterward.
//
// Why this matters: handleRecoveryTimeout's fallback (awaiting_recovery.go:
// 143) reads exec.recoveryReason and passes it to markFailedLocked if the
// recovery times out. The wrapper's recoveryReason populates that channel
// with the QA-recovery proposal context, which surfaces in observability
// when the post-resume retry also fails.
func TestResumeTerminalForRecoveryLocked_RecordsReasonBeforeResume(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-reason", "req-reason-1")
	c.activeExecs.Set(exec.EntityID, exec)

	proposalID := "plan-decision.reason.test.recovery.deadbeef"
	exec.mu.Lock()
	c.resumeTerminalForRecoveryLocked(context.Background(), exec, proposalID)
	gotReason := exec.recoveryReason
	exec.mu.Unlock()

	want := "QA-recovery: completed req re-dispatched (proposal " + proposalID + ")"
	if gotReason != want {
		t.Errorf("recoveryReason after resume = %q, want %q", gotReason, want)
	}
}

// TestStoriesToReopenForRecovery pins the M:N owner/complete gating: a
// QA-recovery resume reopens only the Stories the requirement OWNS and that are
// currently complete. Reopening a non-owned Story would invert the M:N
// reservation (a non-owner would run the dev loop); reopening a non-complete
// Story is meaningless.
func TestStoriesToReopenForRecovery(t *testing.T) {
	plan := &workflow.Plan{Stories: []workflow.Story{
		// owner = r1 (smallest), complete → reopen candidate for r1
		{ID: "s1", Status: workflow.StoryStatusComplete, RequirementIDs: []string{"r1", "r2"}},
		// owner = r2, complete → reopen candidate for r2 only
		{ID: "s2", Status: workflow.StoryStatusComplete, RequirementIDs: []string{"r2", "r3"}},
		// owner = r1 but NOT complete → never a reopen candidate
		{ID: "s3", Status: workflow.StoryStatusExecuting, RequirementIDs: []string{"r1"}},
	}}

	tests := []struct {
		name  string
		plan  *workflow.Plan
		reqID string
		want  []string
	}{
		{"owner_complete_reopens", plan, "r1", []string{"s1"}},
		{"non_owner_excluded", plan, "r2", []string{"s2"}}, // r2 covers s1 but doesn't own it
		{"covers_but_not_owner", plan, "r3", nil},          // r3 covers s2 but r2 owns it
		{"nil_plan", nil, "r1", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storiesToReopenForRecovery(tt.plan, tt.reqID)
			if !equalStringSlices(got, tt.want) {
				t.Errorf("storiesToReopenForRecovery(%q) = %v, want %v", tt.reqID, got, tt.want)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
