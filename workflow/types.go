// Package workflow provides the Semspec workflow system for managing
// plans and tasks through a structured development process.
package workflow

import (
	"encoding/json"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

func init() {
	// Register BatchTriggerPayload type for message deserialization
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "batch-trigger",
		Version:     "v1",
		Description: "Batch task dispatch trigger payload",
		Factory:     func() any { return &BatchTriggerPayload{} },
	})

	// Register TaskExecutionPayload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "execution",
		Version:     "v1",
		Description: "Task execution payload with context and model",
		Factory:     func() any { return &TaskExecutionPayload{} },
	})

	// Register PlanCoordinatorTrigger type for parallel planning
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "coordinator-trigger",
		Version:     "v1",
		Description: "Plan coordinator trigger for parallel planning",
		Factory:     func() any { return &PlanCoordinatorTrigger{} },
	})

	// Register FocusedPlanTrigger type for focused planning
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "focused-trigger",
		Version:     "v1",
		Description: "Focused planner trigger with context",
		Factory:     func() any { return &FocusedPlanTrigger{} },
	})
}

// Status represents the current state of a plan in the workflow.
type Status string

const (
	// StatusCreated indicates the plan has been created but not yet drafted.
	StatusCreated Status = "created"
	// StatusDrafted indicates the plan document has been generated.
	StatusDrafted Status = "drafted"
	// StatusReviewed indicates the plan has undergone SOP-aware review.
	StatusReviewed Status = "reviewed"
	// StatusApproved indicates the plan has been approved for execution.
	StatusApproved Status = "approved"
	// StatusPhasesGenerated indicates phases have been generated from the plan.
	StatusPhasesGenerated Status = "phases_generated"
	// StatusPhasesApproved indicates generated phases have been reviewed and approved.
	StatusPhasesApproved Status = "phases_approved"
	// StatusTasksGenerated indicates tasks have been generated from the plan.
	StatusTasksGenerated Status = "tasks_generated"
	// StatusTasksApproved indicates generated tasks have been reviewed and approved.
	StatusTasksApproved Status = "tasks_approved"
	// StatusImplementing indicates task execution is in progress.
	StatusImplementing Status = "implementing"
	// StatusComplete indicates all tasks have been completed successfully.
	StatusComplete Status = "complete"
	// StatusArchived indicates the plan has been archived.
	StatusArchived Status = "archived"
	// StatusRejected indicates the plan was rejected during review or approval.
	StatusRejected Status = "rejected"
)

// String returns the string representation of the status.
func (s Status) String() string {
	return string(s)
}

// IsValid returns true if the status is a valid workflow status.
func (s Status) IsValid() bool {
	switch s {
	case StatusCreated, StatusDrafted, StatusReviewed, StatusApproved,
		StatusPhasesGenerated, StatusPhasesApproved,
		StatusTasksGenerated, StatusTasksApproved,
		StatusImplementing, StatusComplete, StatusArchived, StatusRejected:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if the status can transition to the target status.
func (s Status) CanTransitionTo(target Status) bool {
	switch s {
	case StatusCreated:
		return target == StatusDrafted || target == StatusRejected
	case StatusDrafted:
		return target == StatusReviewed || target == StatusRejected
	case StatusReviewed:
		return target == StatusApproved || target == StatusRejected
	case StatusApproved:
		// approved → phases_generated (normal flow with phases)
		// approved → rejected (review loop escalation)
		return target == StatusPhasesGenerated || target == StatusRejected
	case StatusPhasesGenerated:
		// phases_generated → phases_approved (normal) or rejected (phase review escalation)
		return target == StatusPhasesApproved || target == StatusRejected
	case StatusPhasesApproved:
		// phases_approved → tasks_generated (normal) or rejected (task generation failure)
		return target == StatusTasksGenerated || target == StatusRejected
	case StatusTasksGenerated:
		// tasks_generated → tasks_approved (normal) or rejected (task review escalation)
		return target == StatusTasksApproved || target == StatusRejected
	case StatusTasksApproved:
		return target == StatusImplementing || target == StatusRejected
	case StatusImplementing:
		// implementing → complete (normal) or rejected (execution escalation)
		return target == StatusComplete || target == StatusRejected
	case StatusComplete:
		return target == StatusArchived
	case StatusArchived, StatusRejected:
		return false // Terminal states
	default:
		return false
	}
}

// PlanRecord represents an active plan in the workflow.
// PlanRecords live in .semspec/plans/{slug}/ and contain metadata.json and tasks.md.
type PlanRecord struct {
	// Slug is the URL-friendly identifier for the plan
	Slug string `json:"slug"`

	// Title is the human-readable title
	Title string `json:"title"`

	// Description is the original description provided when creating the plan
	Description string `json:"description"`

	// Status is the current workflow state
	Status Status `json:"status"`

	// Author is the user who created the plan
	Author string `json:"author"`

	// CreatedAt is when the plan was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the plan was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// Files tracks which files exist for this plan
	Files PlanFiles `json:"files"`

	// RelatedEntities contains graph entity IDs related to this plan
	RelatedEntities []string `json:"related_entities,omitempty"`

	// GitHub contains GitHub issue tracking metadata
	GitHub *GitHubMetadata `json:"github,omitempty"`
}

// GitHubMetadata tracks GitHub issue information for a plan.
type GitHubMetadata struct {
	// EpicNumber is the GitHub issue number for the epic
	EpicNumber int `json:"epic_number,omitempty"`

	// EpicURL is the web URL for the epic issue
	EpicURL string `json:"epic_url,omitempty"`

	// Repository is the GitHub repository (owner/repo format)
	Repository string `json:"repository,omitempty"`

	// TaskIssues maps task IDs (e.g., "1.1") to GitHub issue numbers
	TaskIssues map[string]int `json:"task_issues,omitempty"`

	// LastSynced is when the GitHub sync was last performed
	LastSynced time.Time `json:"last_synced,omitempty"`
}

// PlanFiles tracks which files exist for a plan.
type PlanFiles struct {
	HasPlan   bool `json:"has_plan"`
	HasTasks  bool `json:"has_tasks"`
	HasPhases bool `json:"has_phases"`
}

// Spec represents a specification in .semspec/specs/{name}/.
type Spec struct {
	// Name is the spec identifier
	Name string `json:"name"`

	// Title is the human-readable title
	Title string `json:"title"`

	// Version is the spec version
	Version string `json:"version"`

	// CreatedAt is when the spec was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the spec was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// OriginPlan is the plan that created this spec (if any)
	OriginPlan string `json:"origin_plan,omitempty"`
}

// Principle represents a constitution principle.
type Principle struct {
	// Number is the principle number (e.g., 1, 2, 3)
	Number int `json:"number"`

	// Title is the principle title
	Title string `json:"title"`

	// Description is the full principle description
	Description string `json:"description"`

	// Rationale explains why this principle exists
	Rationale string `json:"rationale,omitempty"`
}

// Constitution represents the project constitution from .semspec/constitution.md.
type Constitution struct {
	// Version is the constitution version
	Version string `json:"version"`

	// Ratified is when the constitution was ratified
	Ratified time.Time `json:"ratified"`

	// Principles are the governing principles
	Principles []Principle `json:"principles"`
}

// CheckViolation represents a constitution violation found during /check.
type CheckViolation struct {
	// Principle is the principle that was violated
	Principle Principle `json:"principle"`

	// Message describes the violation
	Message string `json:"message"`

	// Location is where the violation was found (optional)
	Location string `json:"location,omitempty"`
}

// CheckResult represents the result of a constitution check.
type CheckResult struct {
	// Passed indicates if all checks passed
	Passed bool `json:"passed"`

	// Violations contains any violations found
	Violations []CheckViolation `json:"violations,omitempty"`

	// CheckedAt is when the check was performed
	CheckedAt time.Time `json:"checked_at"`
}

// Plan represents a structured development plan.
// Plans start as drafts (Approved=false) and must be approved
// via /approve command before task generation.
type Plan struct {
	// ID is the unique identifier for the plan entity
	ID string `json:"id"`

	// Slug is the URL-friendly identifier (used for file paths)
	Slug string `json:"slug"`

	// Title is the human-readable title
	Title string `json:"title"`

	// ProjectID is the entity ID of the parent project.
	// Format: semspec.local.project.{project-slug}
	// Required - defaults to the "default" project if not specified.
	ProjectID string `json:"project_id"`

	// Status is the authoritative workflow state for the plan.
	// When empty, EffectiveStatus() infers status from legacy boolean fields.
	Status Status `json:"status,omitempty"`

	// Approved indicates if this plan is ready for execution.
	// false = draft plan, true = user explicitly approved
	Approved bool `json:"approved"`

	// TasksApproved indicates if generated tasks have been reviewed and approved.
	// When true, task execution is permitted.
	TasksApproved bool `json:"tasks_approved,omitempty"`

	// PhasesApproved indicates if generated phases have been reviewed and approved.
	// When true, task generation is permitted.
	PhasesApproved bool `json:"phases_approved,omitempty"`

	// CreatedAt is when the plan was created
	CreatedAt time.Time `json:"created_at"`

	// ApprovedAt is when the plan was approved for execution
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// PhasesApprovedAt is when the phases were approved
	PhasesApprovedAt *time.Time `json:"phases_approved_at,omitempty"`

	// TasksApprovedAt is when the tasks were approved for execution
	TasksApprovedAt *time.Time `json:"tasks_approved_at,omitempty"`

	// ReviewVerdict is the plan-reviewer's verdict: "approved", "needs_changes", or empty if not reviewed.
	ReviewVerdict string `json:"review_verdict,omitempty"`

	// ReviewSummary is the plan-reviewer's summary of findings.
	ReviewSummary string `json:"review_summary,omitempty"`

	// ReviewedAt is when the plan review completed.
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`

	// ReviewFindings contains the structured findings from the plan reviewer.
	// Stored as raw JSON to avoid coupling to the reviewer's output schema.
	// Updated on each review iteration and on escalation.
	ReviewFindings json.RawMessage `json:"review_findings,omitempty"`

	// ReviewFormattedFindings is the human-readable text version of findings.
	// Updated on each review iteration and on escalation.
	ReviewFormattedFindings string `json:"review_formatted_findings,omitempty"`

	// ReviewIteration is the number of review iterations completed.
	// Incremented on each revision event, set to max on escalation.
	ReviewIteration int `json:"review_iteration,omitempty"`

	// TaskReviewVerdict is the task-reviewer's verdict: "approved", "needs_changes", "escalated", or empty.
	// Separate from ReviewVerdict so the UI can distinguish plan review from task review state.
	TaskReviewVerdict string `json:"task_review_verdict,omitempty"`

	// TaskReviewSummary is the task-reviewer's summary of findings.
	TaskReviewSummary string `json:"task_review_summary,omitempty"`

	// TaskReviewedAt is when the task review last occurred.
	TaskReviewedAt *time.Time `json:"task_reviewed_at,omitempty"`

	// TaskReviewFindings contains structured findings from the task reviewer.
	// Stored as raw JSON to avoid coupling to the reviewer's output schema.
	// Updated on each task review iteration and on escalation.
	TaskReviewFindings json.RawMessage `json:"task_review_findings,omitempty"`

	// TaskReviewFormattedFindings is the human-readable text version of task review findings.
	// Updated on each task review iteration and on escalation.
	TaskReviewFormattedFindings string `json:"task_review_formatted_findings,omitempty"`

	// TaskReviewIteration is the number of task review iterations completed.
	// Incremented on each task revision event, set to max on escalation.
	TaskReviewIteration int `json:"task_review_iteration,omitempty"`

	// PhaseReviewVerdict is the phase-reviewer's verdict: "approved", "needs_changes", "escalated", or empty.
	PhaseReviewVerdict string `json:"phase_review_verdict,omitempty"`

	// PhaseReviewSummary is the phase-reviewer's summary of findings.
	PhaseReviewSummary string `json:"phase_review_summary,omitempty"`

	// PhaseReviewedAt is when the phase review last occurred.
	PhaseReviewedAt *time.Time `json:"phase_reviewed_at,omitempty"`

	// PhaseReviewFindings contains structured findings from the phase reviewer.
	PhaseReviewFindings json.RawMessage `json:"phase_review_findings,omitempty"`

	// PhaseReviewFormattedFindings is the human-readable text version of phase review findings.
	PhaseReviewFormattedFindings string `json:"phase_review_formatted_findings,omitempty"`

	// PhaseReviewIteration is the number of phase review iterations completed.
	PhaseReviewIteration int `json:"phase_review_iteration,omitempty"`

	// LastError is the most recent error from a workflow step for this plan.
	// Set when user.signal.error fires — annotation only, does NOT change status.
	LastError string `json:"last_error,omitempty"`

	// LastErrorAt is when the last error occurred.
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`

	// Goal describes what we're building or fixing
	Goal string `json:"goal,omitempty"`

	// Context describes the current state and why this matters
	Context string `json:"context,omitempty"`

	// Scope defines file/directory boundaries for this plan
	Scope Scope `json:"scope,omitempty"`

	// ExecutionTraceIDs tracks trace IDs from workflow executions.
	// Used by trajectory-api to aggregate LLM metrics per workflow.
	ExecutionTraceIDs []string `json:"execution_trace_ids,omitempty"`

	// LLMCallHistory tracks LLM request IDs per review iteration,
	// enabling the UI to drill down from any loop iteration to the
	// complete prompt/response via the /calls/ endpoint.
	LLMCallHistory *LLMCallHistory `json:"llm_call_history,omitempty"`
}

// LLMCallHistory tracks LLM request IDs per review iteration for both
// plan review and task review loops. This enables the UI to correlate
// each loop iteration with its specific LLM calls for full artifact drill-down.
type LLMCallHistory struct {
	PlanReview  []IterationCalls `json:"plan_review,omitempty"`
	PhaseReview []IterationCalls `json:"phase_review,omitempty"`
	TaskReview  []IterationCalls `json:"task_review,omitempty"`
}

// IterationCalls records the LLM request IDs used during a single review iteration.
type IterationCalls struct {
	Iteration     int      `json:"iteration"`
	LLMRequestIDs []string `json:"llm_request_ids"`
	Verdict       string   `json:"verdict,omitempty"`
}

// EffectiveStatus returns the plan's current status.
// If Status is explicitly set, it is returned directly.
// Otherwise, status is inferred from legacy boolean fields for backward compatibility
// with plan.json files that predate the Status field.
func (p *Plan) EffectiveStatus() Status {
	if p.Status != "" {
		return p.Status
	}
	// Infer from legacy boolean fields
	if p.TasksApproved {
		return StatusTasksApproved
	}
	if p.PhasesApproved {
		return StatusPhasesApproved
	}
	if p.Approved {
		return StatusApproved
	}
	if p.ReviewVerdict == "needs_changes" {
		return StatusReviewed
	}
	// ReviewVerdict tracks the reviewer's opinion; Approved tracks the user's
	// explicit approval. A plan can be reviewed-as-approved but not yet user-approved.
	if p.ReviewVerdict == "approved" {
		return StatusReviewed
	}
	if p.Goal != "" && p.Context != "" {
		return StatusDrafted
	}
	return StatusCreated
}

// Scope defines the file/directory boundaries for a plan.
type Scope struct {
	// Include lists files/directories in scope for this plan
	Include []string `json:"include,omitempty"`

	// Exclude lists files/directories explicitly out of scope
	Exclude []string `json:"exclude,omitempty"`

	// DoNotTouch lists protected files/directories that must not be modified
	DoNotTouch []string `json:"do_not_touch,omitempty"`
}

// PhaseStatus represents the execution state of a phase.
type PhaseStatus string

const (
	// PhaseStatusPending indicates the phase is not yet ready (dependencies not met).
	PhaseStatusPending PhaseStatus = "pending"

	// PhaseStatusReady indicates dependencies are met and the phase is awaiting start.
	PhaseStatusReady PhaseStatus = "ready"

	// PhaseStatusActive indicates tasks within the phase are being executed.
	PhaseStatusActive PhaseStatus = "active"

	// PhaseStatusComplete indicates all tasks in the phase completed successfully.
	PhaseStatusComplete PhaseStatus = "complete"

	// PhaseStatusFailed indicates execution of the phase failed.
	PhaseStatusFailed PhaseStatus = "failed"

	// PhaseStatusBlocked indicates the phase is blocked by a dependency.
	PhaseStatusBlocked PhaseStatus = "blocked"
)

// String returns the string representation of the phase status.
func (s PhaseStatus) String() string {
	return string(s)
}

// IsValid returns true if the phase status is valid.
func (s PhaseStatus) IsValid() bool {
	switch s {
	case PhaseStatusPending, PhaseStatusReady, PhaseStatusActive,
		PhaseStatusComplete, PhaseStatusFailed, PhaseStatusBlocked:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this phase status can transition to the target status.
func (s PhaseStatus) CanTransitionTo(target PhaseStatus) bool {
	switch s {
	case PhaseStatusPending:
		return target == PhaseStatusReady || target == PhaseStatusBlocked
	case PhaseStatusReady:
		return target == PhaseStatusActive || target == PhaseStatusBlocked
	case PhaseStatusActive:
		return target == PhaseStatusComplete || target == PhaseStatusFailed
	case PhaseStatusBlocked:
		return target == PhaseStatusReady || target == PhaseStatusPending
	case PhaseStatusComplete, PhaseStatusFailed:
		return false // Terminal states
	default:
		return false
	}
}

// PhaseAgentConfig configures agent behavior for a phase.
// Allows routing specific agent teams or models to different phases.
type PhaseAgentConfig struct {
	// Roles lists agent roles that should work on this phase.
	Roles []string `json:"roles,omitempty"`

	// Model overrides the default model for this phase.
	Model string `json:"model,omitempty"`

	// MaxConcurrent limits the maximum concurrent tasks within this phase.
	MaxConcurrent int `json:"max_concurrent,omitempty"`

	// ReviewStrategy controls how tasks in this phase are reviewed.
	// "parallel" reviews all tasks at once, "sequential" reviews one by one.
	ReviewStrategy string `json:"review_strategy,omitempty"`
}

// Phase represents a logical grouping of tasks within a plan.
// Phases enable sequenced execution, phase-level dependencies, per-phase
// agent/model routing, and optional human approval gates.
type Phase struct {
	// ID is the unique identifier.
	ID string `json:"id"`

	// PlanID is the parent plan entity ID.
	PlanID string `json:"plan_id"`

	// Sequence is the order within the plan (1-based).
	Sequence int `json:"sequence"`

	// Name is the display name (e.g., "Phase 1: Foundation").
	Name string `json:"name"`

	// Description is the purpose and scope description.
	Description string `json:"description,omitempty"`

	// DependsOn lists phase IDs that must complete before this phase can start.
	DependsOn []string `json:"depends_on,omitempty"`

	// Status is the current execution state.
	Status PhaseStatus `json:"status"`

	// AgentConfig configures agent/model routing for this phase.
	AgentConfig *PhaseAgentConfig `json:"agent_config,omitempty"`

	// RequiresApproval indicates whether this phase requires human approval before execution.
	RequiresApproval bool `json:"requires_approval,omitempty"`

	// Approved indicates whether this phase has been approved.
	Approved bool `json:"approved,omitempty"`

	// ApprovedBy identifies who approved the phase.
	ApprovedBy string `json:"approved_by,omitempty"`

	// ApprovedAt is when the phase was approved.
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// CreatedAt is when the phase was created.
	CreatedAt time.Time `json:"created_at"`

	// StartedAt is when phase execution started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when the phase completed.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// TaskStatus represents the execution state of a task.
type TaskStatus string

const (
	// TaskStatusPending indicates the task has been created but not yet submitted for approval
	TaskStatusPending TaskStatus = "pending"

	// TaskStatusPendingApproval indicates the task is awaiting human review and approval
	TaskStatusPendingApproval TaskStatus = "pending_approval"

	// TaskStatusApproved indicates the task has been approved for execution
	TaskStatusApproved TaskStatus = "approved"

	// TaskStatusRejected indicates the task was rejected during review
	TaskStatusRejected TaskStatus = "rejected"

	// TaskStatusInProgress indicates the task is currently being worked on
	TaskStatusInProgress TaskStatus = "in_progress"

	// TaskStatusCompleted indicates the task finished successfully
	TaskStatusCompleted TaskStatus = "completed"

	// TaskStatusFailed indicates the task failed
	TaskStatusFailed TaskStatus = "failed"
)

// String returns the string representation of the task status.
func (s TaskStatus) String() string {
	return string(s)
}

// IsValid returns true if the task status is valid.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatusPending, TaskStatusPendingApproval, TaskStatusApproved, TaskStatusRejected,
		TaskStatusInProgress, TaskStatusCompleted, TaskStatusFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this status can transition to the target status.
// The task status workflow is:
//
//	pending → pending_approval (submitted for review)
//	pending_approval → approved (human approved)
//	pending_approval → rejected (human rejected)
//	approved → in_progress (execution started)
//	in_progress → completed (success)
//	in_progress → failed (failure)
//	rejected → pending (re-edited for resubmission)
//
// For backward compatibility with legacy tasks that lack the approval step:
//
//	pending → in_progress (direct execution without approval)
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	switch s {
	case TaskStatusPending:
		// Can submit for approval or start directly (legacy compatibility)
		return target == TaskStatusPendingApproval || target == TaskStatusInProgress || target == TaskStatusFailed
	case TaskStatusPendingApproval:
		// Human approval decision
		return target == TaskStatusApproved || target == TaskStatusRejected
	case TaskStatusApproved:
		// Ready for execution
		return target == TaskStatusInProgress
	case TaskStatusRejected:
		// Can be re-edited and resubmitted
		return target == TaskStatusPending
	case TaskStatusInProgress:
		return target == TaskStatusCompleted || target == TaskStatusFailed
	case TaskStatusCompleted, TaskStatusFailed:
		return false // Terminal states
	default:
		return false
	}
}

// TaskType classifies the kind of work a task represents.
type TaskType string

const (
	// TaskTypeImplement is for implementation work (writing code).
	TaskTypeImplement TaskType = "implement"

	// TaskTypeTest is for writing tests.
	TaskTypeTest TaskType = "test"

	// TaskTypeDocument is for documentation work.
	TaskTypeDocument TaskType = "document"

	// TaskTypeReview is for code review.
	TaskTypeReview TaskType = "review"

	// TaskTypeRefactor is for refactoring existing code.
	TaskTypeRefactor TaskType = "refactor"
)

// TaskTypeCapabilities maps TaskType to model capability strings.
// Used by task-dispatcher to select the appropriate model for each task type.
// Capability values match model.Capability constants: planning, writing, coding, reviewing, fast.
var TaskTypeCapabilities = map[TaskType]string{
	TaskTypeImplement: "coding",    // Code generation, implementation
	TaskTypeTest:      "coding",    // Writing tests requires coding capability
	TaskTypeDocument:  "writing",   // Documentation requires writing capability
	TaskTypeReview:    "reviewing", // Code review requires reviewing capability
	TaskTypeRefactor:  "coding",    // Refactoring requires coding capability
}

// AcceptanceCriterion represents a BDD-style acceptance test.
type AcceptanceCriterion struct {
	// Given is the precondition
	Given string `json:"given"`

	// When is the action being performed
	When string `json:"when"`

	// Then is the expected outcome
	Then string `json:"then"`
}

// Task represents an executable unit of work derived from a Plan.
type Task struct {
	// ID is the unique identifier (format: task.{plan_slug}.{sequence})
	ID string `json:"id"`

	// PlanID is the parent plan entity ID
	PlanID string `json:"plan_id"`

	// PhaseID is the parent phase ID. Required when phases exist.
	PhaseID string `json:"phase_id"`

	// Sequence is the order within the plan (1-indexed)
	Sequence int `json:"sequence"`

	// Description is what to implement
	Description string `json:"description"`

	// Type classifies the kind of work (implement, test, document, review, refactor)
	Type TaskType `json:"type,omitempty"`

	// AcceptanceCriteria lists BDD-style conditions for task completion
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"`

	// Files lists files in scope for this task (optional)
	Files []string `json:"files,omitempty"`

	// DependsOn lists task IDs that must complete before this task can start.
	// Used by task-dispatcher for dependency-aware parallel execution.
	DependsOn []string `json:"depends_on,omitempty"`

	// Status is the current execution state
	Status TaskStatus `json:"status"`

	// CreatedAt is when the task was created
	CreatedAt time.Time `json:"created_at"`

	// StartedAt is when the task started execution (dispatched to agent)
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when the task finished (success or failure)
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// ApprovedBy is the identifier of who approved this task (user, system, etc.)
	ApprovedBy string `json:"approved_by,omitempty"`

	// ApprovedAt is when the task was approved for execution
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

	// RejectionReason explains why the task was rejected (required when status is rejected)
	RejectionReason string `json:"rejection_reason,omitempty"`

	// EscalationReason explains why this task was escalated to human review.
	// Set when task-execution-loop or task-review-loop exhausts retry budget.
	EscalationReason string `json:"escalation_reason,omitempty"`

	// EscalationFeedback contains the last reviewer/validator feedback before escalation.
	EscalationFeedback string `json:"escalation_feedback,omitempty"`

	// EscalationIteration is the iteration count when escalation occurred.
	EscalationIteration int `json:"escalation_iteration,omitempty"`

	// EscalatedAt is when the task was escalated.
	EscalatedAt *time.Time `json:"escalated_at,omitempty"`

	// LastError is the most recent error for this task (LLM failure, validation error, etc).
	// Set when user.signal.error fires — annotation only, does NOT change status.
	LastError string `json:"last_error,omitempty"`

	// LastErrorAt is when the last error occurred.
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
}

// TaskExecutionPayload carries all information needed to execute a task.
// This is published by task-dispatcher to trigger task execution by an agent.
type TaskExecutionPayload struct {
	// Task is the task to execute
	Task Task `json:"task"`

	// Slug is the plan slug for file system operations
	Slug string `json:"slug"`

	// BatchID uniquely identifies this execution batch
	BatchID string `json:"batch_id"`

	// Context contains the pre-built context for this task
	Context *ContextPayload `json:"context,omitempty"`

	// Model is the selected model from the registry based on task type
	Model string `json:"model"`

	// Fallbacks is the fallback model chain if the primary fails
	Fallbacks []string `json:"fallbacks,omitempty"`
}

// TaskExecutionType is the message type for task execution payloads.
var TaskExecutionType = message.Type{
	Domain:   "workflow",
	Category: "execution",
	Version:  "v1",
}

// Schema implements message.Payload.
func (p *TaskExecutionPayload) Schema() message.Type {
	return TaskExecutionType
}

// Validate implements message.Payload.
func (p *TaskExecutionPayload) Validate() error {
	if p.Task.ID == "" {
		return &ValidationError{Field: "task.id", Message: "task.id is required"}
	}
	if p.Slug == "" {
		return &ValidationError{Field: "slug", Message: "slug is required"}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *TaskExecutionPayload) MarshalJSON() ([]byte, error) {
	type Alias TaskExecutionPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *TaskExecutionPayload) UnmarshalJSON(data []byte) error {
	type Alias TaskExecutionPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// ContextPayload contains pre-built context for task execution.
// Built by context-builder and inlined by task-dispatcher.
type ContextPayload struct {
	// Documents maps file paths to their content
	Documents map[string]string `json:"documents,omitempty"`

	// Entities are references to graph entities included in context
	Entities []EntityRef `json:"entities,omitempty"`

	// SOPs contains SOP content relevant to the task
	SOPs []string `json:"sops,omitempty"`

	// TokenCount is the total token count for agent awareness
	TokenCount int `json:"token_count"`
}

// EntityRef is a reference to a graph entity in the context.
type EntityRef struct {
	// ID is the entity identifier
	ID string `json:"id"`

	// Type is the entity type (e.g., "sop", "function", "type")
	Type string `json:"type,omitempty"`

	// Content is the hydrated entity content
	Content string `json:"content,omitempty"`
}

// BatchTriggerPayload triggers task-dispatcher to execute all tasks for a plan.
type BatchTriggerPayload struct {
	// CallbackFields supports workflow-processor async dispatch.
	CallbackFields

	// RequestID uniquely identifies this request
	RequestID string `json:"request_id"`

	// Slug is the plan slug
	Slug string `json:"slug"`

	// BatchID uniquely identifies this execution batch
	BatchID string `json:"batch_id"`

	// WorkflowID is the parent workflow ID if applicable
	WorkflowID string `json:"workflow_id,omitempty"`
}

// BatchTriggerType is the message type for batch trigger payloads.
var BatchTriggerType = message.Type{
	Domain:   "workflow",
	Category: "batch-trigger",
	Version:  "v1",
}

// Schema implements message.Payload.
func (p *BatchTriggerPayload) Schema() message.Type {
	return BatchTriggerType
}

// Validate implements message.Payload.
func (p *BatchTriggerPayload) Validate() error {
	if p.RequestID == "" {
		return &ValidationError{Field: "request_id", Message: "request_id is required"}
	}
	if p.Slug == "" {
		return &ValidationError{Field: "slug", Message: "slug is required"}
	}
	if p.BatchID == "" {
		return &ValidationError{Field: "batch_id", Message: "batch_id is required"}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *BatchTriggerPayload) MarshalJSON() ([]byte, error) {
	type Alias BatchTriggerPayload
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *BatchTriggerPayload) UnmarshalJSON(data []byte) error {
	type Alias BatchTriggerPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// PlanCoordinatorTrigger is the payload for triggering the plan coordinator.
type PlanCoordinatorTrigger struct {
	*TriggerPayload

	// Focuses optionally specifies explicit focus areas (bypasses LLM decision).
	// If empty, the coordinator LLM decides focus areas based on the task.
	Focuses []string `json:"focuses,omitempty"`

	// MaxPlanners limits the number of concurrent planners (1-3).
	// 0 means LLM decides based on task complexity.
	MaxPlanners int `json:"max_planners,omitempty"`
}

// PlanCoordinatorTriggerType is the message type for plan coordinator triggers.
// NOTE: Category must match the registration in init() for proper deserialization.
var PlanCoordinatorTriggerType = message.Type{
	Domain:   "workflow",
	Category: "coordinator-trigger",
	Version:  "v1",
}

// Schema implements message.Payload.
func (p *PlanCoordinatorTrigger) Schema() message.Type {
	return PlanCoordinatorTriggerType
}

// Validate implements message.Payload.
func (p *PlanCoordinatorTrigger) Validate() error {
	if p.TriggerPayload == nil {
		return &ValidationError{Field: "workflow_trigger_payload", Message: "base payload is required"}
	}
	if p.MaxPlanners < 0 || p.MaxPlanners > 3 {
		return &ValidationError{Field: "max_planners", Message: "max_planners must be 0-3"}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *PlanCoordinatorTrigger) MarshalJSON() ([]byte, error) {
	type Alias PlanCoordinatorTrigger
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *PlanCoordinatorTrigger) UnmarshalJSON(data []byte) error {
	// Initialize embedded pointer to avoid nil pointer unmarshal error
	if p.TriggerPayload == nil {
		p.TriggerPayload = &WorkflowTriggerPayload{}
	}
	type Alias PlanCoordinatorTrigger
	return json.Unmarshal(data, (*Alias)(p))
}

// FocusedPlanTrigger extends TriggerPayload for focused planning.
// Used by the coordinator to spawn planners with specific focus areas.
type FocusedPlanTrigger struct {
	*TriggerPayload

	// PlannerID uniquely identifies this planner within a session.
	PlannerID string `json:"planner_id,omitempty"`

	// SessionID links this planner to a coordination session.
	SessionID string `json:"session_id,omitempty"`

	// Focus defines what this planner should concentrate on.
	Focus *PlannerFocus `json:"focus,omitempty"`

	// Context contains graph-derived context for this planner.
	Context *PlannerContext `json:"context,omitempty"`
}

// FocusedPlanTriggerType is the message type for focused plan triggers.
// NOTE: Category must match the registration in init() for proper deserialization.
var FocusedPlanTriggerType = message.Type{
	Domain:   "workflow",
	Category: "focused-trigger",
	Version:  "v1",
}

// Schema implements message.Payload.
func (p *FocusedPlanTrigger) Schema() message.Type {
	return FocusedPlanTriggerType
}

// Validate implements message.Payload.
func (p *FocusedPlanTrigger) Validate() error {
	if p.TriggerPayload == nil {
		return &ValidationError{Field: "workflow_trigger_payload", Message: "base payload is required"}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (p *FocusedPlanTrigger) MarshalJSON() ([]byte, error) {
	type Alias FocusedPlanTrigger
	return json.Marshal((*Alias)(p))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *FocusedPlanTrigger) UnmarshalJSON(data []byte) error {
	// Initialize embedded pointer to avoid nil pointer unmarshal error
	if p.TriggerPayload == nil {
		p.TriggerPayload = &WorkflowTriggerPayload{}
	}
	type Alias FocusedPlanTrigger
	return json.Unmarshal(data, (*Alias)(p))
}

// PlannerFocus describes what a focused planner should concentrate on.
type PlannerFocus struct {
	// Area is the focus domain (e.g., "api", "security", "data", "architecture").
	Area string `json:"area"`

	// Description explains what to focus on within this area.
	Description string `json:"description"`

	// Hints are optional file patterns or keywords to guide the planner.
	Hints []string `json:"hints,omitempty"`
}

// PlannerContext contains graph-derived context for a focused planner.
type PlannerContext struct {
	// Entities are entity IDs relevant to this focus area.
	Entities []string `json:"entities,omitempty"`

	// Files are file paths in scope for this focus area.
	Files []string `json:"files,omitempty"`

	// Summary is a brief context summary from the coordinator.
	Summary string `json:"summary,omitempty"`
}

// PlanSession tracks a multi-planner coordination session.
type PlanSession struct {
	// SessionID uniquely identifies this session.
	SessionID string `json:"session_id"`

	// Slug is the plan slug.
	Slug string `json:"slug"`

	// Title is the plan title.
	Title string `json:"title"`

	// Status tracks session progress: "coordinating", "planning", "synthesizing", "complete", "failed".
	Status string `json:"status"`

	// Planners maps planner IDs to their state.
	Planners map[string]*PlannerState `json:"planners,omitempty"`

	// CreatedAt is when the session started.
	CreatedAt time.Time `json:"created_at"`

	// CompletedAt is when the session finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PlannerState tracks an individual planner within a session.
type PlannerState struct {
	// ID uniquely identifies this planner.
	ID string `json:"id"`

	// FocusArea is the area this planner is focusing on.
	FocusArea string `json:"focus_area"`

	// Status is the planner's progress: "pending", "running", "completed", "failed".
	Status string `json:"status"`

	// Result contains the planner's output once completed.
	Result *PlannerResult `json:"result,omitempty"`

	// Error contains error details if failed.
	Error string `json:"error,omitempty"`

	// StartedAt is when this planner started.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt is when this planner finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// PlannerResult contains the output from a focused planner.
type PlannerResult struct {
	// PlannerID identifies which planner produced this result.
	PlannerID string `json:"planner_id"`

	// FocusArea is the area this planner focused on.
	FocusArea string `json:"focus_area"`

	// Goal is the goal from this planner's perspective.
	Goal string `json:"goal"`

	// Context is the context from this planner's perspective.
	Context string `json:"context"`

	// Scope is the scope from this planner's perspective.
	Scope Scope `json:"scope"`
}
