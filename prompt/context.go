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

	// ErrorTrends carries resolved error categories with occurrence counts
	// for trend-based prompt injection (Phase A persistent agent roster).
	ErrorTrends []ErrorTrend

	// AgentID is the persistent agent ID assigned to this task.
	AgentID string
}

// ErrorTrend carries a resolved error category with its occurrence count.
type ErrorTrend struct {
	CategoryID string // e.g. "missing_tests"
	Label      string // e.g. "Missing Tests"
	Guidance   string // actionable remediation from the category def
	Count      int
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

// FocusContextInfo contains context for focused planners (parallel planning).
type FocusContextInfo struct {
	Entities []string
	Files    []string
	Summary  string
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

// HasTool returns true if the named tool is in AvailableTools.
func (ctx *AssemblyContext) HasTool(name string) bool {
	return slices.Contains(ctx.AvailableTools, name)
}
