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

	// LessonsLearned carries role-scoped lesson data for prompt injection.
	LessonsLearned *LessonsLearned

	// Standards carries role-filtered project standards for prompt injection.
	Standards *StandardsContext

	// ScenarioReviewContext carries data for scenario-level review prompts.
	ScenarioReviewContext *ScenarioReviewContext

	// QAReviewContext carries data for QA release-readiness review prompts.
	QAReviewContext *QAReviewContext

	// Persona carries optional persona configuration for this role.
	// When non-nil, a CategoryPersona fragment is injected into the prompt.
	Persona *AgentPersona

	// MaxTokens is the model's context window size from the resolved endpoint.
	// Fragment conditions can use this to skip verbose sections for smaller models.
	// Zero means unknown (treat as large model).
	MaxTokens int

	// Vocabulary provides display labels for prompt rendering.
	// When nil, hardcoded defaults are used.
	Vocabulary *Vocabulary
}

// TaskContext carries data for developer task prompts.
type TaskContext struct {
	// Context is pre-built context with SOPs, entities, and documents.
	Context *workflow.ContextPayload

	// PlanTitle is the parent plan title.
	PlanTitle string

	// PlanGoal is the parent plan goal.
	PlanGoal string

	// Feedback is reviewer feedback for retry prompts.
	Feedback string

	// Iteration is the current TDD cycle number (1-based).
	Iteration int

	// MaxIterations is the maximum dev→validate→review cycles for this task.
	MaxIterations int

	// ErrorTrends carries resolved error categories with occurrence counts
	// for role-scoped lesson trend injection.
	ErrorTrends []ErrorTrend

	// IsRetry indicates this dispatch follows a previous failed attempt.
	// When true, the workspace may contain files from the previous attempt.
	IsRetry bool

	// Checklist carries the project-specific quality gate checks from
	// .semspec/checklist.json. When non-empty, prompt fragments inject the
	// actual check names and commands instead of a hardcoded generic list.
	Checklist []workflow.Check

	// TestSurface is the architect's declared test coverage — integration
	// flows to exercise cross-component behavior and e2e flows to exercise
	// actor-driven user-visible outcomes. When non-nil, developer prompts
	// render this as "tests you must write" guidance. Phase 5 threads the
	// struct + prompt; Phase 5.1 wires the value through execution-manager.
	TestSurface *workflow.TestSurface
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

// ReviewContext carries data for reviewer prompts.
type ReviewContext struct {
	// PlanSlug is the plan slug being reviewed.
	PlanSlug string

	// PlanContent is the plan JSON being reviewed.
	PlanContent string

	// SOPContext is the SOP context for review.
	SOPContext string
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

// QAReviewContext carries data for QA release-readiness review prompts.
// It is populated by qa-reviewer when dispatching the LLM agent.
type QAReviewContext struct {
	// PlanTitle is the human-readable plan title.
	PlanTitle string

	// PlanGoal is the plan's stated goal.
	PlanGoal string

	// Requirements summarises each requirement's execution status.
	Requirements []RequirementSummary

	// TestSurface is the architect's declared test coverage matrix.
	// Contains integration flows and e2e flows the developer was expected to implement.
	// Nil when the plan has no architecture phase (synthesis-level review).
	TestSurface *workflow.TestSurface

	// QALevel is the project QA policy the executor ran at.
	QALevel workflow.QALevel

	// Passed is true when the QA executor reported no test failures.
	// Always false for synthesis-level review (no tests were run).
	Passed bool

	// Failures lists individual test or CI job failures from the QA executor.
	// Empty for synthesis-level review or when Passed is true.
	Failures []workflow.QAFailure

	// Artifacts lists workspace-relative references to logs, screenshots, traces,
	// and coverage reports produced by the QA run.
	Artifacts []workflow.QAArtifactRef

	// FilesModifiedDiff is the aggregate set of files changed across all requirements.
	// Derived from plan.Requirements[*].FilesModified at review time.
	FilesModifiedDiff []string

	// RunnerError describes an infrastructure failure in the QA executor itself
	// (e.g., sandbox timeout, docker failure). Distinct from test failures.
	// Empty when the executor ran cleanly regardless of test outcomes.
	RunnerError string
}

// HasTool returns true if the named tool is in AvailableTools.
func (ctx *AssemblyContext) HasTool(name string) bool {
	return slices.Contains(ctx.AvailableTools, name)
}
