//go:build integration

package planapi

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

func TestExtractSlugScenarioAndAction(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantSlug       string
		wantScenarioID string
		wantAction     string
	}{
		{
			name:           "get scenario",
			path:           "/plan-api/plans/my-feature/scenarios/scenario.my-feature.1",
			wantSlug:       "my-feature",
			wantScenarioID: "scenario.my-feature.1",
			wantAction:     "",
		},
		{
			name:           "with trailing slash",
			path:           "/plan-api/plans/test-slug/scenarios/scenario.test-slug.2/",
			wantSlug:       "test-slug",
			wantScenarioID: "scenario.test-slug.2",
			wantAction:     "",
		},
		{
			name:           "invalid - missing scenarios segment",
			path:           "/plan-api/plans/test-slug/something/scenario.test.1",
			wantSlug:       "",
			wantScenarioID: "",
			wantAction:     "",
		},
		{
			name:           "invalid - insufficient parts",
			path:           "/plan-api/plans/test-slug/scenarios",
			wantSlug:       "",
			wantScenarioID: "",
			wantAction:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSlug, gotScenarioID, gotAction := extractSlugScenarioAndAction(tt.path)
			if gotSlug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", gotSlug, tt.wantSlug)
			}
			if gotScenarioID != tt.wantScenarioID {
				t.Errorf("scenarioID = %q, want %q", gotScenarioID, tt.wantScenarioID)
			}
			if gotAction != tt.wantAction {
				t.Errorf("action = %q, want %q", gotAction, tt.wantAction)
			}
		})
	}
}

func TestHandleListScenarios(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

		slug := "scenario-list-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "Scenario List Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	now := time.Now()
	scenarios := []workflow.Scenario{
		{
			ID:            "scenario.scenario-list-plan.1",
			RequirementID: "requirement.scenario-list-plan.1",
			Given:         "a user is logged out",
			When:          "they visit the login page",
			Then:          []string{"the login form is displayed"},
			Status:        workflow.ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			ID:            "scenario.scenario-list-plan.2",
			RequirementID: "requirement.scenario-list-plan.2",
			Given:         "a user is logged in",
			When:          "they submit valid credentials",
			Then:          []string{"they are redirected to the dashboard"},
			Status:        workflow.ScenarioStatusPassing,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios() error = %v", err)
	}

	c := setupTestComponent(t)

	t.Run("list all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/plan-api/plans/"+slug+"/scenarios", nil)
		w := httptest.NewRecorder()

		c.handleListScenarios(w, req, slug)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var got []workflow.Scenario
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("len(scenarios) = %d, want 2", len(got))
		}
	})

	t.Run("filter by requirement_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/plan-api/plans/"+slug+"/scenarios?requirement_id=requirement.scenario-list-plan.1", nil)
		w := httptest.NewRecorder()

		c.handleListScenarios(w, req, slug)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}

		var got []workflow.Scenario
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("len(filtered scenarios) = %d, want 1", len(got))
		}
		if len(got) > 0 && got[0].RequirementID != "requirement.scenario-list-plan.1" {
			t.Errorf("RequirementID = %q, want %q", got[0].RequirementID, "requirement.scenario-list-plan.1")
		}
	})
}

func TestHandleCreateScenario(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

		slug := "create-scenario-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "Create Scenario Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	body, _ := json.Marshal(CreateScenarioHTTPRequest{
		RequirementID: "requirement.create-scenario-plan.1",
		Given:         "the user is on the login page",
		When:          "they enter valid credentials and click login",
		Then:          []string{"they are authenticated", "they are redirected to dashboard"},
	})

	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/scenarios", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleCreateScenario(w, req, slug)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var got workflow.Scenario
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.RequirementID != "requirement.create-scenario-plan.1" {
		t.Errorf("RequirementID = %q, want %q", got.RequirementID, "requirement.create-scenario-plan.1")
	}
	if len(got.Then) != 2 {
		t.Errorf("len(Then) = %d, want 2", len(got.Then))
	}
	if got.Status != workflow.ScenarioStatusPending {
		t.Errorf("status = %q, want %q", got.Status, workflow.ScenarioStatusPending)
	}
}

func TestHandleCreateScenario_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

		slug := "validation-scenario-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "Validation Scenario Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	c := setupTestComponent(t)

	tests := []struct {
		name string
		body CreateScenarioHTTPRequest
	}{
		{
			name: "missing requirement_id",
			body: CreateScenarioHTTPRequest{Given: "given", When: "when", Then: []string{"then"}},
		},
		{
			name: "missing given",
			body: CreateScenarioHTTPRequest{RequirementID: "req.1", When: "when", Then: []string{"then"}},
		},
		{
			name: "missing when",
			body: CreateScenarioHTTPRequest{RequirementID: "req.1", Given: "given", Then: []string{"then"}},
		},
		{
			name: "empty then",
			body: CreateScenarioHTTPRequest{RequirementID: "req.1", Given: "given", When: "when"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/scenarios", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			c.handleCreateScenario(w, req, slug)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleUpdateScenario(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

		slug := "update-scenario-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "Update Scenario Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	now := time.Now()
	scenarioID := "scenario.update-scenario-plan.1"
	scenarios := []workflow.Scenario{
		{
			ID:            scenarioID,
			RequirementID: "requirement.update-scenario-plan.1",
			Given:         "original given",
			When:          "original when",
			Then:          []string{"original then"},
			Status:        workflow.ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios() error = %v", err)
	}

	c := setupTestComponent(t)

	newGiven := "updated given"
	body, _ := json.Marshal(UpdateScenarioHTTPRequest{Given: &newGiven})

	req := httptest.NewRequest(http.MethodPatch, "/plan-api/plans/"+slug+"/scenarios/"+scenarioID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	c.handleUpdateScenario(w, req, slug, scenarioID)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got workflow.Scenario
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Given != newGiven {
		t.Errorf("Given = %q, want %q", got.Given, newGiven)
	}
	// Other fields should be unchanged
	if got.When != "original when" {
		t.Errorf("When = %q, want %q", got.When, "original when")
	}
}

func TestHandleDeleteScenario(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

		slug := "delete-scenario-plan"
	_, err := workflow.CreatePlan(ctx, nil, slug, "Delete Scenario Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	now := time.Now()
	scenarioID := "scenario.delete-scenario-plan.1"
	scenarios := []workflow.Scenario{
		{
			ID:            scenarioID,
			RequirementID: "requirement.delete-scenario-plan.1",
			Given:         "given",
			When:          "when",
			Then:          []string{"then"},
			Status:        workflow.ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios() error = %v", err)
	}

	c := setupTestComponent(t)

	req := httptest.NewRequest(http.MethodDelete, "/plan-api/plans/"+slug+"/scenarios/"+scenarioID, nil)
	w := httptest.NewRecorder()

	c.handleDeleteScenario(w, req, slug, scenarioID)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	remaining, err := workflow.LoadScenarios(ctx, nil, slug)
	if err != nil {
		t.Fatalf("LoadScenarios() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 scenarios after delete, got %d", len(remaining))
	}
}
