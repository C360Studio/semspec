package planreviewer

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// planReviewerSchema defines the configuration schema.
var planReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan reviewer component.
type Config struct {
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

	// MaxReviewRetries is the maximum number of times to retry a review when
	// the agent loop fails or the output cannot be parsed. Default 2.
	MaxReviewRetries int `json:"max_review_retries" schema:"type:integer,description:Max retries on review failure (parse error or loop failure),category:advanced,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a review failure. See workflow/dispatchretry for semantics.
	// Default 200ms; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between review retries (ms),category:advanced,default:200"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability: "plan_review",
		PlanStateBucket:   "PLAN_STATES",
		MaxReviewRetries:  2,
		RetryBackoffMs:    200,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{},
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
	return nil
}
