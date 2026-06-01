package storypreparer

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// RegistryInterface defines the minimal interface needed for registration.
type RegistryInterface interface {
	RegisterWithConfig(component.RegistrationConfig) error
}

// Register registers the story-preparer component with the given registry
// (ADR-043 Move 3). The component ships dormant (Config.Enabled defaults to
// false) so PR 3 lands without disturbing the existing
// architecture_generated → scenarios_generated flow. PR 4 flips Enabled to
// true alongside execution-manager + scenario-generator rewiring.
func Register(registry RegistryInterface) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "story-preparer",
		Factory:     NewComponent,
		Schema:      storyPreparerSchema,
		Type:        "processor",
		Protocol:    "workflow",
		Domain:      "semspec",
		Description: "Sharded Sarah (BMAD PO) component: shards requirements into ready-for-dev Stories with Task checklists (ADR-043 Move 3)",
		Version:     "0.1.0",
	})
}
