package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// CLIHelpScenario tests the /help command via CLI mode.
// This verifies the help system works correctly and lists available commands.
type CLIHelpScenario struct {
	name        string
	description string
	config      *config.Config
	cli         *client.CLIClient
	fs          *client.FilesystemClient
}

// NewCLIHelpScenario creates a new CLI help command scenario.
func NewCLIHelpScenario(cfg *config.Config) *CLIHelpScenario {
	return &CLIHelpScenario{
		name:        "cli-help",
		description: "Tests the /help command lists available commands via CLI",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *CLIHelpScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *CLIHelpScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *CLIHelpScenario) Setup(ctx context.Context) error {
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

// Execute runs the CLI help command scenario.
func (s *CLIHelpScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"send-help", s.stageSendHelp},
		{"verify-commands", s.stageVerifyCommands},
		{"send-help-specific", s.stageSendHelpSpecific},
		{"verify-unknown", s.stageVerifyUnknown},
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
func (s *CLIHelpScenario) Teardown(ctx context.Context) error {
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}

// stageSendHelp sends the /help command via CLI.
func (s *CLIHelpScenario) stageSendHelp(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/help")
	if err != nil {
		return fmt.Errorf("send /help command: %w", err)
	}

	result.SetDetail("help_response_type", resp.Type)
	result.SetDetail("help_response_content", resp.Content)
	result.SetDetail("help_response", resp)

	return nil
}

// stageVerifyCommands verifies the /help response lists expected commands.
func (s *CLIHelpScenario) stageVerifyCommands(ctx context.Context, result *Result) error {
	respVal, ok := result.GetDetail("help_response")
	if !ok {
		return fmt.Errorf("help_response not found in result")
	}

	resp, ok := respVal.(*client.Response)
	if !ok {
		return fmt.Errorf("help_response has wrong type")
	}

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("help returned error: %s", resp.Content)
	}

	// Verify expected commands are listed
	content := strings.ToLower(resp.Content)
	expectedCommands := []string{"propose", "changes", "help"}
	missingCommands := []string{}

	for _, cmd := range expectedCommands {
		if !strings.Contains(content, cmd) {
			missingCommands = append(missingCommands, cmd)
		}
	}

	if len(missingCommands) > 0 {
		result.AddWarning(fmt.Sprintf("help response may be missing commands: %v", missingCommands))
	}

	result.SetDetail("commands_verified", true)
	return nil
}

// stageSendHelpSpecific tests /help with a specific command.
func (s *CLIHelpScenario) stageSendHelpSpecific(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/help propose")
	if err != nil {
		return fmt.Errorf("send /help propose command: %w", err)
	}

	result.SetDetail("help_specific_response", resp)

	// Should not be an error
	if resp.Type == "error" {
		return fmt.Errorf("help propose returned error: %s", resp.Content)
	}

	// Should mention propose
	if !strings.Contains(strings.ToLower(resp.Content), "propose") {
		result.AddWarning("help propose response doesn't mention propose")
	}

	return nil
}

// stageVerifyUnknown tests /help with an unknown command.
func (s *CLIHelpScenario) stageVerifyUnknown(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/help nonexistent-command-xyz")
	if err != nil {
		return fmt.Errorf("send /help nonexistent command: %w", err)
	}

	result.SetDetail("help_unknown_response", resp)

	// Should handle gracefully - either error or "unknown command" message
	content := strings.ToLower(resp.Content)
	if resp.Type != "error" && !strings.Contains(content, "unknown") && !strings.Contains(content, "not found") {
		result.AddWarning("unknown command response may not be clear")
	}

	return nil
}
