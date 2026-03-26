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

func TestExtractSlugChangeProposalAndAction(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantSlug       string
		wantProposalID string
		wantAction     string
	}{
		{
			name:           "get proposal",
			path:           "/plan-api/plans/my-feature/change-proposals/change-proposal.my-feature.1",
			wantSlug:       "my-feature",
			wantProposalID: "change-proposal.my-feature.1",
			wantAction:     "",
		},
		{
			name:           "accept proposal",
			path:           "/plan-api/plans/my-feature/change-proposals/change-proposal.my-feature.1/accept",
			wantSlug:       "my-feature",
			wantProposalID: "change-proposal.my-feature.1",
			wantAction:     "accept",
		},
		{
			name:           "reject proposal",
			path:           "/plan-api/plans/my-feature/change-proposals/change-proposal.my-feature.1/reject",
			wantSlug:       "my-feature",
			wantProposalID: "change-proposal.my-feature.1",
			wantAction:     "reject",
		},
		{
			name:           "submit proposal",
			path:           "/plan-api/plans/my-feature/change-proposals/change-proposal.my-feature.1/submit",
			wantSlug:       "my-feature",
			wantProposalID: "change-proposal.my-feature.1",
			wantAction:     "submit",
		},
		{
			name:           "invalid - missing segment",
			path:           "/plan-api/plans/test-slug/other/change-proposal.test.1",
			wantSlug:       "",
			wantProposalID: "",
			wantAction:     "",
		},
		{
			name:           "invalid - insufficient parts",
			path:           "/plan-api/plans/test-slug/change-proposals",
			wantSlug:       "",
			wantProposalID: "",
			wantAction:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotProposalID, gotAction := extractSlugChangeProposalAndAction(tt.path)
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

func TestHandleListChangeProposals(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-list-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP List Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposals := []workflow.ChangeProposal{
		{
			ID: "change-proposal.cp-list-plan.1", PlanID: "plan.cp-list-plan",
			Title: "First proposal", Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
		{
			ID: "change-proposal.cp-list-plan.2", PlanID: "plan.cp-list-plan",
			Title: "Second proposal", Status: workflow.ChangeProposalStatusAccepted, ProposedBy: "agent",
		},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	t.Run("list all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug+"/change-proposals", nil)
		w := httptest.NewRecorder()

		c.handleListChangeProposals(w, req, slug)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var got []workflow.ChangeProposal
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("len(proposals) = %d, want 2", len(got))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/plan-api/plans/"+slug+"/change-proposals?status=proposed", nil)
		w := httptest.NewRecorder()

		c.handleListChangeProposals(w, req, slug)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var got []workflow.ChangeProposal
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("len(filtered proposals) = %d, want 1", len(got))
		}
	})
}

func TestHandleCreateChangeProposal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-create-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Create Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	// Seed a requirement so AffectedReqIDs validation passes.
	seedReq := workflow.Requirement{
		ID:     "requirement.cp-create-plan.1",
		PlanID: workflow.PlanEntityID(slug),
		Title:  "Auth requirement",
		Status: workflow.RequirementStatusActive,
	}
	if err := workflow.SaveRequirements(ctx, nil, []workflow.Requirement{seedReq}, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	body, _ := json.Marshal(CreateChangeProposalHTTPRequest{
		Title:          "Expand scope of auth requirement",
		Rationale:      "OAuth login was missed in original scope",
		AffectedReqIDs: []string{"requirement.cp-create-plan.1"},
	})

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateChangeProposal(w, req, slug)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var got workflow.ChangeProposal
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Title != "Expand scope of auth requirement" {
		t.Errorf("Title = %q, want %q", got.Title, "Expand scope of auth requirement")
	}
	if got.Status != workflow.ChangeProposalStatusProposed {
		t.Errorf("Status = %q, want %q", got.Status, workflow.ChangeProposalStatusProposed)
	}
	if got.ProposedBy != "user" {
		t.Errorf("ProposedBy = %q, want %q (default)", got.ProposedBy, "user")
	}
	if len(got.AffectedReqIDs) != 1 {
		t.Errorf("len(AffectedReqIDs) = %d, want 1", len(got.AffectedReqIDs))
	}
}

func TestHandleCreateChangeProposal_MissingTitle(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-missing-title"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Missing Title")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	body, _ := json.Marshal(CreateChangeProposalHTTPRequest{Rationale: "no title"})

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateChangeProposal(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleAcceptChangeProposal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-accept-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Accept Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-accept-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-accept-plan",
			Title: "Add OAuth", Status: workflow.ChangeProposalStatusUnderReview, ProposedBy: "user",
		},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID+"/accept", nil)
	w := httptest.NewRecorder()

	c.handleAcceptChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusAccepted, w.Body.String())
	}

	var resp AcceptChangeProposalResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Proposal.Status != workflow.ChangeProposalStatusAccepted {
		t.Errorf("Status = %q, want %q", resp.Proposal.Status, workflow.ChangeProposalStatusAccepted)
	}
	if resp.Proposal.DecidedAt == nil {
		t.Error("DecidedAt should be set after acceptance")
	}
}

func TestHandleRejectChangeProposal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-reject-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Reject Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-reject-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-reject-plan",
			Title: "Risky change", Status: workflow.ChangeProposalStatusUnderReview, ProposedBy: "agent",
		},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID+"/reject", nil)
	w := httptest.NewRecorder()

	c.handleRejectChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.ChangeProposal
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Status != workflow.ChangeProposalStatusRejected {
		t.Errorf("Status = %q, want %q", got.Status, workflow.ChangeProposalStatusRejected)
	}
	if got.DecidedAt == nil {
		t.Error("DecidedAt should be set after rejection")
	}
}

func TestHandleAcceptChangeProposal_InvalidTransition(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-invalid-transition"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Invalid Transition")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	// A proposal in "proposed" status can't be directly accepted (must go through under_review first
	// per ADR-024 OODA loop — but our status machine allows proposed → accepted)
	// Test an already-accepted proposal can't be accepted again
	proposalID := "change-proposal.cp-invalid-transition.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-invalid-transition",
			Title: "Already accepted", Status: workflow.ChangeProposalStatusAccepted, ProposedBy: "user",
		},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID+"/accept", nil)
	w := httptest.NewRecorder()

	c.handleAcceptChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleSubmitChangeProposal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-submit-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Submit Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-submit-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-submit-plan",
			Title: "Pending proposal", Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID+"/submit", nil)
	w := httptest.NewRecorder()

	c.handleSubmitChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.ChangeProposal
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Status != workflow.ChangeProposalStatusUnderReview {
		t.Errorf("Status = %q, want %q", got.Status, workflow.ChangeProposalStatusUnderReview)
	}
}

func TestHandleDeleteChangeProposal_NotProposed(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cp-delete-guard"
	_, err := workflow.CreatePlan(ctx, nil, slug, "CP Delete Guard")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-delete-guard.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-delete-guard",
			Title: "Accepted proposal", Status: workflow.ChangeProposalStatusAccepted, ProposedBy: "user",
		},
	}
	if err := workflow.SaveChangeProposals(ctx, nil, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)
	setupTestPlan(t, c, slug)

	req := httptest.NewRequest(http.MethodDelete, "/plan-api/plans/"+slug+"/change-proposals/"+proposalID, nil)
	w := httptest.NewRecorder()

	c.handleDeleteChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}
