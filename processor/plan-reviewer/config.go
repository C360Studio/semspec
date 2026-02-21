package planreviewer

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// planReviewerSchema defines the configuration schema.
var planReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan reviewer component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:plan-reviewer"`

	// TriggerSubject is the subject pattern for plan review triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for plan review triggers,category:basic,default:workflow.trigger.plan-reviewer"`

	// ResultSubjectPrefix is the prefix for result subjects.
	ResultSubjectPrefix string `json:"result_subject_prefix" schema:"type:string,description:Subject prefix for plan review results,category:basic,default:workflow.result.plan-reviewer"`

	// LLMTimeout is the timeout for LLM calls.
	LLMTimeout string `json:"llm_timeout" schema:"type:string,description:Timeout for LLM calls (duration string),category:advanced,default:120s"`

	// DefaultCapability is the model capability to use for plan review.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for plan review,category:basic,default:reviewing"`

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
		ConsumerName:          "plan-reviewer",
		TriggerSubject:        "workflow.trigger.plan-reviewer",
		ResultSubjectPrefix:   "workflow.result.plan-reviewer",
		LLMTimeout:            "120s",
		DefaultCapability:     "reviewing",
		ContextSubjectPrefix:  "context.build",
		ContextResponseBucket: "CONTEXT_RESPONSES",
		ContextTimeout:        "30s",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "review-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.plan-reviewer",
					StreamName:  "WORKFLOW",
					Description: "Receive plan review triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "review-results",
					Type:        "nats",
					Subject:     "workflow.result.plan-reviewer.>",
					Description: "Publish plan review results",
					Required:    false,
				},
			},
		},
	}
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
