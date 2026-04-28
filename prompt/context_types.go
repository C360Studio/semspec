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
	FilesOwned  []string
	DependsOn   []string
}
