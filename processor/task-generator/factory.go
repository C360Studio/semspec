package taskgenerator

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the task generator component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "task-generator",
		Factory:     NewComponent,
		Schema:      taskGeneratorSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Generates tasks from plans using LLM with BDD acceptance criteria",
		Version:     "0.1.0",
	})
}
