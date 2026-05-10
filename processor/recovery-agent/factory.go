package recoveryagent

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface required for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the recovery-agent component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "recovery-agent",
		Factory:     NewComponent,
		Schema:      recoveryAgentSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "ADR-037 stage 1: dispatches manager-role recovery agents on plan/exec wedges, publishes RecoveryComplete with a bounded RecoveryAction",
		Version:     "0.1.0",
	})
}
