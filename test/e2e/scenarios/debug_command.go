package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// DebugCommandScenario tests the trajectory-api endpoints for debugging.
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
		description: "Tests trajectory-api endpoints for trace correlation and debugging",
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
		{"trajectory-loop-missing", s.stageTrajectoryLoopMissing},
		{"trajectory-trace-missing", s.stageTrajectoryTraceMissing},
		{"find-trace-id", s.stageFindTraceID},
		{"trajectory-trace-query", s.stageTrajectoryTraceQuery},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

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
func (s *DebugCommandScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageTrajectoryLoopMissing tests trajectory-api with non-existent loop.
func (s *DebugCommandScenario) stageTrajectoryLoopMissing(ctx context.Context, result *Result) error {
	_, status, _ := s.http.GetTrajectoryByLoop(ctx, "nonexistent-loop-id-12345", false)

	// Should return 404 for non-existent loop
	if status != 404 {
		result.AddWarning(fmt.Sprintf("expected 404 for nonexistent loop, got %d", status))
	}

	result.SetDetail("loop_404_verified", status == 404)
	return nil
}

// stageTrajectoryTraceMissing tests trajectory-api with non-existent trace.
func (s *DebugCommandScenario) stageTrajectoryTraceMissing(ctx context.Context, result *Result) error {
	_, status, _ := s.http.GetTrajectoryByTrace(ctx, "00000000000000000000000000000000", false)

	// Should return 404 for non-existent trace
	if status != 404 {
		result.AddWarning(fmt.Sprintf("expected 404 for nonexistent trace, got %d", status))
	}

	result.SetDetail("trace_404_verified", status == 404)
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

	// Try looking in RawData for trace_id in BaseMessage meta
	for _, entry := range entries {
		var baseMsg struct {
			Meta struct {
				TraceID string `json:"trace_id"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(entry.RawData, &baseMsg); err == nil && baseMsg.Meta.TraceID != "" {
			result.SetDetail("trace_id", baseMsg.Meta.TraceID)
			result.SetDetail("trace_id_found", true)
			return nil
		}
	}

	result.SetDetail("trace_id", "")
	result.SetDetail("trace_id_found", false)
	return nil
}

// stageTrajectoryTraceQuery tests trajectory-api with a real or fake trace ID.
func (s *DebugCommandScenario) stageTrajectoryTraceQuery(ctx context.Context, result *Result) error {
	traceID, _ := result.GetDetailString("trace_id")
	traceIDFound, _ := result.GetDetail("trace_id_found")

	// If we found a real trace ID, query it
	if found, ok := traceIDFound.(bool); ok && found && traceID != "" {
		trajectory, status, err := s.http.GetTrajectoryByTrace(ctx, traceID, true)
		if err != nil {
			// 404 is acceptable if no trajectory data exists
			if status == 404 {
				result.SetDetail("trajectory_endpoint_available", true)
				result.SetDetail("trajectory_data_found", false)
				return nil
			}
			return fmt.Errorf("get trajectory by trace: %w", err)
		}

		result.SetDetail("trajectory_endpoint_available", true)
		result.SetDetail("trajectory_data_found", true)
		result.SetDetail("trajectory_trace_id", trajectory.TraceID)
		result.SetDetail("trajectory_steps", trajectory.Steps)
		result.SetDetail("trajectory_model_calls", trajectory.ModelCalls)
		return nil
	}

	// No trace ID found, test with a fake one to verify error handling
	_, status, _ := s.http.GetTrajectoryByTrace(ctx, "00000000000000000000000000000000", false)

	// Should handle gracefully (404 expected)
	result.SetDetail("trajectory_endpoint_available", status == 404 || status == 200)
	return nil
}
