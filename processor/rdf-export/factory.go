package rdfexport

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the rdf-export output component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "rdf-export",
		Factory:     NewComponent,
		Schema:      rdfExportSchema,
		Type:        "output",
		Protocol:    "rdf",
		Domain:      "graph",
		Description: "Serializes graph entities to RDF formats (Turtle, N-Triples, JSON-LD)",
		Version:     "1.0.0",
	})
}
