package scenariogenerator

import "github.com/c360studio/semstreams/payloadregistry"

// RegisterPayloads registers scenario-generator payload types with the supplied
// registry. Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "scenario-generator-result",
		Version:     "v1",
		Description: "Scenario generation result with BDD scenarios for a single requirement",
		Factory: func() any {
			return &Result{}
		},
	})
}
