package taskdispatcher

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "dispatch-result",
		Version:     "v1",
		Description: "Batch task dispatch result with counts",
		Factory:     func() any { return &BatchDispatchResult{} },
	}); err != nil {
		panic("failed to register dispatch result payload: " + err.Error())
	}
}
