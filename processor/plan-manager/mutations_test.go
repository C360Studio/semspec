package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// setupRevisionComponent creates a Component with MaxReviewIterations set.
func setupRevisionComponent(t *testing.T, maxIter int) *Component {
	t.Helper()
	c := setupTestComponent(t)
	c.config.MaxReviewIterations = maxIter
	return c
}

// makeFindings creates a JSON-encoded PlanReviewFinding array for test payloads.
func makeFindings(t *testing.T, findings []workflow.PlanReviewFinding) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	return data
}

// marshalRevision marshals a RevisionMutationRequest to JSON bytes.
func marshalRevision(t *testing.T, req RevisionMutationRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal revision request: %v", err)
	}
	return data
}

func TestHandleRevisionMutation(t *testing.T) {
	ctx := context.Background()

	sampleFindings := []workflow.PlanReviewFinding{
		{
			SOPID:      "completeness.goal",
			SOPTitle:   "Goal Clarity",
			Severity:   "error",
			Status:     "violation",
			Issue:      "Goal is too vague",
			Suggestion: "Be more specific about the endpoint behavior",
		},
	}

	tests := []struct {
		name string
		// setup returns the component and prepares the plan
		setup func(t *testing.T) (*Component, string)
		// req builds the revision request
		req RevisionMutationRequest
		// assertions
		wantSuccess     bool
		wantErrorSubstr string
		// post-condition checks (only if wantSuccess)
		checkPlan func(t *testing.T, plan *workflow.Plan)
	}{
		{
			name: "R1 retry under limit resets Approved (gate must be re-crossed)",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "r1-retry")
				plan.Status = workflow.StatusReviewingDraft
				plan.Goal = "Add /goodbye endpoint"
				plan.Context = "Flask API"
				// Plan was previously approved (rollback came from a later
				// stage). Approved is sticky in the absence of the reset.
				plan.Approved = true
				now := time.Now()
				plan.ApprovedAt = &now
				_ = c.plans.save(ctx, plan)
				return c, "r1-retry"
			},
			req: RevisionMutationRequest{
				Slug:     "r1-retry",
				Round:    1,
				Verdict:  "needs_changes",
				Summary:  "Goal is too vague",
				Findings: nil, // set in test body
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusCreated {
					t.Errorf("status = %s, want created", plan.EffectiveStatus())
				}
				if plan.ReviewIteration != 1 {
					t.Errorf("ReviewIteration = %d, want 1", plan.ReviewIteration)
				}
				if plan.Goal != "Add /goodbye endpoint" {
					t.Errorf("Goal was cleared, should be preserved on R1 retry")
				}
				if plan.Context != "Flask API" {
					t.Errorf("Context was cleared, should be preserved on R1 retry")
				}
				if plan.ReviewVerdict != "needs_changes" {
					t.Errorf("ReviewVerdict = %q, want needs_changes", plan.ReviewVerdict)
				}
				if plan.ReviewSummary != "Goal is too vague" {
					t.Errorf("ReviewSummary = %q, want 'Goal is too vague'", plan.ReviewSummary)
				}
				// Rollback to StatusCreated crosses back through the approval
				// gate; without resetting Approved, downstream consumers
				// (UI auto-promote helper) skip re-promotion and the plan
				// stalls at "reviewed" on the next forward pass.
				if plan.Approved {
					t.Errorf("Approved should be reset to false on R1 rollback to StatusCreated, got true")
				}
				if plan.ApprovedAt != nil {
					t.Errorf("ApprovedAt should be nil after R1 rollback, got %v", plan.ApprovedAt)
				}
			},
		},
		{
			name: "R2 retry under limit clears requirements and scenarios; preserves Approved when target is StatusApproved",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "r2-retry")
				plan.Status = workflow.StatusReviewingScenarios
				plan.Requirements = []workflow.Requirement{{ID: "req-1", Title: "Requirement 1"}}
				plan.Scenarios = []workflow.Scenario{{ID: "sc-1", RequirementID: "req-1"}}
				plan.Approved = true
				now := time.Now()
				plan.ApprovedAt = &now
				_ = c.plans.save(ctx, plan)
				return c, "r2-retry"
			},
			req: RevisionMutationRequest{
				Slug:    "r2-retry",
				Round:   2,
				Verdict: "needs_changes",
				Summary: "Missing coverage",
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusApproved {
					t.Errorf("status = %s, want approved", plan.EffectiveStatus())
				}
				if plan.ReviewIteration != 1 {
					t.Errorf("ReviewIteration = %d, want 1", plan.ReviewIteration)
				}
				if len(plan.Requirements) != 0 {
					t.Errorf("Requirements should be cleared on R2 retry, got %d", len(plan.Requirements))
				}
				if len(plan.Scenarios) != 0 {
					t.Errorf("Scenarios should be cleared on R2 retry, got %d", len(plan.Scenarios))
				}
				// Target is StatusApproved (no findings → fallback in
				// determineR2ReentryPoint), which means the plan is still
				// past the approval gate. Approved must be preserved —
				// resetting it here would force unnecessary re-promotion.
				if !plan.Approved {
					t.Errorf("Approved should be preserved on R2 rollback to StatusApproved, got false")
				}
				if plan.ApprovedAt == nil {
					t.Errorf("ApprovedAt should be preserved on R2 rollback to StatusApproved, got nil")
				}
			},
		},
		{
			name: "R1 escalation at cap",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 2)
				plan := setupTestPlan(t, c, "r1-escalate")
				plan.Status = workflow.StatusReviewingDraft
				plan.ReviewIteration = 1 // already at 1, cap is 2
				_ = c.plans.save(ctx, plan)
				return c, "r1-escalate"
			},
			req: RevisionMutationRequest{
				Slug:    "r1-escalate",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "Still too vague",
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusRejected {
					t.Errorf("status = %s, want rejected", plan.EffectiveStatus())
				}
				if plan.ReviewIteration != 2 {
					t.Errorf("ReviewIteration = %d, want 2", plan.ReviewIteration)
				}
				if plan.LastError == "" {
					t.Error("LastError should be set on escalation")
				}
				if plan.LastErrorAt == nil {
					t.Error("LastErrorAt should be set on escalation")
				}
			},
		},
		{
			name: "R2 escalation at cap",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 2)
				plan := setupTestPlan(t, c, "r2-escalate")
				plan.Status = workflow.StatusReviewingScenarios
				plan.ReviewIteration = 1
				_ = c.plans.save(ctx, plan)
				return c, "r2-escalate"
			},
			req: RevisionMutationRequest{
				Slug:    "r2-escalate",
				Round:   2,
				Verdict: "needs_changes",
				Summary: "Coverage still incomplete",
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusRejected {
					t.Errorf("status = %s, want rejected", plan.EffectiveStatus())
				}
				if plan.LastError == "" {
					t.Error("LastError should be set on escalation")
				}
			},
		},
		{
			name: "wrong status for R1",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "wrong-r1")
				plan.Status = workflow.StatusDrafted
				_ = c.plans.save(ctx, plan)
				return c, "wrong-r1"
			},
			req: RevisionMutationRequest{
				Slug:    "wrong-r1",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "test",
			},
			wantSuccess:     false,
			wantErrorSubstr: "requires status reviewing_draft",
		},
		{
			name: "wrong status for R2",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "wrong-r2")
				plan.Status = workflow.StatusApproved
				_ = c.plans.save(ctx, plan)
				return c, "wrong-r2"
			},
			req: RevisionMutationRequest{
				Slug:    "wrong-r2",
				Round:   2,
				Verdict: "needs_changes",
				Summary: "test",
			},
			wantSuccess:     false,
			wantErrorSubstr: "requires status reviewing_scenarios",
		},
		{
			name: "invalid round 0",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				return c, ""
			},
			req: RevisionMutationRequest{
				Slug:    "test",
				Round:   0,
				Verdict: "needs_changes",
			},
			wantSuccess:     false,
			wantErrorSubstr: "round must be 1 or 2",
		},
		{
			name: "invalid round 3",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				return c, ""
			},
			req: RevisionMutationRequest{
				Slug:    "test",
				Round:   3,
				Verdict: "needs_changes",
			},
			wantSuccess:     false,
			wantErrorSubstr: "round must be 1 or 2",
		},
		{
			name: "plan not found",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				return c, "nonexistent"
			},
			req: RevisionMutationRequest{
				Slug:    "nonexistent",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "test",
			},
			wantSuccess:     false,
			wantErrorSubstr: "plan not found",
		},
		{
			name: "empty slug",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				return c, ""
			},
			req: RevisionMutationRequest{
				Slug:    "",
				Round:   1,
				Verdict: "needs_changes",
			},
			wantSuccess:     false,
			wantErrorSubstr: "slug required",
		},
		{
			name: "MaxReviewIterations 0 normalizes to 1 — immediate escalation",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 0) // zero → normalized to 1
				plan := setupTestPlan(t, c, "max-zero")
				plan.Status = workflow.StatusReviewingDraft
				_ = c.plans.save(ctx, plan)
				return c, "max-zero"
			},
			req: RevisionMutationRequest{
				Slug:    "max-zero",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "immediate escalation",
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.EffectiveStatus() != workflow.StatusRejected {
					t.Errorf("status = %s, want rejected (MaxIter=0 → normalized to 1, first attempt escalates)", plan.EffectiveStatus())
				}
			},
		},
		{
			name: "findings formatted correctly",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "findings-fmt")
				plan.Status = workflow.StatusReviewingDraft
				_ = c.plans.save(ctx, plan)
				return c, "findings-fmt"
			},
			req: RevisionMutationRequest{
				Slug:    "findings-fmt",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "Goal lacks specificity",
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.ReviewFormattedFindings == "" {
					t.Error("ReviewFormattedFindings should be populated")
				}
				if !strings.Contains(plan.ReviewFormattedFindings, "Goal is too vague") {
					t.Errorf("formatted findings should contain issue text, got %q", plan.ReviewFormattedFindings)
				}
				if len(plan.ReviewFindings) == 0 {
					t.Error("ReviewFindings (raw JSON) should be stored")
				}
			},
		},
		{
			name: "malformed findings JSON falls back to summary",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "bad-findings")
				plan.Status = workflow.StatusReviewingDraft
				_ = c.plans.save(ctx, plan)
				return c, "bad-findings"
			},
			req: RevisionMutationRequest{
				Slug:    "bad-findings",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "fallback summary",
				// Findings set to a JSON string (valid JSON, but not a findings array)
				Findings: json.RawMessage(`"not an array"`),
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.ReviewFormattedFindings != "fallback summary" {
					t.Errorf("ReviewFormattedFindings = %q, want 'fallback summary'", plan.ReviewFormattedFindings)
				}
			},
		},
		{
			name: "single increment per call",
			setup: func(t *testing.T) (*Component, string) {
				c := setupRevisionComponent(t, 3)
				plan := setupTestPlan(t, c, "single-increment")
				plan.Status = workflow.StatusReviewingDraft
				plan.ReviewIteration = 0
				_ = c.plans.save(ctx, plan)
				return c, "single-increment"
			},
			req: RevisionMutationRequest{
				Slug:    "single-increment",
				Round:   1,
				Verdict: "needs_changes",
				Summary: "test",
			},
			wantSuccess: true,
			checkPlan: func(t *testing.T, plan *workflow.Plan) {
				if plan.ReviewIteration != 1 {
					t.Errorf("ReviewIteration = %d, want exactly 1", plan.ReviewIteration)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := tt.setup(t)

			// Inject sample findings if not set.
			if tt.req.Findings == nil && tt.wantSuccess {
				tt.req.Findings = makeFindings(t, sampleFindings)
			}

			data := marshalRevision(t, tt.req)
			resp := c.handleRevisionMutation(ctx, data)

			if resp.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v (error: %s)", resp.Success, tt.wantSuccess, resp.Error)
			}
			if !tt.wantSuccess && tt.wantErrorSubstr != "" {
				if !strings.Contains(resp.Error, tt.wantErrorSubstr) {
					t.Errorf("Error = %q, want substring %q", resp.Error, tt.wantErrorSubstr)
				}
			}
			if tt.wantSuccess && tt.checkPlan != nil {
				plan, ok := c.plans.get(tt.req.Slug)
				if !ok {
					t.Fatal("plan not found after mutation")
				}
				tt.checkPlan(t, plan)
			}
		})
	}
}
