package payloads

import (
	"errors"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// RegisterPayloads registers all payload types owned by the workflow/payloads
// package with the supplied registry. Called from cmd/semspec/main.go
// bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return errors.Join(
		registerTriggerPayload(reg),
		registerRequestPayloads(reg),
		registerValidationResult(reg),
	)
}

// registerTriggerPayload registers the workflow trigger payload.
//
// The reactive engine receives triggers on workflow.trigger.* subjects.
// These messages use workflow.trigger.v1 type and need to be registered
// for BaseMessage decoding to deserialize them correctly.
func registerTriggerPayload(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      workflow.WorkflowTriggerType.Domain,
		Category:    workflow.WorkflowTriggerType.Category,
		Version:     workflow.WorkflowTriggerType.Version,
		Description: "Workflow trigger payload for reactive engine",
		Factory:     func() any { return &workflow.TriggerPayload{} },
	})
}

func registerRequestPayloads(reg *payloadregistry.Registry) error {
	payloads := []struct {
		msgType message.Type
		desc    string
		factory func() any
	}{
		{PlannerRequestType, "Planner request from reactive workflow engine", func() any { return &PlannerRequest{} }},
		{PlanReviewRequestType, "Plan review request from reactive workflow engine", func() any { return &PlanReviewRequest{} }},
		{PhaseReviewRequestType, "Phase review request from reactive workflow engine", func() any { return &PhaseReviewRequest{} }},
		{TaskGeneratorRequestType, "Task generator request from reactive workflow engine", func() any { return &TaskGeneratorRequest{} }},
		{TaskReviewRequestType, "Task review request from reactive workflow engine", func() any { return &TaskReviewRequest{} }},
		{DeveloperRequestType, "Developer agent request from reactive workflow engine", func() any { return &DeveloperRequest{} }},
		{ValidationRequestType, "Structural validation request from reactive workflow engine", func() any { return &ValidationRequest{} }},
		{TaskCodeReviewRequestType, "Task code review request from reactive workflow engine", func() any { return &TaskCodeReviewRequest{} }},
		// New reactive request types (replacing legacy trigger types)
		{PlanCoordinatorRequestType, "Plan coordinator request from reactive workflow engine", func() any { return &PlanCoordinatorRequest{} }},
		{TaskDispatchRequestType, "Task dispatch request from reactive workflow engine", func() any { return &TaskDispatchRequest{} }},
		{QuestionAnswerRequestType, "Question answer request from reactive workflow engine", func() any { return &QuestionAnswerRequest{} }},
		{ContextBuildRequestType, "Context build request from reactive workflow engine", func() any { return &ContextBuildRequest{} }},
		// Graph topology refactor payload types (ADR-024)
		{RequirementGeneratorRequestType, "Requirement generator request from reactive workflow engine", func() any { return &RequirementGeneratorRequest{} }},
		{ScenarioGeneratorRequestType, "Scenario generator request from reactive workflow engine", func() any { return &ScenarioGeneratorRequest{} }},
		{PlanDecisionReviewRequestType, "Change proposal review request from reactive workflow engine", func() any { return &PlanDecisionReviewRequest{} }},
		{PlanDecisionCascadeRequestType, "Change proposal cascade request from reactive workflow engine", func() any { return &PlanDecisionCascadeRequest{} }},
		{PlanDecisionAcceptedEventType, "Change proposal accepted event with cascade summary", func() any { return &PlanDecisionAcceptedEvent{} }},
		// Scenario orchestration and execution (Phase 4)
		{ScenarioOrchestrationTriggerType, "Scenario orchestration trigger for plan execution", func() any { return &ScenarioOrchestrationTrigger{} }},
		{ScenarioExecutionRequestType, "Scenario execution request from scenario-orchestrator", func() any { return &ScenarioExecutionRequest{} }},
		{RequirementExecutionRequestType, "Requirement execution request from scenario-orchestrator", func() any { return &RequirementExecutionRequest{} }},
		// Generation event payloads (single-writer fix)
		{ScenariosForRequirementGeneratedType, "Per-requirement scenario generation result", func() any { return &ScenariosForRequirementGeneratedPayload{} }},
		{GenerationFailedType, "Generation failure event from requirement/scenario generators", func() any { return &GenerationFailedPayload{} }},
		// GitHub integration payloads (ADR-031)
		{GitHubPlanCreationRequestType, "GitHub issue-to-plan creation request", func() any { return &GitHubPlanCreationRequest{} }},
		{GitHubPRCreatedEventType, "GitHub PR created event", func() any { return &GitHubPRCreatedEvent{} }},
		{GitHubPRFeedbackRequestType, "GitHub PR feedback request from review", func() any { return &GitHubPRFeedbackRequest{} }},
		// QA phase payloads
		{QARequestedType, "QA execution request dispatched to sandbox (unit) or qa-runner (integration/full)", func() any { return &QARequestedPayload{} }},
		{QACompletedType, "QA execution result event published by sandbox or qa-runner", func() any { return &QACompletedPayload{} }},
	}

	var errs []error
	for _, p := range payloads {
		errs = append(errs, reg.Register(&payloadregistry.Registration{
			Domain:      p.msgType.Domain,
			Category:    p.msgType.Category,
			Version:     p.msgType.Version,
			Description: p.desc,
			Factory:     p.factory,
		}))
	}
	return errors.Join(errs...)
}
