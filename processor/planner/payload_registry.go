package planner

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "planner-result",
		Version:     "v1",
		Description: "Plan generation result with Goal/Context/Scope content",
		Factory:     func() any { return &Result{} },
	}); err != nil {
		panic("failed to register planner result payload: " + err.Error())
	}
}
