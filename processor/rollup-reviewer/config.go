package rollupreviewer

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// rollupReviewerSchema defines the configuration schema.
var rollupReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the rollup-reviewer component configuration.
type Config struct {
	// StreamName is the JetStream stream to consume triggers from.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:rollup-reviewer"`

	// TriggerSubject is the subject pattern for triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:NATS subject for triggers,category:basic,default:workflow.async.rollup-reviewer"`

	// DefaultCapability is the model capability to use for rollup review.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Model capability for review,category:basic,default:qa"`

	// SkipReview when true skips LLM review and auto-approves the rollup.
	// Phase 1 always auto-approves regardless of this setting.
	SkipReview bool `json:"skip_review" schema:"type:bool,description:Skip LLM review and auto-approve,category:basic,default:false"`

	// PlanStateBucket is the KV bucket name to watch for plans reaching
	// "reviewing_rollup" status (KV twofer self-trigger).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for reviewing_rollup plans,category:advanced,default:PLAN_STATES"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:        "WORKFLOW",
		ConsumerName:      "rollup-reviewer",
		TriggerSubject:    "workflow.async.rollup-reviewer",
		DefaultCapability: "qa",
		SkipReview:        false,
		PlanStateBucket:   "PLAN_STATES",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "rollup-triggers",
					Type:        "jetstream",
					Subject:     "workflow.async.rollup-reviewer",
					StreamName:  "WORKFLOW",
					Description: "Receive rollup review triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "rollup-events",
					Type:        "nats",
					Subject:     "plan.mutation.rollup.complete",
					Description: "Publish rollup-complete mutations to plan-manager",
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
