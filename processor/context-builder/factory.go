package contextbuilder

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the context-builder component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "context-builder",
		Factory:     NewComponent,
		Schema:      contextBuilderSchema,
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "semspec",
		Description: "Builds relevant context for workflow tasks based on task type and token budget",
		Version:     "0.1.0",
	})
}
