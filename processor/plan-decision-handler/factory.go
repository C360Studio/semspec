package changeproposalhandler

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the plan-decision-handler component with the given registry.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "plan-decision-handler",
		Factory:     NewComponent,
		Schema:      handlerSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Handles accepted PlanDecision cascade: dirty-marks affected scenarios and tasks, cancels running scenario loops",
		Version:     "0.1.0",
	})
}
