package lessondecomposer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface required for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the lesson-decomposer component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "lesson-decomposer",
		Factory:     NewComponent,
		Schema:      lessonDecomposerSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "ADR-033 Phase 2+: produces evidence-cited lessons from reviewer-rejection trajectories",
		Version:     "0.1.0",
	})
}
