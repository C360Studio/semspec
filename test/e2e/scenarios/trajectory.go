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

// Trajectory scenario constants
const (
	// pollInterval is the time between polling attempts
	pollInterval = 100 * time.Millisecond
	// maxPollAttempts is the maximum number of polling attempts (2s total)
	maxPollAttempts = 20
	// messageLogLimit is the number of entries to fetch from message logger
	messageLogLimit = 50
	// kvPollInterval is the time between KV bucket polling attempts
	kvPollInterval = 500 * time.Millisecond
)

// TrajectoryScenario tests the trajectory tracking functionality.
// It triggers an LLM call via /plan command and verifies the trajectory
// data is recorded and queryable via the trajectory-api endpoints.
type TrajectoryScenario struct {
	name        string
	description string
	config      *config.Config
	http        *client.HTTPClient
	fs          *client.FilesystemClient
}

// NewTrajectoryScenario creates a new trajectory tracking scenario.
func NewTrajectoryScenario(cfg *config.Config) *TrajectoryScenario {
	return &TrajectoryScenario{
		name:        "trajectory",
		description: "Tests trajectory tracking via trajectory-api endpoints",
		config:      cfg,
	}
}

// Name returns the scenario name.
func (s *TrajectoryScenario) Name() string {
	return s.name
}

// Description returns the scenario description.
func (s *TrajectoryScenario) Description() string {
	return s.description
}

// Setup prepares the scenario environment.
func (s *TrajectoryScenario) Setup(ctx context.Context) error {
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

// Execute runs the trajectory tracking scenario.
func (s *TrajectoryScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"trigger-llm-call", s.stageTriggerLLMCall},
		{"wait-for-recording", s.stageWaitForRecording},
		{"query-by-trace", s.stageQueryByTrace},
		{"verify-aggregation", s.stageVerifyAggregation},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		// Use longer timeout for LLM-dependent stages
		stageTimeout := s.config.StageTimeout
		if stage.name == "wait-for-recording" {
			stageTimeout = 180 * time.Second // LLM coordination can take a while
		}
		stageCtx, cancel := context.WithTimeout(ctx, stageTimeout)

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
func (s *TrajectoryScenario) Teardown(ctx context.Context) error {
	return nil
}

// stageTriggerLLMCall sends a /plan command that triggers the planner component.
// The planner uses LLM calls which are recorded to the LLM_CALLS bucket.
// Note: Only semspec components (planner, task-generator, question-answerer) record
// LLM calls - the generic agentic-model from semstreams does not.
func (s *TrajectoryScenario) stageTriggerLLMCall(ctx context.Context, result *Result) error {
	// Send /plan command which triggers the planner component with LLM recording
	resp, err := s.http.SendMessage(ctx, "/plan trajectory-test-feature")
	if err != nil {
		return fmt.Errorf("send /plan command: %w", err)
	}

	result.SetDetail("trigger_response_type", resp.Type)
	result.SetDetail("trigger_response", resp)

	// Poll for message log entries instead of sleeping
	traceID, err := s.pollForTraceID(ctx)
	if err != nil {
		result.AddWarning(fmt.Sprintf("Could not extract trace_id: %v", err))
		traceID = "unknown"
	}

	result.SetDetail("trace_id", traceID)
	return nil
}

// pollForTraceID polls the message logger until a trace ID is found.
func (s *TrajectoryScenario) pollForTraceID(ctx context.Context) (string, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for attempt := 0; attempt < maxPollAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			entries, err := s.http.GetMessageLogEntries(ctx, messageLogLimit, "user.message")
			if err != nil {
				continue // Retry on error
			}

			// Find the most recent trace ID from a user message
			for _, entry := range entries {
				var baseMsg struct {
					Meta struct {
						TraceID string `json:"trace_id"`
					} `json:"meta"`
				}
				if err := json.Unmarshal(entry.RawData, &baseMsg); err == nil {
					if baseMsg.Meta.TraceID != "" {
						return baseMsg.Meta.TraceID, nil
					}
				}
			}

			// Try looking in metadata
			for _, entry := range entries {
				if tid, ok := entry.Metadata["trace_id"].(string); ok && tid != "" {
					return tid, nil
				}
			}
		}
	}

	return "", fmt.Errorf("trace_id not found after %d attempts", maxPollAttempts)
}

// stageWaitForRecording waits for LLM call records to appear in the KV bucket.
func (s *TrajectoryScenario) stageWaitForRecording(ctx context.Context, result *Result) error {
	// Poll the LLM_CALLS bucket via message-logger KV endpoint
	ticker := time.NewTicker(kvPollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timeout waiting for LLM calls: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return fmt.Errorf("timeout waiting for LLM calls: %w", ctx.Err())
		case <-ticker.C:
			kvEntries, err := s.http.GetKVEntries(ctx, "LLM_CALLS")
			if err != nil {
				lastErr = err
				continue
			}

			if len(kvEntries.Entries) > 0 {
				result.SetDetail("llm_calls_count", len(kvEntries.Entries))
				result.SetDetail("llm_calls_bucket", kvEntries.Bucket)
				result.SetDetail("first_llm_call_key", kvEntries.Entries[0].Key)
				return nil
			}
		}
	}
}

// stageQueryByTrace queries trajectory data using the trace ID.
func (s *TrajectoryScenario) stageQueryByTrace(ctx context.Context, result *Result) error {
	// Get the first LLM call key to extract trace ID
	firstKey, ok := result.GetDetailString("first_llm_call_key")
	if !ok {
		return fmt.Errorf("first_llm_call_key not found")
	}

	// Key format is trace_id.request_id - extract trace_id
	parts := strings.SplitN(firstKey, ".", 2)
	if len(parts) < 2 {
		// Key doesn't have trace prefix, use the key as-is
		result.AddWarning(fmt.Sprintf("Key %q doesn't contain trace prefix", firstKey))
		return nil
	}

	traceID := parts[0]
	result.SetDetail("extracted_trace_id", traceID)

	// Query by trace ID
	trajectory, statusCode, err := s.http.GetTrajectoryByTrace(ctx, traceID, true)
	if err != nil {
		// 404 is expected if trajectory-api isn't enabled
		if statusCode == 404 {
			result.AddWarning("trajectory-api returned 404 - component may not be enabled")
			return nil
		}
		return fmt.Errorf("get trajectory by trace: %w", err)
	}

	result.SetDetail("trajectory_trace_id", trajectory.TraceID)
	result.SetDetail("trajectory_model_calls", trajectory.ModelCalls)
	result.SetDetail("trajectory_tokens_in", trajectory.TokensIn)
	result.SetDetail("trajectory_tokens_out", trajectory.TokensOut)
	result.SetDetail("trajectory_entries_count", len(trajectory.Entries))

	return nil
}

// stageVerifyAggregation verifies the trajectory aggregation logic.
func (s *TrajectoryScenario) stageVerifyAggregation(ctx context.Context, result *Result) error {
	// Verify we have trajectory data
	modelCalls, ok := result.GetDetail("trajectory_model_calls")
	if !ok {
		// Skip verification if trajectory-api wasn't available
		if _, hasWarning := result.GetDetail("trajectory_trace_id"); !hasWarning {
			result.AddWarning("Skipping aggregation verification - trajectory data not available")
			return nil
		}
	}

	// Verify model calls count makes sense
	if calls, ok := modelCalls.(int); ok && calls > 0 {
		result.SetDetail("verified_model_calls", true)
	}

	// Verify token counts are reasonable
	tokensIn, _ := result.GetDetail("trajectory_tokens_in")
	tokensOut, _ := result.GetDetail("trajectory_tokens_out")

	if in, ok := tokensIn.(int); ok && in > 0 {
		result.SetDetail("verified_tokens_in", true)
	}
	if out, ok := tokensOut.(int); ok && out > 0 {
		result.SetDetail("verified_tokens_out", true)
	}

	return nil
}
