package semspectools

import (
	"fmt"

	"github.com/c360/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the semspec-tools processor component with the given registry
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "semspec-tools",
		Factory:     NewComponent,
		Schema:      semspecToolsSchema,
		Type:        "processor",
		Protocol:    "tools",
		Domain:      "semantic",
		Description: "File and git tool executor for agentic workflows",
		Version:     "0.1.0",
	})
}
