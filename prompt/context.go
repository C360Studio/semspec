package prompt

import (
	"slices"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
)

// AssemblyContext provides all information needed to select and compose
// the right fragments for a specific prompt assembly.
type AssemblyContext struct {
	// Role is the workflow role (developer, planner, reviewer, etc).
	Role Role

	// Provider is the LLM provider for formatting (anthropic, openai, ollama).
	Provider Provider

	// Capability is the resolved model capability.
	Capability model.Capability

	// Domain selects the domain catalog ("software", "research").
	Domain string

	// AvailableTools lists tool names available to this agent.
	AvailableTools []string

	// SupportsTools indicates whether the resolved model supports tool calling.
	SupportsTools bool

	// TaskContext carries task-specific data for developer prompts.
	TaskContext *TaskContext

	// PlanContext carries plan-specific data for planner prompts.
	PlanContext *PlanContext

	// ReviewContext carries review-specific data for reviewer prompts.
	ReviewContext *ReviewContext

	// RedTeamContext carries data for red team review prompts.
	RedTeamContext *RedTeamContext

	// TeamKnowledge carries team lesson data for prompt injection.
	TeamKnowledge *TeamKnowledge

	// ScenarioReviewContext carries data for scenario-level review prompts.
	ScenarioReviewContext *ScenarioReviewContext

	// RollupReviewContext carries data for plan-level rollup review prompts.
	RollupReviewContext *RollupReviewContext
}

// TaskContext carries data for developer task prompts.
type TaskContext struct {
	// Task is the task being implemented.
	Task workflow.Task

	// Context is pre-built context with SOPs, entities, and documents.
	Context *workflow.ContextPayload

	// PlanTitle is the parent plan title.
	PlanTitle string

	// PlanGoal is the parent plan goal.
	PlanGoal string

	// Feedback is reviewer feedback for retry prompts.
	Feedback string

	// Iteration is the current attempt number (1-based).
	Iteration int

	// MaxIterations is the total tool-use budget for this task.
	MaxIterations int

	// ErrorTrends carries resolved error categories with occurrence counts
	// for trend-based prompt injection (Phase A persistent agent roster).
	ErrorTrends []ErrorTrend

	// AgentID is the persistent agent ID assigned to this task.
	AgentID string

	// IsRetry indicates this dispatch follows a previous failed attempt.
	// When true, the workspace may contain files from the previous attempt.
	IsRetry bool

	// Checklist carries the project-specific quality gate checks from
	// .semspec/checklist.json. When non-empty, prompt fragments inject the
	// actual check names and commands instead of a hardcoded generic list.
	Checklist []workflow.Check
}

// PlanContext carries data for planner prompts.
type PlanContext struct {
	// Title is the plan title.
	Title string

	// Goal is the plan goal (for revision prompts).
	Goal string

	// Context is the plan context (for revision prompts).
	Context string

	// Scope is the plan scope (for finalization prompts).
	Scope []string

	// Slug is the plan slug (for finalization prompts).
	Slug string

	// ReviewSummary is the reviewer's summary (for revision prompts).
	ReviewSummary string

	// ReviewFindings is the reviewer's findings (for revision prompts).
	ReviewFindings string

	// IsRevision indicates this is a plan revision after rejection.
	IsRevision bool

	// IsFromExploration indicates finalization from an existing exploration.
	IsFromExploration bool

	// FocusArea is the planner's focused analysis area (for parallel planners).
	FocusArea string

	// FocusDescription describes the focus area in detail.
	FocusDescription string

	// Hints are coordinator-provided hints for focused planners.
	Hints []string

	// FocusContext is pre-loaded graph context for focused planners.
	FocusContext *FocusContextInfo
}

// RedTeamContext carries data for red team review prompts.
type RedTeamContext struct {
	// BlueTeamFiles lists files the blue team modified.
	BlueTeamFiles []string
	// BlueTeamSummary is the blue team's implementation summary.
	BlueTeamSummary string
}

// TeamKnowledge carries team lesson data for prompt injection.
type TeamKnowledge struct {
	// TeamID is the team identifier.
	TeamID string
	// Lessons from the team's knowledge base.
	Lessons []TeamLesson
}

// TeamLesson is a single lesson from the team knowledge base.
type TeamLesson struct {
	// Category is the error category (e.g., "missing_tests", "wrong_pattern").
	Category string
	// Summary is a one-line description of the lesson.
	Summary string
	// Role is which role this lesson applies to (e.g., "builder", "tester").
	Role string
	// Guidance is prescriptive remediation text from the error category definition.
	// Empty for approved-pattern insights or uncategorized lessons.
	Guidance string
}

// ReviewContext carries data for reviewer prompts.
type ReviewContext struct {
	// PlanSlug is the plan slug being reviewed.
	PlanSlug string

	// PlanContent is the plan JSON being reviewed.
	PlanContent string

	// SOPContext is the SOP context for review.
	SOPContext string

	// Tasks is the list of tasks being reviewed (for task reviewer).
	Tasks []workflow.Task
}

// ScenarioReviewContext carries data for scenario-level review prompts.
type ScenarioReviewContext struct {
	// ScenarioGiven is the BDD Given clause (single-scenario legacy path).
	ScenarioGiven string

	// ScenarioWhen is the BDD When clause (single-scenario legacy path).
	ScenarioWhen string

	// ScenarioThen is the BDD Then assertions (single-scenario legacy path).
	ScenarioThen []string

	// Scenarios carries all scenarios for requirement-level review.
	// When set, the reviewer produces per-scenario verdicts.
	Scenarios []ScenarioSpec

	// NodeResults summarises each completed DAG node.
	NodeResults []NodeResultSummary

	// FilesModified is the aggregate list of files changed across all nodes.
	FilesModified []string

	// RedTeamFindings is present when a red team challenge preceded this review.
	RedTeamFindings *RedTeamContext

	// RetryFeedback carries the reviewer's feedback from a prior rejection.
	// When non-empty, this is a retry — the reviewer should note what was fixed.
	RetryFeedback string
}

// ScenarioSpec identifies a scenario for per-scenario verdict tracking.
type ScenarioSpec struct {
	ID    string   `json:"id"`
	Given string   `json:"given"`
	When  string   `json:"when"`
	Then  []string `json:"then"`
}

// RollupReviewContext carries data for plan-level rollup review prompts.
type RollupReviewContext struct {
	// PlanTitle is the plan title.
	PlanTitle string

	// PlanGoal is the plan goal.
	PlanGoal string

	// Requirements summarises each requirement's completion status.
	Requirements []RequirementSummary

	// ScenarioOutcomes summarises each scenario's result.
	ScenarioOutcomes []ScenarioOutcome

	// AggregateFiles is the total list of files modified across all scenarios.
	AggregateFiles []string
}

// HasTool returns true if the named tool is in AvailableTools.
func (ctx *AssemblyContext) HasTool(name string) bool {
	return slices.Contains(ctx.AvailableTools, name)
}
