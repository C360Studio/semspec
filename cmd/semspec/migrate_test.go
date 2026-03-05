package main

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// setupMigrateTestPlan creates a plan with tasks in a temp repo and returns the manager.
func setupMigrateTestPlan(t *testing.T, slug string, tasks []workflow.Task) *workflow.Manager {
	t.Helper()
	ctx := context.Background()
	m := workflow.NewManager(t.TempDir())

	_, err := m.CreatePlan(ctx, slug, "Test Plan")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks() error = %v", err)
	}
	return m
}

// assertHappyPathRequirement verifies that the single migrated requirement has the expected
// ID, title, description, and status for the happy-path scenario.
func assertHappyPathRequirement(t *testing.T, ctx context.Context, m *workflow.Manager, slug string) {
	t.Helper()
	requirements, err := m.LoadRequirements(ctx, slug)
	if err != nil {
		t.Fatalf("LoadRequirements() error = %v", err)
	}
	if len(requirements) != 1 {
		t.Fatalf("len(requirements) = %d, want 1", len(requirements))
	}
	if requirements[0].ID != "requirement.migration-test.1" {
		t.Errorf("requirement ID = %q, want requirement.migration-test.1", requirements[0].ID)
	}
	wantTitle := "Requirement for: Implement authentication flow"
	if requirements[0].Title != wantTitle {
		t.Errorf("requirement Title = %q, want %q", requirements[0].Title, wantTitle)
	}
	if requirements[0].Description != "Implement authentication flow" {
		t.Errorf("requirement Description = %q", requirements[0].Description)
	}
	if requirements[0].Status != workflow.RequirementStatusActive {
		t.Errorf("requirement Status = %q, want active", requirements[0].Status)
	}
}

// assertHappyPathScenarios verifies the two migrated scenarios: shared requirement linkage,
// pending status, single-element Then slice, and correct Given values.
func assertHappyPathScenarios(t *testing.T, ctx context.Context, m *workflow.Manager, slug string) {
	t.Helper()
	scenarios, err := m.LoadScenarios(ctx, slug)
	if err != nil {
		t.Fatalf("LoadScenarios() error = %v", err)
	}
	if len(scenarios) != 2 {
		t.Fatalf("len(scenarios) = %d, want 2", len(scenarios))
	}
	for i, s := range scenarios {
		if s.RequirementID != "requirement.migration-test.1" {
			t.Errorf("scenario[%d].RequirementID = %q, want requirement.migration-test.1", i, s.RequirementID)
		}
		if s.Status != workflow.ScenarioStatusPending {
			t.Errorf("scenario[%d].Status = %q, want pending", i, s.Status)
		}
		if len(s.Then) != 1 {
			t.Errorf("scenario[%d].Then len = %d, want 1", i, len(s.Then))
		}
	}
	if scenarios[0].Given != "the system is ready" {
		t.Errorf("scenario[0].Given = %q", scenarios[0].Given)
	}
	if scenarios[1].Given != "a session exists" {
		t.Errorf("scenario[1].Given = %q", scenarios[1].Given)
	}
}

// assertHappyPathTasks verifies that the migrated task has its ScenarioIDs populated and
// AcceptanceCriteria cleared, and that the skipped task retains no ScenarioIDs.
func assertHappyPathTasks(t *testing.T, ctx context.Context, m *workflow.Manager, slug string) {
	t.Helper()
	updatedTasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		t.Fatalf("LoadTasks() error = %v", err)
	}
	if len(updatedTasks[0].ScenarioIDs) != 2 {
		t.Errorf("task[0].ScenarioIDs = %v, want 2 items", updatedTasks[0].ScenarioIDs)
	}
	if len(updatedTasks[0].AcceptanceCriteria) != 0 {
		t.Errorf("task[0].AcceptanceCriteria not cleared: %v", updatedTasks[0].AcceptanceCriteria)
	}
	if len(updatedTasks[1].ScenarioIDs) != 0 {
		t.Errorf("task[1].ScenarioIDs should be empty, got %v", updatedTasks[1].ScenarioIDs)
	}
}

func TestMigrateExtractScenarios_HappyPath(t *testing.T) {
	slug := "migration-test"
	tasks := []workflow.Task{
		{
			ID:     "task.migration-test.1",
			PlanID: workflow.PlanEntityID(slug),
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "the system is ready", When: "user logs in", Then: "a session is created"},
				{Given: "a session exists", When: "user logs out", Then: "the session is destroyed"},
			},
			Description: "Implement authentication flow",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "task.migration-test.2",
			PlanID:      workflow.PlanEntityID(slug),
			Description: "Write documentation",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
	}

	m := setupMigrateTestPlan(t, slug, tasks)
	ctx := context.Background()

	result, err := migrateExtractScenariosCtx(ctx, m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	if result.TasksMigrated != 1 {
		t.Errorf("TasksMigrated = %d, want 1", result.TasksMigrated)
	}
	if result.RequirementsCreated != 1 {
		t.Errorf("RequirementsCreated = %d, want 1", result.RequirementsCreated)
	}
	if result.ScenariosCreated != 2 {
		t.Errorf("ScenariosCreated = %d, want 2", result.ScenariosCreated)
	}
	if result.TasksSkipped != 1 {
		t.Errorf("TasksSkipped = %d, want 1 (task without criteria)", result.TasksSkipped)
	}

	assertHappyPathRequirement(t, ctx, m, slug)
	assertHappyPathScenarios(t, ctx, m, slug)
	assertHappyPathTasks(t, ctx, m, slug)
}

func TestMigrateExtractScenarios_Mixed(t *testing.T) {
	// 1 task with criteria, 1 without — only 1 migrated.
	slug := "migration-mixed"
	tasks := []workflow.Task{
		{
			ID:     "task.migration-mixed.1",
			PlanID: workflow.PlanEntityID(slug),
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "g", When: "w", Then: "t"},
			},
			Description: "Has criteria",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "task.migration-mixed.2",
			PlanID:      workflow.PlanEntityID(slug),
			Description: "No criteria",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
	}

	m := setupMigrateTestPlan(t, slug, tasks)

	result, err := migrateExtractScenariosCtx(context.Background(), m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	if result.TasksMigrated != 1 {
		t.Errorf("TasksMigrated = %d, want 1", result.TasksMigrated)
	}
	if result.TasksSkipped != 1 {
		t.Errorf("TasksSkipped = %d, want 1", result.TasksSkipped)
	}
	if result.RequirementsCreated != 1 {
		t.Errorf("RequirementsCreated = %d, want 1", result.RequirementsCreated)
	}
	if result.ScenariosCreated != 1 {
		t.Errorf("ScenariosCreated = %d, want 1", result.ScenariosCreated)
	}
}

func TestMigrateExtractScenarios_AlreadyMigrated(t *testing.T) {
	// Tasks with ScenarioIDs and no AcceptanceCriteria — all skipped.
	slug := "migration-already-done"
	tasks := []workflow.Task{
		{
			ID:          "task.migration-already-done.1",
			PlanID:      workflow.PlanEntityID(slug),
			Description: "Already migrated",
			ScenarioIDs: []string{"scenario.migration-already-done.1"},
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
		{
			ID:          "task.migration-already-done.2",
			PlanID:      workflow.PlanEntityID(slug),
			Description: "Also done",
			ScenarioIDs: []string{"scenario.migration-already-done.2"},
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
	}

	m := setupMigrateTestPlan(t, slug, tasks)

	result, err := migrateExtractScenariosCtx(context.Background(), m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	if result.TasksMigrated != 0 {
		t.Errorf("TasksMigrated = %d, want 0 (already migrated)", result.TasksMigrated)
	}
	if result.TasksSkipped != 2 {
		t.Errorf("TasksSkipped = %d, want 2", result.TasksSkipped)
	}
	if result.RequirementsCreated != 0 {
		t.Errorf("RequirementsCreated = %d, want 0", result.RequirementsCreated)
	}
	if result.ScenariosCreated != 0 {
		t.Errorf("ScenariosCreated = %d, want 0", result.ScenariosCreated)
	}
}

func TestMigrateExtractScenarios_EmptyPlan(t *testing.T) {
	// No tasks — 0 migrated.
	slug := "migration-empty"
	m := setupMigrateTestPlan(t, slug, []workflow.Task{})

	result, err := migrateExtractScenariosCtx(context.Background(), m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	if result.TasksMigrated != 0 {
		t.Errorf("TasksMigrated = %d, want 0", result.TasksMigrated)
	}
	if result.RequirementsCreated != 0 {
		t.Errorf("RequirementsCreated = %d, want 0", result.RequirementsCreated)
	}
	if result.ScenariosCreated != 0 {
		t.Errorf("ScenariosCreated = %d, want 0", result.ScenariosCreated)
	}
}

func TestMigrateExtractScenarios_MultipleCriteria(t *testing.T) {
	// 2 tasks with 3 criteria each → 2 requirements, 6 scenarios.
	slug := "migration-multi-criteria"
	tasks := []workflow.Task{
		{
			ID:     "task.migration-multi-criteria.1",
			PlanID: workflow.PlanEntityID(slug),
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "g1", When: "w1", Then: "t1"},
				{Given: "g2", When: "w2", Then: "t2"},
				{Given: "g3", When: "w3", Then: "t3"},
			},
			Description: "Task one",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
		{
			ID:     "task.migration-multi-criteria.2",
			PlanID: workflow.PlanEntityID(slug),
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "g4", When: "w4", Then: "t4"},
				{Given: "g5", When: "w5", Then: "t5"},
				{Given: "g6", When: "w6", Then: "t6"},
			},
			Description: "Task two",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
	}

	m := setupMigrateTestPlan(t, slug, tasks)
	ctx := context.Background()

	result, err := migrateExtractScenariosCtx(ctx, m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	if result.TasksMigrated != 2 {
		t.Errorf("TasksMigrated = %d, want 2", result.TasksMigrated)
	}
	if result.RequirementsCreated != 2 {
		t.Errorf("RequirementsCreated = %d, want 2", result.RequirementsCreated)
	}
	if result.ScenariosCreated != 6 {
		t.Errorf("ScenariosCreated = %d, want 6", result.ScenariosCreated)
	}

	updatedTasks, err := m.LoadTasks(ctx, slug)
	if err != nil {
		t.Fatalf("LoadTasks() error = %v", err)
	}
	if len(updatedTasks[0].ScenarioIDs) != 3 {
		t.Errorf("task[0].ScenarioIDs len = %d, want 3", len(updatedTasks[0].ScenarioIDs))
	}
	if len(updatedTasks[1].ScenarioIDs) != 3 {
		t.Errorf("task[1].ScenarioIDs len = %d, want 3", len(updatedTasks[1].ScenarioIDs))
	}

	// Each task's scenarios reference its own requirement, not the other's.
	reqs, _ := m.LoadRequirements(ctx, slug)
	scens, _ := m.LoadScenarios(ctx, slug)

	req1ID := reqs[0].ID
	req2ID := reqs[1].ID
	for _, s := range scens[:3] {
		if s.RequirementID != req1ID {
			t.Errorf("scenario %q.RequirementID = %q, want %q", s.ID, s.RequirementID, req1ID)
		}
	}
	for _, s := range scens[3:] {
		if s.RequirementID != req2ID {
			t.Errorf("scenario %q.RequirementID = %q, want %q", s.ID, s.RequirementID, req2ID)
		}
	}
}

func TestMigrateExtractScenarios_TitleTruncation(t *testing.T) {
	slug := "migration-truncation"
	longDesc := "This is a very long task description that exceeds eighty characters total length"
	tasks := []workflow.Task{
		{
			ID:     "task.migration-truncation.1",
			PlanID: workflow.PlanEntityID(slug),
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "g", When: "w", Then: "t"},
			},
			Description: longDesc,
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
	}

	m := setupMigrateTestPlan(t, slug, tasks)
	ctx := context.Background()

	_, err := migrateExtractScenariosCtx(ctx, m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	reqs, err := m.LoadRequirements(ctx, slug)
	if err != nil {
		t.Fatalf("LoadRequirements() error = %v", err)
	}
	if len(reqs[0].Title) > 80 {
		t.Errorf("title not truncated to 80 chars: len=%d, title=%q", len(reqs[0].Title), reqs[0].Title)
	}
	// Description is stored in full, not truncated.
	if reqs[0].Description != longDesc {
		t.Errorf("description was modified, got %q", reqs[0].Description)
	}
}

func TestMigrateExtractScenarios_PreservesExisting(t *testing.T) {
	slug := "migration-preserve"
	ctx := context.Background()
	m := workflow.NewManager(t.TempDir())

	_, err := m.CreatePlan(ctx, slug, "Preserve Test")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	// Seed pre-existing requirement and scenario.
	existingReq := workflow.Requirement{
		ID:        "requirement.migration-preserve.1",
		PlanID:    workflow.PlanEntityID(slug),
		Title:     "Pre-existing requirement",
		Status:    workflow.RequirementStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	existingScenario := workflow.Scenario{
		ID:            "scenario.migration-preserve.1",
		RequirementID: existingReq.ID,
		Given:         "existing given",
		When:          "existing when",
		Then:          []string{"existing then"},
		Status:        workflow.ScenarioStatusPending,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := m.SaveRequirements(ctx, []workflow.Requirement{existingReq}, slug); err != nil {
		t.Fatalf("SaveRequirements() error = %v", err)
	}
	if err := m.SaveScenarios(ctx, []workflow.Scenario{existingScenario}, slug); err != nil {
		t.Fatalf("SaveScenarios() error = %v", err)
	}

	tasks := []workflow.Task{
		{
			ID:     "task.migration-preserve.1",
			PlanID: workflow.PlanEntityID(slug),
			AcceptanceCriteria: []workflow.AcceptanceCriterion{
				{Given: "new g", When: "new w", Then: "new t"},
			},
			Description: "New task",
			Status:      workflow.TaskStatusPending,
			CreatedAt:   time.Now(),
		},
	}
	if err := m.SaveTasks(ctx, tasks, slug); err != nil {
		t.Fatalf("SaveTasks() error = %v", err)
	}

	result, err := migrateExtractScenariosCtx(ctx, m, slug)
	if err != nil {
		t.Fatalf("migrateExtractScenarios() error = %v", err)
	}

	if result.RequirementsCreated != 1 {
		t.Errorf("RequirementsCreated = %d, want 1", result.RequirementsCreated)
	}

	reqs, _ := m.LoadRequirements(ctx, slug)
	if len(reqs) != 2 {
		t.Errorf("len(requirements) = %d, want 2 (1 existing + 1 new)", len(reqs))
	}
	if reqs[0].ID != existingReq.ID {
		t.Errorf("existing requirement replaced, got ID %q", reqs[0].ID)
	}
	// New requirement sequence starts after existing count.
	if reqs[1].ID != "requirement.migration-preserve.2" {
		t.Errorf("new requirement ID = %q, want requirement.migration-preserve.2", reqs[1].ID)
	}

	scens, _ := m.LoadScenarios(ctx, slug)
	if len(scens) != 2 {
		t.Errorf("len(scenarios) = %d, want 2 (1 existing + 1 new)", len(scens))
	}
	if scens[0].ID != existingScenario.ID {
		t.Errorf("existing scenario replaced, got ID %q", scens[0].ID)
	}
	if scens[1].ID != "scenario.migration-preserve.2" {
		t.Errorf("new scenario ID = %q, want scenario.migration-preserve.2", scens[1].ID)
	}
}

func TestRunExtractScenarios_EmptyRepo(t *testing.T) {
	err := runExtractScenarios(context.Background(), t.TempDir())
	if err != nil {
		t.Errorf("runExtractScenarios() with empty repo error = %v", err)
	}
}

func TestRunExtractScenarios_AllPlans(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()
	m := workflow.NewManager(tmpDir)

	for _, slug := range []string{"plan-alpha", "plan-beta"} {
		_, err := m.CreatePlan(ctx, slug, slug)
		if err != nil {
			t.Fatalf("CreatePlan(%q) error = %v", slug, err)
		}
		tasks := []workflow.Task{
			{
				ID:     "task." + slug + ".1",
				PlanID: workflow.PlanEntityID(slug),
				AcceptanceCriteria: []workflow.AcceptanceCriterion{
					{Given: "g", When: "w", Then: "t"},
				},
				Description: "A task",
				Status:      workflow.TaskStatusPending,
				CreatedAt:   time.Now(),
			},
		}
		if err := m.SaveTasks(ctx, tasks, slug); err != nil {
			t.Fatalf("SaveTasks(%q) error = %v", slug, err)
		}
	}

	if err := runExtractScenarios(ctx, tmpDir); err != nil {
		t.Fatalf("runExtractScenarios() error = %v", err)
	}

	for _, slug := range []string{"plan-alpha", "plan-beta"} {
		reqs, err := m.LoadRequirements(ctx, slug)
		if err != nil {
			t.Fatalf("LoadRequirements(%q) error = %v", slug, err)
		}
		if len(reqs) != 1 {
			t.Errorf("plan %q: len(requirements) = %d, want 1", slug, len(reqs))
		}

		scens, err := m.LoadScenarios(ctx, slug)
		if err != nil {
			t.Fatalf("LoadScenarios(%q) error = %v", slug, err)
		}
		if len(scens) != 1 {
			t.Errorf("plan %q: len(scenarios) = %d, want 1", slug, len(scens))
		}
	}
}
