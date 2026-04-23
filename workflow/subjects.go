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

// QA phase events — target-project test execution gate between implementing
// and complete. Executor is selected by Mode: sandbox for unit, qa-runner
// (act-based) for integration and full. qa-reviewer is always the verdict
// gate, consuming QACompletedEvent and emitting QAVerdictEvent.

// QARequestedEvent is published by plan-manager when a plan enters ready_for_qa.
// Consumer routing is by Mode: QALevelUnit → sandbox, QALevelIntegration|Full
// → qa-runner container. Level=synthesis skips this event (plan goes straight
// to reviewing_qa) and level=none skips QA entirely.
type QARequestedEvent struct {
	Slug              string  `json:"slug"`
	PlanID            string  `json:"plan_id"`
	Mode              QALevel `json:"mode"`                   // unit | integration | full
	WorkspaceHostPath string  `json:"workspace_host_path"`    // resolved HOST path for docker -v mount
	WorkflowPath      string  `json:"workflow_path"`          // relative path inside workspace, default .github/workflows/qa.yml (integration+ only)
	TestCommand       string  `json:"test_command,omitempty"` // project-configured command for unit mode, e.g. "go test ./..."
	TimeoutSeconds    int     `json:"timeout_seconds,omitempty"`
	TraceID           string  `json:"trace_id,omitempty"`
}

// QAFailure describes a single test or job failure surfaced by qa-runner.
// qa-reviewer consumes these to produce targeted PlanDecisions.
type QAFailure struct {
	JobName    string `json:"job_name"`
	StepName   string `json:"step_name,omitempty"`
	TestName   string `json:"test_name,omitempty"`
	Message    string `json:"message,omitempty"`
	LogExcerpt string `json:"log_excerpt,omitempty"`
}

// QAArtifactRef is a workspace-relative reference to an artifact produced by a
// QA run (logs, screenshots, traces, coverage reports).
type QAArtifactRef struct {
	Path    string `json:"path"`              // workspace-relative: .semspec/qa-artifacts/{slug}/{run-id}/...
	Type    string `json:"type"`              // log | screenshot | trace | coverage
	Purpose string `json:"purpose,omitempty"` // e.g., "playwright flow X failure"
}

// QACompletedEvent is published by the QA executor (sandbox for unit mode,
// qa-runner for integration/full) after test execution. qa-reviewer consumes
// it and produces a QAVerdictEvent. plan-manager does NOT consume this event
// directly.
type QACompletedEvent struct {
	Slug        string          `json:"slug"`
	PlanID      string          `json:"plan_id"`
	RunID       string          `json:"run_id"`
	Level       QALevel         `json:"level"` // level the executor ran at
	Passed      bool            `json:"passed"`
	Failures    []QAFailure     `json:"failures,omitempty"`
	Artifacts   []QAArtifactRef `json:"artifacts,omitempty"`
	DurationMs  int64           `json:"duration_ms"`
	RunnerError string          `json:"runner_error,omitempty"` // executor infra error, distinct from test failures
	TraceID     string          `json:"trace_id,omitempty"`
}

// QAVerdict describes qa-reviewer's release-readiness decision.
type QAVerdict string

const (
	// QAVerdictApproved means the plan passes qa-reviewer's judgment and can
	// proceed to complete (or awaiting_review when gated).
	QAVerdictApproved QAVerdict = "approved"
	// QAVerdictNeedsChanges means qa-reviewer found issues fixable with a
	// retry. PlanDecisions accompany this verdict.
	QAVerdictNeedsChanges QAVerdict = "needs_changes"
	// QAVerdictRejected means qa-reviewer escalates to human — plan cannot be
	// salvaged by automated retry.
	QAVerdictRejected QAVerdict = "rejected"
)

// QAVerdictDimensions carries the per-axis assessment qa-reviewer produces.
// Dimensions not assessed at the current level stay at their zero value;
// consumers distinguish "not assessed" from "assessed-and-failed" via the
// level recorded on QAVerdictEvent.
type QAVerdictDimensions struct {
	RequirementFulfillment string `json:"requirement_fulfillment,omitempty"` // always — old rollup responsibility
	Coverage               string `json:"coverage,omitempty"`                // level ≥ unit
	AssertionQuality       string `json:"assertion_quality,omitempty"`       // level ≥ unit
	RegressionSurface      string `json:"regression_surface,omitempty"`      // level ≥ unit
	FlakeJudgment          string `json:"flake_judgment,omitempty"`          // level ≥ integration
}

// QAVerdictEvent is published by qa-reviewer. plan-manager consumes it to
// transition the plan to its terminal state (complete, awaiting_review, or
// rejected) and to surface any emitted PlanDecisions.
type QAVerdictEvent struct {
	Slug       string              `json:"slug"`
	PlanID     string              `json:"plan_id"`
	Level      QALevel             `json:"level"` // level this verdict applies to
	Verdict    QAVerdict           `json:"verdict"`
	Summary    string              `json:"summary,omitempty"`
	Dimensions QAVerdictDimensions `json:"dimensions,omitempty"`
	// PlanDecisions carries fully-formed PlanDecision objects for plan-manager to
	// persist. Populated by qa-reviewer when verdict is needs_changes. plan-manager
	// writes them to the plan and fills PlanDecisionIDs from the assigned IDs.
	PlanDecisions   []PlanDecision `json:"plan_decisions,omitempty"`
	PlanDecisionIDs []string       `json:"plan_decision_ids,omitempty"`
	ReviewerError   string         `json:"reviewer_error,omitempty"`
	TraceID         string         `json:"trace_id,omitempty"`
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

	// QA phase events — qa-runner / sandbox publish QACompletedEvent; qa-reviewer
	// delivers its verdict via request/reply mutation plan.mutation.qa.verdict
	// (see processor/plan-manager/mutations.go) rather than a fire-and-forget event.
	QARequested = natsclient.NewSubject[QARequestedEvent](
		"workflow.events.qa.requested")
	QACompleted = natsclient.NewSubject[QACompletedEvent](
		"workflow.events.qa.completed")
)
