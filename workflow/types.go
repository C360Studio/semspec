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
	// Register TaskExecutionPayload type
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "execution",
		Version:     "v1",
		Description: "Task execution payload with context and model",
		Factory:     func() any { return &TaskExecutionPayload{} },
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
	// StatusRequirementsGenerated indicates requirements have been generated for the plan.
	StatusRequirementsGenerated Status = "requirements_generated"
	// StatusScenariosGenerated indicates scenarios have been generated for all requirements.
	StatusScenariosGenerated Status = "scenarios_generated"
	// StatusScenariosReviewed indicates scenarios have passed review but await human approval.
	// Set when auto_approve=false; the human clicks "Approve & Continue" to advance to ready_for_execution.
	StatusScenariosReviewed Status = "scenarios_reviewed"
	// StatusReadyForExecution indicates the plan has scenarios and is ready for the scenario
	// orchestrator to pick up and decompose into tasks at runtime (reactive execution mode).
	// This status is set by task-generator when reactive_mode=true, bypassing task generation.
	StatusReadyForExecution Status = "ready_for_execution"
	// StatusImplementing indicates task execution is in progress.
	StatusImplementing Status = "implementing"
	// StatusReviewingRollup indicates all scenarios have completed and the plan
	// is undergoing a final synthesis review before being marked complete.
	StatusReviewingRollup Status = "reviewing_rollup"
	// StatusComplete indicates all tasks have been completed successfully.
	StatusComplete Status = "complete"
	// StatusArchived indicates the plan has been archived.
	StatusArchived Status = "archived"
	// StatusRejected indicates the plan was rejected during review or approval.
	StatusRejected Status = "rejected"

	// In-progress statuses — claimed by watchers via plan.mutation.claim before
	// starting work. Plan-manager's single-writer serialization ensures only one
	// claim succeeds per transition. Prevents KV watcher re-triggers on partial saves.

	// StatusDrafting indicates a planner has claimed plan creation.
	StatusDrafting Status = "drafting"
	// StatusReviewingDraft indicates plan-reviewer R1 has claimed the drafted plan.
	StatusReviewingDraft Status = "reviewing_draft"
	// StatusGeneratingRequirements indicates requirement-generator has claimed the approved plan.
	StatusGeneratingRequirements Status = "generating_requirements"
	// StatusGeneratingScenarios indicates scenario-generator has claimed requirements_generated.
	StatusGeneratingScenarios Status = "generating_scenarios"
	// StatusReviewingScenarios indicates plan-reviewer R2 has claimed scenarios_generated.
	StatusReviewingScenarios Status = "reviewing_scenarios"
)

// String returns the string representation of the status.
func (s Status) String() string {
	return string(s)
}

// IsValid returns true if the status is a valid workflow status.
func (s Status) IsValid() bool {
	switch s {
	case StatusCreated, StatusDrafted, StatusReviewed, StatusApproved,
		StatusRequirementsGenerated, StatusScenariosGenerated, StatusScenariosReviewed,
		StatusReadyForExecution,
		StatusImplementing, StatusReviewingRollup, StatusComplete, StatusArchived, StatusRejected,
		StatusDrafting, StatusReviewingDraft, StatusGeneratingRequirements,
		StatusGeneratingScenarios, StatusReviewingScenarios:
		return true
	default:
		return false
	}
}

// IsInProgress returns true if the status is an intermediate in-progress state
// claimed by a watcher before starting work.
func (s Status) IsInProgress() bool {
	switch s {
	case StatusDrafting, StatusReviewingDraft, StatusGeneratingRequirements,
		StatusGeneratingScenarios, StatusReviewingScenarios:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if the status can transition to the target status.
func (s Status) CanTransitionTo(target Status) bool {
	switch s {
	case StatusCreated:
		return target == StatusDrafting || target == StatusDrafted || target == StatusRejected
	case StatusDrafting:
		return target == StatusDrafted || target == StatusRejected
	case StatusDrafted:
		// drafted → reviewing_draft (plan-reviewer R1 claims)
		// drafted → requirements_generated (new flow: req/scenario gen happens before review)
		// drafted → reviewed (legacy: review directly after drafting)
		// drafted → rejected (rejection at any stage)
		return target == StatusReviewingDraft || target == StatusRequirementsGenerated || target == StatusReviewed || target == StatusRejected
	case StatusReviewingDraft:
		// reviewing_draft → reviewed (R1 approved)
		// reviewing_draft → created (R1 retry — revision loop, ADR-029)
		// reviewing_draft → rejected (escalation)
		return target == StatusReviewed || target == StatusCreated || target == StatusRejected
	case StatusReviewed:
		return target == StatusApproved || target == StatusRejected
	case StatusApproved:
		// approved → generating_requirements (requirement-generator claims)
		// approved → requirements_generated (auto-cascade: generate requirements)
		// approved → scenarios_generated (auto-cascade race: requirements existed, skip to scenarios)
		// approved → ready_for_execution (auto-approve skips req/scenario step)
		// approved → rejected (review loop escalation)
		return target == StatusGeneratingRequirements || target == StatusRequirementsGenerated || target == StatusScenariosGenerated ||
			target == StatusReadyForExecution || target == StatusRejected
	case StatusGeneratingRequirements:
		return target == StatusRequirementsGenerated || target == StatusRejected
	case StatusRequirementsGenerated:
		return target == StatusGeneratingScenarios || target == StatusScenariosGenerated || target == StatusRejected
	case StatusGeneratingScenarios:
		return target == StatusScenariosGenerated || target == StatusRejected
	case StatusScenariosGenerated:
		// scenarios_generated → reviewing_scenarios (plan-reviewer R2 claims)
		// scenarios_generated → scenarios_reviewed (R2 done, auto_approve=false)
		// scenarios_generated → reviewed (review happens after scenario generation)
		// scenarios_generated → ready_for_execution (reactive mode — task-generator reactive_mode=true, review skipped)
		// scenarios_generated → rejected (validation failure)
		return target == StatusReviewingScenarios || target == StatusScenariosReviewed || target == StatusReviewed || target == StatusReadyForExecution || target == StatusRejected
	case StatusReviewingScenarios:
		// reviewing_scenarios → scenarios_reviewed (R2 approved, auto_approve=false)
		// reviewing_scenarios → reviewed (R2 approved, legacy path)
		// reviewing_scenarios → ready_for_execution (R2 approved, auto_approve=true)
		// reviewing_scenarios → approved (R2 retry — revision loop, ADR-029)
		// reviewing_scenarios → rejected (escalation)
		return target == StatusScenariosReviewed || target == StatusReviewed || target == StatusReadyForExecution || target == StatusApproved || target == StatusRejected
	case StatusScenariosReviewed:
		// scenarios_reviewed → ready_for_execution (human clicks "Approve & Continue")
		return target == StatusReadyForExecution || target == StatusRejected
	case StatusReadyForExecution:
		// ready_for_execution → implementing (scenario orchestrator picks up the plan)
		// ready_for_execution → rejected (orchestration failure)
		return target == StatusImplementing || target == StatusRejected
	case StatusImplementing:
		// implementing → reviewing_rollup (all scenarios done, run final synthesis review)
		// implementing → complete (legacy: no rollup reviewer configured)
		// implementing → rejected (execution escalation)
		return target == StatusReviewingRollup || target == StatusComplete || target == StatusRejected
	case StatusReviewingRollup:
		// reviewing_rollup → complete (rollup approved)
		// reviewing_rollup → rejected (rollup flagged critical issues requiring human intervention)
		return target == StatusComplete || target == StatusRejected
	case StatusComplete:
		// complete → archived (shelve)
		// complete → ready_for_execution (re-execute all requirements)
		return target == StatusArchived || target == StatusReadyForExecution
	case StatusArchived:
		// archived → complete (unarchive)
		// archived → ready_for_execution (unarchive + retry)
		return target == StatusComplete || target == StatusReadyForExecution
	case StatusRejected:
		// rejected → approved (manual R2 restart — human intervenes)
		// rejected → created (manual R1 restart — human intervenes after escalation, ADR-029)
		// rejected → ready_for_execution (retry failed requirements)
		// rejected → implementing (resume stalled plan — orchestrator already dispatched)
		return target == StatusApproved || target == StatusCreated ||
			target == StatusReadyForExecution || target == StatusImplementing
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
	HasPlan            bool `json:"has_plan"`
	HasTasks           bool `json:"has_tasks"`
	HasRequirements    bool `json:"has_requirements"`
	HasScenarios       bool `json:"has_scenarios"`
	HasChangeProposals bool `json:"has_change_proposals"`
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
	// Format: {prefix}.wf.project.project.{project-slug}
	// Required - defaults to the "default" project if not specified.
	ProjectID string `json:"project_id"`

	// Status is the authoritative workflow state for the plan.
	// When empty, EffectiveStatus() infers status from legacy boolean fields.
	Status Status `json:"status,omitempty"`

	// Approved indicates if this plan is ready for execution.
	// false = draft plan, true = user explicitly approved
	Approved bool `json:"approved"`

	// CreatedAt is when the plan was created
	CreatedAt time.Time `json:"created_at"`

	// ApprovedAt is when the plan was approved for execution
	ApprovedAt *time.Time `json:"approved_at,omitempty"`

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
	// NOTE: This is a shared budget across both review rounds (R1 + R2).
	// If R1 uses 2 of 3 iterations, R2 only has 1 remaining before escalation.
	// This is intentional — it bounds total LLM review cost per plan.
	ReviewIteration int `json:"review_iteration,omitempty"`

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

	// Requirements, Scenarios, and ChangeProposals are populated when the plan
	// is written to the PLAN_STATES KV bucket so downstream watchers have
	// everything they need without follow-up queries.
	// Not persisted to graph triples — use SaveRequirements/SaveScenarios/SaveChangeProposals for that.
	Requirements    []Requirement    `json:"requirements,omitempty"`
	Scenarios       []Scenario       `json:"scenarios,omitempty"`
	ChangeProposals []ChangeProposal `json:"change_proposals,omitempty"`
}

// FindRequirement returns a pointer into p.Requirements and its index by ID.
// Returns nil, -1 when the requirement is not found.
func (p *Plan) FindRequirement(id string) (*Requirement, int) {
	for i := range p.Requirements {
		if p.Requirements[i].ID == id {
			return &p.Requirements[i], i
		}
	}
	return nil, -1
}

// FindScenario returns a pointer into p.Scenarios and its index by ID.
// Returns nil, -1 when the scenario is not found.
func (p *Plan) FindScenario(id string) (*Scenario, int) {
	for i := range p.Scenarios {
		if p.Scenarios[i].ID == id {
			return &p.Scenarios[i], i
		}
	}
	return nil, -1
}

// ScenariosForRequirement returns all scenarios whose RequirementID matches reqID.
func (p *Plan) ScenariosForRequirement(reqID string) []Scenario {
	var out []Scenario
	for _, s := range p.Scenarios {
		if s.RequirementID == reqID {
			out = append(out, s)
		}
	}
	return out
}

// FindChangeProposal returns a pointer into p.ChangeProposals and its index by ID.
// Returns nil, -1 when the proposal is not found.
func (p *Plan) FindChangeProposal(id string) (*ChangeProposal, int) {
	for i := range p.ChangeProposals {
		if p.ChangeProposals[i].ID == id {
			return &p.ChangeProposals[i], i
		}
	}
	return nil, -1
}

// LLMCallHistory tracks LLM request IDs per review iteration for both
// plan review and task review loops. This enables the UI to correlate
// each loop iteration with its specific LLM calls for full artifact drill-down.
type LLMCallHistory struct {
	PlanReview []IterationCalls `json:"plan_review,omitempty"`
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

	// TaskStatusBlocked indicates the task is blocked by an unmet dependency
	TaskStatusBlocked TaskStatus = "blocked"

	// TaskStatusDirty indicates an upstream Scenario/Requirement was mutated by an accepted ChangeProposal
	TaskStatusDirty TaskStatus = "dirty"
)

// String returns the string representation of the task status.
func (s TaskStatus) String() string {
	return string(s)
}

// IsValid returns true if the task status is valid.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskStatusPending, TaskStatusPendingApproval, TaskStatusApproved, TaskStatusRejected,
		TaskStatusInProgress, TaskStatusCompleted, TaskStatusFailed,
		TaskStatusBlocked, TaskStatusDirty:
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
		// Can submit for approval, start directly (legacy compatibility), or become dirty
		return target == TaskStatusPendingApproval || target == TaskStatusInProgress ||
			target == TaskStatusFailed || target == TaskStatusDirty
	case TaskStatusPendingApproval:
		// Human approval decision or upstream change
		return target == TaskStatusApproved || target == TaskStatusRejected || target == TaskStatusDirty
	case TaskStatusApproved:
		// Ready for execution, blocked by dependency, or dirtied by upstream change
		return target == TaskStatusInProgress || target == TaskStatusBlocked || target == TaskStatusDirty
	case TaskStatusRejected:
		// Can be re-edited and resubmitted; or dirtied by upstream change
		return target == TaskStatusPending || target == TaskStatusDirty
	case TaskStatusInProgress:
		return target == TaskStatusCompleted || target == TaskStatusFailed
	case TaskStatusBlocked:
		// Unblocked when dependency resolves; dirty if upstream changed while blocked
		return target == TaskStatusInProgress || target == TaskStatusDirty
	case TaskStatusDirty:
		// Re-evaluate after upstream change
		return target == TaskStatusPendingApproval
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

	// Sequence is the order within the plan (1-indexed)
	Sequence int `json:"sequence"`

	// Description is what to implement
	Description string `json:"description"`

	// Type classifies the kind of work (implement, test, document, review, refactor)
	Type TaskType `json:"type,omitempty"`

	// AcceptanceCriteria lists BDD-style conditions for task completion
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria"`

	// ScenarioIDs lists scenario IDs this task satisfies (many-to-many SATISFIES edge).
	// Replaces AcceptanceCriteria after migration.
	ScenarioIDs []string `json:"scenario_ids,omitempty"`

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

// RequirementStatus represents the lifecycle state of a requirement.
type RequirementStatus string

const (
	// RequirementStatusActive indicates the requirement is current and actionable.
	RequirementStatusActive RequirementStatus = "active"

	// RequirementStatusDeprecated indicates the requirement is no longer relevant.
	RequirementStatusDeprecated RequirementStatus = "deprecated"

	// RequirementStatusSuperseded indicates the requirement was replaced by another.
	RequirementStatusSuperseded RequirementStatus = "superseded"
)

// String returns the string representation of the requirement status.
func (s RequirementStatus) String() string {
	return string(s)
}

// IsValid returns true if the requirement status is valid.
func (s RequirementStatus) IsValid() bool {
	switch s {
	case RequirementStatusActive, RequirementStatusDeprecated, RequirementStatusSuperseded:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this requirement status can transition to the target.
func (s RequirementStatus) CanTransitionTo(target RequirementStatus) bool {
	switch s {
	case RequirementStatusActive:
		return target == RequirementStatusDeprecated || target == RequirementStatusSuperseded
	case RequirementStatusSuperseded:
		// Can revert supersession if ChangeProposal is rolled back
		return target == RequirementStatusActive
	case RequirementStatusDeprecated:
		return false // Terminal state
	default:
		return false
	}
}

// Requirement represents a plan-level behavioral intent.
type Requirement struct {
	ID          string            `json:"id"`
	PlanID      string            `json:"plan_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      RequirementStatus `json:"status"`
	DependsOn   []string          `json:"depends_on,omitempty"` // IDs of prerequisite requirements
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ScenarioStatus represents the verification state of a scenario.
type ScenarioStatus string

const (
	// ScenarioStatusPending indicates the scenario has not yet been verified.
	ScenarioStatusPending ScenarioStatus = "pending"

	// ScenarioStatusPassing indicates the scenario is verified and passing.
	ScenarioStatusPassing ScenarioStatus = "passing"

	// ScenarioStatusFailing indicates the scenario is verified and failing.
	ScenarioStatusFailing ScenarioStatus = "failing"

	// ScenarioStatusSkipped indicates the scenario was intentionally skipped.
	ScenarioStatusSkipped ScenarioStatus = "skipped"
)

// String returns the string representation of the scenario status.
func (s ScenarioStatus) String() string {
	return string(s)
}

// IsValid returns true if the scenario status is valid.
func (s ScenarioStatus) IsValid() bool {
	switch s {
	case ScenarioStatusPending, ScenarioStatusPassing, ScenarioStatusFailing, ScenarioStatusSkipped:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this scenario status can transition to the target.
func (s ScenarioStatus) CanTransitionTo(target ScenarioStatus) bool {
	switch s {
	case ScenarioStatusPending:
		return target == ScenarioStatusPassing || target == ScenarioStatusFailing || target == ScenarioStatusSkipped
	case ScenarioStatusPassing:
		return target == ScenarioStatusFailing
	case ScenarioStatusFailing:
		return target == ScenarioStatusPassing
	case ScenarioStatusSkipped:
		return target == ScenarioStatusPending
	default:
		return false
	}
}

// Scenario represents a Given/When/Then behavioral contract derived from a Requirement.
type Scenario struct {
	ID            string         `json:"id"`
	RequirementID string         `json:"requirement_id"`
	Given         string         `json:"given"`
	When          string         `json:"when"`
	Then          []string       `json:"then"`
	Status        ScenarioStatus `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// ChangeProposalStatus represents the lifecycle state of a change proposal.
type ChangeProposalStatus string

const (
	// ChangeProposalStatusProposed indicates the proposal has been submitted for review.
	ChangeProposalStatusProposed ChangeProposalStatus = "proposed"

	// ChangeProposalStatusUnderReview indicates the proposal is being reviewed.
	ChangeProposalStatusUnderReview ChangeProposalStatus = "under_review"

	// ChangeProposalStatusAccepted indicates the proposal was accepted.
	ChangeProposalStatusAccepted ChangeProposalStatus = "accepted"

	// ChangeProposalStatusRejected indicates the proposal was rejected.
	ChangeProposalStatusRejected ChangeProposalStatus = "rejected"

	// ChangeProposalStatusArchived indicates the proposal has been archived.
	ChangeProposalStatusArchived ChangeProposalStatus = "archived"
)

// String returns the string representation of the change proposal status.
func (s ChangeProposalStatus) String() string {
	return string(s)
}

// IsValid returns true if the change proposal status is valid.
func (s ChangeProposalStatus) IsValid() bool {
	switch s {
	case ChangeProposalStatusProposed, ChangeProposalStatusUnderReview,
		ChangeProposalStatusAccepted, ChangeProposalStatusRejected, ChangeProposalStatusArchived:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this change proposal status can transition to the target.
func (s ChangeProposalStatus) CanTransitionTo(target ChangeProposalStatus) bool {
	switch s {
	case ChangeProposalStatusProposed:
		return target == ChangeProposalStatusUnderReview
	case ChangeProposalStatusUnderReview:
		return target == ChangeProposalStatusAccepted || target == ChangeProposalStatusRejected
	case ChangeProposalStatusAccepted:
		return target == ChangeProposalStatusArchived
	case ChangeProposalStatusRejected:
		return target == ChangeProposalStatusArchived
	case ChangeProposalStatusArchived:
		return false // Terminal state
	default:
		return false
	}
}

// ChangeProposal represents a mid-stream proposal to mutate one or more Requirements.
type ChangeProposal struct {
	ID             string               `json:"id"`
	PlanID         string               `json:"plan_id"`
	Title          string               `json:"title"`
	Rationale      string               `json:"rationale"`
	Status         ChangeProposalStatus `json:"status"`
	ProposedBy     string               `json:"proposed_by"`
	AffectedReqIDs []string             `json:"affected_requirement_ids"`
	CreatedAt      time.Time            `json:"created_at"`
	ReviewedAt     *time.Time           `json:"reviewed_at,omitempty"`
	DecidedAt      *time.Time           `json:"decided_at,omitempty"`
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
