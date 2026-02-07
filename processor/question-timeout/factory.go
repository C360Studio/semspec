package questiontimeout

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the question timeout component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "question-timeout",
		Factory:     NewComponent,
		Schema:      timeoutSchema,
		Type:        "processor",
		Protocol:    "question",
		Domain:      "agentic",
		Description: "Monitors question SLAs and triggers escalation when exceeded",
		Version:     "0.1.0",
	})
}
