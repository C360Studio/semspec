package storypreparer

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// storyPreparerSchema defines the configuration schema.
var storyPreparerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the story-preparer component configuration.
//
// ADR-043 PR 4l removed the Enabled flag — it was a PR 3-era safety hatch
// that became a footgun. Sarah is always-on when the component is
// registered; disabling Sarah means removing the component from the
// registry, not flipping a config bit. The scenario-generator now
// watches only stories_generated (PR 4l) so the flow is strictly
// sequential: arch_generated → preparing_stories → stories_generated →
// generating_scenarios.
type Config struct {
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

	// ArchitectureReviewEnabled mirrors plan-reviewer's flag of the same name
	// (ADR-051 Slice 3). When true, the plan-reviewer claims
	// architecture_generated → reviewing_architecture, so Sarah must NOT race it
	// for that state — she instead claims preparing_stories from the post-review
	// architecture_reviewed state. When false (default), no review runs and Sarah
	// claims preparing_stories straight from architecture_generated, as before.
	//
	// CROSS-COMPONENT INVARIANT: this MUST equal plan-reviewer's
	// architecture_review_enabled. Source both from the one
	// ARCHITECTURE_REVIEW_ENABLED env var in semspec.json. A mismatch wedges the
	// architecture phase (one of architecture_generated / architecture_reviewed
	// ends up with no claimant).
	ArchitectureReviewEnabled bool `json:"architecture_review_enabled" schema:"type:bool,description:Mirror of plan-reviewer architecture_review_enabled — claim preparing_stories from architecture_reviewed instead of architecture_generated,category:advanced,default:false"`

	// Ports defines the component's port configuration.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
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
