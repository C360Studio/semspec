package plancoordinator

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "coordinator-result",
		Version:     "v1",
		Description: "Plan coordinator result with planner count and status",
		Factory:     func() any { return &CoordinatorResult{} },
	}); err != nil {
		panic("failed to register coordinator result payload: " + err.Error())
	}
}
