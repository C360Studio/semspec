package sourceingester

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the source-ingester processor component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "source-ingester",
		Factory:     NewComponent,
		Schema:      sourceIngesterSchema,
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "semantic",
		Description: "Document and SOP ingester for knowledge graph population",
		Version:     "0.1.0",
	})
}
