package researchermanager

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface is the minimal registry surface needed for registration.
// Mirrors processor/question-manager's pattern.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register wires the researcher-manager component into the supplied registry.
// Called from cmd/semspec/main.go bootstrap alongside the other component
// Register() calls.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        componentName,
		Factory:     NewComponent,
		Schema:      component.ConfigSchema{},
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Owns RESEARCH KV and routes research requests from the developer to a researcher sub-agent (R-phase skeleton; dispatch wiring lands in R2/R3)",
		Version:     "0.1.0",
	})
}
