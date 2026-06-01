package storypreparer

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// storyPreparerSchema defines the configuration schema.
var storyPreparerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the story-preparer component configuration.
type Config struct {
	// Enabled gates whether the component actively claims plans reaching
	// architecture_generated. Defaults to false so ADR-043 PR 3 ships in a
	// dormant state — the component registers, builds, and tests cleanly,
	// but the workflow flow (architecture_generated → scenarios_generated)
	// is unchanged. PR 4 of ADR-043 flips this on when execution-manager
	// + scenario-generator are reworked to consume Stories. Operators can
	// flip it earlier per-instance for canarying.
	Enabled bool `json:"enabled" schema:"type:bool,description:Enable story-preparer claim path (ADR-043 PR 3 ships off; PR 4 flips on),category:basic,default:false"`

	// DefaultCapability is the model capability to use for story preparation.
	// Sarah is a planning/structural persona — the same capability slot that
	// other planning generators (analyst, architecture) use is the natural
	// default. The instance config can override per-deployment.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Model capability for story preparation,category:basic,default:planning"`

	// PlanStateBucket is the KV bucket name to watch for plans reaching
	// architecture_generated status (KV twofer self-trigger).
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for architecture_generated plans,category:advanced,default:PLAN_STATES"`

	// MaxGenerationRetries is the maximum number of times to retry story
	// preparation when the agent loop fails or output cannot be parsed.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on generation failure,category:basic,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a generation failure. See workflow/dispatchretry for semantics.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between generation retries (ms),category:advanced,default:200"`

	// AttachResponseFormat gates whether dispatches attach the strict
	// response_format JSON-schema wrapper. nil preserves endpoint default;
	// false drops to L3-only (free-form pre-tool reasoning allowed).
	AttachResponseFormat *bool `json:"attach_response_format,omitempty" schema:"type:bool,description:Attach strict response_format to dispatches (L2); nil=endpoint default; false=drop to L3-only,category:advanced"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:              false,
		DefaultCapability:    "planning",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		RetryBackoffMs:       200,
	}
}

// Validate validates the configuration. Always passes — the schema and
// defaults cover the meaningful invariants; runtime validation happens at
// dispatch time.
func (c *Config) Validate() error {
	return nil
}
