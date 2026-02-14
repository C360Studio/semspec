package taskgenerator

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// taskGeneratorSchema defines the configuration schema.
var taskGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the task generator component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOWS"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:task-generator"`

	// TriggerSubject is the subject pattern for task generation triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for task generation triggers,category:basic,default:workflow.trigger.task-generator"`

	// DefaultCapability is the model capability to use for task generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for task generation,category:basic,default:planning"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:        "WORKFLOWS",
		ConsumerName:      "task-generator",
		TriggerSubject:    "workflow.trigger.task-generator",
		DefaultCapability: "planning",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "task-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.task-generator",
					StreamName:  "WORKFLOWS",
					Description: "Receive task generation triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "task-results",
					Type:        "nats",
					Subject:     "workflow.result.task-generator.>",
					Description: "Publish task generation results",
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
