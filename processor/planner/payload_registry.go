package planner

import "github.com/c360studio/semstreams/payloadregistry"

func init() {
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "planner-result",
		Version:     "v1",
		Description: "Plan generation result with Goal/Context/Scope content",
		Factory:     func() any { return &Result{} },
	}); err != nil {
		panic("failed to register planner result payload: " + err.Error())
	}
}
