package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// CLIStatusScenario tests the /changes command via CLI mode.
// This verifies basic CLI communication and backend processing work correctly.
type CLIStatusScenario struct {
	name        string
	description string
	config      *config.Config
	cli         *client.CLIClient
	fs          *client.FilesystemClient
}

// NewCLIStatusScenario creates a new CLI status command scenario.
func NewCLIStatusScenario(cfg *config.Config) *CLIStatusScenario {
	return &CLIStatusScenario{
		name:        "cli-status",
		description: "Tests the /changes command via CLI mode",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *CLIStatusScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *CLIStatusScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *CLIStatusScenario) Setup(ctx context.Context) error {
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

// Execute runs the CLI status command scenario.
func (s *CLIStatusScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"send-status", s.stageSendStatus},
		{"verify-response", s.stageVerifyResponse},
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
func (s *CLIStatusScenario) Teardown(ctx context.Context) error {
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}

// stageSendStatus sends the /changes command via CLI.
func (s *CLIStatusScenario) stageSendStatus(ctx context.Context, result *Result) error {
	resp, err := s.cli.SendCommand(ctx, "/changes")
	if err != nil {
		return fmt.Errorf("send /changes command: %w", err)
	}

	result.SetDetail("response_type", resp.Type)
	result.SetDetail("response_content", resp.Content)
	result.SetDetail("status_response", resp)

	return nil
}

// stageVerifyResponse verifies the /changes response content.
func (s *CLIStatusScenario) stageVerifyResponse(ctx context.Context, result *Result) error {
	respVal, ok := result.GetDetail("status_response")
	if !ok {
		return fmt.Errorf("status_response not found in result")
	}

	resp, ok := respVal.(*client.Response)
	if !ok {
		return fmt.Errorf("status_response has wrong type")
	}

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("status returned error: %s", resp.Content)
	}

	// Verify response contains status information
	content := strings.ToLower(resp.Content)
	hasStatusInfo := strings.Contains(content, "status") ||
		strings.Contains(content, "change") ||
		strings.Contains(content, "no active") ||
		strings.Contains(content, "propose")

	if !hasStatusInfo {
		result.AddWarning(fmt.Sprintf("status response may not contain expected info: %s", resp.Content))
	}

	result.SetDetail("response_validated", true)
	return nil
}
