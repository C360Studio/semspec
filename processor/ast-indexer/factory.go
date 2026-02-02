package astindexer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the ast-indexer processor component with the given registry
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "ast-indexer",
		Factory:     NewComponent,
		Schema:      astIndexerSchema,
		Type:        "processor",
		Protocol:    "ast",
		Domain:      "semantic",
		Description: "Go AST indexer for code entity extraction and graph storage",
		Version:     "0.1.0",
	})
}
