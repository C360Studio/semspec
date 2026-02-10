package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// CLIDebugScenario tests the /debug command via CLI mode.
// This verifies the debug infrastructure works for trace correlation.
type CLIDebugScenario struct {
	name        string
	description string
	config      *config.Config
	cli         *client.CLIClient
	fs          *client.FilesystemClient
}

// NewCLIDebugScenario creates a new CLI debug command scenario.
func NewCLIDebugScenario(cfg *config.Config) *CLIDebugScenario {
	return &CLIDebugScenario{
		name:        "cli-debug",
		description: "Tests the /debug command for trace correlation via CLI",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *CLIDebugScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *CLIDebugScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *CLIDebugScenario) Setup(ctx context.Context) error {
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

// Execute runs the CLI debug command scenario.
func (s *CLIDebugScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"debug-help", s.stageDebugHelp},
		{"debug-workflow-missing", s.stageDebugWorkflowMissing},
		{"debug-trace-missing", s.stageDebugTraceMissing},
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
func (s *CLIDebugScenario) Teardown(ctx context.Context) error {
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}

// stageDebugHelp tests /debug help to list subcommands.
func (s *CLIDebugScenario) stageDebugHelp(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/debug help")
	if err != nil {
		return fmt.Errorf("send /debug help command: %w", err)
	}

	result.SetDetail("debug_help_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("debug help returned error: %s", resp.Content)
	}

	// Verify expected subcommands are listed
	content := strings.ToLower(resp.Content)
	expectedSubcommands := []string{"trace", "workflow"}
	missingCommands := []string{}

	for _, cmd := range expectedSubcommands {
		if !strings.Contains(content, cmd) {
			missingCommands = append(missingCommands, cmd)
		}
	}

	if len(missingCommands) > 0 {
		result.AddWarning(fmt.Sprintf("debug help may be missing subcommands: %v", missingCommands))
	}

	return nil
}

// stageDebugWorkflowMissing tests /debug workflow with non-existent workflow.
func (s *CLIDebugScenario) stageDebugWorkflowMissing(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/debug workflow nonexistent-workflow-xyz")
	if err != nil {
		return fmt.Errorf("send /debug workflow command: %w", err)
	}

	result.SetDetail("debug_workflow_response", resp)

	// Should handle gracefully - error or "not found" message
	content := strings.ToLower(resp.Content)
	if resp.Type != "error" && !strings.Contains(content, "not found") && !strings.Contains(content, "no workflow") {
		result.AddWarning("missing workflow response may not be clear")
	}

	return nil
}

// stageDebugTraceMissing tests /debug trace with non-existent trace ID.
func (s *CLIDebugScenario) stageDebugTraceMissing(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/debug trace nonexistent-trace-id-12345")
	if err != nil {
		return fmt.Errorf("send /debug trace command: %w", err)
	}

	result.SetDetail("debug_trace_response", resp)

	// Should handle gracefully - error or "not found" / "no messages" response
	content := strings.ToLower(resp.Content)
	if resp.Type != "error" && !strings.Contains(content, "not found") && !strings.Contains(content, "no messages") && !strings.Contains(content, "no entries") {
		result.AddWarning("missing trace response may not be clear")
	}

	return nil
}
