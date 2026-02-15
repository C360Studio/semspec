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
// Tests: /plan → /approve → /execute (dry-run)
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
		description: "Tests /plan, /approve, /execute commands via CLI mode (ADR-003)",
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
		{"plan-create-draft", s.stagePlanCreateDraft},
		{"plan-verify-filesystem", s.stagePlanVerifyFilesystem},
		{"approve-plan", s.stageApprovePlan},
		{"approve-verify", s.stageApproveVerify},
		{"execute-dry-run", s.stageExecuteDryRun},
		{"plan-direct-create", s.stagePlanDirectCreate},
		{"plan-verify-approved", s.stagePlanDirectVerifyApproved},
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

// stagePlanCreateDraft sends the /plan command with --manual flag to create a draft plan.
func (s *CLIPlanWorkflowScenario) stagePlanCreateDraft(ctx context.Context, result *Result) error {
	planTitle := "authentication options"
	result.SetDetail("plan_title", planTitle)
	result.SetDetail("expected_slug", "authentication-options")

	// Use --manual to skip LLM and create draft plan
	resp, err := s.cli.SendCommand(ctx, "/plan "+planTitle+" --manual")
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
		strings.Contains(content, "authentication")

	if !hasPlanInfo {
		return fmt.Errorf("plan response doesn't contain expected info: %s", resp.Content)
	}

	return nil
}

// stagePlanVerifyFilesystem verifies the plan was created on the filesystem.
func (s *CLIPlanWorkflowScenario) stagePlanVerifyFilesystem(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Wait for change directory to exist
	if err := s.fs.WaitForChange(ctx, expectedSlug); err != nil {
		return fmt.Errorf("plan directory not created: %w", err)
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

	// Verify plan exists (approved field should be present)
	if _, ok := plan["approved"]; !ok {
		// Fall back to committed for backwards compatibility during migration
		if _, ok := plan["committed"]; !ok {
			return fmt.Errorf("plan.json missing 'approved' field")
		}
	}

	result.SetDetail("plan_verified", true)
	result.SetDetail("plan_id", plan["id"])
	return nil
}

// stageApprovePlan approves the plan to enable task generation.
func (s *CLIPlanWorkflowScenario) stageApprovePlan(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// First, edit the plan to add goal/context
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Add plan content for a realistic test
	plan["goal"] = "Explore OAuth, JWT, and session-based auth approaches"
	plan["context"] = "Need to evaluate authentication options for the API"
	plan["scope"] = map[string]any{
		"include": []string{"api/auth/*", "docs/auth.md"},
		"exclude": []string{"api/legacy/*"},
	}

	// Save updated plan
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	if err := s.fs.WriteFile(planPath, string(data)); err != nil {
		return fmt.Errorf("write plan.json: %w", err)
	}

	// Now approve the plan
	resp, err := s.cli.SendCommand(ctx, "/approve "+expectedSlug)
	if err != nil {
		return fmt.Errorf("send /approve command: %w", err)
	}

	result.SetDetail("approve_response_type", resp.Type)
	result.SetDetail("approve_response_content", resp.Content)
	result.SetDetail("approve_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("approve returned error: %s", resp.Content)
	}

	// Verify response contains approval confirmation
	content := strings.ToLower(resp.Content)
	hasApprovalInfo := strings.Contains(content, "approved") ||
		strings.Contains(content, "plan")

	if !hasApprovalInfo {
		return fmt.Errorf("approve response doesn't contain expected info: %s", resp.Content)
	}

	return nil
}

// stageApproveVerify verifies the plan is now approved.
func (s *CLIPlanWorkflowScenario) stageApproveVerify(ctx context.Context, result *Result) error {
	expectedSlug, _ := result.GetDetailString("expected_slug")

	// Load plan.json
	planPath := s.fs.ChangePath(expectedSlug) + "/plan.json"
	var plan map[string]any
	if err := s.fs.ReadJSON(planPath, &plan); err != nil {
		return fmt.Errorf("read plan.json: %w", err)
	}

	// Verify plan is now approved
	approved, ok := plan["approved"].(bool)
	if !ok {
		// Fall back to committed for backwards compatibility
		approved, ok = plan["committed"].(bool)
		if !ok {
			return fmt.Errorf("plan.json missing 'approved' field")
		}
	}
	if !approved {
		return fmt.Errorf("plan should be approved after /approve, but approved=false")
	}

	// Verify approved_at is set
	if plan["approved_at"] == nil && plan["committed_at"] == nil {
		return fmt.Errorf("plan.json missing 'approved_at' field")
	}

	result.SetDetail("approve_verified", true)
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

// stagePlanDirectCreate tests /plan --auto which creates an auto-approved plan.
func (s *CLIPlanWorkflowScenario) stagePlanDirectCreate(ctx context.Context, result *Result) error {
	planTitle := "implement caching layer"
	result.SetDetail("direct_plan_title", planTitle)
	result.SetDetail("direct_plan_slug", "implement-caching-layer")

	// Use --manual --auto to create an auto-approved plan without LLM
	resp, err := s.cli.SendCommand(ctx, "/plan "+planTitle+" --manual --auto")
	if err != nil {
		return fmt.Errorf("send /plan command: %w", err)
	}

	result.SetDetail("direct_plan_response_type", resp.Type)
	result.SetDetail("direct_plan_response_content", resp.Content)
	result.SetDetail("direct_plan_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("plan returned error: %s", resp.Content)
	}

	// Verify response contains plan confirmation
	content := strings.ToLower(resp.Content)
	hasPlanInfo := strings.Contains(content, "plan") ||
		strings.Contains(content, "created") ||
		strings.Contains(content, "approved")

	if !hasPlanInfo {
		return fmt.Errorf("plan response doesn't contain expected info: %s", resp.Content)
	}

	return nil
}

// stagePlanDirectVerifyApproved verifies the auto-approved plan is approved.
func (s *CLIPlanWorkflowScenario) stagePlanDirectVerifyApproved(ctx context.Context, result *Result) error {
	planSlug, _ := result.GetDetailString("direct_plan_slug")

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

	// Verify plan is approved (/plan --auto creates approved plans)
	approved, ok := plan["approved"].(bool)
	if !ok {
		// Fall back to committed for backwards compatibility
		approved, ok = plan["committed"].(bool)
		if !ok {
			return fmt.Errorf("plan.json missing 'approved' field")
		}
	}
	if !approved {
		return fmt.Errorf("/plan --auto should create approved plan, but approved=false")
	}

	result.SetDetail("direct_plan_verified", true)
	return nil
}
