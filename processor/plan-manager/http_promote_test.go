package planmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
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

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
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

func TestHandlePromotePlan_Round2ReentryAutoStartsExecution(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-round2-reentry"

	plan := setupTestPlan(t, c, slug)
	plan.Approved = true
	plan.Status = workflow.StatusScenariosReviewed
	plan.Requirements = []workflow.Requirement{{ID: "requirement.1", Title: "Fix ownership gate"}}
	plan.Stories = []workflow.Story{{ID: "story.1", RequirementIDs: []string{"requirement.1"}, Status: workflow.StoryStatusReady}}
	plan.Scenarios = []workflow.Scenario{{ID: "scenario.1", RequirementID: "requirement.1", StoryID: "story.1"}}
	_ = c.plans.save(context.Background(), plan)

	published := false
	c.orchestratorTriggerPublisher = func(_ context.Context, got *payloads.ScenarioOrchestrationTrigger) error {
		published = true
		if got.PlanSlug != slug {
			t.Fatalf("published slug = %q, want %q", got.PlanSlug, slug)
		}
		if len(got.Requirements) != 1 || got.Requirements[0].ID != "requirement.1" {
			t.Fatalf("published requirements = %v, want requirement.1", got.Requirements)
		}
		if len(got.Scenarios) != 1 || got.Scenarios[0].ID != "scenario.1" {
			t.Fatalf("published scenarios = %v, want scenario.1", got.Scenarios)
		}
		return nil
	}

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !published {
		t.Fatal("expected round-2 promote to publish scenario orchestrator trigger")
	}
	got, ok := c.plans.get(slug)
	if !ok {
		t.Fatal("plan missing after promote")
	}
	if got.EffectiveStatus() != workflow.StatusImplementing {
		t.Fatalf("status = %s, want implementing", got.EffectiveStatus())
	}
}

func TestHandlePromotePlan_NotFound(t *testing.T) {
	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/no-such-plan/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, "no-such-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePromotePlan_CreatedReturnsConflict(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-while-created"

	// A freshly-created plan has not been through review. Per the two-stage
	// approval state machine (ADR-029), promote may only approve a plan that
	// reached `reviewed`; promoting straight from `created` is an invalid
	// transition and must 409 — not silently approve an unreviewed plan.
	setupTestPlan(t, c, slug) // stays at StatusCreated (zero value)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestHandlePromotePlan_DraftingReturnsConflict(t *testing.T) {
	c := setupTestComponent(t)
	slug := "promote-while-drafting"

	plan := setupTestPlan(t, c, slug)
	plan.Status = workflow.StatusDrafting
	_ = c.plans.save(context.Background(), plan)

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
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

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
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

	req := httptest.NewRequest(http.MethodPost, "/plan-manager/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	// Idempotent — should return 200 without error.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}
