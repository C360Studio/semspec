package architecturegenerator

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// architectureGeneratorSchema defines the configuration schema.
var architectureGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the architecture-generator component configuration.
type Config struct {
	// StreamName is the JetStream stream to consume triggers from.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:architecture-generator"`

	// TriggerSubject is the subject pattern for triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:NATS subject for triggers,category:basic,default:workflow.async.architecture-generator"`

	// DefaultCapability is the model capability to use for architecture generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Model capability for generation,category:basic,default:architecture"`

	// PlanStateBucket is the KV bucket name to watch for plans reaching
	// "requirements_generated" status (KV twofer self-trigger).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for requirements_generated plans,category:advanced,default:PLAN_STATES"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:        "WORKFLOW",
		ConsumerName:      "architecture-generator",
		TriggerSubject:    "workflow.async.architecture-generator",
		DefaultCapability: "architecture",
		PlanStateBucket:   "PLAN_STATES",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "architecture-triggers",
					Type:        "jetstream",
					Subject:     "workflow.async.architecture-generator",
					StreamName:  "WORKFLOW",
					Description: "Receive architecture generation triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "architecture-events",
					Type:        "nats",
					Subject:     "plan.mutation.architecture.generated",
					Description: "Publish architecture-generated mutations to plan-manager",
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
