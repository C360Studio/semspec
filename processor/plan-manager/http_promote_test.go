package planmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestHandlePromotePlan_ReviewedToApproved(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-reviewed"

	// Simulate a plan that the reviewer has reviewed (verdict set, summary set)
	// but NOT approved — the auto_approve=false path.
	plan := setupTestPlan(t, c, slug)
	plan.Goal = "Add a /health endpoint"
	plan.Status = workflow.StatusReviewed
	plan.Approved = false
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got PlanWithStatus
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Plan == nil {
		t.Fatal("Plan is nil in response")
	}
	if !got.Plan.Approved {
		t.Error("Plan.Approved should be true after promote")
	}
	if got.Plan.ApprovedAt == nil {
		t.Error("Plan.ApprovedAt should be set after promote")
	}
	if got.Plan.Status != workflow.StatusApproved {
		t.Errorf("Plan.Status = %q, want %q", got.Plan.Status, workflow.StatusApproved)
	}
}

func TestHandlePromotePlan_NotFound(t *testing.T) {
	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/no-such-plan/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, "no-such-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePromotePlan_DraftingReturnsConflict(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-while-drafting"

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusDrafting
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestHandlePromotePlan_GeneratingRequirementsReturnsConflict(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-while-generating"

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusGeneratingRequirements
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestHandlePromotePlan_AlreadyApproved(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-already-approved"

	plan := setupTestPlan(t, c, slug)
	plan.Approved = true
	plan.Status = workflow.StatusApproved
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	// Idempotent — should return 200 without error.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}
