package revieworchestrator

import (
	"encoding/json"
	"strings"
	"sync"
)

// reviewExecution holds in-memory state for a single review workflow execution.
// It is keyed by entityID (semspec.workflow.<review-type>.<slug>) in the
// component's activeReviews sync.Map. This avoids round-trip reads of the
// entity triple store when routing loop-completion events back to the correct
// dispatch handler.
//
// All field access must be guarded by mu. The sync.Map in Component protects
// the map operations (Load/Store/Delete), but the struct itself is shared
// across goroutines (trigger handler, completion handler, timeout callback).
type reviewExecution struct {
	mu sync.Mutex

	// EntityID is the canonical graph entity ID for this review:
	// semspec.workflow.<review-type>.<slug>
	EntityID string

	// ReviewType is one of reviewTypePlanReview, reviewTypePhaseReview,
	// or reviewTypeTaskReview.
	ReviewType string

	// Slug is the plan slug.
	Slug string

	// Iteration tracks how many generator→reviewer cycles have completed
	// (0-based). Compared against MaxIterations to decide whether to retry
	// or escalate on a rejection.
	Iteration int

	// MaxIterations is the per-execution budget read from Config.MaxIterations.
	MaxIterations int

	// --- Fields from the original trigger, kept for re-dispatch ---

	Title         string
	Description   string
	ProjectID     string
	Prompt        string
	ScopePatterns []string
	TraceID       string
	LoopID        string
	RequestID     string
	Auto          bool

	// --- Generator output (populated after generator loop completes) ---

	// PlanContent is the raw JSON content produced by the generator agent.
	// Used when building the reviewer dispatch payload.
	PlanContent json.RawMessage

	// LLMRequestIDs holds the request IDs from the generator LLM calls.
	LLMRequestIDs []string

	// --- Reviewer output (populated after reviewer loop completes) ---

	// Verdict is the latest reviewer verdict ("approved" or "needs_changes").
	Verdict string

	// Summary is the reviewer's free-text assessment.
	Summary string

	// Findings is the structured findings JSON array from the reviewer.
	Findings json.RawMessage

	// FormattedFindings is the human-readable markdown rendering of findings.
	// Used when building revision prompts for the generator on retry passes.
	FormattedFindings string

	// ReviewerLLMRequestIDs holds the request IDs from the reviewer LLM calls.
	ReviewerLLMRequestIDs []string

	// --- Task IDs used to route loop-completion events back to the right handler ---

	// GeneratorTaskID is the TaskID embedded in the TaskMessage sent to the
	// generator agent. Matched against LoopCompletedEvent.TaskID on receipt.
	GeneratorTaskID string

	// ReviewerTaskID is the TaskID embedded in the TaskMessage sent to the
	// reviewer agent. Matched against LoopCompletedEvent.TaskID on receipt.
	ReviewerTaskID string

	// timeoutTimer holds the per-execution timeout. Stopped on completion.
	timeoutTimer *timeoutHandle
}

// timeoutHandle wraps a timer reference so it can be stopped on completion.
type timeoutHandle struct {
	stop func()
}

// buildRevisionContext constructs the prompt addition for a revision pass.
func (e *reviewExecution) buildRevisionContext() string {
	var sb strings.Builder
	sb.WriteString("REVISION REQUEST: Your previous output was rejected by the reviewer.\n\n")
	sb.WriteString("## Review Summary\n")
	sb.WriteString(e.Summary)
	sb.WriteString("\n\n## Specific Findings\n")
	sb.WriteString(e.FormattedFindings)
	sb.WriteString("\n\n## Instructions\n")
	sb.WriteString("Fix ONLY the issues raised by the reviewer. Keep the overall approach unchanged unless specific issues were flagged.")
	return sb.String()
}
