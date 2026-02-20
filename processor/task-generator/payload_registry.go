package taskgenerator

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "task-generator-result",
		Version:     "v1",
		Description: "Task generation result with generated tasks",
		Factory:     func() any { return &Result{} },
	}); err != nil {
		panic("failed to register task-generator result payload: " + err.Error())
	}
}
