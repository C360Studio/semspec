package executionmanager

import (
	"context"
	"testing"

	"github.com/c360studio/semspec/workflow/payloads"
)

// captureRecoveryPublisher returns a stub that records every RecoveryRequested
// emitted by the Component and a fetch closure to read the captured slice.
// Mirrors the helper in plan-manager/recovery_publish_test.go — same pattern
// kept consistent across components so the test seam reads identically wherever
// recovery publishing is wired.
func captureRecoveryPublisher() (func(ctx context.Context, req *payloads.RecoveryRequested), func() []*payloads.RecoveryRequested) {
	var captured []*payloads.RecoveryRequested
	publisher := func(_ context.Context, req *payloads.RecoveryRequested) {
		captured = append(captured, req)
	}
	return publisher, func() []*payloads.RecoveryRequested { return captured }
}

// TestMarkEscalatedLocked_FiresRecoveryRequested closes a pre-existing coverage
// gap: TestMarkEscalatedLocked_IncrementsCounters asserted counter side effects
// but not whether RecoveryRequested actually gets published at the trigger.
// If publishRecoveryRequested were silently broken (wrong subject, malformed
// payload, nil-guard accidentally skipping), the counter assertions would still
// pass. This test catches that class of regression by intercepting the wire.
func TestMarkEscalatedLocked_FiresRecoveryRequested(t *testing.T) {
	c := newTestComponent(t)
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	exec := newTestExec("recovery-fires-slug", "task-recovery")
	exec.RequirementID = "req-recovery-1"
	exec.DeveloperLoopID = "loop-developer-1"
	exec.Feedback = "Reviewer rejected: missing tests for the 5xx path."
	exec.TraceID = "trace-recovery-1"

	c.activeExecs.Set(exec.EntityID, exec)

	exec.mu.Lock()
	c.markEscalatedLocked(testCtx(t), exec, "fixable rejections exceeded TDD cycle budget")
	exec.mu.Unlock()

	got := fetch()
	if len(got) != 1 {
		t.Fatalf("expected 1 RecoveryRequested publish at iteration exhaustion, got %d", len(got))
	}
	r := got[0]
	if r.Slug != "recovery-fires-slug" {
		t.Errorf("Slug = %q, want recovery-fires-slug", r.Slug)
	}
	if r.RequirementID != "req-recovery-1" {
		t.Errorf("RequirementID = %q, want req-recovery-1", r.RequirementID)
	}
	if r.TaskID != "task-recovery" {
		t.Errorf("TaskID = %q, want task-recovery", r.TaskID)
	}
	if r.LoopID != "loop-developer-1" {
		t.Errorf("LoopID = %q, want loop-developer-1", r.LoopID)
	}
	if r.Layer != payloads.RecoveryLayerPhaseLocal {
		t.Errorf("Layer = %q, want phase_local", r.Layer)
	}
	if r.EscalationReason != "fixable rejections exceeded TDD cycle budget" {
		t.Errorf("EscalationReason = %q, want the exhaustion reason verbatim", r.EscalationReason)
	}
	if r.LastFailureFeedback != exec.Feedback {
		t.Errorf("LastFailureFeedback = %q, want exec.Feedback verbatim", r.LastFailureFeedback)
	}
	if r.TraceID != "trace-recovery-1" {
		t.Errorf("TraceID = %q, want trace-recovery-1", r.TraceID)
	}
	if r.RecoveryID == "" {
		t.Error("RecoveryID should be populated with a fresh UUID")
	}
}
