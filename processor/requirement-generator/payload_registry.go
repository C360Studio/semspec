package requirementgenerator

import "github.com/c360studio/semstreams/payloadregistry"

// RegisterPayloads registers requirement-generator payload types with the
// supplied registry. Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "requirement-generator-result",
		Version:     "v1",
		Description: "Requirement generation result with requirement count for a plan",
		Factory: func() any {
			return &Result{}
		},
	})
}
