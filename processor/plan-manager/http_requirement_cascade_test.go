//go:build integration

package planmanager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupCascadeFixture creates a plan with requirements and scenarios.
// Returns the tmpDir for SEMSPEC_REPO_PATH.
//
// Requirement graph:
//
//	R1 (no deps)
//	R2 → depends on R1
//	R3 → depends on R2 (transitive dep on R1)
//	R4 (independent)
//
// Each requirement has 2 scenarios.
func setupCascadeFixture(t *testing.T, slug string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", tmpDir)

	ctx := context.Background()

	if _, err := workflow.CreatePlan(ctx, nil, slug, "Cascade Test Plan"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	now := time.Now()
	reqs := []workflow.Requirement{
		{ID: rid(slug, 1), Title: "Base requirement", Status: workflow.RequirementStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: rid(slug, 2), Title: "Depends on R1", Status: workflow.RequirementStatusActive, DependsOn: []string{rid(slug, 1)}, CreatedAt: now, UpdatedAt: now},
		{ID: rid(slug, 3), Title: "Depends on R2 (transitive R1)", Status: workflow.RequirementStatusActive, DependsOn: []string{rid(slug, 2)}, CreatedAt: now, UpdatedAt: now},
		{ID: rid(slug, 4), Title: "Independent", Status: workflow.RequirementStatusActive, CreatedAt: now, UpdatedAt: now},
	}
	if err := workflow.SaveRequirements(ctx, nil, reqs, slug); err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}

	var scenarios []workflow.Scenario
	for _, r := range reqs {
		for j := 1; j <= 2; j++ {
			scenarios = append(scenarios, workflow.Scenario{
				ID:            fmt.Sprintf("scenario.%s.%s.%d", slug, r.ID, j),
				RequirementID: r.ID,
				Given:         "given",
				When:          "when",
				Then:          []string{"then"},
				Status:        workflow.ScenarioStatusPending,
				CreatedAt:     now,
				UpdatedAt:     now,
			})
		}
	}
	if err := workflow.SaveScenarios(ctx, nil, scenarios, slug); err != nil {
		t.Fatalf("SaveScenarios: %v", err)
	}
}

func rid(slug string, n int) string {
	return fmt.Sprintf("requirement.%s.%d", slug, n)
}

// ---------------------------------------------------------------------------
// requirementBlastRadius unit tests
// ---------------------------------------------------------------------------

func TestRequirementBlastRadius_SingleNoDependent(t *testing.T) {
	reqs := []workflow.Requirement{
		{ID: "r.1"},
		{ID: "r.2"},
	}
	got := requirementBlastRadius(reqs, "r.1")
	if len(got) != 1 || !got["r.1"] {
		t.Errorf("blast radius = %v, want {r.1}", got)
	}
}

func TestRequirementBlastRadius_DirectDependent(t *testing.T) {
	reqs := []workflow.Requirement{
		{ID: "r.1"},
		{ID: "r.2", DependsOn: []string{"r.1"}},
		{ID: "r.3"},
	}
	got := requirementBlastRadius(reqs, "r.1")
	if len(got) != 2 || !got["r.1"] || !got["r.2"] {
		t.Errorf("blast radius = %v, want {r.1, r.2}", got)
	}
}

func TestRequirementBlastRadius_TransitiveChain(t *testing.T) {
	reqs := []workflow.Requirement{
		{ID: "r.1"},
		{ID: "r.2", DependsOn: []string{"r.1"}},
		{ID: "r.3", DependsOn: []string{"r.2"}},
		{ID: "r.4"},
	}
	got := requirementBlastRadius(reqs, "r.1")
	if len(got) != 3 || !got["r.1"] || !got["r.2"] || !got["r.3"] {
		t.Errorf("blast radius = %v, want {r.1, r.2, r.3}", got)
	}
	if got["r.4"] {
		t.Error("r.4 should not be in blast radius (independent)")
	}
}

func TestRequirementBlastRadius_LeafNode(t *testing.T) {
	reqs := []workflow.Requirement{
		{ID: "r.1"},
		{ID: "r.2", DependsOn: []string{"r.1"}},
		{ID: "r.3", DependsOn: []string{"r.2"}},
	}
	// Deleting the leaf — no dependents.
	got := requirementBlastRadius(reqs, "r.3")
	if len(got) != 1 || !got["r.3"] {
		t.Errorf("blast radius = %v, want {r.3}", got)
	}
}

func TestRequirementBlastRadius_Diamond(t *testing.T) {
	// r.1 → r.2, r.1 → r.3, r.2 → r.4, r.3 → r.4
	reqs := []workflow.Requirement{
		{ID: "r.1"},
		{ID: "r.2", DependsOn: []string{"r.1"}},
		{ID: "r.3", DependsOn: []string{"r.1"}},
		{ID: "r.4", DependsOn: []string{"r.2", "r.3"}},
	}
	got := requirementBlastRadius(reqs, "r.1")
	if len(got) != 4 {
		t.Errorf("blast radius = %v, want all 4", got)
	}
}

// ---------------------------------------------------------------------------
// Cascade delete integration tests (HTTP handler)
// ---------------------------------------------------------------------------

func TestHandleDeleteRequirement_CascadeRemovesDependents(t *testing.T) {
	slug := "cascade-delete"
	setupCascadeFixture(t, slug)
	ctx := context.Background()

	c := setupTestComponent(t)

	// Delete R1 — should cascade to R2 (depends on R1) and R3 (depends on R2).
	req := httptest.NewRequest(http.MethodDelete, "/plan-api/plans/"+slug+"/requirements/"+rid(slug, 1), nil)
	w := httptest.NewRecorder()
	c.handleDeleteRequirement(w, req, slug, rid(slug, 1))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Only R4 should remain.
	remaining, err := workflow.LoadRequirements(ctx, nil, slug)
	if err != nil {
		t.Fatalf("LoadRequirements: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 requirement remaining, got %d", len(remaining))
	}
	if remaining[0].ID != rid(slug, 4) {
		t.Errorf("remaining requirement = %q, want %q", remaining[0].ID, rid(slug, 4))
	}

	// Only R4's scenarios should remain (2 scenarios).
	scenarios, err := workflow.LoadScenarios(ctx, nil, slug)
	if err != nil {
		t.Fatalf("LoadScenarios: %v", err)
	}
	if len(scenarios) != 2 {
		t.Fatalf("expected 2 scenarios remaining (R4's), got %d", len(scenarios))
	}
	for _, s := range scenarios {
		if s.RequirementID != rid(slug, 4) {
			t.Errorf("orphaned scenario %q belongs to %q, want %q", s.ID, s.RequirementID, rid(slug, 4))
		}
	}
}

func TestHandleDeleteRequirement_LeafDeleteNoCollateral(t *testing.T) {
	slug := "cascade-leaf"
	setupCascadeFixture(t, slug)
	ctx := context.Background()

	c := setupTestComponent(t)

	// Delete R4 (independent leaf) — no cascade, only R4 removed.
	req := httptest.NewRequest(http.MethodDelete, "/plan-api/plans/"+slug+"/requirements/"+rid(slug, 4), nil)
	w := httptest.NewRecorder()
	c.handleDeleteRequirement(w, req, slug, rid(slug, 4))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	remaining, _ := workflow.LoadRequirements(ctx, nil, slug)
	if len(remaining) != 3 {
		t.Fatalf("expected 3 requirements remaining, got %d", len(remaining))
	}

	scenarios, _ := workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 6 {
		t.Fatalf("expected 6 scenarios remaining (R1+R2+R3), got %d", len(scenarios))
	}
}

func TestHandleDeleteRequirement_MiddleNodeCascade(t *testing.T) {
	slug := "cascade-middle"
	setupCascadeFixture(t, slug)
	ctx := context.Background()

	c := setupTestComponent(t)

	// Delete R2 — should cascade to R3 (depends on R2). R1 and R4 survive.
	req := httptest.NewRequest(http.MethodDelete, "/plan-api/plans/"+slug+"/requirements/"+rid(slug, 2), nil)
	w := httptest.NewRecorder()
	c.handleDeleteRequirement(w, req, slug, rid(slug, 2))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	remaining, _ := workflow.LoadRequirements(ctx, nil, slug)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 requirements remaining (R1+R4), got %d", len(remaining))
	}

	remainingIDs := map[string]bool{}
	for _, r := range remaining {
		remainingIDs[r.ID] = true
	}
	if !remainingIDs[rid(slug, 1)] || !remainingIDs[rid(slug, 4)] {
		t.Errorf("expected R1 and R4 to survive, got %v", remainingIDs)
	}

	scenarios, _ := workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 4 {
		t.Fatalf("expected 4 scenarios remaining (R1+R4), got %d", len(scenarios))
	}
}

// ---------------------------------------------------------------------------
// Cascade deprecate integration tests (HTTP handler)
// ---------------------------------------------------------------------------

func TestHandleDeprecateRequirement_CascadeDeprecatesDependents(t *testing.T) {
	slug := "cascade-deprecate"
	setupCascadeFixture(t, slug)
	ctx := context.Background()

	c := setupTestComponent(t)

	// Deprecate R1 — should cascade deprecate R2 and R3.
	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/requirements/"+rid(slug, 1)+"/deprecate", nil)
	w := httptest.NewRecorder()
	c.handleDeprecateRequirement(w, req, slug, rid(slug, 1))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// All 4 requirements should still exist (soft delete).
	reqs, _ := workflow.LoadRequirements(ctx, nil, slug)
	if len(reqs) != 4 {
		t.Fatalf("expected 4 requirements (soft delete preserves), got %d", len(reqs))
	}

	// R1, R2, R3 should be deprecated. R4 should be active.
	statusMap := map[string]workflow.RequirementStatus{}
	for _, r := range reqs {
		statusMap[r.ID] = r.Status
	}

	for _, id := range []string{rid(slug, 1), rid(slug, 2), rid(slug, 3)} {
		if statusMap[id] != workflow.RequirementStatusDeprecated {
			t.Errorf("%s status = %q, want deprecated", id, statusMap[id])
		}
	}
	if statusMap[rid(slug, 4)] != workflow.RequirementStatusActive {
		t.Errorf("R4 status = %q, want active", statusMap[rid(slug, 4)])
	}

	// Scenarios for R1, R2, R3 should be removed. R4's 2 scenarios remain.
	scenarios, _ := workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 2 {
		t.Fatalf("expected 2 scenarios remaining (R4's), got %d", len(scenarios))
	}
	for _, s := range scenarios {
		if s.RequirementID != rid(slug, 4) {
			t.Errorf("orphaned scenario %q belongs to %q", s.ID, s.RequirementID)
		}
	}
}

func TestHandleDeprecateRequirement_LeafNoCollateral(t *testing.T) {
	slug := "deprecate-leaf"
	setupCascadeFixture(t, slug)
	ctx := context.Background()

	c := setupTestComponent(t)

	// Deprecate R3 (leaf) — only R3 deprecated, R1/R2/R4 untouched.
	req := httptest.NewRequest(http.MethodPost, "/plan-api/plans/"+slug+"/requirements/"+rid(slug, 3)+"/deprecate", nil)
	w := httptest.NewRecorder()
	c.handleDeprecateRequirement(w, req, slug, rid(slug, 3))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	reqs, _ := workflow.LoadRequirements(ctx, nil, slug)
	activeCount := 0
	for _, r := range reqs {
		if r.Status == workflow.RequirementStatusActive {
			activeCount++
		}
	}
	if activeCount != 3 {
		t.Errorf("expected 3 active requirements, got %d", activeCount)
	}

	// R3's 2 scenarios removed, 6 remain.
	scenarios, _ := workflow.LoadScenarios(ctx, nil, slug)
	if len(scenarios) != 6 {
		t.Fatalf("expected 6 scenarios remaining, got %d", len(scenarios))
	}
}
