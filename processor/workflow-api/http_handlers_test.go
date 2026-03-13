package workflowapi

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
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "get-plan-exists"
	_, err := m.CreatePlan(ctx, slug, "Get Plan Exists")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug, nil)
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
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/nonexistent-plan", nil)
	w := httptest.NewRecorder()

	c.handleGetPlan(w, req, "nonexistent-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleListPlans(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	for _, slug := range []string{"list-plan-one", "list-plan-two"} {
		if _, err := m.CreatePlan(ctx, slug, slug); err != nil {
			t.Fatalf("CreatePlan(%q) error = %v", slug, err)
		}
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans", nil)
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
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans", nil)
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
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	c := setupTestComponent(t)

	newTitle := "Updated Title"
	body, _ := json.Marshal(UpdatePlanHTTPRequest{Title: &newTitle})
	req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/no-such-plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdatePlan(w, req, "no-such-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePromotePlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "promote-plan"
	_, err := m.CreatePlan(ctx, slug, "Promote Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

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
	if !got.Plan.Approved {
		t.Error("Plan.Approved should be true after promote")
	}
}

func TestHandlePromotePlan_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/no-such-plan/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, "no-such-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandlePromotePlan_AlreadyApproved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "promote-already-approved"
	plan, err := m.CreatePlan(ctx, slug, "Already Approved Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/promote", nil)
	w := httptest.NewRecorder()

	c.handlePromotePlan(w, req, slug)

	// Idempotent — should return 200 without error.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Task collection handlers
// ---------------------------------------------------------------------------

func TestHandleListTasks_WithTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "list-tasks-plan"
	plan, err := m.CreatePlan(ctx, slug, "List Tasks Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	tasks := make([]workflow.Task, 3)
	for i := range tasks {
		task, err := workflow.CreateTask(plan.ID, slug, i+1, "test task")
		if err != nil {
			t.Fatalf("CreateTask() error = %v", err)
		}
		tasks[i] = *task
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/tasks", nil)
	w := httptest.NewRecorder()

	c.handleListTasks(w, req, slug)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got []workflow.Task
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != 3 {
		t.Errorf("len(tasks) = %d, want 3", len(got))
	}
}

func TestHandleListTasks_NoTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "list-tasks-empty"
	if _, err := m.CreatePlan(ctx, slug, "Empty Tasks Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/tasks", nil)
	w := httptest.NewRecorder()

	c.handleListTasks(w, req, slug)

	// Handler returns empty array when no tasks exist.
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// The handler writes a raw "[]" byte slice (no trailing newline from json.Encoder).
	// Decode to verify the payload is a valid empty JSON array.
	var got []workflow.Task
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(got))
	}
}

func TestHandleApproveTasksPlan(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "approve-tasks-plan"
	plan, err := m.CreatePlan(ctx, slug, "Approve Tasks Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan() error = %v", err)
	}

	task, err := workflow.CreateTask(plan.ID, slug, 1, "first task")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := m.SaveTasks(ctx, []workflow.Task{*task}, slug); err != nil {
		t.Fatalf("SaveTasks() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/tasks/approve", nil)
	w := httptest.NewRecorder()

	c.handleApproveTasksPlan(w, req, slug)

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
}

func TestHandleApproveTasksPlan_PlanNotApproved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "approve-tasks-unapproved-plan"
	if _, err := m.CreatePlan(ctx, slug, "Unapproved Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/tasks/approve", nil)
	w := httptest.NewRecorder()

	c.handleApproveTasksPlan(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleApproveTasksPlan_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/no-such-plan/tasks/approve", nil)
	w := httptest.NewRecorder()

	c.handleApproveTasksPlan(w, req, "no-such-plan")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleApproveTasksPlan_NoTasks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "approve-tasks-no-tasks"
	plan, err := m.CreatePlan(ctx, slug, "No Tasks Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if err := m.ApprovePlan(ctx, plan); err != nil {
		t.Fatalf("ApprovePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/tasks/approve", nil)
	w := httptest.NewRecorder()

	c.handleApproveTasksPlan(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Change proposal handlers (previously untested)
// ---------------------------------------------------------------------------

func TestHandleGetChangeProposal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-get-plan"
	if _, err := m.CreatePlan(ctx, slug, "CP Get Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-get-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-get-plan",
			Title: "Add feature X", Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}
	if err := m.SaveChangeProposals(ctx, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/change-proposals/"+proposalID, nil)
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
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-get-notfound"
	if _, err := m.CreatePlan(ctx, slug, "CP Get NotFound"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/change-proposals/nonexistent", nil)
	w := httptest.NewRecorder()

	c.handleGetChangeProposal(w, req, slug, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleUpdateChangeProposal(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-update-plan"
	if _, err := m.CreatePlan(ctx, slug, "CP Update Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-update-plan.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-update-plan",
			Title: "Original title", Rationale: "Original rationale",
			Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}
	if err := m.SaveChangeProposals(ctx, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)

	newTitle := "Updated title"
	newRationale := "Updated rationale"
	body, _ := json.Marshal(UpdateChangeProposalHTTPRequest{
		Title:     &newTitle,
		Rationale: &newRationale,
	})

	req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/change-proposals/"+proposalID, bytes.NewReader(body))
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
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-update-invalid-status"
	if _, err := m.CreatePlan(ctx, slug, "CP Update Invalid Status"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-update-invalid-status.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-update-invalid-status",
			Title: "Accepted proposal", Status: workflow.ChangeProposalStatusAccepted, ProposedBy: "user",
		},
	}
	if err := m.SaveChangeProposals(ctx, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)

	newTitle := "Try to change accepted"
	body, _ := json.Marshal(UpdateChangeProposalHTTPRequest{Title: &newTitle})

	req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/change-proposals/"+proposalID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleUpdateChangeProposal_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-update-notfound"
	if _, err := m.CreatePlan(ctx, slug, "CP Update NotFound"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	newTitle := "Nope"
	body, _ := json.Marshal(UpdateChangeProposalHTTPRequest{Title: &newTitle})

	req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/change-proposals/nonexistent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateChangeProposal(w, req, slug, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteChangeProposal_Success(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-delete-success"
	if _, err := m.CreatePlan(ctx, slug, "CP Delete Success"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	proposalID := "change-proposal.cp-delete-success.1"
	proposals := []workflow.ChangeProposal{
		{
			ID: proposalID, PlanID: "plan.cp-delete-success",
			Title: "To delete", Status: workflow.ChangeProposalStatusProposed, ProposedBy: "user",
		},
	}
	if err := m.SaveChangeProposals(ctx, proposals, slug); err != nil {
		t.Fatalf("SaveChangeProposals() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodDelete, "/workflow-api/plans/"+slug+"/change-proposals/"+proposalID, nil)
	w := httptest.NewRecorder()

	c.handleDeleteChangeProposal(w, req, slug, proposalID)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Verify the proposal was removed.
	remaining, err := m.LoadChangeProposals(ctx, slug)
	if err != nil {
		t.Fatalf("LoadChangeProposals() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 proposals after delete, got %d", len(remaining))
	}
}

func TestHandleCreateChangeProposal_InvalidRequirementID(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "cp-bad-req-id"
	if _, err := m.CreatePlan(ctx, slug, "CP Bad Req ID"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	// Reference a requirement ID that does not exist in this plan.
	body, _ := json.Marshal(CreateChangeProposalHTTPRequest{
		Title:          "Change with missing req",
		Rationale:      "Testing validation",
		AffectedReqIDs: []string{"requirement.cp-bad-req-id.999"},
	})

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/change-proposals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateChangeProposal(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Scenario GET handler (covered elsewhere as list/create but GET by ID is not)
// ---------------------------------------------------------------------------

func TestHandleGetScenario(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "get-scenario-plan"
	if _, err := m.CreatePlan(ctx, slug, "Get Scenario Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

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
	if err := m.SaveScenarios(ctx, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/scenarios/"+scenarioID, nil)
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
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "get-scenario-notfound"
	if _, err := m.CreatePlan(ctx, slug, "Get Scenario NotFound"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/scenarios/nonexistent", nil)
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
		{
			name:      "default drafting",
			plan:      &workflow.Plan{},
			wantStage: "drafting",
		},
		{
			name:      "approved plan",
			plan:      &workflow.Plan{Approved: true},
			wantStage: "approved",
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
