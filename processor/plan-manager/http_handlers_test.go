//go:build integration

package planmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// ---------------------------------------------------------------------------
// Plan handlers
// ---------------------------------------------------------------------------

func TestHandleGetPlan(t *testing.T) {
	slug := "get-plan-exists"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug, nil)
	w := httptest.NewRecorder()

	c.handleGetPlan(w, req, slug)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got PlanWithStatus
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Plan == nil {
		t.Fatal("Plan is nil in response")
	}
	if got.Plan.Slug != slug {
		t.Errorf("Slug = %q, want %q", got.Plan.Slug, slug)
	}
	if got.Stage == "" {
		t.Error("Stage should not be empty")
	}
}

func TestHandleGetPlan_NotFound(t *testing.T) {
	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/nonexistent-plan", nil)
	w := httptest.NewRecorder()

	c.handleGetPlan(w, req, "nonexistent-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleListPlans(t *testing.T) {
	c := setupTestComponent(t)
	for _, slug := range []string{"list-plan-one", "list-plan-two"} {
		setupTestPlan(t, c, slug)
	}

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans", nil)
	w := httptest.NewRecorder()

	c.handleListPlans(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got []*PlanWithStatus
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("len(plans) = %d, want 2", len(got))
	}
}

func TestHandleListPlans_Empty(t *testing.T) {
	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans", nil)
	w := httptest.NewRecorder()

	c.handleListPlans(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got []*PlanWithStatus
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("len(plans) = %d, want 0", len(got))
	}
}

func TestHandleUpdatePlan_NotFound(t *testing.T) {
	c := setupTestComponent(t)

	newTitle := "Updated Title"
	body, _ := json.Marshal(UpdatePlanHTTPRequest{Title: &newTitle})
	req := httptest.NewRequest(http.MethodPatch, "/plan-api/plans/no-such-plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdatePlan(w, req, "no-such-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Promote tests moved to http_promote_test.go (no build tag — runs as unit tests).

// ---------------------------------------------------------------------------
// Task collection handlers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Change proposal handlers (previously untested)
// ---------------------------------------------------------------------------

func TestHandleGetChangeProposal(t *testing.T) {
	slug := "cp-get-plan"
	proposalID := "change-proposal.cp-get-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-get-plan",
			Title: "Add feature X", Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.ChangeProposals = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID, nil)
	w := httptest.NewRecorder()

	c.handleGetChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.ChangeProposal
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ID != proposalID {
		t.Errorf("ID = %q, want %q", got.ID, proposalID)
	}
	if got.Title != "Add feature X" {
		t.Errorf("Title = %q, want %q", got.Title, "Add feature X")
	}
}

func TestHandleGetChangeProposal_NotFound(t *testing.T) {
	slug := "cp-get-notfound"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug+"/change-proposals/nonexistent", nil)
	w := httptest.NewRecorder()

	c.handleGetChangeProposal(w, req, slug, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleUpdateChangeProposal(t *testing.T) {
	slug := "cp-update-plan"
	proposalID := "change-proposal.cp-update-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-update-plan",
			Title: "Original title", Rationale: "Original rationale",
			Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.ChangeProposals = proposals
	_ = c.plans.save(context.Background(), plan)

	newTitle := "Updated title"
	newRationale := "Updated rationale"
	body, _ := json.Marshal(UpdateChangeProposalHTTPRequest{
		Title:     &newTitle,
		Rationale: &newRationale,
	})

	req := httptest.NewRequest(http.MethodPatch, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.ChangeProposal
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Title != newTitle {
		t.Errorf("Title = %q, want %q", got.Title, newTitle)
	}
	if got.Rationale != newRationale {
		t.Errorf("Rationale = %q, want %q", got.Rationale, newRationale)
	}
}

func TestHandleUpdateChangeProposal_InvalidStatus(t *testing.T) {
	slug := "cp-update-invalid-status"
	proposalID := "change-proposal.cp-update-invalid-status.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-update-invalid-status",
			Title: "Accepted proposal", Status: workflow.ChangeProposalStatusAccepted, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.ChangeProposals = proposals
	_ = c.plans.save(context.Background(), plan)

	newTitle := "Try to change accepted"
	body, _ := json.Marshal(UpdateChangeProposalHTTPRequest{Title: &newTitle})

	req := httptest.NewRequest(http.MethodPatch, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleUpdateChangeProposal_NotFound(t *testing.T) {
	slug := "cp-update-notfound"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	newTitle := "Nope"
	body, _ := json.Marshal(UpdateChangeProposalHTTPRequest{Title: &newTitle})

	req := httptest.NewRequest(http.MethodPatch, "/plan-api/plans/"+slug+"/change-proposals/nonexistent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateChangeProposal(w, req, slug, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteChangeProposal_Success(t *testing.T) {
	slug := "cp-delete-success"
	proposalID := "change-proposal.cp-delete-success.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-delete-success",
			Title: "To delete", Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.ChangeProposals = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodDelete, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID, nil)
	w := httptest.NewRecorder()

	c.handleDeleteChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Verify the proposal was removed from the in-memory store.
	stored, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan not found in store after delete")
	}
	if len(stored.ChangeProposals) != 0 {
		t.Errorf("expected 0 proposals after delete, got %d", len(stored.ChangeProposals))
	}
}

func TestHandleCreateChangeProposal_InvalidRequirementID(t *testing.T) {
	slug := "cp-bad-req-id"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	// Reference a requirement ID that does not exist in this plan.
	body, _ := json.Marshal(CreateChangeProposalHTTPRequest{
		Title:          "Change with missing req",
		Rationale:      "Testing validation",
		AffectedReqIDs: []string{"requirement.cp-bad-req-id.999"},
	})

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateChangeProposal(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Scenario GET handler
// ---------------------------------------------------------------------------

func TestHandleGetScenario(t *testing.T) {
	slug := "get-scenario-plan"

	now := time.Now()
	scenarioID := "scenario.get-scenario-plan.1"
	scenarios := []workflow.Scenario{
		{
			ID:            scenarioID,
			RequirementID: "requirement.get-scenario-plan.1",
			Given:         "a user exists",
			When:          "they log in",
			Then:          []string{"they see the dashboard"},
			Status:        workflow.ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	c := setupTestComponent(t)
	setupTestPlanWith(t, c, slug, nil, scenarios)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug+"/scenarios/"+scenarioID, nil)
	w := httptest.NewRecorder()

	c.handleGetScenario(w, req, slug, scenarioID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.Scenario
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ID != scenarioID {
		t.Errorf("ID = %q, want %q", got.ID, scenarioID)
	}
}

func TestHandleGetScenario_NotFound(t *testing.T) {
	slug := "get-scenario-notfound"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug+"/scenarios/nonexistent", nil)
	w := httptest.NewRecorder()

	c.handleGetScenario(w, req, slug, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// determinePlanStage coverage
// ---------------------------------------------------------------------------

func TestDeterminePlanStage(t *testing.T) {
	c := &Component{}

	tests := []struct {
		name      string
		plan      *workflow.Plan
		wantStage string
	}{
		// --- zero-value / legacy field paths ---
		{
			name:      "zero value defaults to drafting",
			plan:      &workflow.Plan{},
			wantStage: "drafting",
		},
		{
			// EffectiveStatus() returns StatusApproved when Approved==true and Status is empty.
			name:      "legacy Approved field maps to approved",
			plan:      &workflow.Plan{Approved: true},
			wantStage: "approved",
		},

		// --- explicit Status field: drafting cluster ---
		{
			name:      "StatusCreated",
			plan:      &workflow.Plan{Status: workflow.StatusCreated},
			wantStage: "drafting",
		},
		{
			name:      "StatusDrafting",
			plan:      &workflow.Plan{Status: workflow.StatusDrafting},
			wantStage: "drafting",
		},

		// --- explicit Status field: ready_for_approval cluster ---
		{
			name:      "StatusDrafted",
			plan:      &workflow.Plan{Status: workflow.StatusDrafted},
			wantStage: "ready_for_approval",
		},
		{
			name:      "StatusReviewingDraft",
			plan:      &workflow.Plan{Status: workflow.StatusReviewingDraft},
			wantStage: "ready_for_approval",
		},

		// --- StatusReviewed branching: verdict drives the stage ---
		{
			name:      "StatusReviewed without needs_changes verdict",
			plan:      &workflow.Plan{Status: workflow.StatusReviewed},
			wantStage: "reviewed",
		},
		{
			name:      "StatusReviewed with approved verdict",
			plan:      &workflow.Plan{Status: workflow.StatusReviewed, ReviewVerdict: "approved"},
			wantStage: "reviewed",
		},
		{
			name:      "StatusReviewed with needs_changes verdict",
			plan:      &workflow.Plan{Status: workflow.StatusReviewed, ReviewVerdict: "needs_changes"},
			wantStage: "needs_changes",
		},

		// --- post-approval pipeline ---
		{
			name:      "StatusApproved",
			plan:      &workflow.Plan{Status: workflow.StatusApproved},
			wantStage: "approved",
		},
		{
			name:      "StatusGeneratingRequirements",
			plan:      &workflow.Plan{Status: workflow.StatusGeneratingRequirements},
			wantStage: "generating_requirements",
		},
		{
			name:      "StatusRequirementsGenerated",
			plan:      &workflow.Plan{Status: workflow.StatusRequirementsGenerated},
			wantStage: "requirements_generated",
		},
		{
			name:      "StatusGeneratingArchitecture",
			plan:      &workflow.Plan{Status: workflow.StatusGeneratingArchitecture},
			wantStage: "generating_architecture",
		},
		{
			name:      "StatusArchitectureGenerated",
			plan:      &workflow.Plan{Status: workflow.StatusArchitectureGenerated},
			wantStage: "architecture_generated",
		},
		{
			name:      "StatusGeneratingScenarios",
			plan:      &workflow.Plan{Status: workflow.StatusGeneratingScenarios},
			wantStage: "generating_scenarios",
		},
		{
			name:      "StatusReviewingScenarios",
			plan:      &workflow.Plan{Status: workflow.StatusReviewingScenarios},
			wantStage: "reviewing_scenarios",
		},
		{
			name:      "StatusScenariosGenerated",
			plan:      &workflow.Plan{Status: workflow.StatusScenariosGenerated},
			wantStage: "scenarios_generated",
		},
		{
			name:      "StatusScenariosReviewed",
			plan:      &workflow.Plan{Status: workflow.StatusScenariosReviewed},
			wantStage: "scenarios_reviewed",
		},

		// --- execution pipeline ---
		{
			name:      "StatusReadyForExecution",
			plan:      &workflow.Plan{Status: workflow.StatusReadyForExecution},
			wantStage: "ready_for_execution",
		},
		{
			name:      "StatusImplementing",
			plan:      &workflow.Plan{Status: workflow.StatusImplementing},
			wantStage: "implementing",
		},
		{
			name:      "StatusReviewingRollup",
			plan:      &workflow.Plan{Status: workflow.StatusReviewingRollup},
			wantStage: "reviewing_rollup",
		},
		{
			name:      "StatusAwaitingReview",
			plan:      &workflow.Plan{Status: workflow.StatusAwaitingReview},
			wantStage: "awaiting_review",
		},

		// --- terminal statuses ---
		{
			name:      "StatusComplete",
			plan:      &workflow.Plan{Status: workflow.StatusComplete},
			wantStage: "complete",
		},
		{
			name:      "StatusChanged",
			plan:      &workflow.Plan{Status: workflow.StatusChanged},
			wantStage: "changed",
		},
		{
			name:      "StatusRejected",
			plan:      &workflow.Plan{Status: workflow.StatusRejected},
			wantStage: "rejected",
		},
		{
			name:      "StatusArchived",
			plan:      &workflow.Plan{Status: workflow.StatusArchived},
			wantStage: "archived",
		},

		// --- default case: unknown/future status falls back to drafting ---
		{
			name:      "unknown status defaults to drafting",
			plan:      &workflow.Plan{Status: workflow.Status("unknown_future_status")},
			wantStage: "drafting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.determinePlanStage(tt.plan)
			if got != tt.wantStage {
				t.Errorf("determinePlanStage() = %q, want %q", got, tt.wantStage)
			}
		})
	}
}
