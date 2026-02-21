package trajectoryapi

import "time"

// WorkflowTrajectory aggregates LLM call data for an entire workflow.
type WorkflowTrajectory struct {
	// Slug is the plan identifier.
	Slug string `json:"slug"`

	// Status is the current workflow status.
	Status string `json:"status"`

	// Phases contains token breakdown by workflow phase.
	Phases map[string]*PhaseMetrics `json:"phases"`

	// Totals contains aggregate metrics across all phases.
	Totals *AggregateMetrics `json:"totals"`

	// TraceIDs lists all trace IDs associated with this workflow.
	TraceIDs []string `json:"trace_ids"`

	// TruncationSummary summarizes context truncation across the workflow.
	TruncationSummary *TruncationSummary `json:"truncation_summary,omitempty"`

	// StartedAt is when the workflow began.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when the workflow finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PhaseMetrics contains token metrics for a workflow phase.
type PhaseMetrics struct {
	// TokensIn is total prompt tokens for this phase.
	TokensIn int `json:"tokens_in"`

	// TokensOut is total completion tokens for this phase.
	TokensOut int `json:"tokens_out"`

	// CallCount is the number of LLM calls in this phase.
	CallCount int `json:"call_count"`

	// DurationMs is total duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Capabilities breaks down usage by capability type.
	Capabilities map[string]*CapabilityMetrics `json:"capabilities,omitempty"`
}

// CapabilityMetrics contains metrics for a specific capability type.
type CapabilityMetrics struct {
	// TokensIn is total prompt tokens for this capability.
	TokensIn int `json:"tokens_in"`

	// TokensOut is total completion tokens for this capability.
	TokensOut int `json:"tokens_out"`

	// CallCount is the number of calls for this capability.
	CallCount int `json:"call_count"`

	// TruncatedCount is calls where context was truncated.
	TruncatedCount int `json:"truncated_count,omitempty"`
}

// AggregateMetrics contains totals across all phases.
type AggregateMetrics struct {
	// TokensIn is total prompt tokens.
	TokensIn int `json:"tokens_in"`

	// TokensOut is total completion tokens.
	TokensOut int `json:"tokens_out"`

	// TotalTokens is sum of TokensIn + TokensOut.
	TotalTokens int `json:"total_tokens"`

	// CallCount is total LLM calls.
	CallCount int `json:"call_count"`

	// DurationMs is total duration in milliseconds.
	DurationMs int64 `json:"duration_ms"`
}

// TruncationSummary summarizes context truncation statistics.
type TruncationSummary struct {
	// TotalCalls is total calls with context budget set.
	TotalCalls int `json:"total_calls"`

	// TruncatedCalls is calls where context was truncated.
	TruncatedCalls int `json:"truncated_calls"`

	// TruncationRate is percentage of truncated calls.
	TruncationRate float64 `json:"truncation_rate"`

	// ByCapability breaks down truncation rate by capability.
	ByCapability map[string]float64 `json:"by_capability,omitempty"`
}

// ContextStats provides context utilization metrics.
type ContextStats struct {
	// Summary contains aggregate context statistics.
	Summary *ContextSummary `json:"summary"`

	// ByCapability breaks down stats by capability type.
	ByCapability map[string]*CapabilityContextStats `json:"by_capability"`

	// Calls contains per-call details (only with format=json).
	Calls []CallContextDetail `json:"calls,omitempty"`
}

// ContextSummary contains aggregate context statistics.
type ContextSummary struct {
	// TotalCalls is total number of LLM calls analyzed.
	TotalCalls int `json:"total_calls"`

	// CallsWithBudget is calls that had a context budget set.
	CallsWithBudget int `json:"calls_with_budget"`

	// AvgUtilization is average context utilization percentage.
	AvgUtilization float64 `json:"avg_utilization"`

	// TruncationRate is percentage of calls truncated.
	TruncationRate float64 `json:"truncation_rate"`

	// TotalBudget is sum of all context budgets.
	TotalBudget int `json:"total_budget"`

	// TotalUsed is sum of all prompt tokens used.
	TotalUsed int `json:"total_used"`
}

// CapabilityContextStats contains context stats for a capability.
type CapabilityContextStats struct {
	// CallCount is number of calls for this capability.
	CallCount int `json:"call_count"`

	// AvgBudget is average context budget.
	AvgBudget int `json:"avg_budget,omitempty"`

	// AvgUsed is average tokens used.
	AvgUsed int `json:"avg_used,omitempty"`

	// AvgUtilization is average utilization percentage.
	AvgUtilization float64 `json:"avg_utilization"`

	// TruncationRate is percentage of calls truncated.
	TruncationRate float64 `json:"truncation_rate"`

	// MaxUtilization is highest utilization seen.
	MaxUtilization float64 `json:"max_utilization,omitempty"`
}

// CallContextDetail contains context details for a single call.
type CallContextDetail struct {
	// RequestID uniquely identifies the call.
	RequestID string `json:"request_id"`

	// TraceID is the trace correlation ID.
	TraceID string `json:"trace_id,omitempty"`

	// Capability is the capability type used.
	Capability string `json:"capability"`

	// Model is the model used for this call.
	Model string `json:"model,omitempty"`

	// Budget is the context budget (max tokens).
	Budget int `json:"budget"`

	// Used is prompt tokens actually used.
	Used int `json:"used"`

	// Utilization is utilization percentage.
	Utilization float64 `json:"utilization"`

	// Truncated indicates if context was truncated.
	Truncated bool `json:"truncated"`

	// Timestamp is when the call was made.
	Timestamp time.Time `json:"timestamp"`
}
