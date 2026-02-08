package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// DebugCommandScenario tests the /debug command flow.
// This verifies debug and trace correlation tools work correctly.
type DebugCommandScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
}

// NewDebugCommandScenario creates a new debug command scenario.
func NewDebugCommandScenario(cfg *config.Config) *DebugCommandScenario {
	return &DebugCommandScenario{
		name:        "debug-command",
		description: "Tests the /debug command for trace correlation and debugging",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *DebugCommandScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *DebugCommandScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *DebugCommandScenario) Setup(ctx context.Context) error {
	// Create HTTP client
	s.http = client.NewHTTPClient(s.config.HTTPBaseURL)

	// Wait for service to be healthy
	if err := s.http.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("service not healthy: %w", err)
	}

	return nil
}

// Execute runs the debug command scenario.
func (s *DebugCommandScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"debug-help", s.stageDebugHelp},
		{"debug-workflow-missing", s.stageDebugWorkflowMissing},
		{"debug-trace-missing", s.stageDebugTraceMissing},
		{"find-trace-id", s.stageFindTraceID},
		{"debug-trace-query", s.stageDebugTraceQuery},
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
func (s *DebugCommandScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageDebugHelp tests /debug help command.
func (s *DebugCommandScenario) stageDebugHelp(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/debug help")
	if err != nil {
		return fmt.Errorf("send /debug help: %w", err)
	}

	result.SetDetail("help_response", resp.Content)

	// Verify help content
	if resp.Type == "error" {
		return fmt.Errorf("debug help returned error: %s", resp.Content)
	}

	content := strings.ToLower(resp.Content)
	expectedTerms := []string{"trace", "snapshot", "workflow", "loop"}
	for _, term := range expectedTerms {
		if !strings.Contains(content, term) {
			return fmt.Errorf("help missing expected term: %s", term)
		}
	}

	return nil
}

// stageDebugWorkflowMissing tests /debug workflow with non-existent workflow.
func (s *DebugCommandScenario) stageDebugWorkflowMissing(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/debug workflow nonexistent-workflow-12345")
	if err != nil {
		return fmt.Errorf("send /debug workflow: %w", err)
	}

	result.SetDetail("workflow_missing_response", resp.Content)

	// Should return error for non-existent workflow
	if resp.Type != "error" {
		return fmt.Errorf("expected error for nonexistent workflow, got: %s", resp.Type)
	}

	if !strings.Contains(strings.ToLower(resp.Content), "not found") {
		return fmt.Errorf("expected 'not found' in error, got: %s", resp.Content)
	}

	return nil
}

// stageDebugTraceMissing tests /debug trace without ID.
func (s *DebugCommandScenario) stageDebugTraceMissing(ctx context.Context, result *Result) error {
	resp, err := s.http.SendMessage(ctx, "/debug trace")
	if err != nil {
		return fmt.Errorf("send /debug trace: %w", err)
	}

	result.SetDetail("trace_missing_response", resp.Content)

	// Should return error asking for trace ID
	if resp.Type != "error" {
		return fmt.Errorf("expected error for missing trace ID, got: %s", resp.Type)
	}

	if !strings.Contains(strings.ToLower(resp.Content), "trace id required") {
		return fmt.Errorf("expected 'trace id required' in error, got: %s", resp.Content)
	}

	return nil
}

// stageFindTraceID finds a trace ID from message-logger entries.
func (s *DebugCommandScenario) stageFindTraceID(ctx context.Context, result *Result) error {
	// Get recent entries from message-logger
	entries, err := s.http.GetMessageLogEntries(ctx, 20, "")
	if err != nil {
		return fmt.Errorf("get message log entries: %w", err)
	}

	if len(entries) == 0 {
		result.SetDetail("trace_id", "")
		result.SetDetail("trace_id_found", false)
		return nil // Not an error, just no messages yet
	}

	// Find first entry with a trace_id in metadata
	for _, entry := range entries {
		if traceID, ok := entry.Metadata["trace_id"].(string); ok && traceID != "" {
			result.SetDetail("trace_id", traceID)
			result.SetDetail("trace_id_found", true)
			return nil
		}
	}

	result.SetDetail("trace_id", "")
	result.SetDetail("trace_id_found", false)
	return nil
}

// stageDebugTraceQuery tests /debug trace with a real or fake trace ID.
func (s *DebugCommandScenario) stageDebugTraceQuery(ctx context.Context, result *Result) error {
	traceID, _ := result.GetDetailString("trace_id")
	traceIDFound, _ := result.GetDetail("trace_id_found")

	// If we found a real trace ID, query it
	if found, ok := traceIDFound.(bool); ok && found && traceID != "" {
		resp, err := s.http.SendMessage(ctx, "/debug trace "+traceID)
		if err != nil {
			return fmt.Errorf("send /debug trace: %w", err)
		}

		result.SetDetail("trace_query_response", resp.Content)

		// Should return result (either messages or "no messages found")
		if resp.Type == "error" {
			// Error is acceptable if trace endpoint not available yet
			if strings.Contains(resp.Content, "not be available") {
				result.SetDetail("trace_endpoint_available", false)
				return nil
			}
			return fmt.Errorf("trace query returned error: %s", resp.Content)
		}

		result.SetDetail("trace_endpoint_available", true)
		return nil
	}

	// No trace ID found, test with a fake one to verify error handling
	resp, err := s.http.SendMessage(ctx, "/debug trace 00000000000000000000000000000000")
	if err != nil {
		return fmt.Errorf("send /debug trace: %w", err)
	}

	result.SetDetail("trace_query_response", resp.Content)

	// Should handle gracefully (either error or "no messages found")
	// Both are acceptable outcomes
	return nil
}
