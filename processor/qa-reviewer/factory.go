package qareviewer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the qa-reviewer component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "qa-reviewer",
		Factory:     NewComponent,
		Schema:      qaReviewerSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Renders the release-readiness verdict for completed plans; Phase 2 auto-approves, Phase 6 wires LLM review scoped by qa.level",
		Version:     "0.1.0",
	})
}
