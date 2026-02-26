package developer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface required for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the developer component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "developer",
		Factory:     NewComponent,
		Schema:      developerSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Bridges LLM development to reactive workflow state",
		Version:     "0.1.0",
	})
}
