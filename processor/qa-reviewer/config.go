package qareviewer

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// qaReviewerSchema defines the configuration schema.
var qaReviewerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the qa-reviewer component configuration.
type Config struct {
	// DefaultCapability is the model capability to use for the QA verdict.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Model capability for QA verdict,category:basic,default:qa"`

	// PlanStateBucket is the KV bucket name to watch for plans reaching
	// ready_for_qa with level=synthesis. Non-synthesis plans are driven by
	// the QACompleted JetStream consumer instead.
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for synthesis-level plans entering QA review,category:advanced,default:PLAN_STATES"`

	// MaxReviewRetries is the maximum number of times to retry the QA reviewer
	// agent loop when the loop fails or the submit_work output cannot be parsed.
	// After exhausting retries the plan is rejected (fail-closed). Default 2.
	MaxReviewRetries int `json:"max_review_retries" schema:"type:integer,description:Max retries on QA review failure,category:advanced,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a QA review failure. Effective per-attempt sleep is in
	// [RetryBackoffMs, 2*RetryBackoffMs). Default 200ms — short enough to keep
	// happy-path latency invisible, long enough to keep parse-failure storms
	// from saturating agentic-dispatch and the LLM endpoint. Set to 1 to
	// effectively disable in tests; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between QA review retries (ms),category:advanced,default:200"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability: "qa",
		PlanStateBucket:   "PLAN_STATES",
		MaxReviewRetries:  2,
		RetryBackoffMs:    200,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
