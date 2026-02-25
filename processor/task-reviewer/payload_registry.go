package taskreviewer

import "github.com/c360studio/semstreams/component"

func init() {
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
