//go:build integration

package planmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestExtractSlugPlanDecisionAndAction(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantSlug       string
		wantProposalID string
		wantAction     string
	}{
		{
			name:           "get proposal",
			path:           "/plan-manager/plans/my-feature/plan-decisions/plan-decision.my-feature.1",
			wantSlug:       "my-feature",
			wantProposalID: "plan-decision.my-feature.1",
			wantAction:     "",
		},
		{
			name:           "accept proposal",
			path:           "/plan-manager/plans/my-feature/plan-decisions/plan-decision.my-feature.1/accept",
			wantSlug:       "my-feature",
			wantProposalID: "plan-decision.my-feature.1",
			wantAction:     "accept",
		},
		{
			name:           "reject proposal",
			path:           "/plan-manager/plans/my-feature/plan-decisions/plan-decision.my-feature.1/reject",
			wantSlug:       "my-feature",
			wantProposalID: "plan-decision.my-feature.1",
			wantAction:     "reject",
		},
		{
			name:           "submit proposal",
			path:           "/plan-manager/plans/my-feature/plan-decisions/plan-decision.my-feature.1/submit",
			wantSlug:       "my-feature",
			wantProposalID: "plan-decision.my-feature.1",
			wantAction:     "submit",
		},
		{
			name:           "invalid - missing segment",
			path:           "/plan-manager/plans/test-slug/other/plan-decision.test.1",
			wantSlug:       "",
			wantProposalID: "",
			wantAction:     "",
		},
		{
			name:           "invalid - insufficient parts",
			path:           "/plan-manager/plans/test-slug/plan-decisions",
			wantSlug:       "",
			wantProposalID: "",
			wantAction:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotProposalID, gotAction := extractSlugPlanDecisionAndAction(tt.path)
			if gotSlug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", gotSlug, tt.wantSlug)
			}
			if gotProposalID != tt.wantProposalID {
				t.Errorf("proposalID = %q, want %q", gotProposalID, tt.wantProposalID)
			}
			if gotAction != tt.wantAction {
				t.Errorf("action = %q, want %q", gotAction, tt.wantAction)
			}
		})
	}
}

func TestHandleListPlanDecisions(t *testing.T) {
	slug := "cp-list-plan"
	proposals := []workflow.PlanDecision{
		{
			ID: "plan-decision.cp-list-plan.1", PlanID: "plan.cp-list-plan",
			Title: "First proposal", Status: workflow.PlanDecisionStatusProposed, ProposedBy: "user",
		},
		{
			ID: "plan-decision.cp-list-plan.2", PlanID: "plan.cp-list-plan",
			Title: "Second proposal", Status: workflow.PlanDecisionStatusAccepted, ProposedBy: "agent",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.PlanDecisions = proposals
	_ = c.plans.save(context.Background(), plan)

	t.Run("list all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/plan-manager/plans/"+slug+"/plan-decisions", nil)
		w := httptest.NewRecorder()

		c.handleListPlanDecisions(w, req, slug)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var got []workflow.PlanDecision
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("len(proposals) = %d, want 2", len(got))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/plan-manager/plans/"+slug+"/plan-decisions?status=proposed", nil)
		w := httptest.NewRecorder()

		c.handleListPlanDecisions(w, req, slug)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var got []workflow.PlanDecision
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("len(filtered proposals) = %d, want 1", len(got))
		}
	})
}

func TestHandleCreatePlanDecision(t *testing.T) {
	slug := "cp-create-plan"

	// Seed a requirement so AffectedReqIDs validation passes.
	seedReq := workflow.Requirement{
		ID:     "requirement.cp-create-plan.1",
		PlanID: workflow.PlanEntityID(slug),
		Title:  "Auth requirement",
		Status: workflow.RequirementStatusActive,
	}

	c := setupTestComponent(t)
	setupTestPlanWith(t, c, slug, []workflow.Requirement{seedReq}, nil)

	body, _ := json.Marshal(CreatePlanDecisionHTTPRequest{
		Title:          "Expand scope of auth requirement",
		Rationale:      "OAuth login was missed in original scope",
		AffectedReqIDs: []string{"requirement.cp-create-plan.1"},
	})

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/plan-decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreatePlanDecision(w, req, slug)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var got workflow.PlanDecision
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Title != "Expand scope of auth requirement" {
		t.Errorf("Title = %q, want %q", got.Title, "Expand scope of auth requirement")
	}
	if got.Status != workflow.PlanDecisionStatusProposed {
		t.Errorf("Status = %q, want %q", got.Status, workflow.PlanDecisionStatusProposed)
	}
	if got.ProposedBy != "user" {
		t.Errorf("ProposedBy = %q, want %q (default)", got.ProposedBy, "user")
	}
	if len(got.AffectedReqIDs) != 1 {
		t.Errorf("len(AffectedReqIDs) = %d, want 1", len(got.AffectedReqIDs))
	}
}

func TestHandleCreatePlanDecision_MissingTitle(t *testing.T) {
	slug := "cp-missing-title"

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	body, _ := json.Marshal(CreatePlanDecisionHTTPRequest{Rationale: "no title"})

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/plan-decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreatePlanDecision(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleAcceptPlanDecision(t *testing.T) {
	slug := "cp-accept-plan"
	proposalID := "plan-decision.cp-accept-plan.1"
	proposals := []workflow.PlanDecision{
		{
			ID: proposalID, PlanID: "plan.cp-accept-plan",
			Title: "Add OAuth", Status: workflow.PlanDecisionStatusUnderReview, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.PlanDecisions = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/plan-decisions/"+proposalID+"/accept", nil)
	w := httptest.NewRecorder()

	c.handleAcceptPlanDecision(w, req, slug, proposalID)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusAccepted, w.Body.String())
	}

	var resp AcceptPlanDecisionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Proposal.Status != workflow.PlanDecisionStatusAccepted {
		t.Errorf("Status = %q, want %q", resp.Proposal.Status, workflow.PlanDecisionStatusAccepted)
	}
	if resp.Proposal.DecidedAt == nil {
		t.Error("DecidedAt should be set after acceptance")
	}
}

func TestHandleRejectPlanDecision(t *testing.T) {
	slug := "cp-reject-plan"
	proposalID := "plan-decision.cp-reject-plan.1"
	proposals := []workflow.PlanDecision{
		{
			ID: proposalID, PlanID: "plan.cp-reject-plan",
			Title: "Risky change", Status: workflow.PlanDecisionStatusUnderReview, ProposedBy: "agent",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.PlanDecisions = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/plan-decisions/"+proposalID+"/reject", nil)
	w := httptest.NewRecorder()

	c.handleRejectPlanDecision(w, req, slug, proposalID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.PlanDecision
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Status != workflow.PlanDecisionStatusRejected {
		t.Errorf("Status = %q, want %q", got.Status, workflow.PlanDecisionStatusRejected)
	}
	if got.DecidedAt == nil {
		t.Error("DecidedAt should be set after rejection")
	}
}

func TestHandleAcceptPlanDecision_InvalidTransition(t *testing.T) {
	slug := "cp-invalid-transition"

	// A proposal already accepted cannot be accepted again.
	proposalID := "plan-decision.cp-invalid-transition.1"
	proposals := []workflow.PlanDecision{
		{
			ID: proposalID, PlanID: "plan.cp-invalid-transition",
			Title: "Already accepted", Status: workflow.PlanDecisionStatusAccepted, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.PlanDecisions = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/plan-decisions/"+proposalID+"/accept", nil)
	w := httptest.NewRecorder()

	c.handleAcceptPlanDecision(w, req, slug, proposalID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleSubmitPlanDecision(t *testing.T) {
	slug := "cp-submit-plan"
	proposalID := "plan-decision.cp-submit-plan.1"
	proposals := []workflow.PlanDecision{
		{
			ID: proposalID, PlanID: "plan.cp-submit-plan",
			Title: "Pending proposal", Status: workflow.PlanDecisionStatusProposed, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.PlanDecisions = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/plan-decisions/"+proposalID+"/submit", nil)
	w := httptest.NewRecorder()

	c.handleSubmitPlanDecision(w, req, slug, proposalID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.PlanDecision
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Status != workflow.PlanDecisionStatusUnderReview {
		t.Errorf("Status = %q, want %q", got.Status, workflow.PlanDecisionStatusUnderReview)
	}
}

func TestHandleDeletePlanDecision_NotProposed(t *testing.T) {
	slug := "cp-delete-guard"
	proposalID := "plan-decision.cp-delete-guard.1"
	proposals := []workflow.PlanDecision{
		{
			ID: proposalID, PlanID: "plan.cp-delete-guard",
			Title: "Accepted proposal", Status: workflow.PlanDecisionStatusAccepted, ProposedBy: "user",
		},
	}

	c := setupTestComponent(t)
	plan := setupTestPlanWith(t, c, slug, nil, nil)
	plan.PlanDecisions = proposals
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodDelete, "/plan-manager/plans/"+slug+"/plan-decisions/"+proposalID, nil)
	w := httptest.NewRecorder()

	c.handleDeletePlanDecision(w, req, slug, proposalID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}
