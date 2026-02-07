package workflowdocuments

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the workflow-documents output component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "workflow-documents",
		Factory:     NewComponent,
		Schema:      workflowDocumentsSchema,
		Type:        "output",
		Protocol:    "workflow",
		Domain:      "documents",
		Description: "Transforms workflow JSON content to markdown files in .semspec/changes/",
		Version:     "1.0.0",
	})
}
