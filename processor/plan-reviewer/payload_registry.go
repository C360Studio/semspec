package planreviewer

import "github.com/c360studio/semstreams/component"

func init() {
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
