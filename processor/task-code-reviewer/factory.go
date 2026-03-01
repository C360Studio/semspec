package taskcodereview

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the task-code-reviewer component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "task-code-reviewer",
		Factory:     NewComponent,
		Schema:      component.ConfigSchema{},
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Reviews code changes made by the developer agent",
		Version:     "0.1.0",
	})
}
