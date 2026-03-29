package requirementgenerator

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// requirementGeneratorSchema defines the configuration schema.
var requirementGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the requirement-generator processor component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:requirement-generator"`

	// TriggerSubject is the subject pattern for requirement-generator triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for requirement-generator triggers,category:basic,default:workflow.async.requirement-generator"`

	// DefaultCapability is the model capability to use for requirement generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for requirement generation,category:basic,default:planning"`

	// PlanStateBucket is the KV bucket name to watch for approved plans (KV twofer).
	// The requirement-generator self-triggers when any plan transitions to "approved".
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for approved plans,category:advanced,default:PLAN_STATES"`

	// MaxGenerationRetries is the maximum number of times to re-dispatch the agent
	// after a parse failure before sending plan.mutation.generation.failed.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on parse failure,category:basic,default:2"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:           "WORKFLOW",
		ConsumerName:         "requirement-generator",
		TriggerSubject:       "workflow.async.requirement-generator",
		DefaultCapability:    "planning",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "requirement-generator-triggers",
					Type:        "jetstream",
					Subject:     "workflow.async.requirement-generator",
					StreamName:  "WORKFLOW",
					Description: "Receive requirement-generator triggers",
					Required:    true,
				},
			},
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
