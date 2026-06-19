package planmanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

// marshalClaim marshals a ClaimMutationRequest to JSON bytes.
func marshalClaim(t *testing.T, req ClaimMutationRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalClaim: %v", err)
	}
	return data
}

// marshalGenerationFailed marshals a GenerationFailedRequest to JSON bytes.
func marshalGenerationFailed(t *testing.T, req GenerationFailedRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalGenerationFailed: %v", err)
	}
	return data
}

// marshalReviewed marshals a ReviewedMutationRequest to JSON bytes.
func marshalReviewed(t *testing.T, req ReviewedMutationRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalReviewed: %v", err)
	}
	return data
}

// marshalApproved marshals an ApprovedMutationRequest to JSON bytes.
func marshalApproved(t *testing.T, req ApprovedMutationRequest) []byte {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalApproved: %v", err)
	}
	return data
}

// TestHandleClaimMutation_LegalEdges verifies the happy-path claim transitions:
// each in-progress target status is reachable exactly once from the correct
// source, and the plan persists the new status on success.
func TestHandleClaimMutation_LegalEdges(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		slug         string // explicit slug — must be lowercase alphanumeric+hyphens only
		sourceStatus workflow.Status
		target       workflow.Status
	}{
		// analyst sub-phase
		{"created->exploring", "claim-created-exploring", workflow.StatusCreated, workflow.StatusExploring},
		// planner sub-phase
		{"created->drafting", "claim-created-drafting", workflow.StatusCreated, workflow.StatusDrafting},
		{"explored->drafting", "claim-explored-drafting", workflow.StatusExplored, workflow.StatusDrafting},
		// reviewer round 1
		{"drafted->reviewing_draft", "claim-drafted-reviewing", workflow.StatusDrafted, workflow.StatusReviewingDraft},
		// requirement generator
		{"approved->generating_requirements", "claim-approved-genreqs", workflow.StatusApproved, workflow.StatusGeneratingRequirements},
		// architecture generator
		{"requirements_generated->generating_architecture", "claim-reqs-genarch", workflow.StatusRequirementsGenerated, workflow.StatusGeneratingArchitecture},
		// story preparer (Sarah)
		{"architecture_generated->preparing_stories", "claim-arch-stories", workflow.StatusArchitectureGenerated, workflow.StatusPreparingStories},
		// scenario generator (Bob) — claims from stories_generated
		{"stories_generated->generating_scenarios", "claim-stories-genscen", workflow.StatusStoriesGenerated, workflow.StatusGeneratingScenarios},
		// reviewer round 2
		{"scenarios_generated->reviewing_scenarios", "claim-scen-reviewing", workflow.StatusScenariosGenerated, workflow.StatusReviewingScenarios},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupTestComponent(t)
			plan := setupTestPlan(t, c, tt.slug)
			plan.Status = tt.sourceStatus
			if err := c.plans.save(ctx, plan); err != nil {
				t.Fatalf("seed plan: %v", err)
			}

			req := ClaimMutationRequest{
				Slug:   tt.slug,
				Status: tt.target,
			}
			resp := c.handleClaimMutation(ctx, marshalClaim(t, req))
			if !resp.Success {
				t.Fatalf("handleClaimMutation returned Success=false: %s", resp.Error)
			}

			got, ok := c.plans.get(tt.slug)
			if !ok {
				t.Fatal("plan not found after claim")
			}
			if got.Status != tt.target {
				t.Errorf("plan.Status = %q, want %q", got.Status, tt.target)
			}
		})
	}
}

// TestHandleClaimMutation_Guards verifies rejection cases: non-in-progress
// target, double-claim (target == current status), plan not found, and empty
// fields.
func TestHandleClaimMutation_Guards(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		setup           func(t *testing.T, c *Component) string // returns slug
		target          workflow.Status
		wantErrorSubstr string
	}{
		{
			name: "non-in-progress target rejected",
			setup: func(t *testing.T, c *Component) string {
				plan := setupTestPlan(t, c, "guard-non-inprogress")
				plan.Status = workflow.StatusCreated
				_ = c.plans.save(ctx, plan)
				return plan.Slug
			},
			// StatusDrafted is not in-progress — handler must reject it.
			target:          workflow.StatusDrafted,
			wantErrorSubstr: "in-progress",
		},
		{
			name: "double-claim same status rejected (invalid transition)",
			setup: func(t *testing.T, c *Component) string {
				plan := setupTestPlan(t, c, "guard-double-claim")
				plan.Status = workflow.StatusDrafting
				_ = c.plans.save(ctx, plan)
				return plan.Slug
			},
			// drafting → drafting is an invalid transition per CanTransitionTo.
			target:          workflow.StatusDrafting,
			wantErrorSubstr: "invalid transition",
		},
		{
			name: "plan not found returns error",
			setup: func(t *testing.T, c *Component) string {
				return "nonexistent-plan"
			},
			target:          workflow.StatusDrafting,
			wantErrorSubstr: "plan not found",
		},
		{
			name: "empty slug rejected",
			setup: func(t *testing.T, c *Component) string {
				return ""
			},
			target:          workflow.StatusDrafting,
			wantErrorSubstr: "slug and status required",
		},
		{
			name: "empty target status rejected",
			setup: func(t *testing.T, c *Component) string {
				plan := setupTestPlan(t, c, "guard-empty-status")
				return plan.Slug
			},
			target:          "",
			wantErrorSubstr: "slug and status required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupTestComponent(t)
			slug := tt.setup(t, c)

			req := ClaimMutationRequest{Slug: slug, Status: tt.target}
			resp := c.handleClaimMutation(ctx, marshalClaim(t, req))

			if resp.Success {
				t.Fatalf("expected Success=false, got Success=true")
			}
			if !strings.Contains(resp.Error, tt.wantErrorSubstr) {
				t.Errorf("error %q does not contain %q", resp.Error, tt.wantErrorSubstr)
			}
		})
	}
}

// TestHandleGenerationFailedMutation drives the handler from each generation
// source status that can transition to rejected, asserting that the plan is
// marked rejected with LastError and LastErrorAt populated.
func TestHandleGenerationFailedMutation(t *testing.T) {
	ctx := context.Background()

	// All in-progress generation phases that emit →rejected on failure.
	tests := []struct {
		name         string
		sourceStatus workflow.Status
		phase        string
	}{
		{"from generating_requirements", workflow.StatusGeneratingRequirements, "requirements"},
		{"from generating_architecture", workflow.StatusGeneratingArchitecture, "architecture"},
		{"from preparing_stories", workflow.StatusPreparingStories, "stories"},
		{"from generating_scenarios", workflow.StatusGeneratingScenarios, "scenarios"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupTestComponent(t)
			plan := setupTestPlan(t, c, "genfail-"+tt.phase)
			plan.Status = tt.sourceStatus
			if err := c.plans.save(ctx, plan); err != nil {
				t.Fatalf("seed plan: %v", err)
			}

			req := GenerationFailedRequest{
				Slug:  plan.Slug,
				Phase: tt.phase,
				Error: "upstream LLM returned 503",
			}
			resp := c.handleGenerationFailedMutation(ctx, marshalGenerationFailed(t, req))
			if !resp.Success {
				t.Fatalf("handleGenerationFailedMutation failed: %s", resp.Error)
			}

			got, ok := c.plans.get(plan.Slug)
			if !ok {
				t.Fatal("plan not found after mutation")
			}
			if got.Status != workflow.StatusRejected {
				t.Errorf("plan.Status = %q, want %q", got.Status, workflow.StatusRejected)
			}
			if got.LastError == "" {
				t.Error("LastError should be populated after generation failure")
			}
			if !strings.Contains(got.LastError, "upstream LLM") {
				t.Errorf("LastError = %q, want it to contain the request error string", got.LastError)
			}
			if got.LastErrorAt == nil {
				t.Error("LastErrorAt should be set after generation failure")
			}
		})
	}
}

// TestHandleGenerationFailedMutation_InvalidTransition confirms that the
// handler respects the CanTransitionTo(rejected) gate: a plan whose current
// status cannot reach rejected is refused.
func TestHandleGenerationFailedMutation_InvalidTransition(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	// StatusComplete cannot transition to rejected.
	plan := setupTestPlan(t, c, "genfail-invalid")
	plan.Status = workflow.StatusComplete
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	req := GenerationFailedRequest{
		Slug:  plan.Slug,
		Phase: "requirements",
		Error: "should be blocked",
	}
	resp := c.handleGenerationFailedMutation(ctx, marshalGenerationFailed(t, req))
	if resp.Success {
		t.Fatal("expected Success=false for invalid transition, got Success=true")
	}
	if !strings.Contains(resp.Error, "invalid transition") {
		t.Errorf("error %q should mention invalid transition", resp.Error)
	}

	// Plan status must be unchanged.
	got, ok := c.plans.get(plan.Slug)
	if !ok {
		t.Fatal("plan disappeared")
	}
	if got.Status != workflow.StatusComplete {
		t.Errorf("plan.Status = %q after rejected guard; want status unchanged (%q)", got.Status, workflow.StatusComplete)
	}
}

// TestHandleReviewedMutation verifies that a plan at reviewing_draft
// transitions to reviewed with verdict, summary, and ReviewedAt populated.
func TestHandleReviewedMutation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		slug         string // explicit slug — must be lowercase alphanumeric+hyphens only
		sourceStatus workflow.Status
		verdict      string
		summary      string
	}{
		{
			name:         "reviewing_draft->reviewed with approved verdict",
			slug:         "reviewed-from-draft",
			sourceStatus: workflow.StatusReviewingDraft,
			verdict:      "approved",
			summary:      "Looks good",
		},
		{
			name:         "reviewing_scenarios->reviewed with approved verdict",
			slug:         "reviewed-from-scenarios",
			sourceStatus: workflow.StatusReviewingScenarios,
			verdict:      "approved",
			summary:      "All scenarios covered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupTestComponent(t)
			plan := setupTestPlan(t, c, tt.slug)
			plan.Status = tt.sourceStatus
			if err := c.plans.save(ctx, plan); err != nil {
				t.Fatalf("seed plan: %v", err)
			}

			req := ReviewedMutationRequest{
				Slug:    tt.slug,
				Verdict: tt.verdict,
				Summary: tt.summary,
			}
			resp := c.handleReviewedMutation(ctx, marshalReviewed(t, req))
			if !resp.Success {
				t.Fatalf("handleReviewedMutation failed: %s", resp.Error)
			}

			got, ok := c.plans.get(tt.slug)
			if !ok {
				t.Fatal("plan not found after mutation")
			}
			if got.Status != workflow.StatusReviewed {
				t.Errorf("plan.Status = %q, want %q", got.Status, workflow.StatusReviewed)
			}
			if got.ReviewVerdict != tt.verdict {
				t.Errorf("ReviewVerdict = %q, want %q", got.ReviewVerdict, tt.verdict)
			}
			if got.ReviewSummary != tt.summary {
				t.Errorf("ReviewSummary = %q, want %q", got.ReviewSummary, tt.summary)
			}
			if got.ReviewedAt == nil {
				t.Error("ReviewedAt should be set after reviewed mutation")
			}
		})
	}
}

// TestHandleReviewedMutation_Guards verifies that the handler rejects invalid
// source states and missing input.
func TestHandleReviewedMutation_Guards(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		sourceStatus    workflow.Status
		slug            string
		seedPlan        bool
		wantErrorSubstr string
	}{
		{
			// StatusCreated cannot transition to StatusReviewed — it can only
			// go to exploring/drafting/drafted/rejected.
			name:            "wrong source status rejected",
			sourceStatus:    workflow.StatusCreated,
			slug:            "reviewed-wrong-src",
			seedPlan:        true,
			wantErrorSubstr: "invalid transition",
		},
		{
			name:            "plan not found",
			sourceStatus:    workflow.StatusReviewingDraft, // irrelevant — plan doesn't exist
			slug:            "nonexistent-reviewed",
			seedPlan:        false,
			wantErrorSubstr: "plan not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupTestComponent(t)
			if tt.seedPlan {
				plan := setupTestPlan(t, c, tt.slug)
				plan.Status = tt.sourceStatus
				_ = c.plans.save(ctx, plan)
			}

			req := ReviewedMutationRequest{
				Slug:    tt.slug,
				Verdict: "approved",
				Summary: "test",
			}
			resp := c.handleReviewedMutation(ctx, marshalReviewed(t, req))
			if resp.Success {
				t.Fatalf("expected Success=false, got Success=true")
			}
			if !strings.Contains(resp.Error, tt.wantErrorSubstr) {
				t.Errorf("error %q does not contain %q", resp.Error, tt.wantErrorSubstr)
			}
		})
	}
}

// TestHandleApprovedMutation verifies the auto-approve path: reviewed →
// approved with Approved=true and ApprovedAt set.
func TestHandleApprovedMutation(t *testing.T) {
	ctx := context.Background()
	c := setupTestComponent(t)

	plan := setupTestPlan(t, c, "approved-happy")
	plan.Status = workflow.StatusReviewed
	if err := c.plans.save(ctx, plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	req := ApprovedMutationRequest{Slug: plan.Slug}
	resp := c.handleApprovedMutation(ctx, marshalApproved(t, req))
	if !resp.Success {
		t.Fatalf("handleApprovedMutation failed: %s", resp.Error)
	}

	got, ok := c.plans.get(plan.Slug)
	if !ok {
		t.Fatal("plan not found after mutation")
	}
	if got.Status != workflow.StatusApproved {
		t.Errorf("plan.Status = %q, want %q", got.Status, workflow.StatusApproved)
	}
	if !got.Approved {
		t.Error("Approved should be true after approved mutation")
	}
	if got.ApprovedAt == nil {
		t.Error("ApprovedAt should be set after approved mutation")
	}
}

// TestHandleApprovedMutation_Guards confirms that the handler enforces the
// reviewed → approved gate: attempting to approve from an invalid source
// status fails.
func TestHandleApprovedMutation_Guards(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		sourceStatus    workflow.Status
		slug            string
		wantErrorSubstr string
	}{
		{
			name:            "approved from drafting (not reviewed) rejected",
			sourceStatus:    workflow.StatusDrafting,
			slug:            "approved-guard-drafting",
			wantErrorSubstr: "invalid transition",
		},
		{
			name:            "approved from drafted (not reviewed) rejected",
			sourceStatus:    workflow.StatusDrafted,
			slug:            "approved-guard-drafted",
			wantErrorSubstr: "invalid transition",
		},
		{
			name:            "empty slug rejected",
			sourceStatus:    workflow.StatusReviewed,
			slug:            "",
			wantErrorSubstr: "slug required",
		},
		{
			name:            "plan not found",
			sourceStatus:    workflow.StatusReviewed,
			slug:            "nonexistent-approved",
			wantErrorSubstr: "plan not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupTestComponent(t)
			if tt.slug != "" && tt.name != "plan not found" {
				plan := setupTestPlan(t, c, tt.slug)
				plan.Status = tt.sourceStatus
				_ = c.plans.save(ctx, plan)
			}

			req := ApprovedMutationRequest{Slug: tt.slug}
			resp := c.handleApprovedMutation(ctx, marshalApproved(t, req))
			if resp.Success {
				t.Fatalf("expected Success=false, got Success=true")
			}
			if !strings.Contains(resp.Error, tt.wantErrorSubstr) {
				t.Errorf("error %q does not contain %q", resp.Error, tt.wantErrorSubstr)
			}
		})
	}
}
