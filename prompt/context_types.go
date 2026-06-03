package prompt

// ErrorTrend carries a resolved error category with its occurrence count.
type ErrorTrend struct {
	CategoryID string
	Label      string
	Guidance   string
	Count      int
}

// FocusContextInfo contains context for focused planners (parallel planning).
type FocusContextInfo struct {
	Entities []string
	Files    []string
	Summary  string
}

// NodeResultSummary is a compact summary of a completed DAG node for scenario review.
type NodeResultSummary struct {
	NodeID  string
	Summary string
	Files   []string
}

// RequirementSummary is a compact summary of a requirement for rollup review.
type RequirementSummary struct {
	// Title is the requirement title.
	Title string

	// Status is the satisfaction status: "satisfied", "partially", or "failed".
	Status string
}

// QACapabilityEvidence is the per-capability rollup the QA-reviewer audits
// under ADR-044 release-readiness contract: each capability must have
// evidence from at least one shipped Story (the M:N coverage join's QA
// implication). Empty CoveringStoryIDs is a gap — the capability is
// declared but no Story claims to cover it.
type QACapabilityEvidence struct {
	Name        string
	Description string
	// CoveringStoryIDs lists every Story whose CapabilityNames contains
	// this capability. Each entry includes the Story's terminal status so
	// the reviewer can see whether the evidence actually shipped.
	CoveringStoryIDs []string
	// ShippedCount is the number of covering Stories whose Status reached
	// the terminal complete state. Zero means no shipped evidence.
	ShippedCount int
}

// QAStorySummary is the compact per-Story rollup for QA-reviewer.
type QAStorySummary struct {
	ID              string
	Title           string
	ComponentName   string
	RequirementIDs  []string
	CapabilityNames []string
	Status          string
}

// ExistingRequirementSummary is the lightweight view of a requirement used
// across multiple user prompts (requirement-generator partial regen,
// architect requirement context, etc). Only the fields the user prompt
// actually shows the LLM — keeps the prompt context decoupled from the
// full workflow.Requirement shape. Renderers consult only the fields they
// need; "" / nil values are silently skipped.
type ExistingRequirementSummary struct {
	ID          string
	Title       string
	Description string
	Status      string
	DependsOn   []string
}
