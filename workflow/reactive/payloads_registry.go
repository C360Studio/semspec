package reactive

import (
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

func init() {
	// Register trigger payload for reactive engine to receive workflow triggers.
	registerTriggerPayload()

	// Register request payload types for BaseMessage deserialization.
	// These enable components to deserialize reactive engine dispatches.
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
		{PhaseGeneratorRequestType, "Phase generator request from reactive workflow engine", func() any { return &PhaseGeneratorRequest{} }},
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
		// Coordination loop payload types
		{CoordinationPlannerMessageType, "Coordination planner dispatch message", func() any { return &CoordinationPlannerMessage{} }},
		{CoordinationPlannerResultType, "Coordination planner result for engine merge", func() any { return &CoordinationPlannerResult{} }},
		{CoordinationSynthesisRequestType, "Coordination synthesis request from engine", func() any { return &CoordinationSynthesisRequest{} }},
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


// RegisterResultTypes registers callback result type factories with the
// reactive engine's workflow registry. This enables the callback handler
// to deserialize component outputs into typed structs.
//
// Called from RegisterAll after the workflow registry is available.
func RegisterResultTypes(registry *reactiveEngine.WorkflowRegistry) error {
	// Planner result — output from workflow.async.planner callbacks
	if err := registry.RegisterResultType(
		"workflow.planner-result.v1",
		func() message.Payload { return &PlannerResult{} },
	); err != nil {
		return err
	}

	// Plan review result — output from workflow.async.plan-reviewer callbacks
	if err := registry.RegisterResultType(
		"workflow.review-result.v1",
		func() message.Payload { return &ReviewResult{} },
	); err != nil {
		return err
	}

	// Phase generator result — output from workflow.async.phase-generator callbacks
	if err := registry.RegisterResultType(
		"workflow.phase-generator-result.v1",
		func() message.Payload { return &PhaseGeneratorResult{} },
	); err != nil {
		return err
	}

	// Task generator result — output from workflow.async.task-generator callbacks
	if err := registry.RegisterResultType(
		"workflow.task-generator-result.v1",
		func() message.Payload { return &TaskGeneratorResult{} },
	); err != nil {
		return err
	}

	// Task review result — output from workflow.async.task-reviewer callbacks
	if err := registry.RegisterResultType(
		"workflow.task-review-result.v1",
		func() message.Payload { return &TaskReviewResult{} },
	); err != nil {
		return err
	}

	// Validation result — output from structural-validator callbacks
	if err := registry.RegisterResultType(
		"workflow.validation-result.v1",
		func() message.Payload { return &ValidationResult{} },
	); err != nil {
		return err
	}

	// Developer result — output from dev.task.development callbacks
	if err := registry.RegisterResultType(
		"workflow.developer-result.v1",
		func() message.Payload { return &DeveloperResult{} },
	); err != nil {
		return err
	}

	// Task code review result — output from agent.task.review callbacks
	return registry.RegisterResultType(
		"workflow.task-code-review-result.v1",
		func() message.Payload { return &TaskCodeReviewResult{} },
	)
}
