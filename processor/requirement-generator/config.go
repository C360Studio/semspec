package requirementgenerator

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// requirementGeneratorSchema defines the configuration schema.
var requirementGeneratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the requirement-generator processor component.
type Config struct {
	// DefaultCapability is the model capability to use for requirement generation.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for requirement generation,category:basic,default:planning"`

	// PlanStateBucket is the KV bucket name to watch for approved plans (KV twofer).
	// The requirement-generator self-triggers when any plan transitions to "approved".
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for approved plans,category:advanced,default:PLAN_STATES"`

	// MaxGenerationRetries is the maximum number of times to re-dispatch the agent
	// after a parse failure before sending plan.mutation.generation.failed.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on parse failure,category:basic,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a generation failure. See workflow/dispatchretry for semantics.
	// Default 200ms; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between generation retries (ms),category:advanced,default:200"`

	// SandboxURL is the URL of the sandbox container. When set, the
	// requirement-generator fetches a `git ls-files` snapshot of the project
	// at dispatch time and injects it into the user prompt as ground-truth
	// file inventory. Without this the persona's files_owned partitioning
	// rule fires against scope.include alone, and weak models can still
	// invent idiomatic-looking paths (api/handlers/*.go on projects with no
	// api/ directory). Greenfield-safe: empty output skips the section
	// silently. Same shape as plan-reviewer/planner sandbox_url.
	SandboxURL string `json:"sandbox_url,omitempty" schema:"type:string,description:Sandbox URL for project file tree snapshot,category:advanced"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		DefaultCapability:    "planning",
		PlanStateBucket:      "PLAN_STATES",
		MaxGenerationRetries: 2,
		RetryBackoffMs:       200,
		Ports: &component.PortConfig{
			Outputs: []component.PortDefinition{
				{
					Name:        "requirements-generated-events",
					Type:        "nats",
					Subject:     "workflow.events.requirements.generated",
					Description: "Publish requirements-generated events",
					Required:    false,
				},
			},
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
