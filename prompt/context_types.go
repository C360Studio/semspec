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

// ScenarioOutcome summarises a single scenario's execution result.
type ScenarioOutcome struct {
	// ScenarioID is the scenario entity ID.
	ScenarioID string

	// Given is the BDD Given clause.
	Given string

	// When is the BDD When clause.
	When string

	// Then is the BDD Then assertions.
	Then []string

	// Verdict is the execution outcome: "approved", "rejected", or "failed".
	Verdict string

	// FilesModified lists files changed during this scenario's execution.
	FilesModified []string

	// RedTeamIssues is the number of red team issues raised during this scenario.
	RedTeamIssues int
}
