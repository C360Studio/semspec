package reactive

import (
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	reactiveEngine "github.com/c360studio/semstreams/processor/reactive"
)

func init() {
	// Register request payload types for BaseMessage deserialization.
	// These enable components to deserialize reactive engine dispatches.
	registerRequestPayloads()

	// Register result payload types for callback deserialization.
	// These are registered with the reactive engine's WorkflowRegistry at
	// workflow registration time (see register.go), but the component registry
	// also needs them for BaseMessage wrapping/unwrapping.
	registerResultPayloads()
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

func registerResultPayloads() {
	// Register AsyncStepResult for callback deserialization.
	// The reactive callback handler (callback.go:handleCallbackMessage) unmarshals
	// callbacks as BaseMessage, which requires this type in the global payload registry.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "async-result",
		Version:     "v1",
		Description: "Async step result for reactive workflow callbacks",
		Factory:     func() any { return &reactiveEngine.AsyncStepResult{} },
	}); err != nil {
		panic("failed to register async-result payload: " + err.Error())
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

	// Developer result — output from agent.task.development callbacks
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
