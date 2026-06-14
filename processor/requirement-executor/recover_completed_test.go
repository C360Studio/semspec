package requirementexecutor

import (
	"context"
	"sync"
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

// TestStoriesToReopenForRecovery pins the M:N owner/complete+executing+failed
// gating: recovery resumes must reset ALL non-ready owned Stories so the dev
// loop can re-claim them. This includes Executing stories left in flight when a
// dev-gate fast-fail (ADR-049 Move-3) transitions the requirement to
// awaiting-recovery — the ADR-049 / #167/#168 fix extends the original
// QA-recovery logic (complete-only) to the iteration-exhaustion path. Reopening
// a non-owned Story would invert the M:N reservation; a non-owner re-skips and
// fast-completes via Tier-1 dedup once the owner re-ships.
func TestStoriesToReopenForRecovery(t *testing.T) {
	plan := &workflow.Plan{Stories: []workflow.Story{
		// owner = r1 (smallest), complete → reopen candidate for r1
		{ID: "s1", Status: workflow.StoryStatusComplete, RequirementIDs: []string{"r1", "r2"}},
		// owner = r2, complete → reopen candidate for r2 only
		{ID: "s2", Status: workflow.StoryStatusComplete, RequirementIDs: []string{"r2", "r3"}},
		// owner = r1, executing → reopen candidate (dev-gate fast-fail leaves story here)
		{ID: "s3", Status: workflow.StoryStatusExecuting, RequirementIDs: []string{"r1"}},
		// owner = r1, failed → reopen candidate
		{ID: "s4", Status: workflow.StoryStatusFailed, RequirementIDs: []string{"r1"}},
		// owner = r1, already ready → NOT a candidate (nothing to reset)
		{ID: "s5", Status: workflow.StoryStatusReady, RequirementIDs: []string{"r1"}},
		// owner = r1, pending → NOT a candidate (nothing to reset)
		{ID: "s6", Status: workflow.StoryStatusPending, RequirementIDs: []string{"r1"}},
	}}

	tests := []struct {
		name  string
		plan  *workflow.Plan
		reqID string
		want  []string
	}{
		// r1 owns s1 (complete), s3 (executing), s4 (failed), s6 (pending) — all
		// are walk candidates (C2 fix adds Pending: Pending→Executing is invalid,
		// so Pending must be walked to Ready before re-dispatch).
		// s5 (ready) is the only excluded status — Ready→Executing is the valid
		// re-claim path; no walk needed.
		{"owner_complete_executing_failed_pending", plan, "r1", []string{"s1", "s3", "s4", "s6"}},
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

// TestStoryStatusWalkToReady pins the walk helper used by the
// reopenOwnedStoriesForRecoveryLocked side-effecting path. A single-hop target
// (Complete→Ready) must emit exactly one step; multi-hop targets (Executing→
// Failed→Pending→Ready) must emit the full intermediate chain.
func TestStoryStatusWalkToReady(t *testing.T) {
	tests := []struct {
		name string
		from workflow.StoryStatus
		want []workflow.StoryStatus
	}{
		{
			name: "complete_single_hop",
			from: workflow.StoryStatusComplete,
			want: []workflow.StoryStatus{workflow.StoryStatusReady},
		},
		{
			name: "failed_two_hops",
			from: workflow.StoryStatusFailed,
			want: []workflow.StoryStatus{workflow.StoryStatusPending, workflow.StoryStatusReady},
		},
		{
			name: "executing_three_hops",
			from: workflow.StoryStatusExecuting,
			want: []workflow.StoryStatus{
				workflow.StoryStatusFailed,
				workflow.StoryStatusPending,
				workflow.StoryStatusReady,
			},
		},
		// Already-ready: no steps needed (no-op path).
		{name: "ready_no_steps", from: workflow.StoryStatusReady, want: nil},
		// Pending: one step to ready.
		{name: "pending_one_step", from: workflow.StoryStatusPending, want: []workflow.StoryStatus{workflow.StoryStatusReady}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storyStatusWalkToReady(tt.from)
			if len(got) != len(tt.want) {
				t.Fatalf("storyStatusWalkToReady(%q) = %v, want %v", tt.from, got, tt.want)
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("step[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// fakeStoryStatusClaimer is an in-process fake for the story-status claim
// round-trip. It maintains authoritative per-story status and enforces
// CanTransitionTo so tests can assert the full walk chain without a live
// plan-manager or NATS substrate.
//
// Thread-safe: tests that drive claims from a single goroutine don't need extra
// locking; the mu protects against concurrent accesses in future tests.
type fakeStoryStatusClaimer struct {
	mu       sync.Mutex
	statuses map[string]workflow.StoryStatus // storyID → current status
	// calls records (storyID, target) pairs in the order they were made.
	calls []struct {
		storyID string
		target  workflow.StoryStatus
	}
	// failOn, when set, causes the first call matching (storyID, target) to
	// return false (simulating a transient NATS failure).
	failOn *struct {
		storyID string
		target  workflow.StoryStatus
	}
}

func newFakeClaimer(initial map[string]workflow.StoryStatus) *fakeStoryStatusClaimer {
	return &fakeStoryStatusClaimer{statuses: initial}
}

func (f *fakeStoryStatusClaimer) claim(_ context.Context, _ string, storyID string, target workflow.StoryStatus) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		storyID string
		target  workflow.StoryStatus
	}{storyID, target})
	if f.failOn != nil && f.failOn.storyID == storyID && f.failOn.target == target {
		f.failOn = nil // one-shot
		return false
	}
	current, ok := f.statuses[storyID]
	if !ok {
		return false
	}
	if !current.CanTransitionTo(target) {
		return false
	}
	f.statuses[storyID] = target
	return true
}

func (f *fakeStoryStatusClaimer) statusOf(storyID string) workflow.StoryStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[storyID]
}

// TestReopenStoriesFromPlan_ExecutingWalk is the honest end-to-end regression
// for the ADR-049 dev-gate false-completion bug (slug a6819e22bb85).
//
// It proves the complete Executing→Failed→Pending→Ready walk using a
// fakeStoryStatusClaimer that enforces CanTransitionTo — so the test fails if
// the walk tries an invalid hop (e.g. Executing→Ready directly) or if the
// selection omits an Executing story. This is the test the go-reviewer flagged
// as missing: it is genuinely RED against pre-fix code and GREEN after the fix.
//
// Red evidence (pre-fix behavior): before the ADR-049 fix,
// storiesToReopenForRecovery excluded Executing and Pending stories, so
// reopenStoriesFromPlan received an empty `ids` slice and returned
// reopened=0, candidates=0. The test asserts candidates==1 and
// statusOf("story-exec")=="ready", which fails when candidates==0.
//
// See also TestStoriesToReopenForRecovery (selection) and
// TestStoryStatusWalkToReady (hop sequence) which pin the building blocks.
func TestReopenStoriesFromPlan_ExecutingWalk(t *testing.T) {
	t.Run("executing_story_walks_to_ready_via_three_hops", func(t *testing.T) {
		plan := &workflow.Plan{Stories: []workflow.Story{
			{ID: "story-exec", Status: workflow.StoryStatusExecuting, RequirementIDs: []string{"req-1"}},
		}}
		fake := newFakeClaimer(map[string]workflow.StoryStatus{
			"story-exec": workflow.StoryStatusExecuting,
		})
		ids := storiesToReopenForRecovery(plan, "req-1")
		if len(ids) != 1 {
			t.Fatalf("storiesToReopenForRecovery returned %v; pre-fix code returns [] — "+
				"this is the selection half of the regression", ids)
		}

		reopened, candidates := reopenStoriesFromPlan(context.Background(), "plan-x", plan, ids, fake.claim)

		if candidates != 1 {
			t.Errorf("candidates = %d, want 1", candidates)
		}
		if reopened != 1 {
			t.Errorf("reopened = %d, want 1 (walk must complete)", reopened)
		}
		if got := fake.statusOf("story-exec"); got != workflow.StoryStatusReady {
			t.Errorf("final story status = %q, want %q", got, workflow.StoryStatusReady)
		}
		// Assert the exact 3-hop sequence enforced by the state machine.
		wantCalls := []struct {
			storyID string
			target  workflow.StoryStatus
		}{
			{"story-exec", workflow.StoryStatusFailed},
			{"story-exec", workflow.StoryStatusPending},
			{"story-exec", workflow.StoryStatusReady},
		}
		if len(fake.calls) != len(wantCalls) {
			t.Fatalf("claimer calls = %v, want %v", fake.calls, wantCalls)
		}
		for i, w := range wantCalls {
			if fake.calls[i] != w {
				t.Errorf("call[%d] = %v, want %v", i, fake.calls[i], w)
			}
		}
	})

	t.Run("pending_story_walks_to_ready_in_one_hop", func(t *testing.T) {
		// C2 regression: a Pending story was excluded by the pre-fix selection
		// (only Complete/Executing/Failed were included). Pending→Executing is
		// invalid per CanTransitionTo, so an unreseted Pending story causes the
		// same false-complete path as Executing.
		plan := &workflow.Plan{Stories: []workflow.Story{
			{ID: "story-pend", Status: workflow.StoryStatusPending, RequirementIDs: []string{"req-2"}},
		}}
		fake := newFakeClaimer(map[string]workflow.StoryStatus{
			"story-pend": workflow.StoryStatusPending,
		})
		ids := storiesToReopenForRecovery(plan, "req-2")
		if len(ids) != 1 {
			t.Fatalf("storiesToReopenForRecovery returned %v for Pending story; "+
				"pre-fix code excludes Pending — C2 regression", ids)
		}

		reopened, candidates := reopenStoriesFromPlan(context.Background(), "plan-x", plan, ids, fake.claim)

		if candidates != 1 || reopened != 1 {
			t.Errorf("reopened=%d, candidates=%d; want both 1", reopened, candidates)
		}
		if got := fake.statusOf("story-pend"); got != workflow.StoryStatusReady {
			t.Errorf("final story status = %q, want ready", got)
		}
		// Exactly one hop: Pending→Ready.
		if len(fake.calls) != 1 || fake.calls[0].target != workflow.StoryStatusReady {
			t.Errorf("claimer calls = %v, want [{story-pend ready}]", fake.calls)
		}
	})

	t.Run("partial_walk_reports_incomplete_and_is_self_healing_on_retry", func(t *testing.T) {
		// C1 regression: a partial walk (Executing→Failed succeeds, Failed→Pending
		// fails) left the story at Failed. On the next call the story is re-selected
		// (Failed is a candidate) and the walk restarts from its CURRENT status
		// (Failed), not from the original Executing, so it only needs 2 more hops.
		plan := &workflow.Plan{Stories: []workflow.Story{
			{ID: "story-partial", Status: workflow.StoryStatusExecuting, RequirementIDs: []string{"req-3"}},
		}}
		fake := newFakeClaimer(map[string]workflow.StoryStatus{
			"story-partial": workflow.StoryStatusExecuting,
		})
		// First call: fail at Failed→Pending so the walk strands at Failed.
		fake.failOn = &struct {
			storyID string
			target  workflow.StoryStatus
		}{"story-partial", workflow.StoryStatusPending}

		ids := storiesToReopenForRecovery(plan, "req-3")
		reopened, candidates := reopenStoriesFromPlan(context.Background(), "plan-x", plan, ids, fake.claim)

		if candidates != 1 || reopened != 0 {
			t.Errorf("first call: reopened=%d candidates=%d; want 0/1", reopened, candidates)
		}
		if got := fake.statusOf("story-partial"); got != workflow.StoryStatusFailed {
			t.Errorf("after partial walk story status = %q, want failed", got)
		}

		// Simulate second recovery cycle: plan is updated to reflect the new status.
		plan2 := &workflow.Plan{Stories: []workflow.Story{
			{ID: "story-partial", Status: workflow.StoryStatusFailed, RequirementIDs: []string{"req-3"}},
		}}
		ids2 := storiesToReopenForRecovery(plan2, "req-3")
		if len(ids2) != 1 {
			t.Fatalf("second cycle: storiesToReopenForRecovery(%q) = %v; "+
				"Failed must still be selected for self-healing", "req-3", ids2)
		}

		fake.calls = nil // reset call log for second cycle
		reopened2, candidates2 := reopenStoriesFromPlan(context.Background(), "plan-x", plan2, ids2, fake.claim)

		if candidates2 != 1 || reopened2 != 1 {
			t.Errorf("second call: reopened=%d candidates=%d; want 1/1", reopened2, candidates2)
		}
		if got := fake.statusOf("story-partial"); got != workflow.StoryStatusReady {
			t.Errorf("after self-heal story status = %q, want ready", got)
		}
		// Self-healing: only 2 hops needed (Failed→Pending→Ready), not 3.
		wantCalls := []struct {
			storyID string
			target  workflow.StoryStatus
		}{
			{"story-partial", workflow.StoryStatusPending},
			{"story-partial", workflow.StoryStatusReady},
		}
		if len(fake.calls) != len(wantCalls) {
			t.Fatalf("second cycle claimer calls = %v, want %v", fake.calls, wantCalls)
		}
		for i, w := range wantCalls {
			if fake.calls[i] != w {
				t.Errorf("second cycle call[%d] = %v, want %v", i, fake.calls[i], w)
			}
		}
	})
}

// TestResumeFromRecoveryLocked_ResetsExecutingOwnedStory keeps the selection-
// half sub-test (it's a genuine selection regression pinning the bug root).
// The tautological "resume_does_not_false_complete" sub-test that asserted
// natsClient==nil bookkeeping fields is REMOVED — it proved nothing about the
// story walk because reopenOwnedStoriesForRecoveryLocked is a no-op when
// natsClient==nil. The honest end-to-end walk coverage is in
// TestReopenStoriesFromPlan_ExecutingWalk above.
//
// This test retains the selection assertion because it exercises
// storiesToReopenForRecovery directly and is genuinely RED against pre-fix
// code (pre-fix excludes Executing; post-fix includes it).
func TestResumeFromRecoveryLocked_ResetsExecutingOwnedStory(t *testing.T) {
	t.Run("executing_owned_story_is_selected_for_reopen", func(t *testing.T) {
		plan := &workflow.Plan{Stories: []workflow.Story{
			{ID: "story-1", Status: workflow.StoryStatusExecuting, RequirementIDs: []string{"req-gate"}},
		}}
		got := storiesToReopenForRecovery(plan, "req-gate")
		if len(got) != 1 || got[0] != "story-1" {
			t.Errorf("storiesToReopenForRecovery: got %v, want [story-1]; "+
				"an Executing owned story must be a reopen candidate for recovery resume", got)
		}
	})
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
