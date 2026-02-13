package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// CLIPlanWorkflowScenario tests the ADR-003 workflow commands via CLI mode.
// Tests: /explore → /promote → /execute (dry-run)
// Also tests: /plan direct creation
type CLIPlanWorkflowScenario struct {
	name        string
	description string
	config      *config.Config
	cli         *client.CLIClient
	fs          *client.FilesystemClient
}

// NewCLIPlanWorkflowScenario creates a new CLI plan workflow scenario.
func NewCLIPlanWorkflowScenario(cfg *config.Config) *CLIPlanWorkflowScenario {
	return &CLIPlanWorkflowScenario{
		name:        "cli-plan-workflow",
		description: "Tests /explore, /promote, /plan, /execute commands via CLI mode (ADR-003)",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *CLIPlanWorkflowScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *CLIPlanWorkflowScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *CLIPlanWorkflowScenario) Setup(ctx context.Context) error {
	// Create filesystem client and setup workspace
	s.fs = client.NewFilesystemClient(s.config.WorkspacePath)
	if err := s.fs.SetupWorkspace(); err != nil {
		return fmt.Errorf("setup workspace: %w", err)
	}

	// Create CLI client
	var err error
	s.cli, err = client.NewCLIClient(s.config.BinaryPath, s.config.ConfigPath, s.config.WorkspacePath)
	if err != nil {
		return fmt.Errorf("create CLI client: %w", err)
	}

	// Start CLI process
	if err := s.cli.Start(ctx); err != nil {
		return fmt.Errorf("start CLI: %w", err)
	}

	// Wait for CLI to be ready
	if err := s.cli.WaitForReady(ctx); err != nil {
		return fmt.Errorf("CLI not ready: %w", err)
	}

	return nil
}

// Execute runs the CLI plan workflow scenario.
func (s *CLIPlanWorkflowScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"explore-create", s.stageExploreCreate},
		{"explore-verify-filesystem", s.stageExploreVerifyFilesystem},
		{"promote-exploration", s.stagePromoteExploration},
		{"promote-verify-committed", s.stagePromoteVerifyCommitted},
		{"execute-dry-run", s.stageExecuteDryRun},
		{"plan-direct-create", s.stagePlanDirectCreate},
		{"plan-verify-committed", s.stagePlanVerifyCommitted},
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
func (s *CLIPlanWorkflowScenario) Teardown(ctx context.Context) error {
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}

// stageExploreCreate sends the /explore command to create an exploration.
func (s *CLIPlanWorkflowScenario) stageExploreCreate(ctx context.Context, result *Result) error {
	explorationTopic := "authentication options"
	result.SetDetail("exploration_topic", explorationTopic)
	result.SetDetail("expected_slug", "authentication-options")

	resp, err := s.cli.SendCommand(ctx, "/explore "+explorationTopic)
	if err != nil {
		return fmt.Errorf("send /explore command: %w", err)
	}

	result.SetDetail("explore_response_type", resp.Type)
	result.SetDetail("explore_response_content", resp.Content)
	result.SetDetail("explore_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("explore returned error: %s", resp.Content)
	}

	// Verify response contains exploration confirmation
	content := strings.ToLower(resp.Content)
	hasExplorationInfo := strings.Contains(content, "exploration") ||
		strings.Contains(content, "created") ||
		strings.Contains(content, "authentication")

	if !hasExplorationInfo {
		return fmt.Errorf("explore response doesn't contain expected info: %s", resp.Content)
	}

	return nil
}

// stageExploreVerifyFilesystem verifies the exploration was created on the filesystem.
func (s *CLIPlanWorkflowScenario) stageExploreVerifyFilesystem(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Wait for change directory to exist
	if err := s.fs.WaitForChange(ctx, expectedSlug); err != nil {
		return fmt.Errorf("exploration directory not created: %w", err)
	}

	// Verify plan.json exists
	if err := s.fs.WaitForChangeFile(ctx, expectedSlug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Load and verify plan.json
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Verify plan is uncommitted
	committed, ok := plan["committed"].(bool)
	if !ok {
		return fmt.Errorf("plan.json missing 'committed' field")
	}
	if committed {
		return fmt.Errorf("exploration should be uncommitted, but committed=true")
	}

	result.SetDetail("explore_verified", true)
	result.SetDetail("plan_id", plan["id"])
	return nil
}

// stagePromoteExploration promotes the exploration to a committed plan.
func (s *CLIPlanWorkflowScenario) stagePromoteExploration(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// First, edit the plan to add execution steps
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Add SMEAC content for a realistic test
	plan["situation"] = "Need to evaluate authentication options for the API"
	plan["mission"] = "Explore OAuth, JWT, and session-based auth approaches"
	plan["execution"] = "1. Research OAuth2 providers\n2. Evaluate JWT libraries\n3. Compare session storage options\n4. Write comparison document"

	// Save updated plan
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := s.fs.WriteFile(planPath, string(data)); err != nil {
		return fmt.Errorf("write plan.json: %w", err)
	}

	// Now promote
	resp, err := s.cli.SendCommand(ctx, "/promote "+expectedSlug)
	if err != nil {
		return fmt.Errorf("send /promote command: %w", err)
	}

	result.SetDetail("promote_response_type", resp.Type)
	result.SetDetail("promote_response_content", resp.Content)
	result.SetDetail("promote_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("promote returned error: %s", resp.Content)
	}

	// Verify response contains promotion confirmation
	content := strings.ToLower(resp.Content)
	hasPromotionInfo := strings.Contains(content, "committed") ||
		strings.Contains(content, "plan") ||
		strings.Contains(content, "promoted")

	if !hasPromotionInfo {
		return fmt.Errorf("promote response doesn't contain expected info: %s", resp.Content)
	}

	return nil
}

// stagePromoteVerifyCommitted verifies the plan is now committed.
func (s *CLIPlanWorkflowScenario) stagePromoteVerifyCommitted(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load plan.json
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Verify plan is now committed
	committed, ok := plan["committed"].(bool)
	if !ok {
		return fmt.Errorf("plan.json missing 'committed' field")
	}
	if !committed {
		return fmt.Errorf("plan should be committed after /promote, but committed=false")
	}

	// Verify committed_at is set
	if plan["committed_at"] == nil {
		return fmt.Errorf("plan.json missing 'committed_at' field")
	}

	result.SetDetail("promote_verified", true)
	return nil
}

// stageExecuteDryRun tests /execute without --run flag (dry run).
func (s *CLIPlanWorkflowScenario) stageExecuteDryRun(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	resp, err := s.cli.SendCommand(ctx, "/execute "+expectedSlug)
	if err != nil {
		return fmt.Errorf("send /execute command: %w", err)
	}

	result.SetDetail("execute_response_type", resp.Type)
	result.SetDetail("execute_response_content", resp.Content)
	result.SetDetail("execute_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("execute returned error: %s", resp.Content)
	}

	// Verify response contains task information
	content := strings.ToLower(resp.Content)
	hasTaskInfo := strings.Contains(content, "task") ||
		strings.Contains(content, "generated") ||
		strings.Contains(content, "execution")

	if !hasTaskInfo {
		return fmt.Errorf("execute response doesn't contain task info: %s", resp.Content)
	}

	// Verify tasks.json was created
	if err := s.fs.WaitForChangeFile(ctx, expectedSlug, "tasks.json"); err != nil {
		return fmt.Errorf("tasks.json not created: %w", err)
	}

	// Load and verify tasks
	tasksPath := s.fs.ChangePath(expectedSlug) + "/tasks.json"
	var tasks []map[string]any
	if err := s.fs.ReadJSON(tasksPath, &tasks); err != nil {
		return fmt.Errorf("read tasks.json: %w", err)
	}

	// We added 4 execution steps, should have 4 tasks
	if len(tasks) != 4 {
		return fmt.Errorf("expected 4 tasks, got %d", len(tasks))
	}

	result.SetDetail("execute_verified", true)
	result.SetDetail("task_count", len(tasks))
	return nil
}

// stagePlanDirectCreate tests /plan which creates a committed plan directly.
func (s *CLIPlanWorkflowScenario) stagePlanDirectCreate(ctx context.Context, result *Result) error {
	planTitle := "implement caching layer"
	result.SetDetail("plan_title", planTitle)
	result.SetDetail("plan_slug", "implement-caching-layer")

	resp, err := s.cli.SendCommand(ctx, "/plan "+planTitle)
	if err != nil {
		return fmt.Errorf("send /plan command: %w", err)
	}

	result.SetDetail("plan_response_type", resp.Type)
	result.SetDetail("plan_response_content", resp.Content)
	result.SetDetail("plan_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("plan returned error: %s", resp.Content)
	}

	// Verify response contains plan confirmation
	content := strings.ToLower(resp.Content)
	hasPlanInfo := strings.Contains(content, "plan") ||
		strings.Contains(content, "created") ||
		strings.Contains(content, "committed")

	if !hasPlanInfo {
		return fmt.Errorf("plan response doesn't contain expected info: %s", resp.Content)
	}

	return nil
}

// stagePlanVerifyCommitted verifies the directly created plan is committed.
func (s *CLIPlanWorkflowScenario) stagePlanVerifyCommitted(ctx context.Context, result *Result) error {
	planSlug, _ := result.GetDetailString("plan_slug")

	// Wait for change directory to exist
	if err := s.fs.WaitForChange(ctx, planSlug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
	}

	// Verify plan.json exists
	if err := s.fs.WaitForChangeFile(ctx, planSlug, "plan.json"); err != nil {
		return fmt.Errorf("plan.json not created: %w", err)
	}

	// Load and verify plan.json
	planPath := s.fs.ChangePath(planSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Verify plan is committed (direct /plan creates committed plans)
	committed, ok := plan["committed"].(bool)
	if !ok {
		return fmt.Errorf("plan.json missing 'committed' field")
	}
	if !committed {
		return fmt.Errorf("/plan should create committed plan, but committed=false")
	}

	result.SetDetail("plan_direct_verified", true)
	return nil
}
