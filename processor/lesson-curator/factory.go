package lessoncurator

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface is the minimal surface needed to register the component.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register adds the lesson-curator component to the supplied registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "lesson-curator",
		Factory:     NewComponent,
		Schema:      curatorSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "ADR-033 Phase 5: retires stale lessons via periodic sweep over the lessons graph",
		Version:     "0.1.0",
	})
}
