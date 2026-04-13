package rollupreviewer

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// rollupReviewerSchema defines the configuration schema.
var rollupReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the rollup-reviewer component configuration.
type Config struct {
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
		DefaultCapability: "qa",
		SkipReview:        false,
		PlanStateBucket:   "PLAN_STATES",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
