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
		return target == StatusImplementing
	case StatusImplementing:
		return target == StatusComplete
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
	HasPlan  bool `json:"has_plan"`
	HasTasks bool `json:"has_tasks"`
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

	// Goal describes what we're building or fixing
	Goal string `json:"goal,omitempty"`

	// Context describes the current state and why this matters
	Context string `json:"context,omitempty"`

	// Scope defines file/directory boundaries for this plan
	Scope Scope `json:"scope,omitempty"`
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
	// TaskStatusPending indicates the task has not started
	TaskStatusPending TaskStatus = "pending"

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
	case TaskStatusPending, TaskStatusInProgress, TaskStatusCompleted, TaskStatusFailed:
		return true
	default:
		return false
	}
}

// CanTransitionTo returns true if this status can transition to the target status.
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	switch s {
	case TaskStatusPending:
		return target == TaskStatusInProgress || target == TaskStatusFailed
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

	// CompletedAt is when the task finished (success or failure)
	CompletedAt *time.Time `json:"completed_at,omitempty"`
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
