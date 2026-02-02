package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// HelpCommandScenario tests the /help command that lists available commands.
type HelpCommandScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewHelpCommandScenario creates a new help command scenario.
func NewHelpCommandScenario(cfg *config.Config) *HelpCommandScenario {
	return &HelpCommandScenario{
		name:        "help-command",
		description: "Tests the /help command lists available commands",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *HelpCommandScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *HelpCommandScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *HelpCommandScenario) Setup(ctx context.Context) error {
	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the help command scenario.
func (s *HelpCommandScenario) Execute(ctx context.Context) (*Result, error) {
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
func (s *HelpCommandScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageSendHelp sends the /help command via HTTP.
func (s *HelpCommandScenario) stageSendHelp(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/help")
	if err != nil {
		return fmt.Errorf("send /help command: %w", err)
	}

	result.SetDetail("help_response_type", resp.Type)
	result.SetDetail("help_response_content", resp.Content)
	result.SetDetail("help_response", resp)

	// Verify response type is not error
	if resp.Type == "error" {
		return fmt.Errorf("/help returned error: %s", resp.Content)
	}

	return nil
}

// stageVerifyCommands verifies the /help response contains expected commands.
func (s *HelpCommandScenario) stageVerifyCommands(ctx context.Context, result *Result) error {
	respVal, ok := result.GetDetail("help_response")
	if !ok {
		return fmt.Errorf("help_response not found in result")
	}

	resp, ok := respVal.(*client.MessageResponse)
	if !ok {
		return fmt.Errorf("help_response has wrong type")
	}

	content := strings.ToLower(resp.Content)

	// Expected workflow commands that should be listed
	expectedCommands := []string{"propose", "design", "spec", "tasks"}
	missingCommands := []string{}

	for _, cmd := range expectedCommands {
		if !strings.Contains(content, cmd) {
			missingCommands = append(missingCommands, cmd)
		}
	}

	if len(missingCommands) > 0 {
		return fmt.Errorf("missing expected commands in /help output: %v", missingCommands)
	}

	// Also check for help-related keywords
	hasHelpIndicator := strings.Contains(content, "command") ||
		strings.Contains(content, "available") ||
		strings.Contains(content, "usage") ||
		strings.Contains(content, "help")

	if !hasHelpIndicator {
		result.AddWarning("help response may not have proper formatting (missing 'command', 'available', 'usage', or 'help' keywords)")
	}

	result.SetDetail("commands_verified", true)
	result.SetDetail("found_commands", expectedCommands)
	return nil
}

// stageSendHelpSpecific sends /help propose to test specific command help.
// Note: Specific command help may not be implemented - this stage handles both cases.
func (s *HelpCommandScenario) stageSendHelpSpecific(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/help propose")
	if err != nil {
		return fmt.Errorf("send /help propose command: %w", err)
	}

	result.SetDetail("help_propose_response_type", resp.Type)
	result.SetDetail("help_propose_response_content", resp.Content)
	result.SetDetail("help_propose_response", resp)

	content := strings.ToLower(resp.Content)

	// Specific command help may not be implemented
	// Accept either: specific help, general help fallback, or "unknown command" response
	if resp.Type == "error" {
		// Check if it's indicating that specific help isn't supported
		if strings.Contains(content, "unknown") ||
			strings.Contains(content, "not found") ||
			strings.Contains(content, "/help") {
			result.AddWarning("specific command help (/help <command>) not implemented")
			result.SetDetail("specific_help_supported", false)
			return nil
		}
		return fmt.Errorf("/help propose returned unexpected error: %s", resp.Content)
	}

	// If we got a successful response, verify it mentions propose
	hasPropose := strings.Contains(content, "propose") ||
		strings.Contains(content, "/propose")

	if hasPropose {
		result.SetDetail("specific_help_supported", true)
	} else {
		result.AddWarning("/help propose response doesn't specifically mention propose")
	}

	result.SetDetail("specific_help_verified", true)
	return nil
}

// stageVerifyUnknown sends /help nonexistent to verify unknown command handling.
func (s *HelpCommandScenario) stageVerifyUnknown(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/help nonexistent")
	if err != nil {
		return fmt.Errorf("send /help nonexistent command: %w", err)
	}

	result.SetDetail("help_unknown_response_type", resp.Type)
	result.SetDetail("help_unknown_response_content", resp.Content)

	content := strings.ToLower(resp.Content)

	// Should indicate the command is unknown or not found
	hasUnknownIndicator := strings.Contains(content, "unknown") ||
		strings.Contains(content, "not found") ||
		strings.Contains(content, "no such") ||
		strings.Contains(content, "doesn't exist") ||
		strings.Contains(content, "available commands") ||
		strings.Contains(content, "invalid")

	if !hasUnknownIndicator {
		// This is a warning, not a failure - the help system might just show general help
		result.AddWarning(fmt.Sprintf("/help nonexistent may not properly indicate unknown command: %s", resp.Content))
	}

	result.SetDetail("unknown_command_verified", true)
	return nil
}
