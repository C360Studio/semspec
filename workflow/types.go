// Package workflow provides the Semspec workflow system for managing
// proposals, specs, and changes through a structured development process.
package workflow

import (
	"time"
)

// Status represents the current state of a change in the workflow.
type Status string

const (
	StatusCreated      Status = "created"
	StatusDrafted      Status = "drafted"
	StatusReviewed     Status = "reviewed"
	StatusApproved     Status = "approved"
	StatusImplementing Status = "implementing"
	StatusComplete     Status = "complete"
	StatusArchived     Status = "archived"
	StatusRejected     Status = "rejected"
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

// Change represents an active change in the workflow.
// Changes live in .semspec/changes/{slug}/ and contain proposal, design, spec, and tasks.
type Change struct {
	// Slug is the URL-friendly identifier for the change
	Slug string `json:"slug"`

	// Title is the human-readable title
	Title string `json:"title"`

	// Description is the original description provided when creating the change
	Description string `json:"description"`

	// Status is the current workflow state
	Status Status `json:"status"`

	// Author is the user who created the change
	Author string `json:"author"`

	// CreatedAt is when the change was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the change was last modified
	UpdatedAt time.Time `json:"updated_at"`

	// Files tracks which files exist for this change
	Files ChangeFiles `json:"files"`

	// RelatedEntities contains graph entity IDs related to this change
	RelatedEntities []string `json:"related_entities,omitempty"`

	// GitHub contains GitHub issue tracking metadata
	GitHub *GitHubMetadata `json:"github,omitempty"`
}

// GitHubMetadata tracks GitHub issue information for a change.
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

// ChangeFiles tracks which files exist for a change.
type ChangeFiles struct {
	HasProposal bool `json:"has_proposal"`
	HasDesign   bool `json:"has_design"`
	HasSpec     bool `json:"has_spec"`
	HasTasks    bool `json:"has_tasks"`
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

	// OriginChange is the change that created this spec (if any)
	OriginChange string `json:"origin_change,omitempty"`
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
// Plans start as explorations (Committed=false) and can be promoted
// to committed plans ready for execution.
type Plan struct {
	// ID is the unique identifier for the plan entity
	ID string `json:"id"`

	// Slug is the URL-friendly identifier (used for file paths)
	Slug string `json:"slug"`

	// Title is the human-readable title
	Title string `json:"title"`

	// Committed indicates if this plan is ready for execution.
	// false = exploration phase, true = committed plan
	Committed bool `json:"committed"`

	// CreatedAt is when the plan was created
	CreatedAt time.Time `json:"created_at"`

	// CommittedAt is when the plan was promoted to committed status
	CommittedAt *time.Time `json:"committed_at,omitempty"`

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

	// Status is the current execution state
	Status TaskStatus `json:"status"`

	// CreatedAt is when the task was created
	CreatedAt time.Time `json:"created_at"`

	// CompletedAt is when the task finished (success or failure)
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
