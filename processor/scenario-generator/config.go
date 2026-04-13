package scenariogenerator

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// scenarioGeneratorSchema defines the configuration schema.
var scenarioGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the scenario-generator component configuration.
type Config struct {
	// DefaultCapability is the model capability to use for scenario generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Model capability for generation,category:basic,default:planning"`

	// PlanStateBucket is the KV bucket name to watch for plans reaching
	// "requirements_generated" status (KV twofer self-trigger).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for requirements_generated plans,category:advanced,default:PLAN_STATES"`

	// MaxGenerationRetries is the maximum number of times to retry scenario
	// generation for a single requirement when the LLM output cannot be parsed.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries per requirement on parse failure,category:basic,default:2"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability:    "planning",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		Ports: &component.PortConfig{
			Outputs: []component.PortDefinition{
				{
					Name:        "scenario-events",
					Type:        "nats",
					Subject:     "workflow.events.scenarios.generated",
					Description: "Publish scenarios-generated events",
					Required:    false,
				},
			},
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
