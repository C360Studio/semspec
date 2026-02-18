package planreviewer

import "github.com/c360studio/semstreams/component"

func init() {
	// Register PlanReviewTrigger type for message deserialization.
	// Uses "plan-review-trigger" category to avoid conflict with semstreams' generic
	// workflow trigger type registered as {workflow, trigger, v1}.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "plan-review-trigger",
		Version:     "v1",
		Description: "Plan review trigger payload",
		Factory:     func() any { return &PlanReviewTrigger{} },
	}); err != nil {
		panic("failed to register plan review trigger payload: " + err.Error())
	}

	// Register PlanReviewResult type for message deserialization
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "review-result",
		Version:     "v1",
		Description: "Plan review result payload",
		Factory:     func() any { return &PlanReviewResult{} },
	}); err != nil {
		panic("failed to register plan review result payload: " + err.Error())
	}
}
