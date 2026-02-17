package scenarios

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// NewDeveloperScenario tests the complete new developer experience:
// setup hello-world project → create plan → wait for LLM generation →
// verify plan quality → approve → generate tasks → verify tasks quality →
// capture trajectory data for provider comparison.
type NewDeveloperScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewNewDeveloperScenario creates a new developer experience scenario.
func NewNewDeveloperScenario(cfg *config.Config) *NewDeveloperScenario {
	return &NewDeveloperScenario{
		name:        "new-developer",
		description: "Tests complete new-developer workflow: plan → approve → tasks with LLM trajectory capture",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *NewDeveloperScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *NewDeveloperScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *NewDeveloperScenario) Setup(ctx context.Context) error {
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

// Execute runs the new developer scenario.
func (s *NewDeveloperScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name    string
		fn      func(context.Context, *Result) error
		timeout time.Duration
	}{
		{"setup-project", s.stageSetupProject, 30 * time.Second},
		{"create-plan", s.stageCreatePlan, 30 * time.Second},
		{"wait-for-plan", s.stageWaitForPlan, 180 * time.Second},
		{"verify-plan-quality", s.stageVerifyPlanQuality, 10 * time.Second},
		{"approve-plan", s.stageApprovePlan, 30 * time.Second},
		{"generate-tasks", s.stageGenerateTasks, 30 * time.Second},
		{"wait-for-tasks", s.stageWaitForTasks, 180 * time.Second},
		{"verify-tasks-quality", s.stageVerifyTasksQuality, 10 * time.Second},
		{"capture-trajectory", s.stageCaptureTrajectory, 30 * time.Second},
		{"generate-report", s.stageGenerateReport, 10 * time.Second},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, stage.timeout)
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
func (s *NewDeveloperScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageSetupProject creates a minimal Go hello-world project in the workspace.
func (s *NewDeveloperScenario) stageSetupProject(ctx context.Context, result *Result) error {
	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "main.go"), mainGo); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	goMod := `module hello-world

go 1.22
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "go.mod"), goMod); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	readme := `# Hello World

A minimal Go project for demonstrating semspec workflows.
`
	if err := s.fs.WriteFile(filepath.Join(s.config.WorkspacePath, "README.md"), readme); err != nil {
		return fmt.Errorf("write README.md: %w", err)
	}

	if err := s.fs.InitGit(); err != nil {
		return fmt.Errorf("init git: %w", err)
	}
	if err := s.fs.GitAdd("."); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := s.fs.GitCommit("Initial commit"); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	result.SetDetail("project_ready", true)
	return nil
}

// stageCreatePlan creates a plan via the REST API.
func (s *NewDeveloperScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	resp, err := s.http.CreatePlan(ctx, "add greeting personalization")
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	if resp.Slug == "" {
		return fmt.Errorf("plan creation returned empty slug")
	}

	result.SetDetail("plan_slug", resp.Slug)
	result.SetDetail("plan_response", resp)
	return nil
}

// stageWaitForPlan waits for the plan directory and plan.json to appear on disk
// with a non-empty "goal" field, indicating the planner LLM has finished generating.
func (s *NewDeveloperScenario) stageWaitForPlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlan(ctx, slug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}

	if err := s.fs.WaitForPlanFile(ctx, slug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Poll until plan.json has a non-empty "goal" field, meaning the LLM finished.
	// The file appears immediately with a skeleton, but Goal/Context/Scope are
	// populated asynchronously by the planner agent loop.
	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plan.json never received goal from LLM: %w", ctx.Err())
		case <-ticker.C:
			var plan map[string]any
			if err := s.fs.ReadJSON(planPath, &plan); err != nil {
				continue
			}
			if goal, ok := plan["goal"].(string); ok && goal != "" {
				result.SetDetail("plan_file_exists", true)
				return nil
			}
		}
	}
}

// stageVerifyPlanQuality reads plan.json and verifies it has meaningful content.
func (s *NewDeveloperScenario) stageVerifyPlanQuality(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	planPath := s.fs.DefaultProjectPlanPath(slug) + "/plan.json"

	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	if len(plan) == 0 {
		return fmt.Errorf("plan.json is empty")
	}

	// Verify the LLM populated the required fields
	goal, _ := plan["goal"].(string)
	if goal == "" {
		return fmt.Errorf("plan.json missing 'goal' field (LLM may not have finished)")
	}

	result.SetDetail("plan_id", plan["id"])
	result.SetDetail("plan_goal", goal)
	result.SetDetail("plan_data_present", true)
	return nil
}

// stageApprovePlan approves the plan via the REST API.
func (s *NewDeveloperScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.PromotePlan(ctx, slug)
	if err != nil {
		return fmt.Errorf("promote plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("promote returned error: %s", resp.Error)
	}

	result.SetDetail("approve_response", resp)
	return nil
}

// stageGenerateTasks triggers LLM-based task generation via the REST API.
func (s *NewDeveloperScenario) stageGenerateTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	resp, err := s.http.GenerateTasks(ctx, slug)
	if err != nil {
		return fmt.Errorf("generate tasks: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("generate tasks returned error: %s", resp.Error)
	}

	result.SetDetail("generate_response", resp)
	return nil
}

// stageWaitForTasks waits for tasks.json to be created by the LLM.
func (s *NewDeveloperScenario) stageWaitForTasks(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")

	if err := s.fs.WaitForPlanFile(ctx, slug, "tasks.json"); err != nil {
		return fmt.Errorf("tasks.json not created: %w", err)
	}

	return nil
}

// stageVerifyTasksQuality reads tasks.json and verifies it has at least one valid task.
func (s *NewDeveloperScenario) stageVerifyTasksQuality(ctx context.Context, result *Result) error {
	slug, _ := result.GetDetailString("plan_slug")
	tasksPath := s.fs.DefaultProjectPlanPath(slug) + "/tasks.json"

	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("tasks.json contains no tasks")
	}

	for i, task := range tasks {
		desc, ok := task["description"].(string)
		if !ok || desc == "" {
			return fmt.Errorf("task %d missing non-empty 'description' field", i)
		}
	}

	result.SetDetail("task_count", len(tasks))
	return nil
}

// stageCaptureTrajectory polls the LLM_CALLS KV bucket and retrieves trajectory data.
func (s *NewDeveloperScenario) stageCaptureTrajectory(ctx context.Context, result *Result) error {
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	var kvEntries *client.KVEntriesResponse
	var lastErr error

	// Poll until entries appear or context times out
	for kvEntries == nil {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v (last error: %v)", ctx.Err(), lastErr))
			} else {
				result.AddWarning(fmt.Sprintf("trajectory capture timed out: %v", ctx.Err()))
			}
			return nil
		case <-ticker.C:
			entries, err := s.http.GetKVEntries(ctx, "LLM_CALLS")
			if err != nil {
				lastErr = err
				continue
			}
			if len(entries.Entries) > 0 {
				kvEntries = entries
			}
		}
	}

	// Extract trace ID from the first key (format: trace_id.request_id)
	firstKey := kvEntries.Entries[0].Key
	parts := strings.SplitN(firstKey, ".", 2)
	if len(parts) < 2 {
		result.AddWarning(fmt.Sprintf("LLM_CALLS key %q doesn't contain trace prefix", firstKey))
		return nil
	}

	traceID := parts[0]
	result.SetDetail("trajectory_trace_id", traceID)

	// Query trajectory data by trace ID
	trajectory, statusCode, err := s.http.GetTrajectoryByTrace(ctx, traceID, true)
	if err != nil {
		if statusCode == 404 {
			result.AddWarning("trajectory-api returned 404 - component may not be enabled")
			return nil
		}
		return fmt.Errorf("get trajectory by trace: %w", err)
	}

	result.SetDetail("trajectory_model_calls", trajectory.ModelCalls)
	result.SetDetail("trajectory_tokens_in", trajectory.TokensIn)
	result.SetDetail("trajectory_tokens_out", trajectory.TokensOut)
	result.SetDetail("trajectory_duration_ms", trajectory.DurationMs)
	result.SetDetail("trajectory_entries_count", len(trajectory.Entries))
	return nil
}

// stageGenerateReport compiles a summary report with provider and trajectory data.
func (s *NewDeveloperScenario) stageGenerateReport(ctx context.Context, result *Result) error {
	providerName := os.Getenv(config.ProviderNameEnvVar)
	if providerName == "" {
		providerName = config.DefaultProviderName
	}

	taskCount, _ := result.GetDetail("task_count")
	modelCalls, _ := result.GetDetail("trajectory_model_calls")
	tokensIn, _ := result.GetDetail("trajectory_tokens_in")
	tokensOut, _ := result.GetDetail("trajectory_tokens_out")
	durationMs, _ := result.GetDetail("trajectory_duration_ms")

	result.SetDetail("provider", providerName)
	result.SetDetail("report", map[string]any{
		"provider":      providerName,
		"model_calls":   modelCalls,
		"tokens_in":     tokensIn,
		"tokens_out":    tokensOut,
		"duration_ms":   durationMs,
		"plan_created":  true,
		"tasks_created": taskCount,
	})
	return nil
}
