package planreviewer

import "github.com/c360studio/semstreams/payloadregistry"

// RegisterPayloads registers plan-reviewer payload types with the supplied
// registry. Called from cmd/semspec/main.go bootstrap.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "workflow",
		Category:    "review-result",
		Version:     "v1",
		Description: "Plan review result payload",
		Factory:     func() any { return &PlanReviewResult{} },
	})
}
