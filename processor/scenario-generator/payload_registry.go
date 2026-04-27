package scenariogenerator

import "github.com/c360studio/semstreams/payloadregistry"

func init() {
	if err := payloadregistry.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "scenario-generator-result",
		Version:     "v1",
		Description: "Scenario generation result with BDD scenarios for a single requirement",
		Factory: func() any {
			return &Result{}
		},
	}); err != nil {
		panic("failed to register scenario-generator result payload: " + err.Error())
	}
}
