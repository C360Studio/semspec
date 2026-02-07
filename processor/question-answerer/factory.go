package questionanswerer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the question answerer component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "question-answerer",
		Factory:     NewComponent,
		Schema:      answererSchema,
		Type:        "processor",
		Protocol:    "question",
		Domain:      "agentic",
		Description: "Answers questions using LLM agents based on topic and capability",
		Version:     "0.1.0",
	})
}
