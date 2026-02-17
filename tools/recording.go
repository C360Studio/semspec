// Package tools provides file and git operation tools for the Semspec agent.
package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// MaxRecordedParamsLength is the max length for serialized parameters stored in a record.
const MaxRecordedParamsLength = 1000

// MaxRecordedResultLength is the max length for result content stored in a record.
const MaxRecordedResultLength = 2000

// RecordingExecutor wraps a ToolExecutor and records each call to the global ToolCallStore.
// If the global store is not initialized, calls pass through transparently without recording.
type RecordingExecutor struct {
	inner  agentictools.ToolExecutor
	logger *slog.Logger
}

// NewRecordingExecutor wraps an executor with tool call recording.
func NewRecordingExecutor(inner agentictools.ToolExecutor) *RecordingExecutor {
	return &RecordingExecutor{
		inner:  inner,
		logger: slog.Default(),
	}
}

// Execute runs the underlying tool executor and records the call to the ToolCallStore.
func (r *RecordingExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	startedAt := time.Now()

	// Execute the actual tool
	result, execErr := r.inner.Execute(ctx, call)

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()

	// Record the call asynchronously to avoid slowing down tool execution
	go r.recordCall(call, result, execErr, startedAt, completedAt, durationMs)

	return result, execErr
}

// ListTools delegates to the inner executor.
func (r *RecordingExecutor) ListTools() []agentic.ToolDefinition {
	return r.inner.ListTools()
}

// recordCall stores the tool execution record in the global ToolCallStore.
func (r *RecordingExecutor) recordCall(
	call agentic.ToolCall,
	result agentic.ToolResult,
	execErr error,
	startedAt, completedAt time.Time,
	durationMs int64,
) {
	store := llm.GlobalToolCallStore()
	if store == nil {
		return // Recording disabled - store not initialized
	}

	// Determine status
	status := "success"
	var errMsg string
	if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
	} else if result.Error != "" {
		status = "error"
		errMsg = result.Error
	}

	// Serialize parameters (truncated)
	params := truncateJSON(call.Arguments, MaxRecordedParamsLength)

	// Truncate result content
	resultPreview := result.Content
	if len(resultPreview) > MaxRecordedResultLength {
		resultPreview = resultPreview[:MaxRecordedResultLength] + "..."
	}

	record := &llm.ToolCallRecord{
		CallID:      call.ID,
		ToolName:    call.Name,
		Parameters:  params,
		Result:      resultPreview,
		Status:      status,
		Error:       errMsg,
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		DurationMs:  durationMs,
		// TraceID and LoopID are not available at the executor level.
		// They exist in the BaseMessage envelope which is handled by the
		// agentic-tools component in semstreams. For the alpha, tool calls
		// are stored by call_id and correlated via message-logger traces.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := store.Store(ctx, record); err != nil {
		r.logger.Warn("Failed to record tool call",
			"tool", call.Name,
			"call_id", call.ID,
			"error", err)
	}
}

// truncateJSON marshals a map to JSON and truncates to maxLen.
func truncateJSON(m map[string]any, maxLen int) string {
	if m == nil {
		return "{}"
	}

	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}

	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
