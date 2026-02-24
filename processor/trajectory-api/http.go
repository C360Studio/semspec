package trajectoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
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
	mux.HandleFunc(prefix+"workflows/", c.handleGetWorkflowTrajectory)
	mux.HandleFunc(prefix+"calls/", c.handleGetCall)
	mux.HandleFunc(prefix+"context-stats", c.handleGetContextStats)
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

	// RequestID uniquely identifies this LLM call for drill-down to full record.
	RequestID string `json:"request_id,omitempty"`

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

	// StorageRef points to the full CallRecord in ObjectStore (for model_call).
	// Present when the call data has been offloaded to ObjectStore.
	StorageRef *message.StorageReference `json:"storage_ref,omitempty"`

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
// NOTE: LLM calls are now stored in the knowledge graph, not KV.
// This function returns empty results until graph queries are implemented.
func (c *Component) getLLMCallsByLoopID(_ context.Context, loopID string) ([]*llm.CallRecord, error) {
	// LLM calls are now graph entities - KV storage has been removed
	// TODO: Implement graph query for LLM calls by loop_id using agent.activity.loop predicate
	c.logger.Debug("LLM calls now in graph - KV query skipped",
		"loop_id", loopID,
		"hint", "use graph query with agent.activity.loop predicate")
	return []*llm.CallRecord{}, nil
}

// getLLMCallsByTraceID retrieves LLM call records for a trace.
// NOTE: LLM calls are now stored in the knowledge graph, not KV.
// This function returns empty results until graph queries are implemented.
func (c *Component) getLLMCallsByTraceID(_ context.Context, traceID string) ([]*llm.CallRecord, error) {
	// LLM calls are now graph entities - KV storage has been removed
	// TODO: Implement graph query for LLM calls by trace_id using dc.terms.identifier predicate
	c.logger.Debug("LLM calls now in graph - KV query skipped",
		"trace_id", traceID,
		"hint", "use graph query with dc.terms.identifier predicate")
	return []*llm.CallRecord{}, nil
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
		t.TokensIn += call.PromptTokens
		t.TokensOut += call.CompletionTokens
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
				RequestID:     call.RequestID,
				Model:         call.Model,
				Provider:      call.Provider,
				Capability:    call.Capability,
				TokensIn:      call.PromptTokens,
				TokensOut:     call.CompletionTokens,
				FinishReason:  call.FinishReason,
				Error:         call.Error,
				Retries:       call.Retries,
				StorageRef:    call.StorageRef,
			}

			// Use MessagesCount from index if available, otherwise count from inline Messages
			if call.MessagesCount > 0 {
				entry.MessagesCount = call.MessagesCount
			} else {
				entry.MessagesCount = len(call.Messages)
			}

			// Use ResponsePreview from index if available, otherwise truncate inline Response
			if call.ResponsePreview != "" {
				entry.ResponsePreview = call.ResponsePreview
			} else if call.Response != "" {
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

// handleGetWorkflowTrajectory handles GET /workflows/{slug}?format={summary|json}
// Returns aggregated trajectory data for all LLM calls in the workflow.
func (c *Component) handleGetWorkflowTrajectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract slug from path
	slug := extractIDFromPath(r.URL.Path, "/workflows/")
	if slug == "" {
		http.Error(w, "Workflow slug required", http.StatusBadRequest)
		return
	}

	// Get workflow manager
	c.mu.RLock()
	manager := c.workflowManager
	c.mu.RUnlock()

	if manager == nil {
		http.Error(w, "Workflow manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Load plan to get trace IDs
	plan, err := manager.LoadPlan(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Workflow not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Collect all LLM calls across trace IDs with bounded concurrency
	allCalls := c.collectLLMCallsForTraces(r.Context(), plan.ExecutionTraceIDs)

	// Build workflow trajectory response
	wt := c.buildWorkflowTrajectory(
		slug,
		string(plan.EffectiveStatus()),
		plan.ExecutionTraceIDs,
		allCalls,
		&plan.CreatedAt,
		plan.ReviewedAt,
	)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(wt); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// collectLLMCallsForTraces fetches LLM calls for multiple trace IDs with bounded concurrency.
// It respects context cancellation for early termination.
func (c *Component) collectLLMCallsForTraces(ctx context.Context, traceIDs []string) []*llm.CallRecord {
	if len(traceIDs) == 0 {
		return nil
	}

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)

	var (
		mu       sync.Mutex
		allCalls []*llm.CallRecord
		wg       sync.WaitGroup
	)

	for _, traceID := range traceIDs {
		// Check context cancellation before spawning goroutine
		if ctx.Err() != nil {
			c.logger.Debug("Request cancelled during trace collection")
			break
		}

		wg.Add(1)
		go func(tid string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			calls, err := c.getLLMCallsByTraceID(ctx, tid)
			if err != nil {
				c.logger.Warn("Failed to get LLM calls for trace",
					"trace_id", tid, "error", err)
				return
			}

			mu.Lock()
			allCalls = append(allCalls, calls...)
			mu.Unlock()
		}(traceID)
	}

	wg.Wait()
	return allCalls
}

// handleGetContextStats handles GET /context-stats?trace_id=X&workflow=Y&capability=Z
// Returns context utilization statistics for proving context management effectiveness.
func (c *Component) handleGetContextStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	traceID := r.URL.Query().Get("trace_id")
	workflowSlug := r.URL.Query().Get("workflow")
	capability := r.URL.Query().Get("capability")

	// At least one filter is required
	if traceID == "" && workflowSlug == "" {
		http.Error(w, "At least one of trace_id or workflow parameter is required", http.StatusBadRequest)
		return
	}

	// Get LLM calls based on filters
	var calls []*llm.CallRecord
	var err error

	if traceID != "" {
		calls, err = c.getLLMCallsByTraceID(r.Context(), traceID)
	} else {
		// Get trace IDs from workflow manager
		c.mu.RLock()
		manager := c.workflowManager
		c.mu.RUnlock()

		if manager == nil {
			http.Error(w, "Workflow manager not initialized", http.StatusServiceUnavailable)
			return
		}

		plan, loadErr := manager.LoadPlan(r.Context(), workflowSlug)
		if loadErr != nil {
			if errors.Is(loadErr, workflow.ErrPlanNotFound) {
				http.Error(w, "Workflow not found", http.StatusNotFound)
				return
			}
			c.logger.Error("Failed to load plan", "slug", workflowSlug, "error", loadErr)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Collect calls from all trace IDs with bounded concurrency
		calls = c.collectLLMCallsForTraces(r.Context(), plan.ExecutionTraceIDs)
	}

	if err != nil {
		c.logger.Error("Failed to get LLM calls", "error", err)
		http.Error(w, "Failed to retrieve call data", http.StatusInternalServerError)
		return
	}

	// Filter by capability if requested
	if capability != "" {
		filtered := make([]*llm.CallRecord, 0)
		for _, call := range calls {
			if call.Capability == capability {
				filtered = append(filtered, call)
			}
		}
		calls = filtered
	}

	// Build context stats
	includeDetails := r.URL.Query().Get("format") == "json"
	stats := c.buildContextStats(calls, includeDetails)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// buildWorkflowTrajectory aggregates LLM calls into a workflow-level trajectory.
func (c *Component) buildWorkflowTrajectory(slug, status string, traceIDs []string, calls []*llm.CallRecord, startedAt, completedAt *time.Time) *WorkflowTrajectory {
	wt := &WorkflowTrajectory{
		Slug:        slug,
		Status:      status,
		TraceIDs:    traceIDs,
		Phases:      make(map[string]*PhaseMetrics),
		Totals:      &AggregateMetrics{},
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}

	// Track truncation stats
	truncatedCount := 0
	totalWithBudget := 0
	capabilityTruncation := make(map[string]*struct {
		total     int
		truncated int
	})

	// Aggregate by phase and capability
	for _, call := range calls {
		// Determine phase from capability
		phase := determinePhase(call.Capability)

		// Initialize phase if needed
		if wt.Phases[phase] == nil {
			wt.Phases[phase] = &PhaseMetrics{
				Capabilities: make(map[string]*CapabilityMetrics),
			}
		}

		// Initialize capability if needed
		if wt.Phases[phase].Capabilities[call.Capability] == nil {
			wt.Phases[phase].Capabilities[call.Capability] = &CapabilityMetrics{}
		}

		// Update phase metrics
		pm := wt.Phases[phase]
		pm.TokensIn += call.PromptTokens
		pm.TokensOut += call.CompletionTokens
		pm.CallCount++
		pm.DurationMs += call.DurationMs

		// Update capability metrics
		cm := pm.Capabilities[call.Capability]
		cm.TokensIn += call.PromptTokens
		cm.TokensOut += call.CompletionTokens
		cm.CallCount++
		if call.ContextTruncated {
			cm.TruncatedCount++
		}

		// Update totals
		wt.Totals.TokensIn += call.PromptTokens
		wt.Totals.TokensOut += call.CompletionTokens
		wt.Totals.CallCount++
		wt.Totals.DurationMs += call.DurationMs

		// Track truncation for summary
		if call.ContextBudget > 0 {
			totalWithBudget++
			if call.ContextTruncated {
				truncatedCount++
			}

			// Track by capability
			if capabilityTruncation[call.Capability] == nil {
				capabilityTruncation[call.Capability] = &struct {
					total     int
					truncated int
				}{}
			}
			capabilityTruncation[call.Capability].total++
			if call.ContextTruncated {
				capabilityTruncation[call.Capability].truncated++
			}
		}
	}

	wt.Totals.TotalTokens = wt.Totals.TokensIn + wt.Totals.TokensOut

	// Build truncation summary
	if totalWithBudget > 0 {
		wt.TruncationSummary = &TruncationSummary{
			TotalCalls:     totalWithBudget,
			TruncatedCalls: truncatedCount,
			TruncationRate: float64(truncatedCount) / float64(totalWithBudget) * 100.0,
			ByCapability:   make(map[string]float64),
		}

		for cap, stats := range capabilityTruncation {
			if stats.total > 0 {
				wt.TruncationSummary.ByCapability[cap] = float64(stats.truncated) / float64(stats.total) * 100.0
			}
		}
	}

	return wt
}

// determinePhase maps a capability to a workflow phase.
func determinePhase(capability string) string {
	switch capability {
	case "planning":
		return "planning"
	case "reviewing":
		return "review"
	case "coding", "writing":
		return "execution"
	default:
		// Default to execution for unknown capabilities
		return "execution"
	}
}

// buildContextStats calculates context utilization metrics.
func (c *Component) buildContextStats(calls []*llm.CallRecord, includeDetails bool) *ContextStats {
	stats := &ContextStats{
		Summary:      &ContextSummary{},
		ByCapability: make(map[string]*CapabilityContextStats),
	}

	if includeDetails {
		stats.Calls = make([]CallContextDetail, 0, len(calls))
	}

	// Track per-capability stats
	capabilityData := make(map[string]*struct {
		totalBudget int
		totalUsed   int
		callCount   int
		truncated   int
		maxUtil     float64
	})

	totalBudget := 0
	totalUsed := 0
	callsWithBudget := 0
	truncatedCalls := 0

	for _, call := range calls {
		stats.Summary.TotalCalls++

		if call.ContextBudget > 0 {
			callsWithBudget++
			totalBudget += call.ContextBudget
			totalUsed += call.PromptTokens

			if call.ContextTruncated {
				truncatedCalls++
			}

			utilization := float64(call.PromptTokens) / float64(call.ContextBudget) * 100.0

			// Track capability stats
			if capabilityData[call.Capability] == nil {
				capabilityData[call.Capability] = &struct {
					totalBudget int
					totalUsed   int
					callCount   int
					truncated   int
					maxUtil     float64
				}{}
			}
			cd := capabilityData[call.Capability]
			cd.totalBudget += call.ContextBudget
			cd.totalUsed += call.PromptTokens
			cd.callCount++
			if call.ContextTruncated {
				cd.truncated++
			}
			if utilization > cd.maxUtil {
				cd.maxUtil = utilization
			}

			// Add detail if requested
			if includeDetails {
				stats.Calls = append(stats.Calls, CallContextDetail{
					RequestID:   call.RequestID,
					TraceID:     call.TraceID,
					Capability:  call.Capability,
					Model:       call.Model,
					Budget:      call.ContextBudget,
					Used:        call.PromptTokens,
					Utilization: utilization,
					Truncated:   call.ContextTruncated,
					Timestamp:   call.StartedAt,
				})
			}
		}
	}

	// Calculate summary metrics
	stats.Summary.CallsWithBudget = callsWithBudget
	stats.Summary.TotalBudget = totalBudget
	stats.Summary.TotalUsed = totalUsed

	if callsWithBudget > 0 {
		stats.Summary.AvgUtilization = float64(totalUsed) / float64(totalBudget) * 100.0
		stats.Summary.TruncationRate = float64(truncatedCalls) / float64(callsWithBudget) * 100.0
	}

	// Build capability breakdown
	for cap, cd := range capabilityData {
		capStats := &CapabilityContextStats{
			CallCount:      cd.callCount,
			MaxUtilization: cd.maxUtil,
		}

		if cd.callCount > 0 {
			capStats.AvgBudget = cd.totalBudget / cd.callCount
			capStats.AvgUsed = cd.totalUsed / cd.callCount
			capStats.AvgUtilization = float64(cd.totalUsed) / float64(cd.totalBudget) * 100.0
			capStats.TruncationRate = float64(cd.truncated) / float64(cd.callCount) * 100.0
		}

		stats.ByCapability[cap] = capStats
	}

	return stats
}

// handleGetCall returns the full LLM call record including Messages and Response.
// GET /trajectory-api/calls/{request_id}?trace_id={trace_id}
//
// NOTE: LLM calls are now stored in the knowledge graph, not KV.
// This endpoint requires graph queries which are not yet implemented.
// Use graph queries with llm.call.* predicates to fetch LLM call data.
func (c *Component) handleGetCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// LLM calls are now graph entities - KV storage has been removed
	// TODO: Implement graph query for LLM call by request_id
	http.Error(w, "LLM calls are now stored in the knowledge graph. Use graph queries with llm.call.* predicates.", http.StatusNotImplemented)
}
