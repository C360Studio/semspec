package trajectoryapi

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the trajectory-api component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "trajectory-api",
		Factory:     NewComponent,
		Schema:      trajectoryAPISchema,
		Type:        "processor",
		Protocol:    "http",
		Domain:      "semspec",
		Description: "HTTP endpoints for querying LLM call trajectories and agent loop history",
		Version:     "0.1.0",
	})
}
