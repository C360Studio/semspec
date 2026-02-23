// Package workflow provides typed NATS subject definitions for semspec domain events.
//
// These subjects split the former single "workflow.events" subject into per-event-type
// subjects under "workflow.events.<domain>.<action>", enabling type-safe subscribe
// and subject-based routing (ADR-020).
//
// Workflow publish actions emit GenericJSONPayload+BaseMessage on the wire.
// Use ParseNATSMessage[T] to unwrap into these typed events on the consumer side.
package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/natsclient"
)

// Plan review lifecycle events (from plan-review-loop workflow)

// PlanApprovedEvent is published when a plan passes review.
type PlanApprovedEvent struct {
	Slug    string `json:"slug"`
	Verdict string `json:"verdict"`
	Summary string `json:"summary,omitempty"`
}

// PlanRevisionNeededEvent is published when a plan needs revision.
type PlanRevisionNeededEvent struct {
	Slug      string          `json:"slug"`
	Iteration int             `json:"iteration"`
	Verdict   string          `json:"verdict"`
	Findings  json.RawMessage `json:"findings,omitempty"`
}

// PlanReviewLoopCompleteEvent is published when the plan review loop finishes.
type PlanReviewLoopCompleteEvent struct {
	Slug       string `json:"slug"`
	Iterations int    `json:"iterations"`
}

// Task review lifecycle events (from task-review-loop workflow)

// TasksApprovedEvent is published when generated tasks pass review.
type TasksApprovedEvent struct {
	Slug      string `json:"slug"`
	Verdict   string `json:"verdict"`
	Summary   string `json:"summary,omitempty"`
	TaskCount int    `json:"task_count,omitempty"`
}

// TasksRevisionNeededEvent is published when generated tasks need revision.
type TasksRevisionNeededEvent struct {
	Slug      string          `json:"slug"`
	Iteration int             `json:"iteration"`
	Verdict   string          `json:"verdict"`
	Findings  json.RawMessage `json:"findings,omitempty"`
}

// TaskReviewLoopCompleteEvent is published when the task review loop finishes.
type TaskReviewLoopCompleteEvent struct {
	Slug       string `json:"slug"`
	Iterations int    `json:"iterations"`
}

// Task execution lifecycle events (from task-execution-loop workflow)

// StructuralValidationPassedEvent is published when structural validation passes.
type StructuralValidationPassedEvent struct {
	TaskID    string `json:"task_id"`
	ChecksRun int    `json:"checks_run,omitempty"`
}

// RejectionCategorizedEvent is published when a reviewer rejection is categorized.
type RejectionCategorizedEvent struct {
	Type string `json:"type"`
}

// TaskExecutionCompleteEvent is published when task execution finishes.
type TaskExecutionCompleteEvent struct {
	TaskID     string `json:"task_id"`
	Iterations int    `json:"iterations"`
}

// Typed subject definitions for semspec domain events.
// These provide compile-time type safety for NATS publish/subscribe operations.
var (
	// Plan review events
	PlanApproved = natsclient.NewSubject[PlanApprovedEvent](
		"workflow.events.plan.approved")
	PlanRevisionNeeded = natsclient.NewSubject[PlanRevisionNeededEvent](
		"workflow.events.plan.revision_needed")
	PlanReviewLoopComplete = natsclient.NewSubject[PlanReviewLoopCompleteEvent](
		"workflow.events.plan.review_complete")

	// Task review events
	TasksApproved = natsclient.NewSubject[TasksApprovedEvent](
		"workflow.events.tasks.approved")
	TasksRevisionNeeded = natsclient.NewSubject[TasksRevisionNeededEvent](
		"workflow.events.tasks.revision_needed")
	TaskReviewLoopComplete = natsclient.NewSubject[TaskReviewLoopCompleteEvent](
		"workflow.events.tasks.review_complete")

	// Task execution events
	StructuralValidationPassed = natsclient.NewSubject[StructuralValidationPassedEvent](
		"workflow.events.task.validation_passed")
	RejectionCategorized = natsclient.NewSubject[RejectionCategorizedEvent](
		"workflow.events.task.rejection_categorized")
	TaskExecutionComplete = natsclient.NewSubject[TaskExecutionCompleteEvent](
		"workflow.events.task.execution_complete")
)
