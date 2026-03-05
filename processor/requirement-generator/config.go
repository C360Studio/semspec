package requirementgenerator

import (
	"fmt"
	"reflect"
	"time"

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

	// ContextSubjectPrefix is the subject prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Subject prefix for context build requests,category:advanced,default:context.build"`

	// ContextTimeout is the timeout for context building.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Timeout for context building,category:advanced,default:30s"`

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
		ContextSubjectPrefix: "context.build",
		ContextTimeout:       "30s",
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
