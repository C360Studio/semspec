package requirementexecutor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	sscache "github.com/c360studio/semstreams/pkg/cache"
)

// newTestComponentWithRecoveryDefer mirrors newTestComponent but enables
// the ADR-037 race-closure path so the deferToAwaitingRecoveryLocked
// transition fires.
func newTestComponentWithRecoveryDefer(t *testing.T, timeout time.Duration, maxRestarts int) *Component {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DecomposerModel = "default"
	cfg.ReviewerModel = "default"
	cfg.DeferTerminalOnRecovery = true
	cfg.RecoveryTimeoutSeconds = int(timeout.Seconds())
	cfg.MaxRecoveryRestarts = maxRestarts
	raw, _ := json.Marshal(cfg)
	comp, err := NewComponent(raw, component.Dependencies{})
	if err != nil {
		t.Fatalf("newTestComponentWithRecoveryDefer: %v", err)
	}
	c := comp.(*Component)
	ae, err := sscache.NewTTL[*requirementExecution](context.Background(), 4*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("create active execs cache: %v", err)
	}
	c.activeExecs = ae
	return c
}

func newAwaitingExec(slug, reqID string) *requirementExecution {
	return &requirementExecution{
		EntityID:       "entity-" + reqID,
		Slug:           slug,
		RequirementID:  reqID,
		storeKey:       "req." + slug + "." + reqID,
		CurrentNodeIdx: -1,
		VisitedNodes:   make(map[string]bool),
		MaxRetries:     2,
	}
}

// Defer + happy-path resume — the core of the race-closure contract.
//  1. With recovery deferral enabled, deferToAwaitingRecoveryLocked
//     transitions to awaiting-recovery instead of immediately marking
//     terminated.
//  2. PlanDecisionAcceptedEvent dispatch resumes via
//     resumeFromRecoveryLocked, clearing awaitingRecovery + reusing the
//     same exec.
func TestDeferToAwaitingRecoveryLocked_TransitionsWithoutTerminating(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-1", "req-1")
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	deferred := c.deferToAwaitingRecoveryLocked(context.Background(), exec, "TDD budget exhausted")
	exec.mu.Unlock()

	if !deferred {
		t.Fatal("deferToAwaitingRecoveryLocked returned false; expected defer to succeed when feature enabled")
	}
	if exec.terminated {
		t.Error("exec.terminated should be false after defer (interim state, not terminal)")
	}
	if !exec.awaitingRecovery {
		t.Error("exec.awaitingRecovery should be true after defer")
	}
	if exec.recoveryReason != "TDD budget exhausted" {
		t.Errorf("exec.recoveryReason = %q, want %q", exec.recoveryReason, "TDD budget exhausted")
	}
	if exec.recoveryTimer == nil {
		t.Error("exec.recoveryTimer should be set after defer")
	}
	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d, want 0 (defer should not increment failed counter)", c.requirementsFailed.Load())
	}
}

// Disabled feature: defer is a no-op and caller falls through to
// markFailedLocked. Pin so the existing non-deferring code paths stay
// covered.
func TestDeferToAwaitingRecoveryLocked_FeatureDisabled_NoOp(t *testing.T) {
	c := newTestComponent(t) // DeferTerminalOnRecovery=false (default)
	exec := newAwaitingExec("plan-2", "req-2")
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	deferred := c.deferToAwaitingRecoveryLocked(context.Background(), exec, "reason")
	exec.mu.Unlock()

	if deferred {
		t.Fatal("deferToAwaitingRecoveryLocked returned true with feature disabled")
	}
	if exec.awaitingRecovery {
		t.Error("exec.awaitingRecovery should be false when feature disabled")
	}
	if exec.recoveryTimer != nil {
		t.Error("exec.recoveryTimer should be nil when feature disabled")
	}
}

// Goodhart guard: once the restart budget is exhausted, defer falls
// through so the next failure is terminal even with feature on.
func TestDeferToAwaitingRecoveryLocked_RestartBudgetExhausted_FallsThrough(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-3", "req-3")
	exec.recoveryRestarts = 1 // already at max
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	deferred := c.deferToAwaitingRecoveryLocked(context.Background(), exec, "reason")
	exec.mu.Unlock()

	if deferred {
		t.Fatal("defer succeeded despite budget exhausted; expected fall-through")
	}
}

// Timer-fires-terminal: recovery deadline expiry terminal-fails the exec
// with the captured reason, ensuring no orphan awaiting-recovery state.
func TestRecoveryTimeout_TerminalFailsWithCapturedReason(t *testing.T) {
	// 1s timeout — the smallest the int-seconds config allows. Test wait
	// window has plenty of margin so timer-firing is deterministic on slow
	// CI without making the test slow on developer machines.
	c := newTestComponentWithRecoveryDefer(t, 1*time.Second, 1)
	exec := newAwaitingExec("plan-4", "req-4")
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	_ = c.deferToAwaitingRecoveryLocked(context.Background(), exec, "captured-reason")
	exec.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		exec.mu.Lock()
		done := exec.terminated
		exec.mu.Unlock()
		if done {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if !exec.terminated {
		t.Fatal("exec.terminated should be true after recovery timeout")
	}
	if exec.awaitingRecovery {
		t.Error("exec.awaitingRecovery should be cleared after timer-fail")
	}
	if c.requirementsFailed.Load() != 1 {
		t.Errorf("requirementsFailed = %d, want 1", c.requirementsFailed.Load())
	}
}

// findAwaitingByRequirement: matches on slug+reqID, ignores non-awaiting
// execs and non-matching reqIDs. Pinned because the resume path depends
// on the lookup being narrow.
func TestFindAwaitingByRequirement(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	awaiting := newAwaitingExec("plan-5", "req-5")
	awaiting.awaitingRecovery = true
	notAwaiting := newAwaitingExec("plan-5", "req-other")
	wrongSlug := newAwaitingExec("plan-other", "req-5")
	wrongSlug.awaitingRecovery = true
	// EntityID is "entity-<reqID>" so wrongSlug would collide with
	// awaiting in the cache; namespace by slug here so the lookup is
	// genuinely matching on (slug, reqID) rather than just key.
	wrongSlug.EntityID = "entity-plan-other-req-5"

	c.activeExecs.Set(awaiting.EntityID, awaiting)
	c.activeExecs.Set(notAwaiting.EntityID, notAwaiting)
	c.activeExecs.Set(wrongSlug.EntityID, wrongSlug)

	got := c.findAwaitingByRequirement("plan-5", "req-5")
	if got != awaiting {
		t.Errorf("findAwaitingByRequirement returned wrong exec; got=%v want=%v", got, awaiting)
	}

	if c.findAwaitingByRequirement("plan-5", "no-such-req") != nil {
		t.Error("findAwaitingByRequirement should return nil for unknown reqID")
	}
	if c.findAwaitingByRequirement("plan-5", "req-other") != nil {
		t.Error("findAwaitingByRequirement should not match a non-awaiting exec")
	}
}

// handlePlanDecisionAccepted: the resume path increments
// recoveryRestarts and clears awaitingRecovery via the message handler.
// We exercise the handler shape by invoking resumeFromRecoveryLocked
// directly — the wire-message decode path is exercised by the
// integration test in the e2e harness.
func TestResumeFromRecoveryLocked_ClearsAwaitingAndIncrementsRestarts(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-6", "req-6")
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	_ = c.deferToAwaitingRecoveryLocked(context.Background(), exec, "reason")
	exec.mu.Unlock()

	if !exec.awaitingRecovery {
		t.Fatal("precondition: exec should be awaiting recovery")
	}

	exec.mu.Lock()
	c.resumeFromRecoveryLocked(context.Background(), exec)
	exec.mu.Unlock()

	if exec.awaitingRecovery {
		t.Error("exec.awaitingRecovery should be false after resume")
	}
	if exec.terminated {
		t.Error("exec.terminated should be false after resume (resumption re-opens the exec)")
	}
	if exec.recoveryRestarts != 1 {
		t.Errorf("exec.recoveryRestarts = %d, want 1", exec.recoveryRestarts)
	}
	if exec.recoveryTimer != nil {
		t.Error("exec.recoveryTimer should be nil after resume")
	}
	if exec.CurrentNodeIdx != -1 {
		t.Errorf("exec.CurrentNodeIdx = %d, want -1 (reset on resume)", exec.CurrentNodeIdx)
	}
}

// Issue #36: prior to the fix, resumeFromRecoveryLocked preserved
// exec.RetryCount, which meant every recovery resume exhausted on the
// first req-review attempt (RetryCount >= MaxRetries fired immediately)
// and deferred back to recovery — wasting outer budget. Verify the
// per-recovery retry budget is reset to 0 on resume so each recovery
// gets a real chance at the requirement before the next exhaustion.
//
// Mavlink-hard run 2026-05-31 surfaced this empirically: 4
// implement-lifecycle attempts + 3 test-lifecycle + 1 unknown + 2
// requirement-rev worktrees observed in the sandbox, with ~$40-50
// burned on a single requirement before operator kill.
func TestResumeFromRecoveryLocked_ResetsRetryCount(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-rc", "req-rc")
	c.activeExecs.Set(exec.EntityID, exec)

	// Simulate having burned the per-recovery budget before the resume.
	exec.RetryCount = 2
	exec.MaxRetries = 2
	exec.ReviewRetryCount = 1

	exec.mu.Lock()
	_ = c.deferToAwaitingRecoveryLocked(context.Background(), exec, "exhausted")
	exec.mu.Unlock()

	if !exec.awaitingRecovery {
		t.Fatal("precondition: exec should be awaiting recovery")
	}

	exec.mu.Lock()
	c.resumeFromRecoveryLocked(context.Background(), exec)
	exec.mu.Unlock()

	if exec.RetryCount != 0 {
		t.Errorf("exec.RetryCount = %d, want 0 (per-recovery budget reset on resume)", exec.RetryCount)
	}
	if exec.MaxRetries != 2 {
		t.Errorf("exec.MaxRetries = %d, want 2 (operator-config budget cap preserved)", exec.MaxRetries)
	}
	if exec.ReviewRetryCount != 0 {
		t.Errorf("exec.ReviewRetryCount = %d, want 0 (reviewer parse-retry budget reset on resume)", exec.ReviewRetryCount)
	}
	if exec.recoveryRestarts != 1 {
		t.Errorf("exec.recoveryRestarts = %d, want 1 (outer recovery budget incremented)", exec.recoveryRestarts)
	}
}

// Idempotent timeout: when resume already cleared awaitingRecovery, a
// late-firing timer must not increment failed-counter or transition
// state again.
func TestRecoveryTimeout_NoOpAfterResume(t *testing.T) {
	c := newTestComponentWithRecoveryDefer(t, 60*time.Second, 1)
	exec := newAwaitingExec("plan-7", "req-7")
	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	_ = c.deferToAwaitingRecoveryLocked(context.Background(), exec, "reason")
	c.resumeFromRecoveryLocked(context.Background(), exec)
	exec.mu.Unlock()

	// Simulate a late timer firing on the same exec.
	c.handleRecoveryTimeout(exec, 50*time.Millisecond)

	if exec.terminated {
		t.Error("exec.terminated should remain false; timeout fired after resume should no-op")
	}
	if c.requirementsFailed.Load() != 0 {
		t.Errorf("requirementsFailed = %d, want 0 (late timer after resume should not count as failure)", c.requirementsFailed.Load())
	}
}

// Wire-level: PlanDecisionAcceptedEvent payload Validate() must pass for
// the messages we expect to consume. Quick smoke; the heavy lift is in
// the integration tests.
func TestPlanDecisionAcceptedEvent_ValidatesAffectedRequirements(t *testing.T) {
	evt := payloads.PlanDecisionAcceptedEvent{
		ProposalID:             "plan-decision.foo.exhaust.bar.1",
		Slug:                   "plan-x",
		AffectedRequirementIDs: []string{"req-x"},
	}
	if err := evt.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
}
