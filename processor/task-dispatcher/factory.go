package taskdispatcher

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the task-dispatcher component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "task-dispatcher",
		Factory:     NewComponent,
		Schema:      taskDispatcherSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Dispatches tasks with parallel context building and dependency-aware execution",
		Version:     "0.1.0",
	})
}
