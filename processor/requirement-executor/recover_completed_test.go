package requirementexecutor

import (
	"context"
	"testing"
	"time"
)

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
