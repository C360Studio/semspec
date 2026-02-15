package plancoordinator

import "github.com/c360studio/semstreams/component"

func init() {
	// Register CoordinatorResult type for message deserialization
	_ = component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "result",
		Version:     "v1",
		Description: "Plan coordinator result payload",
		Factory:     func() any { return &CoordinatorResult{} },
	})
}
