package explorer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the explorer component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "explorer",
		Factory:     NewComponent,
		Schema:      explorerSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Generates exploration content (Goal/Context/Questions) using LLM",
		Version:     "0.1.0",
	})
}
