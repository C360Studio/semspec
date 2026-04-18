package prompt

// ErrorTrend carries a resolved error category with its occurrence count.
type ErrorTrend struct {
	CategoryID string // e.g. "missing_tests"
	Label      string // e.g. "Missing Tests"
	Guidance   string // actionable remediation from the category def
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
