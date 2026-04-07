// Package workflow provides typed NATS subject definitions for semspec domain events.
//
// These subjects split the former single "workflow.events" subject into per-event-type
// subjects under "workflow.events.<domain>.<action>", enabling type-safe subscribe
// and subject-based routing (ADR-020).
package workflow

import (
	"encoding/json"

	"github.com/c360studio/semstreams/natsclient"
)

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
	// Requirement/Scenario generation events (ADR-026 cascade)
	RequirementsGenerated = natsclient.NewSubject[RequirementsGeneratedEvent](
		"workflow.events.requirements.generated")
	ScenariosForRequirementGenerated = natsclient.NewSubject[ScenariosForRequirementGeneratedEvent](
		"workflow.events.scenarios.requirement_generated")
	ScenariosGenerated = natsclient.NewSubject[ScenariosGeneratedEvent](
		"workflow.events.scenarios.generated")
	GenerationFailed = natsclient.NewSubject[GenerationFailedEvent](
		"workflow.events.generation.failed")

	// Requirement execution events
	RequirementExecutionComplete = natsclient.NewSubject[RequirementExecutionCompleteEvent](
		"workflow.events.requirement.execution_complete")

	// User signal events (on USER stream)
	UserEscalation = natsclient.NewSubject[EscalationEvent](
		"user.signal.escalate")
	UserSignalError = natsclient.NewSubject[UserSignalErrorEvent](
		"user.signal.error")
)
