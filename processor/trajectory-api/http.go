package trajectoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
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
	mux.HandleFunc(prefix+"context-stats", c.handleGetContextStats)
}

// Trajectory represents aggregated data about an agent loop's interactions.
type Trajectory struct {
	// LoopID is the agent loop identifier.
	LoopID string `json:"loop_id"`

	// TraceID is the trace correlation identifier.
	TraceID string `json:"trace_id,omitempty"`

	// Steps is the total number of iterations in the loop.
	Steps int `json:"steps"`

	// ToolCalls is the number of tool call steps.
	ToolCalls int `json:"tool_calls"`

	// ModelCalls is the number of model call steps.
	ModelCalls int `json:"model_calls"`

	// TokensIn is the total input tokens across all model call steps.
	TokensIn int `json:"tokens_in"`

	// TokensOut is the total output tokens across all model call steps.
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

// TrajectoryEntry represents a single step in the agent loop trajectory.
type TrajectoryEntry struct {
	// Type is the entry type ("model_call" or "tool_call").
	Type string `json:"type"`

	// Timestamp is when this step occurred.
	Timestamp time.Time `json:"timestamp"`

	// DurationMs is how long this step took in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Model is the model used (for model_call steps).
	Model string `json:"model,omitempty"`

	// Provider is the provider used (for model_call steps).
	Provider string `json:"provider,omitempty"`

	// Capability is the role or purpose of this step.
	Capability string `json:"capability,omitempty"`

	// TokensIn is input tokens (for model_call steps).
	TokensIn int `json:"tokens_in,omitempty"`

	// TokensOut is output tokens (for model_call steps).
	TokensOut int `json:"tokens_out,omitempty"`

	// Retries is the number of retries before this step succeeded.
	Retries int `json:"retries,omitempty"`

	// Error is any error message.
	Error string `json:"error,omitempty"`

	// ResponsePreview is a truncated preview of the model response (for model_call, format=json).
	ResponsePreview string `json:"response_preview,omitempty"`

	// ToolName is the tool that was executed (for tool_call steps).
	ToolName string `json:"tool_name,omitempty"`

	// ToolArguments is the JSON-encoded tool arguments (for tool_call, format=json).
	ToolArguments string `json:"tool_arguments,omitempty"`

	// ResultPreview is a truncated preview of the tool result (for tool_call, format=json).
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
//
// Note: With the step entity model, trace-level queries are not directly supported
// because step entities do not have a trace_id predicate. This endpoint uses the
// loop state to find loops matching the trace, then aggregates their steps.
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
	// Get loop state for metadata (status, trace_id, start/end time).
	loopState, err := c.getLoopState(ctx, loopID)
	if err != nil {
		return nil, err
	}
	if loopState == nil {
		return nil, nil
	}

	// Query step entities from the knowledge graph.
	steps, err := c.getStepsByLoopID(ctx, loopID)
	if err != nil {
		// Log but continue — loop state alone is still useful.
		c.logger.Warn("Failed to get step entities", "loop_id", loopID, "error", err)
		steps = nil
	}

	return c.buildTrajectory(loopState, steps, includeEntries), nil
}

// getTrajectoryByTraceID retrieves trajectory data for a specific trace.
// This scans the AGENT_LOOPS bucket for loops matching the trace ID, then
// aggregates step data across all matching loops.
func (c *Component) getTrajectoryByTraceID(ctx context.Context, traceID string, includeEntries bool) (*Trajectory, error) {
	// Find the loop state for this trace (first matching loop).
	loopState, err := c.findLoopStateByTraceID(ctx, traceID)
	if err != nil {
		return nil, err
	}

	if loopState == nil {
		// Return empty trajectory scoped to the trace — no loops found yet.
		return nil, nil
	}

	// Query steps for the found loop.
	steps, err := c.getStepsByLoopID(ctx, loopState.ID)
	if err != nil {
		c.logger.Warn("Failed to get step entities for trace", "trace_id", traceID, "loop_id", loopState.ID, "error", err)
		steps = nil
	}

	return c.buildTrajectory(loopState, steps, includeEntries), nil
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

// findLoopStateByTraceID scans the AGENT_LOOPS bucket for the first loop matching a trace ID.
func (c *Component) findLoopStateByTraceID(ctx context.Context, traceID string) (*LoopState, error) {
	bucket, err := c.getLoopsBucket(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return nil, nil
		}
		return nil, err
	}

	for _, key := range keys {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			if !errors.Is(err, jetstream.ErrKeyDeleted) && !errors.Is(err, jetstream.ErrKeyNotFound) {
				c.logger.Warn("Failed to get loop key", "key", key, "error", err)
			}
			continue
		}

		var state LoopState
		if err := json.Unmarshal(entry.Value(), &state); err != nil {
			continue
		}

		if state.TraceID == traceID {
			return &state, nil
		}
	}

	return nil, nil
}

// getStepsByLoopID fetches step entities for a loop from the knowledge graph.
// Returns nil when the step querier is not configured or org/platform are missing.
func (c *Component) getStepsByLoopID(ctx context.Context, loopID string) ([]*StepRecord, error) {
	c.mu.RLock()
	querier := c.stepQuerier
	c.mu.RUnlock()

	if querier == nil {
		c.logger.Debug("Step querier not initialized, returning empty steps")
		return nil, nil
	}

	entityID := c.loopEntityID(loopID)
	if entityID == "" {
		c.logger.Debug("Org/platform not configured, cannot construct loop entity ID",
			"loop_id", loopID)
		return nil, nil
	}

	steps, err := querier.QueryStepsByLoopEntityID(ctx, entityID)
	if err != nil {
		return nil, err
	}

	return steps, nil
}

// buildTrajectory constructs a Trajectory from loop state and step entity records.
func (c *Component) buildTrajectory(loopState *LoopState, steps []*StepRecord, includeEntries bool) *Trajectory {
	t := &Trajectory{
		LoopID:    loopState.ID,
		TraceID:   loopState.TraceID,
		Status:    loopState.Status,
		Steps:     loopState.Iteration,
		StartedAt: loopState.StartedAt,
		EndedAt:   loopState.EndedAt,
	}

	// Aggregate metrics from step records.
	for _, step := range steps {
		switch step.Type {
		case "model_call":
			t.ModelCalls++
			t.TokensIn += step.TokensIn
			t.TokensOut += step.TokensOut
			t.DurationMs += step.DurationMs
		case "tool_call":
			t.ToolCalls++
			t.DurationMs += step.DurationMs
		}
	}

	// Calculate total duration from loop state timestamps if available.
	if loopState.StartedAt != nil && loopState.EndedAt != nil {
		t.DurationMs = loopState.EndedAt.Sub(*loopState.StartedAt).Milliseconds()
	}

	// Build detailed entries if requested.
	if includeEntries {
		t.Entries = make([]TrajectoryEntry, 0, len(steps))
		for _, step := range steps {
			entry := TrajectoryEntry{
				Type:       step.Type,
				Timestamp:  step.Timestamp,
				DurationMs: step.DurationMs,
				Retries:    step.Retries,
				Capability: step.Capability,
			}

			switch step.Type {
			case "model_call":
				entry.Model = step.Model
				entry.Provider = step.Provider
				entry.TokensIn = step.TokensIn
				entry.TokensOut = step.TokensOut
			case "tool_call":
				entry.ToolName = step.ToolName
			}

			t.Entries = append(t.Entries, entry)
		}

		// Steps are already sorted by index from the querier, but ensure
		// chronological ordering by timestamp as a tiebreaker.
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
// Returns aggregated trajectory data for all loops in the workflow.
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

	// Get KV store for workflow state operations
	c.mu.RLock()
	kvStore := c.kvStore
	c.mu.RUnlock()

	if kvStore == nil {
		http.Error(w, "Workflow state not available", http.StatusServiceUnavailable)
		return
	}

	// Load plan to get trace IDs
	plan, err := workflow.LoadPlan(r.Context(), kvStore, slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Workflow not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Collect all step records across trace IDs with bounded concurrency.
	allSteps := c.collectStepsForTraces(r.Context(), plan.ExecutionTraceIDs)

	wt := c.buildWorkflowTrajectory(
		slug,
		string(plan.EffectiveStatus()),
		plan.ExecutionTraceIDs,
		allSteps,
		&plan.CreatedAt,
		plan.ReviewedAt,
	)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(wt); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// collectStepsForTraces fetches steps for multiple trace IDs by mapping them through
// the AGENT_LOOPS bucket to find matching loop IDs, then querying step entities.
// Uses bounded concurrency to avoid overwhelming the graph gateway.
func (c *Component) collectStepsForTraces(ctx context.Context, traceIDs []string) []*StepRecord {
	if len(traceIDs) == 0 {
		return nil
	}

	var allSteps []*StepRecord
	for _, traceID := range traceIDs {
		if ctx.Err() != nil {
			break
		}

		loopState, err := c.findLoopStateByTraceID(ctx, traceID)
		if err != nil || loopState == nil {
			continue
		}

		steps, err := c.getStepsByLoopID(ctx, loopState.ID)
		if err != nil {
			c.logger.Warn("Failed to get steps for trace", "trace_id", traceID, "error", err)
			continue
		}

		allSteps = append(allSteps, steps...)
	}

	return allSteps
}

// handleGetContextStats handles GET /context-stats?trace_id=X&workflow=Y&capability=Z
// Returns context utilization statistics.
//
// Note: The new step entity model does not include context budget or truncation data.
// This endpoint returns structural metrics (call counts, token totals) only.
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

	var steps []*StepRecord

	if traceID != "" {
		loopState, err := c.findLoopStateByTraceID(r.Context(), traceID)
		if err != nil {
			c.logger.Error("Failed to find loop for trace", "trace_id", traceID, "error", err)
			http.Error(w, "Failed to retrieve trace data", http.StatusInternalServerError)
			return
		}

		if loopState != nil {
			var err error
			steps, err = c.getStepsByLoopID(r.Context(), loopState.ID)
			if err != nil {
				c.logger.Warn("Failed to get steps for context stats", "trace_id", traceID, "error", err)
			}
		}
	} else {
		c.mu.RLock()
		kvStore := c.kvStore
		c.mu.RUnlock()

		if kvStore == nil {
			http.Error(w, "Workflow state not available", http.StatusServiceUnavailable)
			return
		}

		plan, loadErr := workflow.LoadPlan(r.Context(), kvStore, workflowSlug)
		if loadErr != nil {
			if errors.Is(loadErr, workflow.ErrPlanNotFound) {
				http.Error(w, "Workflow not found", http.StatusNotFound)
				return
			}
			c.logger.Error("Failed to load plan", "slug", workflowSlug, "error", loadErr)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		steps = c.collectStepsForTraces(r.Context(), plan.ExecutionTraceIDs)
	}

	// Filter by capability if requested
	if capability != "" {
		filtered := make([]*StepRecord, 0)
		for _, step := range steps {
			if step.Capability == capability {
				filtered = append(filtered, step)
			}
		}
		steps = filtered
	}

	stats := c.buildContextStats(steps)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		c.logger.Warn("Failed to encode response", "error", err)
	}
}

// buildWorkflowTrajectory aggregates step records into a workflow-level trajectory.
func (c *Component) buildWorkflowTrajectory(slug, status string, traceIDs []string, steps []*StepRecord, startedAt, completedAt *time.Time) *WorkflowTrajectory {
	wt := &WorkflowTrajectory{
		Slug:        slug,
		Status:      status,
		TraceIDs:    traceIDs,
		Phases:      make(map[string]*PhaseMetrics),
		Totals:      &AggregateMetrics{},
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}

	// Only model_call steps have token and phase data.
	for _, step := range steps {
		if step.Type != "model_call" {
			continue
		}

		phase := determinePhase(step.Capability)

		if wt.Phases[phase] == nil {
			wt.Phases[phase] = &PhaseMetrics{
				Capabilities: make(map[string]*CapabilityMetrics),
			}
		}

		if wt.Phases[phase].Capabilities[step.Capability] == nil {
			wt.Phases[phase].Capabilities[step.Capability] = &CapabilityMetrics{}
		}

		pm := wt.Phases[phase]
		pm.TokensIn += step.TokensIn
		pm.TokensOut += step.TokensOut
		pm.CallCount++
		pm.DurationMs += step.DurationMs

		cm := pm.Capabilities[step.Capability]
		cm.TokensIn += step.TokensIn
		cm.TokensOut += step.TokensOut
		cm.CallCount++

		wt.Totals.TokensIn += step.TokensIn
		wt.Totals.TokensOut += step.TokensOut
		wt.Totals.CallCount++
		wt.Totals.DurationMs += step.DurationMs
	}

	wt.Totals.TotalTokens = wt.Totals.TokensIn + wt.Totals.TokensOut

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
		return "execution"
	}
}

// buildContextStats calculates context utilization metrics from step records.
// Note: Context budget and truncation are not available in the step entity model.
// This returns token totals and call counts only.
func (c *Component) buildContextStats(steps []*StepRecord) *ContextStats {
	stats := &ContextStats{
		Summary:      &ContextSummary{},
		ByCapability: make(map[string]*CapabilityContextStats),
	}

	capabilityData := make(map[string]*struct {
		totalTokensIn  int
		totalTokensOut int
		callCount      int
	})

	for _, step := range steps {
		if step.Type != "model_call" {
			continue
		}

		stats.Summary.TotalCalls++

		if capabilityData[step.Capability] == nil {
			capabilityData[step.Capability] = &struct {
				totalTokensIn  int
				totalTokensOut int
				callCount      int
			}{}
		}
		cd := capabilityData[step.Capability]
		cd.totalTokensIn += step.TokensIn
		cd.totalTokensOut += step.TokensOut
		cd.callCount++
	}

	// Build capability breakdown.
	for cap, cd := range capabilityData {
		stats.ByCapability[cap] = &CapabilityContextStats{
			CallCount: cd.callCount,
		}
		if cd.callCount > 0 {
			stats.ByCapability[cap].AvgUsed = (cd.totalTokensIn + cd.totalTokensOut) / cd.callCount
		}
	}

	return stats
}
