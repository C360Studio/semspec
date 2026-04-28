//go:build integration

package planmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
)

// newIntegrationComponent creates a Component backed by a real NATS testcontainer
// with PLAN_STATES and ENTITY_STATES KV buckets provisioned.
func newIntegrationComponent(t *testing.T) (*Component, *natsclient.Client) {
	t.Helper()
	tc := natsclient.NewTestClient(t,
		natsclient.WithJetStream(),
		natsclient.WithKVBuckets("PLAN_STATES", "ENTITY_STATES"),
	)

	js, err := tc.Client.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}

	kvBucket, err := js.KeyValue(context.Background(), "PLAN_STATES")
	if err != nil {
		t.Fatalf("KeyValue(PLAN_STATES): %v", err)
	}

	// nil TripleWriter — no graph-ingest responder in test NATS.
	// KV bucket is real; triple writes are skipped.
	ps, err := newPlanStore(context.Background(), kvBucket, nil, slog.Default())
	if err != nil {
		t.Fatalf("newPlanStore: %v", err)
	}

	c := &Component{
		logger:     slog.Default(),
		plans:      ps,
		natsClient: tc.Client,
		config:     Config{MaxReviewIterations: 3},
	}

	return c, tc.Client
}

func TestIntegration_RevisionMutation_R1Retry(t *testing.T) {
	c, natsClient := newIntegrationComponent(t)
	ctx := context.Background()

	// Create a plan and advance it to reviewing_draft.
	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("int-r1"),
		Slug:   "int-r1",
		Title:  "Integration R1 Test",
		Goal:   "Add /hello endpoint",
		Status: workflow.StatusReviewingDraft,
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	// Start the mutation handler.
	handlerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := c.startMutationHandler(handlerCtx); err != nil {
		t.Fatalf("startMutationHandler: %v", err)
	}

	// Send revision request via NATS request/reply.
	findings := []workflow.PlanReviewFinding{{
		SOPID:    "completeness.goal",
		SOPTitle: "Goal Clarity",
		Severity: "error",
		Status:   "violation",
		Category: "completeness",
		Issue:    "Goal is too vague",
	}}
	findingsJSON, _ := json.Marshal(findings)

	revReq, _ := json.Marshal(RevisionMutationRequest{
		Slug:     "int-r1",
		Round:    1,
		Verdict:  "needs_changes",
		Summary:  "Goal needs more specificity",
		Findings: findingsJSON,
	})

	resp, err := natsClient.RequestWithRetry(ctx, "plan.mutation.revision", revReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("revision request: %v", err)
	}

	var mutResp MutationResponse
	if err := json.Unmarshal(resp, &mutResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !mutResp.Success {
		t.Fatalf("revision mutation failed: %s", mutResp.Error)
	}

	// Verify plan state after revision.
	updated, ok := c.plans.get("int-r1")
	if !ok {
		t.Fatal("plan not found after revision")
	}
	if updated.EffectiveStatus() != workflow.StatusCreated {
		t.Errorf("status = %s, want created", updated.EffectiveStatus())
	}
	if updated.ReviewIteration != 1 {
		t.Errorf("ReviewIteration = %d, want 1", updated.ReviewIteration)
	}
	if updated.Goal != "Add /hello endpoint" {
		t.Errorf("Goal should be preserved, got %q", updated.Goal)
	}
}

func TestIntegration_RevisionMutation_R2ClearsReqScenarios(t *testing.T) {
	c, natsClient := newIntegrationComponent(t)
	ctx := context.Background()

	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("int-r2"),
		Slug:   "int-r2",
		Title:  "Integration R2 Test",
		Status: workflow.StatusReviewingScenarios,
		Requirements: []workflow.Requirement{
			{ID: "req-1", Title: "Test Req"},
		},
		Scenarios: []workflow.Scenario{
			{ID: "sc-1", RequirementID: "req-1"},
		},
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	handlerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := c.startMutationHandler(handlerCtx); err != nil {
		t.Fatalf("startMutationHandler: %v", err)
	}

	revReq, _ := json.Marshal(RevisionMutationRequest{
		Slug:     "int-r2",
		Round:    2,
		Verdict:  "needs_changes",
		Summary:  "Missing coverage",
		Findings: json.RawMessage(`[]`),
	})

	resp, err := natsClient.RequestWithRetry(ctx, "plan.mutation.revision", revReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("revision request: %v", err)
	}

	var mutResp MutationResponse
	if err := json.Unmarshal(resp, &mutResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !mutResp.Success {
		t.Fatalf("revision mutation failed: %s", mutResp.Error)
	}

	updated, ok := c.plans.get("int-r2")
	if !ok {
		t.Fatal("plan not found after revision")
	}
	if updated.EffectiveStatus() != workflow.StatusApproved {
		t.Errorf("status = %s, want approved", updated.EffectiveStatus())
	}
	if len(updated.Requirements) != 0 {
		t.Errorf("Requirements should be cleared on R2 retry, got %d", len(updated.Requirements))
	}
	if len(updated.Scenarios) != 0 {
		t.Errorf("Scenarios should be cleared on R2 retry, got %d", len(updated.Scenarios))
	}
}

func TestIntegration_RevisionMutation_EscalationAtCap(t *testing.T) {
	c, natsClient := newIntegrationComponent(t)
	c.config.MaxReviewIterations = 2
	ctx := context.Background()

	plan := &workflow.Plan{
		ID:              workflow.PlanEntityID("int-esc"),
		Slug:            "int-esc",
		Title:           "Escalation Test",
		Status:          workflow.StatusReviewingDraft,
		ReviewIteration: 1, // already at 1, cap is 2
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	handlerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := c.startMutationHandler(handlerCtx); err != nil {
		t.Fatalf("startMutationHandler: %v", err)
	}

	revReq, _ := json.Marshal(RevisionMutationRequest{
		Slug:     "int-esc",
		Round:    1,
		Verdict:  "needs_changes",
		Summary:  "Still too vague",
		Findings: json.RawMessage(`[]`),
	})

	resp, err := natsClient.RequestWithRetry(ctx, "plan.mutation.revision", revReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("revision request: %v", err)
	}

	var mutResp MutationResponse
	if err := json.Unmarshal(resp, &mutResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !mutResp.Success {
		t.Fatalf("revision mutation failed: %s", mutResp.Error)
	}

	updated, ok := c.plans.get("int-esc")
	if !ok {
		t.Fatal("plan not found after revision")
	}
	if updated.EffectiveStatus() != workflow.StatusRejected {
		t.Errorf("status = %s, want rejected (escalation at cap)", updated.EffectiveStatus())
	}
	if updated.LastError == "" {
		t.Error("LastError should be set on escalation")
	}
}

func TestIntegration_RevisionMutation_ConcurrentRejected(t *testing.T) {
	c, natsClient := newIntegrationComponent(t)
	ctx := context.Background()

	plan := &workflow.Plan{
		ID:     workflow.PlanEntityID("int-conc"),
		Slug:   "int-conc",
		Title:  "Concurrent Test",
		Status: workflow.StatusReviewingDraft,
	}
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("save plan: %v", err)
	}

	handlerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := c.startMutationHandler(handlerCtx); err != nil {
		t.Fatalf("startMutationHandler: %v", err)
	}

	revReq, _ := json.Marshal(RevisionMutationRequest{
		Slug:     "int-conc",
		Round:    1,
		Verdict:  "needs_changes",
		Summary:  "First revision",
		Findings: json.RawMessage(`[]`),
	})

	// First request should succeed.
	resp, err := natsClient.RequestWithRetry(ctx, "plan.mutation.revision", revReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("first revision request: %v", err)
	}
	var mutResp1 MutationResponse
	json.Unmarshal(resp, &mutResp1)
	if !mutResp1.Success {
		t.Fatalf("first revision should succeed: %s", mutResp1.Error)
	}

	// Second request should fail — plan is no longer in reviewing_draft.
	resp2, err := natsClient.RequestWithRetry(ctx, "plan.mutation.revision", revReq,
		5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		t.Fatalf("second revision request: %v", err)
	}
	var mutResp2 MutationResponse
	json.Unmarshal(resp2, &mutResp2)
	if mutResp2.Success {
		t.Error("second revision should fail — plan already transitioned out of reviewing_draft")
	}
}
