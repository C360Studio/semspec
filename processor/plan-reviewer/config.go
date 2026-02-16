package planreviewer

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// planReviewerSchema defines the configuration schema.
var planReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan reviewer component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOWS"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:plan-reviewer"`

	// TriggerSubject is the subject pattern for plan review triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for plan review triggers,category:basic,default:workflow.trigger.plan-reviewer"`

	// ResultSubjectPrefix is the prefix for result subjects.
	ResultSubjectPrefix string `json:"result_subject_prefix" schema:"type:string,description:Subject prefix for plan review results,category:basic,default:workflow.result.plan-reviewer"`

	// ContextBuildTimeout is the timeout for context building requests.
	ContextBuildTimeout string `json:"context_build_timeout" schema:"type:string,description:Timeout for context building (duration string),category:advanced,default:30s"`

	// LLMTimeout is the timeout for LLM calls.
	LLMTimeout string `json:"llm_timeout" schema:"type:string,description:Timeout for LLM calls (duration string),category:advanced,default:120s"`

	// DefaultCapability is the model capability to use for plan review.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for plan review,category:basic,default:reviewing"`

	// GraphGatewayURL is the URL for the graph gateway to query entities.
	GraphGatewayURL string `json:"graph_gateway_url" schema:"type:string,description:Graph gateway URL for context queries,category:advanced,default:http://localhost:8082"`

	// ContextTokenBudget is the token budget for additional context building.
	ContextTokenBudget int `json:"context_token_budget" schema:"type:int,description:Token budget for additional context,category:advanced,default:4000,min:1000,max:16000"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:          "WORKFLOWS",
		ConsumerName:        "plan-reviewer",
		TriggerSubject:      "workflow.trigger.plan-reviewer",
		ResultSubjectPrefix: "workflow.result.plan-reviewer",
		ContextBuildTimeout: "30s",
		LLMTimeout:          "120s",
		DefaultCapability:   "reviewing",
		GraphGatewayURL:     "http://localhost:8082",
		ContextTokenBudget:  4000,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "review-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.plan-reviewer",
					StreamName:  "WORKFLOWS",
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
