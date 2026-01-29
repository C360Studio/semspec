package query

import (
	"fmt"

	"github.com/c360/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the query processor component with the given registry
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "query",
		Factory:     NewComponent,
		Schema:      querySchema,
		Type:        "processor",
		Protocol:    "graph",
		Domain:      "semantic",
		Description: "Graph query processor for entity and relationship queries",
		Version:     "0.1.0",
	})
}
