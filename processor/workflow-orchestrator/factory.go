package workfloworchestrator

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the workflow orchestrator component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "workflow-orchestrator",
		Factory:     NewComponent,
		Schema:      orchestratorSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "agentic",
		Description: "Watches loop completions and triggers next workflow steps for autonomous mode",
		Version:     "0.1.0",
	})
}
