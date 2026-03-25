// Package workflow provides typed NATS subject definitions for semspec domain events.
//
// These subjects split the former single "workflow.events" subject into per-event-type
// subjects under "workflow.events.<domain>.<action>", enabling type-safe subscribe
// and subject-based routing (ADR-020).
//
// Use reactive.ParseReactivePayload[T] to parse these typed events on the consumer side.
package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/natsclient"
)

// Plan review lifecycle events (from plan-review-loop workflow)

// PlanApprovedEvent is published when a plan passes review.
type PlanApprovedEvent struct {
	Slug          string   `json:"slug"`
	Verdict       string   `json:"verdict"`
	Summary       string   `json:"summary,omitempty"`
	LLMRequestIDs []string `json:"llm_request_ids,omitempty"`

	// IterationHistory carries the complete LLM call history accumulated across
	// all review iterations (rejections + final approval). Written to plan.json
	// atomically by the event handler, avoiding a race with the planner's
	// concurrent plan.json save that could lose intermediate revision entries.
	IterationHistory []IterationCalls `json:"iteration_history,omitempty"`
}

// PlanRevisionNeededEvent is published when a plan needs revision.
type PlanRevisionNeededEvent struct {
	Slug          string          `json:"slug"`
	Iteration     int             `json:"iteration"`
	Verdict       string          `json:"verdict"`
	Findings      json.RawMessage `json:"findings,omitempty"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// PlanReviewLoopCompleteEvent is published when the plan review loop finishes.
type PlanReviewLoopCompleteEvent struct {
	Slug       string `json:"slug"`
	Iterations int    `json:"iterations"`
}

// Requirement/Scenario generation lifecycle events (ADR-026 cascade)

// RequirementsGeneratedEvent is published by the requirement-generator when
// requirements are generated for a plan. Carries the full requirement data
// so plan-manager (the single writer) can persist through its store.
type RequirementsGeneratedEvent struct {
	Slug             string        `json:"slug"`
	Requirements     []Requirement `json:"requirements"`
	RequirementCount int           `json:"requirement_count"` // len(Requirements), for logging
	TraceID          string        `json:"trace_id,omitempty"`
}

// ScenariosForRequirementGeneratedEvent is published by the scenario-generator
// for a single requirement. Plan-manager accumulates these and checks convergence
// (all requirements covered) before advancing to scenarios_generated.
type ScenariosForRequirementGeneratedEvent struct {
	Slug          string     `json:"slug"`
	RequirementID string     `json:"requirement_id"`
	Scenarios     []Scenario `json:"scenarios"`
	TraceID       string     `json:"trace_id,omitempty"`
}

// ScenariosGeneratedEvent is published by plan-manager when all requirements
// have scenarios (convergence). Downstream consumers use this to advance the pipeline.
type ScenariosGeneratedEvent struct {
	Slug          string `json:"slug"`
	ScenarioCount int    `json:"scenario_count"`
	TraceID       string `json:"trace_id,omitempty"`
}

// GenerationFailedEvent is published when a generator fails after all retries.
// Plan-manager marks the plan as errored on receipt.
type GenerationFailedEvent struct {
	Slug    string `json:"slug"`
	Phase   string `json:"phase"` // "requirements" or "scenarios"
	Error   string `json:"error"`
	TraceID string `json:"trace_id,omitempty"`
}

// Phase generation lifecycle event (from phase-generator workflow)

// PhasesGeneratedEvent is published when phases are generated from a plan.
type PhasesGeneratedEvent struct {
	Slug       string `json:"slug"`
	PhaseCount int    `json:"phase_count"`
	RequestID  string `json:"request_id,omitempty"`
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

// ScenarioExecutionCompleteEvent is published when a scenario finishes execution
// (all DAG nodes completed or one failed). Published to workflow.events.scenario.execution_complete.
type ScenarioExecutionCompleteEvent struct {
	Slug          string   `json:"slug"`
	ScenarioID    string   `json:"scenario_id"`
	ProjectID     string   `json:"project_id,omitempty"`
	TraceID       string   `json:"trace_id,omitempty"`
	Outcome       string   `json:"outcome"` // "completed" or "failed"
	NodeCount     int      `json:"node_count"`
	FilesModified []string `json:"files_modified,omitempty"`
}

// RequirementExecutionCompleteEvent is published when a requirement finishes execution
// (all DAG nodes completed and scenarios validated). Published to workflow.events.requirement.execution_complete.
type RequirementExecutionCompleteEvent struct {
	Slug            string   `json:"slug"`
	RequirementID   string   `json:"requirement_id"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	ProjectID       string   `json:"project_id,omitempty"`
	TraceID         string   `json:"trace_id,omitempty"`
	Outcome         string   `json:"outcome"` // "completed" or "failed"
	NodeCount       int      `json:"node_count"`
	FilesModified   []string `json:"files_modified,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	ScenariosTotal  int      `json:"scenarios_total"`
	ScenariosPassed int      `json:"scenarios_passed"`
}

// User signal events (from USER stream — escalation and error signals)

// EscalationEvent is published when a workflow exhausts its retry budget and
// needs human intervention. Published to user.signal.escalate.
type EscalationEvent struct {
	Slug              string          `json:"slug"`
	TaskID            string          `json:"task_id,omitempty"`
	Reason            string          `json:"reason"`
	LastVerdict       string          `json:"last_verdict,omitempty"`
	LastFindings      json.RawMessage `json:"last_findings,omitempty"`
	LastFeedback      string          `json:"last_feedback,omitempty"`
	FormattedFindings string          `json:"formatted_findings,omitempty"`
	Iteration         int             `json:"iteration,omitempty"`
}

// UserSignalErrorEvent is published when a workflow step fails unexpectedly.
// Published to user.signal.error.
type UserSignalErrorEvent struct {
	Slug   string `json:"slug"`
	TaskID string `json:"task_id,omitempty"`
	Error  string `json:"error"`
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

	// Phase generation event
	PhasesGenerated = natsclient.NewSubject[PhasesGeneratedEvent](
		"workflow.events.phases.generated")

	// Requirement/Scenario generation events (ADR-026 cascade)
	RequirementsGenerated = natsclient.NewSubject[RequirementsGeneratedEvent](
		"workflow.events.requirements.generated")
	ScenariosForRequirementGenerated = natsclient.NewSubject[ScenariosForRequirementGeneratedEvent](
		"workflow.events.scenarios.requirement_generated")
	ScenariosGenerated = natsclient.NewSubject[ScenariosGeneratedEvent](
		"workflow.events.scenarios.generated")
	GenerationFailed = natsclient.NewSubject[GenerationFailedEvent](
		"workflow.events.generation.failed")

	// Task execution events
	StructuralValidationPassed = natsclient.NewSubject[StructuralValidationPassedEvent](
		"workflow.events.task.validation_passed")
	RejectionCategorized = natsclient.NewSubject[RejectionCategorizedEvent](
		"workflow.events.task.rejection_categorized")
	TaskExecutionComplete = natsclient.NewSubject[TaskExecutionCompleteEvent](
		"workflow.events.task.execution_complete")

	// Scenario execution events (legacy — kept for backward compat during migration)
	ScenarioExecutionComplete = natsclient.NewSubject[ScenarioExecutionCompleteEvent](
		"workflow.events.scenario.execution_complete")

	// Requirement execution events
	RequirementExecutionComplete = natsclient.NewSubject[RequirementExecutionCompleteEvent](
		"workflow.events.requirement.execution_complete")

	// User signal events (on USER stream)
	UserEscalation = natsclient.NewSubject[EscalationEvent](
		"user.signal.escalate")
	UserSignalError = natsclient.NewSubject[UserSignalErrorEvent](
		"user.signal.error")
)
