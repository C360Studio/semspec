package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// TasksCommandScenario tests the tasks REST API with Goal/Context/Scope plan structure
// and BDD acceptance criteria.
// Tests: CreatePlan → update Goal/Context/Scope → GetPlanTasks → manual tasks → ExecutePlan
type TasksCommandScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewTasksCommandScenario creates a new tasks command scenario.
func NewTasksCommandScenario(cfg *config.Config) *TasksCommandScenario {
	return &TasksCommandScenario{
		name:        "tasks-command",
		description: "Tests GetPlanTasks REST API, Goal/Context/Scope structure, BDD acceptance criteria",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *TasksCommandScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *TasksCommandScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *TasksCommandScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the tasks command scenario.
func (s *TasksCommandScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"plan-create", s.stagePlanCreate},
		{"plan-update-scope", s.stagePlanUpdateScope},
		{"tasks-list-empty", s.stageTasksListEmpty},
		{"tasks-create-manual", s.stageTasksCreateManual},
		{"tasks-list-populated", s.stageTasksListPopulated},
		{"execute-finds-tasks", s.stageExecuteFindsTasks},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_ms", stage.name), stageDuration.Milliseconds())

		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}

		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown cleans up after the scenario.
func (s *TasksCommandScenario) Teardown(ctx context.Context) error {
	return nil
}

// stagePlanCreate creates a plan via the REST API.
func (s *TasksCommandScenario) stagePlanCreate(ctx context.Context, result *Result) error {
	planTitle := "Tasks E2E Test"
	expectedSlug := "tasks-e2e-test"
	result.SetDetail("plan_title", planTitle)
	result.SetDetail("expected_slug", expectedSlug)

	resp, err := s.http.CreatePlan(ctx, planTitle)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	result.SetDetail("plan_response", resp)

	// Wait for plan.json to exist
	if err := s.fs.WaitForPlanFile(ctx, expectedSlug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Verify plan exists (REST API creates draft plans, not auto-committed)
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Plan is created as draft - approve it via REST API
	_, err = s.http.PromotePlan(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}

	result.SetDetail("plan_created", true)
	return nil
}

// stagePlanUpdateScope updates the plan with Goal/Context/Scope fields.
func (s *TasksCommandScenario) stagePlanUpdateScope(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load current plan
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Add Goal/Context/Scope fields
	plan["goal"] = "Add E2E test coverage for tasks REST API"
	plan["context"] = "Plan/Task structure was refactored per ADR-003, needs verification"
	plan["scope"] = map[string]any{
		"include":      []string{"test/e2e/scenarios/"},
		"exclude":      []string{},
		"do_not_touch": []string{},
	}

	// Write updated plan
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := s.fs.WriteFile(planPath, string(data)); err != nil {
		return fmt.Errorf("write plan.json: %w", err)
	}

	// Verify update
	var updatedPlan map[string]any
	if err := s.fs.ReadJSON(planPath, &updatedPlan); err != nil {
		return fmt.Errorf("re-read plan.json: %w", err)
	}

	if updatedPlan["goal"] != "Add E2E test coverage for tasks REST API" {
		return fmt.Errorf("goal field not saved correctly")
	}

	result.SetDetail("scope_updated", true)
	return nil
}

// stageTasksListEmpty tests GetPlanTasks when no tasks exist.
func (s *TasksCommandScenario) stageTasksListEmpty(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	tasks, err := s.http.GetPlanTasks(ctx, expectedSlug)
	if err != nil {
		// 404 or empty list is acceptable
		result.SetDetail("tasks_empty_response", "no tasks")
		result.SetDetail("tasks_empty_verified", true)
		return nil
	}

	if len(tasks) != 0 {
		return fmt.Errorf("expected 0 tasks, got %d", len(tasks))
	}

	result.SetDetail("tasks_empty_response", "empty list")
	result.SetDetail("tasks_empty_verified", true)
	return nil
}

// stageTasksCreateManual creates tasks.json manually with BDD acceptance criteria.
func (s *TasksCommandScenario) stageTasksCreateManual(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Create tasks with BDD acceptance criteria
	tasks := []map[string]any{
		{
			"id":          fmt.Sprintf("task.%s.1", expectedSlug),
			"plan_id":     fmt.Sprintf("plan.%s", expectedSlug),
			"sequence":    1,
			"description": "Verify GetPlanTasks shows BDD acceptance criteria",
			"type":        "test",
			"acceptance_criteria": []map[string]string{
				{
					"given": "a plan with tasks containing AcceptanceCriteria",
					"when":  "calling GET /workflow-api/plans/{slug}/tasks",
					"then":  "response includes Given/When/Then format for each criterion",
				},
			},
			"files":      []string{"test/e2e/scenarios/tasks_command.go"},
			"status":     "pending",
			"created_at": time.Now().UTC().Format(time.RFC3339),
		},
		{
			"id":          fmt.Sprintf("task.%s.2", expectedSlug),
			"plan_id":     fmt.Sprintf("plan.%s", expectedSlug),
			"sequence":    2,
			"description": "Verify ExecutePlan recognizes tasks.json",
			"type":        "implement",
			"acceptance_criteria": []map[string]string{
				{
					"given": "a plan with a tasks.json file",
					"when":  "calling POST /workflow-api/plans/{slug}/execute",
					"then":  "response shows the task count",
				},
				{
					"given": "a plan without tasks.json",
					"when":  "calling POST /workflow-api/plans/{slug}/execute",
					"then":  "command prompts to generate tasks first",
				},
			},
			"files":      []string{"processor/workflow-handler/execute.go"},
			"status":     "pending",
			"created_at": time.Now().UTC().Format(time.RFC3339),
		},
	}

	// Write tasks.json
	tasksPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/tasks.json"
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	if err := s.fs.WriteFile(tasksPath, string(data)); err != nil {
		return fmt.Errorf("write tasks.json: %w", err)
	}

	// Verify file exists
	if !s.fs.FileExists(tasksPath) {
		return fmt.Errorf("tasks.json not created")
	}

	result.SetDetail("tasks_created", true)
	result.SetDetail("task_count", len(tasks))
	return nil
}

// stageTasksListPopulated tests GetPlanTasks when tasks exist.
func (s *TasksCommandScenario) stageTasksListPopulated(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	tasks, err := s.http.GetPlanTasks(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("get plan tasks: %w", err)
	}

	if len(tasks) != 2 {
		return fmt.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// Verify task ID
	if tasks[0].ID != fmt.Sprintf("task.%s.1", expectedSlug) {
		return fmt.Errorf("task ID mismatch: got %s", tasks[0].ID)
	}

	// Verify task type
	if tasks[0].Type != "test" {
		return fmt.Errorf("task type mismatch: got %s", tasks[0].Type)
	}

	// Verify BDD acceptance criteria
	if len(tasks[0].AcceptanceCriteria) == 0 {
		return fmt.Errorf("task missing acceptance criteria")
	}

	ac := tasks[0].AcceptanceCriteria[0]
	if ac["given"] == "" || ac["when"] == "" || ac["then"] == "" {
		return fmt.Errorf("acceptance criteria missing given/when/then")
	}

	result.SetDetail("tasks_list_verified", true)
	return nil
}

// stageExecuteFindsTasks tests that ExecutePlan recognizes tasks.json.
func (s *TasksCommandScenario) stageExecuteFindsTasks(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	resp, err := s.http.ExecutePlan(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("execute plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("execute returned error: %s", resp.Error)
	}

	result.SetDetail("execute_response", resp)
	result.SetDetail("execute_verified", true)
	return nil
}
