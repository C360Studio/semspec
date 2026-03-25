package planmanager

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the plan-manager component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "plan-manager",
		Factory:     NewComponent,
		Schema:      workflowAPISchema,
		Type:        "processor",
		Protocol:    "http",
		Domain:      "semspec",
		Description: "Plan lifecycle manager: CRUD, coordination, requirements, scenarios",
		Version:     "0.1.0",
	})
}
