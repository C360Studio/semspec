package requirementgenerator

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// requirementGeneratorSchema defines the configuration schema.
var requirementGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the requirement-generator processor component.
type Config struct {
	// DefaultCapability is the model capability to use for requirement generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for requirement generation,category:basic,default:planning"`

	// PlanStateBucket is the KV bucket name to watch for approved plans (KV twofer).
	// The requirement-generator self-triggers when any plan transitions to "approved".
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for approved plans,category:advanced,default:PLAN_STATES"`

	// MaxGenerationRetries is the maximum number of times to re-dispatch the agent
	// after a parse failure before sending plan.mutation.generation.failed.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on parse failure,category:basic,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a generation failure. See workflow/dispatchretry for semantics.
	// Default 200ms; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between generation retries (ms),category:advanced,default:200"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability:    "planning",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		RetryBackoffMs:       200,
		Ports: &component.PortConfig{
			Outputs: []component.PortDefinition{
				{
					Name:        "requirements-generated-events",
					Type:        "nats",
					Subject:     "workflow.events.requirements.generated",
					Description: "Publish requirements-generated events",
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
