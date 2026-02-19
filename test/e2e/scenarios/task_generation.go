package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// TaskGenerationScenario tests the GenerateTasks REST API which triggers
// the task-generator component to use LLM for creating tasks with BDD criteria.
// This scenario requires Ollama to be running on the host machine.
type TaskGenerationScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	llmClient   *http.Client
}

// NewTaskGenerationScenario creates a new task generation scenario.
func NewTaskGenerationScenario(cfg *config.Config) *TaskGenerationScenario {
	return &TaskGenerationScenario{
		name:        "task-generation",
		description: "Tests GenerateTasks REST API with LLM: creates tasks.json with BDD criteria",
		config:      cfg,
		llmClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Name returns the scenario name.
func (s *TaskGenerationScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *TaskGenerationScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *TaskGenerationScenario) Setup(ctx context.Context) error {
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

	// Check if LLM (Ollama) is available
	if !s.isLLMAvailable() {
		return fmt.Errorf("LLM not available at localhost:11434 - start Ollama to run this test")
	}

	return nil
}

// isLLMAvailable checks if the LLM endpoint is reachable.
func (s *TaskGenerationScenario) isLLMAvailable() bool {
	resp, err := s.llmClient.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Execute runs the task generation scenario.
func (s *TaskGenerationScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"create-plan", s.stageCreatePlan},
		{"add-goal-context-scope", s.stageAddGoalContextScope},
		{"trigger-task-generation", s.stageTriggerTaskGeneration},
		{"wait-for-tasks", s.stageWaitForTasks},
		{"verify-bdd-criteria", s.stageVerifyBDDCriteria},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		// Use a longer timeout for LLM stages
		stageTimeout := s.config.StageTimeout
		if stage.name == "wait-for-tasks" {
			stageTimeout = 120 * time.Second // LLM can take a while
		}
		stageCtx, cancel := context.WithTimeout(ctx, stageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())

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
func (s *TaskGenerationScenario) Teardown(_ context.Context) error {
	return nil
}

// stageCreatePlan creates a plan via the REST API.
func (s *TaskGenerationScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	planTitle := "LLM Task Generation Test"
	expectedSlug := "llm-task-generation-test"
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

// stageAddGoalContextScope updates the plan with Goal/Context/Scope fields.
func (s *TaskGenerationScenario) stageAddGoalContextScope(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load current plan
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Add Goal/Context/Scope fields required for task generation
	plan["goal"] = "Add user authentication with JWT tokens for secure API access"
	plan["context"] = "The current API endpoints are unauthenticated. We need to protect sensitive endpoints with JWT-based authentication including login, token refresh, and logout functionality."
	plan["scope"] = map[string]any{
		"include":      []string{"api/auth/", "api/middleware/"},
		"exclude":      []string{"api/public/"},
		"do_not_touch": []string{"api/health.go"},
	}

	// Write updated plan
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := s.fs.WriteFile(planPath, string(data)); err != nil {
		return fmt.Errorf("write plan.json: %w", err)
	}

	result.SetDetail("scope_updated", true)
	return nil
}

// stageTriggerTaskGeneration calls GenerateTasks REST API to trigger task generation.
func (s *TaskGenerationScenario) stageTriggerTaskGeneration(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Call GenerateTasks which triggers the task-generator component
	resp, err := s.http.GenerateTasks(ctx, expectedSlug)
	if err != nil {
		return fmt.Errorf("generate tasks: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("generate tasks returned error: %s", resp.Error)
	}

	result.SetDetail("generate_response", resp)
	result.SetDetail("generation_triggered", true)
	return nil
}

// stageWaitForTasks waits for tasks.json to be created by the LLM.
func (s *TaskGenerationScenario) stageWaitForTasks(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")
	tasksPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/tasks.json"

	// Poll for tasks.json to be created
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for tasks.json to be created by LLM")
		case <-ticker.C:
			if s.fs.FileExists(tasksPath) {
				// Read and validate it's not empty
				var tasks []map[string]any
				if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
					// File exists but not valid JSON yet, keep waiting
					continue
				}
				if len(tasks) > 0 {
					result.SetDetail("task_count", len(tasks))
					result.SetDetail("tasks_created", true)
					return nil
				}
			}
		}
	}
}

// stageVerifyBDDCriteria verifies that tasks have proper BDD acceptance criteria.
func (s *TaskGenerationScenario) stageVerifyBDDCriteria(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")
	tasksPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/tasks.json"

	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no tasks generated")
	}

	// Check that at least one task has acceptance criteria with given/when/then
	foundBDD := false
	var taskWithBDD string

	for _, task := range tasks {
		ac, ok := task["acceptance_criteria"].([]any)
		if !ok || len(ac) == 0 {
			continue
		}

		for _, criterion := range ac {
			c, ok := criterion.(map[string]any)
			if !ok {
				continue
			}

			given, hasGiven := c["given"].(string)
			when, hasWhen := c["when"].(string)
			then, hasThen := c["then"].(string)

			if hasGiven && hasWhen && hasThen && given != "" && when != "" && then != "" {
				foundBDD = true
				desc, _ := task["description"].(string)
				taskWithBDD = desc
				break
			}
		}

		if foundBDD {
			break
		}
	}

	if !foundBDD {
		// Log first task for debugging
		if len(tasks) > 0 {
			taskJSON, _ := json.MarshalIndent(tasks[0], "", "  ")
			result.SetDetail("first_task", string(taskJSON))
		}
		return fmt.Errorf("no tasks have BDD acceptance criteria (given/when/then)")
	}

	result.SetDetail("bdd_verified", true)
	result.SetDetail("task_with_bdd", taskWithBDD)

	// Additional validations
	for i, task := range tasks {
		// Each task should have required fields
		if _, ok := task["id"].(string); !ok {
			return fmt.Errorf("task %d missing 'id' field", i)
		}
		if _, ok := task["description"].(string); !ok {
			return fmt.Errorf("task %d missing 'description' field", i)
		}
		if _, ok := task["status"].(string); !ok {
			return fmt.Errorf("task %d missing 'status' field", i)
		}
	}

	// Verify task IDs follow expected pattern
	firstTask := tasks[0]
	taskID, _ := firstTask["id"].(string)
	if !strings.Contains(taskID, expectedSlug) {
		result.AddWarning(fmt.Sprintf("task ID '%s' doesn't contain slug '%s'", taskID, expectedSlug))
	}

	result.SetDetail("all_tasks_valid", true)
	return nil
}
