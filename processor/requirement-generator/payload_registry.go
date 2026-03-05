package requirementgenerator

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "requirement-generator-result",
		Version:     "v1",
		Description: "Requirement generation result with requirement count for a plan",
		Factory: func() any {
			return &Result{}
		},
	}); err != nil {
		panic("failed to register requirement-generator result payload: " + err.Error())
	}
}
