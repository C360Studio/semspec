package scenariogenerator

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// scenarioGeneratorSchema defines the configuration schema.
var scenarioGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the scenario-generator component configuration.
type Config struct {
	// StreamName is the JetStream stream to consume triggers from.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:scenario-generator"`

	// TriggerSubject is the subject pattern for triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:NATS subject for triggers,category:basic,default:workflow.async.scenario-generator"`

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
		StreamName:           "WORKFLOW",
		ConsumerName:         "scenario-generator",
		TriggerSubject:       "workflow.async.scenario-generator",
		DefaultCapability:    "planning",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "scenario-triggers",
					Type:        "jetstream",
					Subject:     "workflow.async.scenario-generator",
					StreamName:  "WORKFLOW",
					Description: "Receive scenario generation triggers",
					Required:    true,
				},
			},
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
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.TriggerSubject == "" {
		return fmt.Errorf("trigger_subject is required")
	}
	return nil
}
