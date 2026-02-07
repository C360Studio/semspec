package workflowvalidator

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the workflow-validator processor with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "workflow-validator",
		Factory:     NewComponent,
		Schema:      workflowValidatorSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "validation",
		Description: "Request/reply service for validating workflow documents",
		Version:     "1.0.0",
	})
}
