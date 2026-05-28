package planmanager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// captureRecoveryPublisher returns a stub that records every RecoveryRequested
// emitted by the Component. The returned closure goes onto Component.recoveryPublisher
// for the lifetime of the test; the returned function returns the captured slice
// on each call. Use it for tests that need to assert RecoveryRequested was
// published at a specific trigger site without spinning up a real natsClient.
func captureRecoveryPublisher() (func(ctx context.Context, req *payloads.RecoveryRequested), func() []*payloads.RecoveryRequested) {
	var captured []*payloads.RecoveryRequested
	publisher := func(_ context.Context, req *payloads.RecoveryRequested) {
		captured = append(captured, req)
	}
	return publisher, func() []*payloads.RecoveryRequested { return captured }
}

// ---------------------------------------------------------------------------
// QA verdict trigger — the new wire added 2026-05-28 after the gemini
// mavlink-decode run showed the plan terminating at rejected with no retry
// even though qa-reviewer rendered an actionable diagnosis.
// ---------------------------------------------------------------------------

func TestHandleQAVerdictMutation_NeedsChangesFiresRecoveryRequested(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	slug := "qa-needs-changes-fires-recovery"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingQA
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	event := workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   workflow.QALevelIntegration,
		Verdict: workflow.QAVerdictNeedsChanges,
		Summary: "Integration test has hardcoded time.Sleep — replace with active polling.",
		TraceID: "trace-qa-1",
	}
	data, _ := json.Marshal(event)

	resp := c.handleQAVerdictMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got := fetch()
	if len(got) != 1 {
		t.Fatalf("expected 1 RecoveryRequested publish, got %d", len(got))
	}
	r := got[0]
	if r.Slug != slug {
		t.Errorf("Slug = %q, want %q", r.Slug, slug)
	}
	if r.Layer != payloads.RecoveryLayerPhaseLocal {
		t.Errorf("Layer = %q, want phase_local", r.Layer)
	}
	if r.LastFailureFeedback != event.Summary {
		t.Errorf("LastFailureFeedback = %q, want qa summary verbatim", r.LastFailureFeedback)
	}
	if r.TraceID != "trace-qa-1" {
		t.Errorf("TraceID = %q, want trace-qa-1", r.TraceID)
	}
	if r.RecoveryID == "" {
		t.Error("RecoveryID should be populated with a fresh UUID")
	}
	if r.EscalationReason == "" {
		t.Error("EscalationReason should name the verdict and level")
	}
}

func TestHandleQAVerdictMutation_RejectedFiresRecoveryRequested(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	slug := "qa-rejected-fires-recovery"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingQA
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	event := workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   workflow.QALevelIntegration,
		Verdict: workflow.QAVerdictRejected,
		Summary: "Unrecoverable: scope drift across all requirements.",
		TraceID: "trace-qa-2",
	}
	data, _ := json.Marshal(event)

	resp := c.handleQAVerdictMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("mutation failed: %s", resp.Error)
	}

	got := fetch()
	if len(got) != 1 {
		t.Fatalf("expected 1 RecoveryRequested publish for rejected verdict, got %d", len(got))
	}
}

func TestHandleQAVerdictMutation_ApprovedDoesNotFireRecoveryRequested(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	slug := "qa-approved-no-recovery"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingQA
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	event := workflow.QAVerdictEvent{
		Slug:    slug,
		Level:   workflow.QALevelIntegration,
		Verdict: workflow.QAVerdictApproved,
		Summary: "All requirements met.",
		TraceID: "trace-qa-3",
	}
	data, _ := json.Marshal(event)

	// The approved branch attempts a plan-level merge via the sandbox. With
	// no sandbox configured, the assemble step is a no-op (nil-guarded
	// upstream) and the mutation succeeds. We don't assert mutation success
	// here — we ONLY assert that recovery did not fire. A future change to
	// the approved path that swaps merge behavior should not regress this
	// guarantee.
	_ = c.handleQAVerdictMutation(ctx, data)

	if got := fetch(); len(got) != 0 {
		t.Errorf("expected 0 RecoveryRequested publishes on approved verdict, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Drive-by coverage for the two pre-existing trigger sites. Both previously
// had zero assertions on whether publishRecoveryRequested actually fires —
// the only existing tests checked plan-state side effects. These tests close
// that gap. Discovered 2026-05-28 while investigating why qa-rejection had
// no retry path: the wire pattern was correct at two of three sites but
// nothing actually verified it.
// ---------------------------------------------------------------------------

func TestHandleRevisionMutation_EscalationAtCapFiresRecoveryRequested(t *testing.T) {
	ctx := context.Background()
	c := setupRevisionComponent(t, 2) // cap = 2
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	slug := "revision-escalation-fires-recovery"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingDraft
	plan.ReviewIteration = 1 // already at 1, cap is 2
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	req := RevisionMutationRequest{
		Slug:    slug,
		Round:   1,
		Verdict: "needs_changes",
		Summary: "Still too vague",
	}
	data := marshalRevision(t, req)

	resp := c.handleRevisionMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("revision mutation failed: %s", resp.Error)
	}

	got := fetch()
	if len(got) != 1 {
		t.Fatalf("expected 1 RecoveryRequested publish at revision cap, got %d", len(got))
	}
	r := got[0]
	if r.Slug != slug {
		t.Errorf("Slug = %q, want %q", r.Slug, slug)
	}
	if r.Layer != payloads.RecoveryLayerPhaseLocal {
		t.Errorf("Layer = %q, want phase_local", r.Layer)
	}
	if r.EscalationReason == "" {
		t.Error("EscalationReason should be populated from plan.LastError")
	}
}

func TestHandleRevisionMutation_BelowCapDoesNotFireRecoveryRequested(t *testing.T) {
	ctx := context.Background()
	c := setupRevisionComponent(t, 3) // cap = 3, well above iteration
	publisher, fetch := captureRecoveryPublisher()
	c.recoveryPublisher = publisher

	slug := "revision-below-cap-no-recovery"
	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusReviewingDraft
	plan.ReviewIteration = 0
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	req := RevisionMutationRequest{
		Slug:    slug,
		Round:   1,
		Verdict: "needs_changes",
		Summary: "Needs refinement",
	}
	data := marshalRevision(t, req)

	resp := c.handleRevisionMutation(ctx, data)
	if !resp.Success {
		t.Fatalf("revision mutation failed: %s", resp.Error)
	}

	if got := fetch(); len(got) != 0 {
		t.Errorf("expected 0 RecoveryRequested publishes when below revision cap, got %d", len(got))
	}
}
