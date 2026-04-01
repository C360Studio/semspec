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
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:plan-reviewer"`

	// TriggerSubject is the subject pattern for plan review triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for plan review triggers,category:basic,default:workflow.async.plan-reviewer"`

	// ResultSubjectPrefix is the prefix for result subjects.
	ResultSubjectPrefix string `json:"result_subject_prefix" schema:"type:string,description:Subject prefix for plan review results,category:basic,default:workflow.result.plan-reviewer"`

	// LLMTimeout is the timeout for LLM calls.
	LLMTimeout string `json:"llm_timeout" schema:"type:string,description:Timeout for LLM calls (duration string),category:advanced,default:120s"`

	// DefaultCapability is the model capability to use for plan review.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for plan review,category:basic,default:plan_review"`

	// AutoApprove controls whether the reviewer automatically sends the
	// approved mutation after a successful round-1 review. When false, the
	// plan stays at StatusReviewed and waits for human approval via the
	// /promote endpoint. Default true preserves backward compatibility.
	AutoApprove *bool `json:"auto_approve" schema:"type:bool,description:Skip human approval gate after review,category:basic,default:true"`

	// PlanStateBucket is the KV bucket name to watch for plan state transitions
	// (KV twofer). The plan-reviewer self-triggers when a plan reaches "drafted"
	// (round 1) or "scenarios_generated" (round 2).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for plan state transitions,category:advanced,default:PLAN_STATES"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:          "WORKFLOW",
		ConsumerName:        "plan-reviewer",
		TriggerSubject:      "workflow.async.plan-reviewer",
		ResultSubjectPrefix: "workflow.result.plan-reviewer",
		LLMTimeout:          "120s",
		DefaultCapability:   "plan_review",
		PlanStateBucket:     "PLAN_STATES",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "review-triggers",
					Type:        "jetstream",
					Subject:     "workflow.async.plan-reviewer",
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

// IsAutoApprove returns true when the reviewer should automatically approve
// plans after a successful review. Defaults to true when not set.
func (c *Config) IsAutoApprove() bool {
	if c.AutoApprove == nil {
		return true
	}
	return *c.AutoApprove
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
