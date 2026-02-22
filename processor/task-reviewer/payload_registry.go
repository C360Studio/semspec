package taskreviewer

import "github.com/c360studio/semstreams/component"

func init() {
	// Register TaskReviewTrigger type for message deserialization.
	// Uses "task-review-trigger" category to avoid conflict with other trigger types.
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "task-review-trigger",
		Version:     "v1",
		Description: "Task review trigger payload",
		Factory:     func() any { return &TaskReviewTrigger{} },
	}); err != nil {
		panic("failed to register task review trigger payload: " + err.Error())
	}

	// Register TaskReviewResult type for message deserialization
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "task-review-result",
		Version:     "v1",
		Description: "Task review result payload",
		Factory:     func() any { return &TaskReviewResult{} },
	}); err != nil {
		panic("failed to register task review result payload: " + err.Error())
	}
}
