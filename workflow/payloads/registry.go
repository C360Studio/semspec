package payloads

import (
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
)

// RegisterPayloads registers all payload types from this package with the
// semstreams component registry. Call this once during startup.
//
// NOTE: During the reactive→orchestrator migration, the reactive package's
// init() still registers these same types. Call RegisterPayloads() AFTER
// removing the reactive package's registrations to avoid duplicate panics.
// This will happen in Phase 1 when the first orchestrator component is created.
func RegisterPayloads() {
	registerTriggerPayload()
	registerRequestPayloads()
}

func registerTriggerPayload() {
	// The reactive engine receives triggers on workflow.trigger.* subjects.
	// These messages use workflow.trigger.v1 type and need to be registered
	// for BaseMessage.UnmarshalJSON to deserialize them correctly.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      workflow.WorkflowTriggerType.Domain,
		Category:    workflow.WorkflowTriggerType.Category,
		Version:     workflow.WorkflowTriggerType.Version,
		Description: "Workflow trigger payload for reactive engine",
		Factory:     func() any { return &workflow.TriggerPayload{} },
	}); err != nil {
		panic("failed to register trigger payload: " + err.Error())
	}
}

func registerRequestPayloads() {
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
		{ChangeProposalReviewRequestType, "Change proposal review request from reactive workflow engine", func() any { return &ChangeProposalReviewRequest{} }},
		{ChangeProposalCascadeRequestType, "Change proposal cascade request from reactive workflow engine", func() any { return &ChangeProposalCascadeRequest{} }},
		{ChangeProposalAcceptedEventType, "Change proposal accepted event with cascade summary", func() any { return &ChangeProposalAcceptedEvent{} }},
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

	for _, p := range payloads {
		if err := component.RegisterPayload(&component.PayloadRegistration{
			Domain:      p.msgType.Domain,
			Category:    p.msgType.Category,
			Version:     p.msgType.Version,
			Description: p.desc,
			Factory:     p.factory,
		}); err != nil {
			panic("failed to register reactive payload " + p.msgType.Category + ": " + err.Error())
		}
	}
}
