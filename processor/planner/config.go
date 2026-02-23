package planner

import (
	"fmt"
	"reflect"
	"time"

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

	// DefaultCapability is the model capability to use for planning.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for planning,category:basic,default:planning"`

	// ContextSubjectPrefix is the subject prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Subject prefix for context build requests,category:advanced,default:context.build"`

	// ContextResponseBucket is the KV bucket for context responses.
	ContextResponseBucket string `json:"context_response_bucket" schema:"type:string,description:KV bucket for context responses,category:advanced,default:CONTEXT_RESPONSES"`

	// ContextTimeout is the timeout for context building.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Timeout for context building,category:advanced,default:30s"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:            "WORKFLOW",
		ConsumerName:          "planner",
		TriggerSubject:        "workflow.async.planner",
		DefaultCapability:     "planning",
		ContextSubjectPrefix:  "context.build",
		ContextResponseBucket: "CONTEXT_RESPONSES",
		ContextTimeout:        "30s",
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

// GetContextTimeout parses the context timeout duration.
func (c *Config) GetContextTimeout() time.Duration {
	if c.ContextTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ContextTimeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}
