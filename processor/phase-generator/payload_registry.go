package phasegenerator

import "github.com/c360studio/semstreams/component"

func init() {
	if err := component.RegisterPayload(&component.PayloadRegistration{
		Domain:      "workflow",
		Category:    "phase-generator-result",
		Version:     "v1",
		Description: "Phase generation result with generated phases",
		Factory: func() any {
			return &Result{}
		},
	}); err != nil {
		panic("failed to register phase-generator result payload: " + err.Error())
	}
}
