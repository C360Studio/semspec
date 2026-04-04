package planner

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// plannerSchema defines the configuration schema.
var plannerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the planner processor component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:planner"`

	// TriggerSubject is the subject pattern for planner triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for planner triggers,category:basic,default:workflow.async.planner"`

	// MaxGenerationRetries is the maximum number of times to retry planning
	// after a loop failure (timeout, max iterations) or parse error before
	// rejecting the plan. Set to 0 to disable retries.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on loop failure or parse error,category:basic,default:2"`

	// DefaultCapability is the model capability to use for planning.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for planning,category:basic,default:planning"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:           "WORKFLOW",
		ConsumerName:         "planner",
		TriggerSubject:       "workflow.async.planner",
		MaxGenerationRetries: 2,
		DefaultCapability:    "planning",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "planner-triggers",
					Type:        "jetstream",
					Subject:     "workflow.async.planner",
					StreamName:  "WORKFLOW",
					Description: "Receive planner triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "planner-results",
					Type:        "nats",
					Subject:     "workflow.result.planner.>",
					Description: "Publish planner results",
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
