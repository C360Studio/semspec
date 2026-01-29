package constitution

import (
	"fmt"

	"github.com/c360/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the constitution processor component with the given registry
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "constitution",
		Factory:     NewComponent,
		Schema:      constitutionSchema,
		Type:        "processor",
		Protocol:    "constitution",
		Domain:      "semantic",
		Description: "Constitution enforcement for project-wide constraints",
		Version:     "0.1.0",
	})
}
