package developer

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// developerSchema defines the configuration schema.
var developerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the developer component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:AGENT"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:developer"`

	// TriggerSubject is the subject to subscribe to for developer requests.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject to subscribe to,category:basic,default:dev.task.development"`

	// StateBucket is the KV bucket for workflow state (reactive engine state).
	StateBucket string `json:"state_bucket" schema:"type:string,description:KV bucket for workflow state,category:basic,default:REACTIVE_STATE"`

	// DefaultCapability is the model capability for development tasks.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default LLM capability,category:basic,default:coding"`

	// Timeout is the LLM call timeout.
	Timeout string `json:"timeout" schema:"type:string,description:LLM call timeout,category:advanced,default:120s"`

	// MaxToolIterations is the maximum number of tool call iterations before stopping.
	// Each iteration involves an LLM call that may request tools, execute them, and loop.
	MaxToolIterations int `json:"max_tool_iterations" schema:"type:int,description:Maximum tool call iterations,category:advanced,default:10"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:        "AGENT",
		ConsumerName:      "developer",
		TriggerSubject:    "dev.task.development",
		StateBucket:       "REACTIVE_STATE",
		DefaultCapability: "coding",
		Timeout:           "120s",
		MaxToolIterations: 10,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "development-triggers",
					Type:        "jetstream",
					Subject:     "dev.task.development",
					StreamName:  "AGENT",
					Description: "Receive development task requests",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "development-results",
					Type:        "nats",
					Subject:     "workflow.result.developer.>",
					Description: "Publish development results",
					Required:    false,
				},
			},
		},
	}
}

// GetTimeout parses the timeout duration.
// Returns 120 seconds if the field is empty or unparseable.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout == "" {
		return 120 * time.Second
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 120 * time.Second
	}
	return d
}

// Validate checks the configuration for errors.
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
	if c.StateBucket == "" {
		return fmt.Errorf("state_bucket is required")
	}
	return nil
}
