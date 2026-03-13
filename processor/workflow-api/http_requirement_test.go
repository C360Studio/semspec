package workflowapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestExtractSlugRequirementAndAction(t *testing.T) {
	tests := []struct {
		name              string
		path              string
		wantSlug          string
		wantRequirementID string
		wantAction        string
	}{
		{
			name:              "get requirement",
			path:              "/workflow-api/plans/my-feature/requirements/requirement.my-feature.1",
			wantSlug:          "my-feature",
			wantRequirementID: "requirement.my-feature.1",
			wantAction:        "",
		},
		{
			name:              "deprecate requirement",
			path:              "/workflow-api/plans/my-feature/requirements/requirement.my-feature.1/deprecate",
			wantSlug:          "my-feature",
			wantRequirementID: "requirement.my-feature.1",
			wantAction:        "deprecate",
		},
		{
			name:              "with trailing slash",
			path:              "/workflow-api/plans/test-slug/requirements/requirement.test-slug.2/",
			wantSlug:          "test-slug",
			wantRequirementID: "requirement.test-slug.2",
			wantAction:        "",
		},
		{
			name:              "invalid - missing requirements segment",
			path:              "/workflow-api/plans/test-slug/something/requirement.test.1",
			wantSlug:          "",
			wantRequirementID: "",
			wantAction:        "",
		},
		{
			name:              "invalid - insufficient parts",
			path:              "/workflow-api/plans/test-slug/requirements",
			wantSlug:          "",
			wantRequirementID: "",
			wantAction:        "",
		},
		{
			name:              "no plans segment",
			path:              "/workflow-api/other/my-feature/requirements/r.1",
			wantSlug:          "",
			wantRequirementID: "",
			wantAction:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotReqID, gotAction := extractSlugRequirementAndAction(tt.path)
			if gotSlug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", gotSlug, tt.wantSlug)
			}
			if gotReqID != tt.wantRequirementID {
				t.Errorf("requirementID = %q, want %q", gotReqID, tt.wantRequirementID)
			}
			if gotAction != tt.wantAction {
				t.Errorf("action = %q, want %q", gotAction, tt.wantAction)
			}
		})
	}
}

func TestHandleListRequirements(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "test-plan"

	// Create a plan so the slug validates
	_, err := m.CreatePlan(ctx, slug, "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	// Pre-populate requirements
	reqs := []workflow.Requirement{
		{ID: "requirement.test-plan.1", PlanID: "plan.test-plan", Title: "First requirement", Status: workflow.RequirementStatusActive},
		{ID: "requirement.test-plan.2", PlanID: "plan.test-plan", Title: "Second requirement", Status: workflow.RequirementStatusDeprecated},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/requirements", nil)
	w := httptest.NewRecorder()

	c.handleListRequirements(w, req, slug)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got []workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("len(requirements) = %d, want 2", len(got))
	}
}

func TestHandleListRequirements_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	c := setupTestComponent(t)

	slug := "empty-plan"
	// Create plan directory structure by creating the plan
	m := workflow.NewManager(tmpDir)
	_, err := m.CreatePlan(context.Background(), slug, "Empty Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/requirements", nil)
	w := httptest.NewRecorder()

	c.handleListRequirements(w, req, slug)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got []workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("len(requirements) = %d, want 0", len(got))
	}
}

func TestHandleCreateRequirement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "create-req-plan"
	_, err := m.CreatePlan(ctx, slug, "Create Req Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	body, _ := json.Marshal(CreateRequirementHTTPRequest{
		Title:       "User can log in",
		Description: "The system must allow users to authenticate",
	})

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateRequirement(w, req, slug)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var got workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Title != "User can log in" {
		t.Errorf("title = %q, want %q", got.Title, "User can log in")
	}
	if got.Status != workflow.RequirementStatusActive {
		t.Errorf("status = %q, want %q", got.Status, workflow.RequirementStatusActive)
	}
	if got.ID == "" {
		t.Error("ID is empty")
	}
}

func TestHandleCreateRequirement_MissingTitle(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "missing-title-plan"
	_, err := m.CreatePlan(context.Background(), slug, "Missing Title Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	body, _ := json.Marshal(CreateRequirementHTTPRequest{Description: "No title here"})

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateRequirement(w, req, slug)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetRequirement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "get-req-plan"
	_, err := m.CreatePlan(ctx, slug, "Get Req Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	reqID := "requirement.get-req-plan.1"
	reqs := []workflow.Requirement{
		{ID: reqID, PlanID: "plan.get-req-plan", Title: "Auth requirement", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/requirements/"+reqID, nil)
	w := httptest.NewRecorder()

	c.handleGetRequirement(w, req, slug, reqID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.ID != reqID {
		t.Errorf("ID = %q, want %q", got.ID, reqID)
	}
}

func TestHandleGetRequirement_NotFound(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "notfound-req-plan"
	_, err := m.CreatePlan(ctx, slug, "NotFound Req Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodGet, "/workflow-api/plans/"+slug+"/requirements/nonexistent", nil)
	w := httptest.NewRecorder()

	c.handleGetRequirement(w, req, slug, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleUpdateRequirement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "update-req-plan"
	_, err := m.CreatePlan(ctx, slug, "Update Req Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	reqID := "requirement.update-req-plan.1"
	reqs := []workflow.Requirement{
		{ID: reqID, PlanID: "plan.update-req-plan", Title: "Old title", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	newTitle := "New title"
	body, _ := json.Marshal(UpdateRequirementHTTPRequest{Title: &newTitle})

	req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/requirements/"+reqID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateRequirement(w, req, slug, reqID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Title != newTitle {
		t.Errorf("title = %q, want %q", got.Title, newTitle)
	}
}

func TestHandleDeleteRequirement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "delete-req-plan"
	_, err := m.CreatePlan(ctx, slug, "Delete Req Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	reqID := "requirement.delete-req-plan.1"
	reqs := []workflow.Requirement{
		{ID: reqID, PlanID: "plan.delete-req-plan", Title: "To be deleted", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodDelete, "/workflow-api/plans/"+slug+"/requirements/"+reqID, nil)
	w := httptest.NewRecorder()

	c.handleDeleteRequirement(w, req, slug, reqID)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify it's gone
	remaining, err := m.LoadRequirements(ctx, slug)
	if err != nil {
		t.Fatalf("LoadRequirements() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 requirements after delete, got %d", len(remaining))
	}
}

func TestHandleDeprecateRequirement(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "deprecate-req-plan"
	_, err := m.CreatePlan(ctx, slug, "Deprecate Req Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	reqID := "requirement.deprecate-req-plan.1"
	reqs := []workflow.Requirement{
		{ID: reqID, PlanID: "plan.deprecate-req-plan", Title: "To be deprecated", Status: workflow.RequirementStatusActive},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements/"+reqID+"/deprecate", nil)
	w := httptest.NewRecorder()

	c.handleDeprecateRequirement(w, req, slug, reqID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Status != workflow.RequirementStatusDeprecated {
		t.Errorf("status = %q, want %q", got.Status, workflow.RequirementStatusDeprecated)
	}
}

// TestHandleCreateRequirement_DependsOn exercises the depends_on field during
// requirement creation, covering the valid case, unknown references, and cycle
// detection.
func TestHandleCreateRequirement_DependsOn(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, ctx context.Context, m *workflow.Manager, slug string)
		reqBody    CreateRequirementHTTPRequest
		wantStatus int
		// checkResp is called only when wantStatus is 201.
		checkResp func(t *testing.T, got workflow.Requirement)
	}{
		{
			name: "valid depends_on persisted in response",
			setup: func(t *testing.T, ctx context.Context, m *workflow.Manager, slug string) {
				// Pre-create the requirement that will be referenced.
				existing := []workflow.Requirement{
					{
						ID:     "requirement.dep-plan.1",
						PlanID: workflow.PlanEntityID(slug),
						Title:  "Pre-existing requirement",
						Status: workflow.RequirementStatusActive,
					},
				}
				if err := m.SaveRequirements(ctx, existing, slug); err != nil {
					t.Fatalf("SaveRequirements() error = %v", err)
				}
			},
			reqBody: CreateRequirementHTTPRequest{
				Title:     "Dependent requirement",
				DependsOn: []string{"requirement.dep-plan.1"},
			},
			wantStatus: http.StatusCreated,
			checkResp: func(t *testing.T, got workflow.Requirement) {
				if len(got.DependsOn) != 1 || got.DependsOn[0] != "requirement.dep-plan.1" {
					t.Errorf("DependsOn = %v, want [requirement.dep-plan.1]", got.DependsOn)
				}
			},
		},
		{
			name:  "unknown depends_on reference returns 422",
			setup: nil,
			reqBody: CreateRequirementHTTPRequest{
				Title:     "Bad dependency",
				DependsOn: []string{"requirement.dep-plan.nonexistent"},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tmpDir := t.TempDir()
			t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

			slug := "dep-plan"
			m := workflow.NewManager(tmpDir)
			if _, err := m.CreatePlan(ctx, slug, "Dep Plan"); err != nil {
				t.Fatalf("CreatePlan() error = %v", err)
			}

			if tt.setup != nil {
				tt.setup(t, ctx, m, slug)
			}

			c := setupTestComponent(t)

			body, _ := json.Marshal(tt.reqBody)
			req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleCreateRequirement(w, req, slug)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var got workflow.Requirement
				if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if tt.checkResp != nil {
					tt.checkResp(t, got)
				}
			}

			if tt.wantStatus == http.StatusUnprocessableEntity {
				var errResp struct {
					Error string `json:"error"`
				}
				if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if errResp.Error == "" {
					t.Error("expected non-empty error message in JSON error envelope")
				}
			}
		})
	}
}

// TestHandleCreateRequirement_CycleViaUpdate verifies that a cycle introduced
// through an update (A → B, then update A to depend on B while B already
// depends on A) is rejected with 422.
func TestHandleCreateRequirement_CycleViaUpdate(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "cycle-plan"
	m := workflow.NewManager(tmpDir)
	if _, err := m.CreatePlan(ctx, slug, "Cycle Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	// Create requirement A (no deps).
	bodyA, _ := json.Marshal(CreateRequirementHTTPRequest{Title: "Requirement A"})
	reqA := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements", bytes.NewReader(bodyA))
	reqA.Header.Set("Content-Type", "application/json")
	wA := httptest.NewRecorder()
	c.handleCreateRequirement(wA, reqA, slug)
	if wA.Code != http.StatusCreated {
		t.Fatalf("create A: status = %d, want %d; body: %s", wA.Code, http.StatusCreated, wA.Body.String())
	}
	var reqARespBody workflow.Requirement
	if err := json.NewDecoder(wA.Body).Decode(&reqARespBody); err != nil {
		t.Fatalf("decode A: %v", err)
	}
	idA := reqARespBody.ID

	// Create requirement B depending on A.
	bodyB, _ := json.Marshal(CreateRequirementHTTPRequest{
		Title:     "Requirement B",
		DependsOn: []string{idA},
	})
	reqB := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements", bytes.NewReader(bodyB))
	reqB.Header.Set("Content-Type", "application/json")
	wB := httptest.NewRecorder()
	c.handleCreateRequirement(wB, reqB, slug)
	if wB.Code != http.StatusCreated {
		t.Fatalf("create B: status = %d, want %d; body: %s", wB.Code, http.StatusCreated, wB.Body.String())
	}
	var reqBRespBody workflow.Requirement
	if err := json.NewDecoder(wB.Body).Decode(&reqBRespBody); err != nil {
		t.Fatalf("decode B: %v", err)
	}
	idB := reqBRespBody.ID

	// Attempt to update A to depend on B — creates cycle A → B → A.
	bodyUpdate, _ := json.Marshal(UpdateRequirementHTTPRequest{DependsOn: []string{idB}})
	reqUpdate := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/requirements/"+idA, bytes.NewReader(bodyUpdate))
	reqUpdate.Header.Set("Content-Type", "application/json")
	wUpdate := httptest.NewRecorder()
	c.handleUpdateRequirement(wUpdate, reqUpdate, slug, idA)

	if wUpdate.Code != http.StatusUnprocessableEntity {
		t.Errorf("update cycle: status = %d, want %d; body: %s", wUpdate.Code, http.StatusUnprocessableEntity, wUpdate.Body.String())
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(wUpdate.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error == "" {
		t.Error("expected non-empty error message in JSON error envelope")
	}
}

// TestHandleUpdateRequirement_DependsOn exercises adding, clearing, and
// rejecting invalid depends_on values via PATCH.
func TestHandleUpdateRequirement_DependsOn(t *testing.T) {
	tests := []struct {
		name string
		// existingReqs are saved before the update is attempted.
		existingReqs []workflow.Requirement
		targetID     string
		updateBody   UpdateRequirementHTTPRequest
		wantStatus   int
		checkResp    func(t *testing.T, got workflow.Requirement)
	}{
		{
			name: "add valid depends_on succeeds",
			existingReqs: []workflow.Requirement{
				{ID: "requirement.upd-dep-plan.1", PlanID: "plan.upd-dep-plan", Title: "Base", Status: workflow.RequirementStatusActive},
				{ID: "requirement.upd-dep-plan.2", PlanID: "plan.upd-dep-plan", Title: "Dependent", Status: workflow.RequirementStatusActive},
			},
			targetID:   "requirement.upd-dep-plan.2",
			updateBody: UpdateRequirementHTTPRequest{DependsOn: []string{"requirement.upd-dep-plan.1"}},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, got workflow.Requirement) {
				if len(got.DependsOn) != 1 || got.DependsOn[0] != "requirement.upd-dep-plan.1" {
					t.Errorf("DependsOn = %v, want [requirement.upd-dep-plan.1]", got.DependsOn)
				}
			},
		},
		{
			name: "add unknown depends_on returns 422",
			existingReqs: []workflow.Requirement{
				{ID: "requirement.upd-dep-plan.1", PlanID: "plan.upd-dep-plan", Title: "Only req", Status: workflow.RequirementStatusActive},
			},
			targetID:   "requirement.upd-dep-plan.1",
			updateBody: UpdateRequirementHTTPRequest{DependsOn: []string{"requirement.upd-dep-plan.ghost"}},
			wantStatus: http.StatusUnprocessableEntity,
		},
		// "clear depends_on" is tested separately below as TestHandleUpdateRequirement_ClearDependsOn
		// because it requires sending a raw JSON body to avoid omitempty dropping the empty array
		// during struct marshalling.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tmpDir := t.TempDir()
			t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

			slug := "upd-dep-plan"
			m := workflow.NewManager(tmpDir)
			if _, err := m.CreatePlan(ctx, slug, "Upd Dep Plan"); err != nil {
				t.Fatalf("CreatePlan() error = %v", err)
			}
			if err := m.SaveRequirements(ctx, tt.existingReqs, slug); err != nil {
				t.Fatalf("SaveRequirements() error = %v", err)
			}

			c := setupTestComponent(t)

			body, _ := json.Marshal(tt.updateBody)
			req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/requirements/"+tt.targetID, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleUpdateRequirement(w, req, slug, tt.targetID)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK && tt.checkResp != nil {
				var got workflow.Requirement
				if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				tt.checkResp(t, got)
			}

			if tt.wantStatus == http.StatusUnprocessableEntity {
				var errResp struct {
					Error string `json:"error"`
				}
				if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if errResp.Error == "" {
					t.Error("expected non-empty error message in JSON error envelope")
				}
			}
		})
	}
}

// TestHandleUpdateRequirement_ClearDependsOn verifies that sending an explicit
// empty JSON array for depends_on clears an existing dependency list.
//
// Note: the UpdateRequirementHTTPRequest struct has omitempty on DependsOn, so
// marshalling a Go []string{} would silently drop the field from the JSON body.
// This test sends a raw JSON string instead to exercise the actual wire behavior
// a client would use to clear dependencies.
func TestHandleUpdateRequirement_ClearDependsOn(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "clear-dep-plan"
	m := workflow.NewManager(tmpDir)
	if _, err := m.CreatePlan(ctx, slug, "Clear Dep Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	// Pre-save two requirements where req.2 depends on req.1.
	existing := []workflow.Requirement{
		{ID: "requirement.clear-dep-plan.1", PlanID: "plan.clear-dep-plan", Title: "Base", Status: workflow.RequirementStatusActive},
		{
			ID:        "requirement.clear-dep-plan.2",
			PlanID:    "plan.clear-dep-plan",
			Title:     "Had deps",
			Status:    workflow.RequirementStatusActive,
			DependsOn: []string{"requirement.clear-dep-plan.1"},
		},
	}
	if err := m.SaveRequirements(ctx, existing, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	// Send {"depends_on": []} as raw JSON — bypasses Go's omitempty marshalling.
	rawBody := []byte(`{"depends_on": []}`)
	req := httptest.NewRequest(http.MethodPatch, "/workflow-api/plans/"+slug+"/requirements/requirement.clear-dep-plan.2", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateRequirement(w, req, slug, "requirement.clear-dep-plan.2")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.Requirement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.DependsOn) != 0 {
		t.Errorf("DependsOn = %v, want empty after clear", got.DependsOn)
	}

	// Confirm persistence: reload and verify the field is gone.
	stored, err := m.LoadRequirements(ctx, slug)
	if err != nil {
		t.Fatalf("LoadRequirements() error = %v", err)
	}
	for _, r := range stored {
		if r.ID == "requirement.clear-dep-plan.2" && len(r.DependsOn) != 0 {
			t.Errorf("stored DependsOn = %v, want empty after clear", r.DependsOn)
		}
	}
}

// TestHandleCreateRequirement_IndependentRequirementsHaveNoDeps verifies that
// two requirements created without a depends_on field have nil/empty DependsOn
// in their responses and when reloaded from storage.
func TestHandleCreateRequirement_IndependentRequirementsHaveNoDeps(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	slug := "nodep-plan"
	m := workflow.NewManager(tmpDir)
	if _, err := m.CreatePlan(ctx, slug, "NoDep Plan"); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	for i, title := range []string{"First requirement", "Second requirement"} {
		body, _ := json.Marshal(CreateRequirementHTTPRequest{Title: title})
		req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		c.handleCreateRequirement(w, req, slug)

		if w.Code != http.StatusCreated {
			t.Fatalf("req %d: status = %d, want %d; body: %s", i+1, w.Code, http.StatusCreated, w.Body.String())
		}

		var got workflow.Requirement
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("req %d: decode response: %v", i+1, err)
		}
		if len(got.DependsOn) != 0 {
			t.Errorf("req %d: DependsOn = %v, want empty", i+1, got.DependsOn)
		}
	}

	// Confirm storage also shows no deps.
	stored, err := m.LoadRequirements(ctx, slug)
	if err != nil {
		t.Fatalf("LoadRequirements() error = %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored requirements count = %d, want 2", len(stored))
	}
	for _, r := range stored {
		if len(r.DependsOn) != 0 {
			t.Errorf("stored %q: DependsOn = %v, want empty", r.ID, r.DependsOn)
		}
	}
}

func TestHandleDeprecateRequirement_AlreadyDeprecated(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	m := workflow.NewManager(tmpDir)
	slug := "already-deprecated-plan"
	_, err := m.CreatePlan(ctx, slug, "Already Deprecated Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	reqID := "requirement.already-deprecated-plan.1"
	reqs := []workflow.Requirement{
		{ID: reqID, PlanID: "plan.already-deprecated-plan", Title: "Already deprecated", Status: workflow.RequirementStatusDeprecated},
	}
	if err := m.SaveRequirements(ctx, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodPost, "/workflow-api/plans/"+slug+"/requirements/"+reqID+"/deprecate", nil)
	w := httptest.NewRecorder()

	c.handleDeprecateRequirement(w, req, slug, reqID)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}
