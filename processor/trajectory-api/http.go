package trajectoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/nats-io/nats.go/jetstream"
)

// RegisterHTTPHandlers registers HTTP handlers for the trajectory-api component.
// The prefix may or may not include trailing slash.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Ensure prefix has trailing slash
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}
	mux.HandleFunc(prefix+"loops/", c.handleGetLoopTrajectory)
	mux.HandleFunc(prefix+"traces/", c.handleGetTraceTrajectory)
}

// Trajectory represents aggregated data about an agent loop's LLM interactions.
type Trajectory struct {
	// LoopID is the agent loop identifier.
	LoopID string `json:"loop_id"`

	// TraceID is the trace correlation identifier.
	TraceID string `json:"trace_id,omitempty"`

	// Steps is the total number of iterations in the loop.
	Steps int `json:"steps"`

	// ToolCalls is the number of tool calls made.
	ToolCalls int `json:"tool_calls"`

	// ModelCalls is the number of LLM calls made.
	ModelCalls int `json:"model_calls"`

	// TokensIn is the total input tokens across all calls.
	TokensIn int `json:"tokens_in"`

	// TokensOut is the total output tokens across all calls.
	TokensOut int `json:"tokens_out"`

	// DurationMs is the total duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Status is the loop status (running, completed, failed).
	Status string `json:"status,omitempty"`

	// StartedAt is when the loop started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// EndedAt is when the loop ended (if completed).
	EndedAt *time.Time `json:"ended_at,omitempty"`

	// Entries contains the detailed trajectory entries (only if format=json).
	Entries []TrajectoryEntry `json:"entries,omitempty"`
}

// TrajectoryEntry represents a single event in the trajectory.
type TrajectoryEntry struct {
	// Type is the entry type (model_call, tool_call).
	Type string `json:"type"`

	// Timestamp is when this entry occurred.
	Timestamp time.Time `json:"timestamp"`

	// DurationMs is how long this entry took.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Model is the model used (for model_call).
	Model string `json:"model,omitempty"`

	// Provider is the provider used (for model_call).
	Provider string `json:"provider,omitempty"`

	// Capability is the requested capability (for model_call).
	Capability string `json:"capability,omitempty"`

	// TokensIn is input tokens (for model_call).
	TokensIn int `json:"tokens_in,omitempty"`

	// TokensOut is output tokens (for model_call).
	TokensOut int `json:"tokens_out,omitempty"`

	// FinishReason is why the model stopped (for model_call).
	FinishReason string `json:"finish_reason,omitempty"`

	// Error is any error message.
	Error string `json:"error,omitempty"`

	// Retries is number of retry attempts (for model_call).
	Retries int `json:"retries,omitempty"`

	// MessagesCount is the number of messages sent (for model_call).
	MessagesCount int `json:"messages_count,omitempty"`

	// ResponsePreview is a truncated preview of the response (for model_call).
	ResponsePreview string `json:"response_preview,omitempty"`

	// ToolName is the tool that was executed (for tool_call).
	ToolName string `json:"tool_name,omitempty"`

	// Status is the execution result status (for tool_call: "success", "error").
	Status string `json:"status,omitempty"`

	// ResultPreview is a truncated preview of the tool result (for tool_call).
	ResultPreview string `json:"result_preview,omitempty"`
}

// LoopState represents the agent loop state from AGENT_LOOPS bucket.
type LoopState struct {
	ID        string     `json:"id"`
	TraceID   string     `json:"trace_id,omitempty"`
	Status    string     `json:"status"`
	Role      string     `json:"role,omitempty"`
	Model     string     `json:"model,omitempty"`
	Iteration int        `json:"iteration"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// handleGetLoopTrajectory handles GET /loops/{loop_id}?format={summary|json}
// Returns aggregated trajectory data for the given loop ID.
func (c *Component) handleGetLoopTrajectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract loop ID from path: /trajectory-api/loops/{loop_id}
	loopID := extractIDFromPath(r.URL.Path, "/loops/")
	if loopID == "" {
		http.Error(w, "Loop ID required", http.StatusBadRequest)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "summary"
	}

	trajectory, err := c.getTrajectoryByLoopID(r.Context(), loopID, format == "json")
	if err != nil {
		c.logger.Error("Failed to get trajectory", "loop_id", loopID, "error", err)
		http.Error(w, "Failed to retrieve trajectory", http.StatusInternalServerError)
		return
	}

	if trajectory == nil {
		http.Error(w, "Loop not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(trajectory); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// handleGetTraceTrajectory handles GET /traces/{trace_id}?format={summary|json}
// Returns aggregated trajectory data for the given trace ID.
func (c *Component) handleGetTraceTrajectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract trace ID from path: /trajectory-api/traces/{trace_id}
	traceID := extractIDFromPath(r.URL.Path, "/traces/")
	if traceID == "" {
		http.Error(w, "Trace ID required", http.StatusBadRequest)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "summary"
	}

	trajectory, err := c.getTrajectoryByTraceID(r.Context(), traceID, format == "json")
	if err != nil {
		c.logger.Error("Failed to get trajectory", "trace_id", traceID, "error", err)
		http.Error(w, "Failed to retrieve trajectory", http.StatusInternalServerError)
		return
	}

	if trajectory == nil {
		http.Error(w, "Trace not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(trajectory); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// getTrajectoryByLoopID retrieves trajectory data for a specific loop.
func (c *Component) getTrajectoryByLoopID(ctx context.Context, loopID string, includeEntries bool) (*Trajectory, error) {
	// Get loop state to find trace_id
	loopState, err := c.getLoopState(ctx, loopID)
	if err != nil {
		return nil, err
	}
	if loopState == nil {
		return nil, nil
	}

	// Get LLM calls for this loop
	calls, err := c.getLLMCallsByLoopID(ctx, loopID)
	if err != nil {
		// Log but continue - we can still return loop state
		c.logger.Warn("Failed to get LLM calls", "loop_id", loopID, "error", err)
		calls = []*llm.CallRecord{}
	}

	// Get tool calls for this loop
	toolCalls, err := c.getToolCallsByLoopID(ctx, loopID)
	if err != nil {
		// Log but continue - tool calls are supplementary
		c.logger.Warn("Failed to get tool calls", "loop_id", loopID, "error", err)
		toolCalls = []*llm.ToolCallRecord{}
	}

	return c.buildTrajectory(loopState, calls, toolCalls, includeEntries), nil
}

// getTrajectoryByTraceID retrieves trajectory data for a specific trace.
func (c *Component) getTrajectoryByTraceID(ctx context.Context, traceID string, includeEntries bool) (*Trajectory, error) {
	// Get LLM calls for this trace
	calls, err := c.getLLMCallsByTraceID(ctx, traceID)
	if err != nil {
		return nil, err
	}

	// Get tool calls for this trace
	toolCalls, err := c.getToolCallsByTraceID(ctx, traceID)
	if err != nil {
		// Log but continue - tool calls are supplementary
		c.logger.Warn("Failed to get tool calls", "trace_id", traceID, "error", err)
		toolCalls = []*llm.ToolCallRecord{}
	}

	if len(calls) == 0 && len(toolCalls) == 0 {
		return nil, nil
	}

	// Try to find loop state if any call has a loop_id
	var loopState *LoopState
	for _, call := range calls {
		if call.LoopID != "" {
			loopState, _ = c.getLoopState(ctx, call.LoopID)
			if loopState != nil {
				break
			}
		}
	}
	// Also check tool calls for loop_id if we haven't found one yet
	if loopState == nil {
		for _, tc := range toolCalls {
			if tc.LoopID != "" {
				loopState, _ = c.getLoopState(ctx, tc.LoopID)
				if loopState != nil {
					break
				}
			}
		}
	}

	// Build trajectory without loop state if not found
	if loopState == nil {
		loopState = &LoopState{
			TraceID: traceID,
		}
	}

	return c.buildTrajectory(loopState, calls, toolCalls, includeEntries), nil
}

// getLoopState retrieves the loop state from the AGENT_LOOPS bucket.
func (c *Component) getLoopState(ctx context.Context, loopID string) (*LoopState, error) {
	bucket, err := c.getLoopsBucket(ctx)
	if err != nil {
		return nil, err
	}

	entry, err := bucket.Get(ctx, loopID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var state LoopState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// getLLMCallsByLoopID retrieves LLM call records for a loop.
func (c *Component) getLLMCallsByLoopID(ctx context.Context, loopID string) ([]*llm.CallRecord, error) {
	bucket, err := c.getLLMCallsBucket(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return []*llm.CallRecord{}, nil
		}
		return nil, err
	}

	var records []*llm.CallRecord
	for _, key := range keys {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			// ErrKeyDeleted is expected during concurrent access
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				c.logger.Warn("Failed to get key", "key", key, "error", err)
			}
			continue
		}

		var record llm.CallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			c.logger.Warn("Failed to unmarshal record", "key", key, "error", err)
			continue
		}

		if record.LoopID == loopID {
			recordCopy := record
			records = append(records, &recordCopy)
		}
	}

	// Sort by StartedAt
	llm.SortByStartTime(records)
	return records, nil
}

// getLLMCallsByTraceID retrieves LLM call records for a trace.
func (c *Component) getLLMCallsByTraceID(ctx context.Context, traceID string) ([]*llm.CallRecord, error) {
	bucket, err := c.getLLMCallsBucket(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return []*llm.CallRecord{}, nil
		}
		return nil, err
	}

	prefix := traceID + "."
	var records []*llm.CallRecord

	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			// ErrKeyDeleted is expected during concurrent access
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				c.logger.Warn("Failed to get key", "key", key, "error", err)
			}
			continue
		}

		var record llm.CallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			c.logger.Warn("Failed to unmarshal record", "key", key, "error", err)
			continue
		}

		recordCopy := record
		records = append(records, &recordCopy)
	}

	// Sort by StartedAt
	llm.SortByStartTime(records)
	return records, nil
}

// getToolCallsByLoopID retrieves tool call records for a loop.
func (c *Component) getToolCallsByLoopID(ctx context.Context, loopID string) ([]*llm.ToolCallRecord, error) {
	bucket, err := c.getToolCallsBucket(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return []*llm.ToolCallRecord{}, nil
		}
		return nil, err
	}

	var records []*llm.ToolCallRecord
	for _, key := range keys {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				c.logger.Warn("Failed to get tool call key", "key", key, "error", err)
			}
			continue
		}

		var record llm.ToolCallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			c.logger.Warn("Failed to unmarshal tool call record", "key", key, "error", err)
			continue
		}

		if record.LoopID == loopID {
			recordCopy := record
			records = append(records, &recordCopy)
		}
	}

	llm.SortToolCallsByStartTime(records)
	return records, nil
}

// getToolCallsByTraceID retrieves tool call records for a trace.
func (c *Component) getToolCallsByTraceID(ctx context.Context, traceID string) ([]*llm.ToolCallRecord, error) {
	bucket, err := c.getToolCallsBucket(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return []*llm.ToolCallRecord{}, nil
		}
		return nil, err
	}

	prefix := traceID + "."
	var records []*llm.ToolCallRecord

	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				c.logger.Warn("Failed to get tool call key", "key", key, "error", err)
			}
			continue
		}

		var record llm.ToolCallRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil {
			c.logger.Warn("Failed to unmarshal tool call record", "key", key, "error", err)
			continue
		}

		recordCopy := record
		records = append(records, &recordCopy)
	}

	llm.SortToolCallsByStartTime(records)
	return records, nil
}

// buildTrajectory constructs a Trajectory from loop state, LLM calls, and tool calls.
func (c *Component) buildTrajectory(loopState *LoopState, calls []*llm.CallRecord, toolCalls []*llm.ToolCallRecord, includeEntries bool) *Trajectory {
	t := &Trajectory{
		LoopID:     loopState.ID,
		TraceID:    loopState.TraceID,
		Status:     loopState.Status,
		Steps:      loopState.Iteration,
		ModelCalls: len(calls),
		ToolCalls:  len(toolCalls),
		StartedAt:  loopState.StartedAt,
		EndedAt:    loopState.EndedAt,
	}

	// Aggregate metrics from LLM calls
	for _, call := range calls {
		t.TokensIn += call.TokensIn
		t.TokensOut += call.TokensOut
		t.DurationMs += call.DurationMs
	}

	// Add tool call durations
	for _, tc := range toolCalls {
		t.DurationMs += tc.DurationMs
	}

	// Calculate total duration from loop state if available
	if loopState.StartedAt != nil && loopState.EndedAt != nil {
		t.DurationMs = loopState.EndedAt.Sub(*loopState.StartedAt).Milliseconds()
	}

	// Build entries if requested — interleave model and tool calls chronologically
	if includeEntries {
		t.Entries = make([]TrajectoryEntry, 0, len(calls)+len(toolCalls))

		// Add model call entries
		for _, call := range calls {
			entry := TrajectoryEntry{
				Type:          "model_call",
				Timestamp:     call.StartedAt,
				DurationMs:    call.DurationMs,
				Model:         call.Model,
				Provider:      call.Provider,
				Capability:    call.Capability,
				TokensIn:      call.TokensIn,
				TokensOut:     call.TokensOut,
				FinishReason:  call.FinishReason,
				Error:         call.Error,
				Retries:       call.Retries,
				MessagesCount: len(call.Messages),
			}

			// Add response preview (truncated)
			if call.Response != "" {
				preview := call.Response
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				entry.ResponsePreview = preview
			}

			t.Entries = append(t.Entries, entry)
		}

		// Add tool call entries
		for _, tc := range toolCalls {
			entry := TrajectoryEntry{
				Type:       "tool_call",
				Timestamp:  tc.StartedAt,
				DurationMs: tc.DurationMs,
				ToolName:   tc.ToolName,
				Status:     tc.Status,
				Error:      tc.Error,
			}

			// Add result preview (truncated)
			if tc.Result != "" {
				preview := tc.Result
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				entry.ResultPreview = preview
			}

			t.Entries = append(t.Entries, entry)
		}

		// Sort all entries chronologically
		sortEntriesByTimestamp(t.Entries)
	}

	return t
}

// sortEntriesByTimestamp sorts trajectory entries chronologically.
func sortEntriesByTimestamp(entries []TrajectoryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})
}

// extractIDFromPath extracts an ID from a path segment.
// Example: extractIDFromPath("/trajectory-api/loops/abc123", "/loops/") returns "abc123"
func extractIDFromPath(path, prefix string) string {
	idx := strings.Index(path, prefix)
	if idx == -1 {
		return ""
	}

	remainder := path[idx+len(prefix):]
	// Remove any trailing segments or slashes
	if slashIdx := strings.Index(remainder, "/"); slashIdx != -1 {
		remainder = remainder[:slashIdx]
	}

	return strings.TrimSpace(remainder)
}
