package planner

import "github.com/c360studio/semstreams/payloadregistry"

// RegisterPayloads registers planner payload types with the supplied registry.
// Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "planner-result",
		Version:     "v1",
		Description: "Plan generation result with Goal/Context/Scope content",
		Factory:     func() any { return &Result{} },
	})
}
