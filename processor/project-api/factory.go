package projectapi

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface required for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the project-api component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "project-api",
		Factory:     NewComponent,
		Schema:      projectAPISchema,
		Type:        "processor",
		Protocol:    "http",
		Domain:      "semspec",
		Description: "HTTP endpoints for project initialization and status",
		Version:     "0.1.0",
	})
}
