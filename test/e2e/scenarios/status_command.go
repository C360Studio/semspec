package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360/semspec/test/e2e/client"
	"github.com/c360/semspec/test/e2e/config"
)

// StatusCommandScenario tests the /changes command flow.
// This is the simplest path that verifies basic HTTP communication works.
type StatusCommandScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewStatusCommandScenario creates a new status command scenario.
func NewStatusCommandScenario(cfg *config.Config) *StatusCommandScenario {
	return &StatusCommandScenario{
		name:        "status-command",
		description: "Tests the /changes command via HTTP gateway",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *StatusCommandScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *StatusCommandScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *StatusCommandScenario) Setup(ctx context.Context) error {
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

// Execute runs the status command scenario.
func (s *StatusCommandScenario) Execute(ctx context.Context) (*Result, error) {
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
func (s *StatusCommandScenario) Teardown(ctx context.Context) error {
	// HTTP client doesn't need cleanup
	return nil
}

// stageSendStatus sends the /changes command via HTTP.
func (s *StatusCommandScenario) stageSendStatus(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/changes")
	if err != nil {
		return fmt.Errorf("send /changes command: %w", err)
	}

	result.SetDetail("response_type", resp.Type)
	result.SetDetail("response_content", resp.Content)
	result.SetDetail("status_response", resp)

	return nil
}

// stageVerifyResponse verifies the /changes response content.
func (s *StatusCommandScenario) stageVerifyResponse(ctx context.Context, result *Result) error {
	respVal, ok := result.GetDetail("status_response")
	if !ok {
		return fmt.Errorf("status_response not found in result")
	}

	resp, ok := respVal.(*client.MessageResponse)
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
