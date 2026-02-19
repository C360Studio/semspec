package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// PlanLLMScenario tests the plan creation with LLM behavior which triggers
// the planner processor to generate Goal/Context/Scope using LLM.
// This scenario requires Ollama to be running on the host machine.
type PlanLLMScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
	llmClient   *http.Client
}

// NewPlanLLMScenario creates a new plan LLM scenario.
func NewPlanLLMScenario(cfg *config.Config) *PlanLLMScenario {
	return &PlanLLMScenario{
		name:        "plan-llm",
		description: "Tests CreatePlan with LLM: planner processor generates Goal/Context/Scope",
		config:      cfg,
		llmClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Name returns the scenario name.
func (s *PlanLLMScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *PlanLLMScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *PlanLLMScenario) Setup(ctx context.Context) error {
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
func (s *PlanLLMScenario) isLLMAvailable() bool {
	resp, err := s.llmClient.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Execute runs the plan LLM scenario.
func (s *PlanLLMScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"create-plan", s.stageCreatePlan},
		{"wait-for-plan-populated", s.stageWaitForPlanPopulated},
		{"verify-goal-context-scope", s.stageVerifyGoalContextScope},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		// Use longer timeout for LLM stages
		stageTimeout := s.config.StageTimeout
		if stage.name == "wait-for-plan-populated" {
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
func (s *PlanLLMScenario) Teardown(_ context.Context) error {
	return nil
}

// stageCreatePlan creates a plan via REST API (LLM is now the default behavior).
func (s *PlanLLMScenario) stageCreatePlan(ctx context.Context, result *Result) error {
	planTitle := "Add user authentication with JWT"
	expectedSlug := "add-user-authentication-with-jwt"
	result.SetDetail("plan_title", planTitle)
	result.SetDetail("expected_slug", expectedSlug)

	// Create plan via REST API
	resp, err := s.http.CreatePlan(ctx, planTitle)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("plan creation returned error: %s", resp.Error)
	}

	result.SetDetail("plan_response", resp)

	// Verify plan was created (it should exist immediately)
	if err := s.fs.WaitForPlanFile(ctx, expectedSlug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	result.SetDetail("plan_created", true)
	return nil
}

// stageWaitForPlanPopulated waits for plan.json to be populated with Goal/Context by LLM.
func (s *PlanLLMScenario) stageWaitForPlanPopulated(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"

	// Poll for plan.json to have Goal populated
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for plan to be populated by LLM")
		case <-ticker.C:
			var plan map[string]any
			if err := s.fs.ReadJSON(planPath, &plan); err != nil {
				continue
			}

			goal, hasGoal := plan["goal"].(string)
			planContext, hasContext := plan["context"].(string)

			// Check if Goal is populated (mandatory)
			if hasGoal && goal != "" {
				result.SetDetail("goal", goal)
				if hasContext && planContext != "" {
					result.SetDetail("context", planContext)
				}
				result.SetDetail("plan_populated", true)
				return nil
			}
		}
	}
}

// stageVerifyGoalContextScope verifies the plan has proper Goal/Context/Scope structure.
func (s *PlanLLMScenario) stageVerifyGoalContextScope(_ context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")
	planPath := s.fs.DefaultProjectPlanPath(expectedSlug) + "/plan.json"

	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Verify Goal is present and non-empty
	goal, hasGoal := plan["goal"].(string)
	if !hasGoal || goal == "" {
		planJSON, _ := json.MarshalIndent(plan, "", "  ")
		result.SetDetail("plan_json", string(planJSON))
		return fmt.Errorf("plan missing 'goal' field")
	}
	result.SetDetail("goal", goal)

	// Context should be present (may be empty for very simple plans)
	if context, ok := plan["context"].(string); ok {
		result.SetDetail("context", context)
	}

	// Check scope structure if present
	if scope, ok := plan["scope"].(map[string]any); ok {
		result.SetDetail("has_scope", true)
		if include, ok := scope["include"].([]any); ok {
			result.SetDetail("scope_include_count", len(include))
		}
		if exclude, ok := scope["exclude"].([]any); ok {
			result.SetDetail("scope_exclude_count", len(exclude))
		}
		if doNotTouch, ok := scope["do_not_touch"].([]any); ok {
			result.SetDetail("scope_protected_count", len(doNotTouch))
		}
	}

	// Verify the plan is committed
	committed, ok := plan["committed"].(bool)
	if !ok || !committed {
		return fmt.Errorf("plan should be committed")
	}
	result.SetDetail("committed", true)

	// Verify required fields
	if _, ok := plan["id"].(string); !ok {
		return fmt.Errorf("plan missing 'id' field")
	}
	if _, ok := plan["slug"].(string); !ok {
		return fmt.Errorf("plan missing 'slug' field")
	}
	if _, ok := plan["title"].(string); !ok {
		return fmt.Errorf("plan missing 'title' field")
	}

	result.SetDetail("all_fields_valid", true)
	return nil
}
