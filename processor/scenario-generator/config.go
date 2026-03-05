package scenariogenerator

import (
	"fmt"
	"reflect"
	"time"

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

	// ContextSubjectPrefix is the prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Context build subject prefix,category:advanced,default:context.build"`

	// ContextTimeout is the timeout for context building.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Context build timeout,category:advanced,default:30s"`

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
		ContextSubjectPrefix: "context.build",
		ContextTimeout:       "30s",
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

// GetContextTimeout returns the context build timeout as a duration.
func (c *Config) GetContextTimeout() time.Duration {
	if c.ContextTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ContextTimeout)
	if err != nil || d == 0 {
		return 30 * time.Second
	}
	return d
}
