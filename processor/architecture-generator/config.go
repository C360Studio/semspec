package architecturegenerator

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// architectureGeneratorSchema defines the configuration schema.
var architectureGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the architecture-generator component configuration.
type Config struct {
	// DefaultCapability is the model capability to use for architecture generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Model capability for generation,category:basic,default:architecture"`

	// PlanStateBucket is the KV bucket name to watch for plans reaching
	// "requirements_generated" status (KV twofer self-trigger).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for requirements_generated plans,category:advanced,default:PLAN_STATES"`

	// RequirementsReviewEnabled mirrors plan-reviewer's flag of the same name
	// (ADR-051 Slice 4). When true, the plan-reviewer claims
	// requirements_generated → reviewing_requirements, so the architect must NOT
	// race it for that state — it instead claims generating_architecture from the
	// post-review requirements_reviewed state. When false (default), no review
	// runs and the architect claims from requirements_generated directly, as
	// before.
	//
	// CROSS-COMPONENT INVARIANT: this MUST equal plan-reviewer's
	// requirements_review_enabled. Source both from the one
	// REQUIREMENTS_REVIEW_ENABLED env var in semspec.json. A mismatch wedges the
	// requirements phase (one of requirements_generated / requirements_reviewed
	// ends up with no claimant).
	RequirementsReviewEnabled bool `json:"requirements_review_enabled" schema:"type:bool,description:Mirror of plan-reviewer requirements_review_enabled — claim generating_architecture from requirements_reviewed instead of requirements_generated,category:advanced,default:false"`

	// MaxGenerationRetries is the maximum number of times to retry architecture
	// generation when the agent loop fails or output cannot be parsed.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on generation failure,category:basic,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a generation failure. See workflow/dispatchretry for semantics.
	// Default 200ms; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between generation retries (ms),category:advanced,default:200"`

	// AttachResponseFormat gates whether dispatches attach the strict
	// response_format JSON-schema wrapper. nil (omitted) preserves existing
	// behavior — attach where the endpoint supports it. Explicit false
	// drops the L2 wire constraint so the model can emit free-form pre-tool
	// reasoning text before the strict tool-args call (L3-only, see
	// docs/structured-output-levels.md). Flows through to
	// AssemblyContext.HasResponseFormat so prompt assembly re-injects
	// schema prose when the wire constraint is off.
	AttachResponseFormat *bool `json:"attach_response_format,omitempty" schema:"type:bool,description:Attach strict response_format to dispatches (L2). nil=endpoint default; false=drop to L3-only.,category:advanced"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability:    "architecture",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		RetryBackoffMs:       200,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
