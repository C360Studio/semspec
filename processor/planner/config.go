package planner

import (
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// plannerSchema defines the configuration schema.
var plannerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the planner processor component.
type Config struct {
	// MaxGenerationRetries is the maximum number of times to retry planning
	// after a loop failure (timeout, max iterations) or parse error before
	// rejecting the plan. Set to 0 to disable retries.
	MaxGenerationRetries int `json:"max_generation_retries" schema:"type:integer,description:Max retries on loop failure or parse error,category:basic,default:2"`

	// RetryBackoffMs is the floor of the jittered delay before re-dispatching
	// after a planning failure. See workflow/dispatchretry for semantics.
	// Default 200ms; non-positive values fall back to the default.
	RetryBackoffMs int `json:"retry_backoff_ms" schema:"type:integer,description:Floor of jittered backoff between planning retries (ms),category:advanced,default:200"`

	// DefaultCapability is the model capability to use for planning.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for planning,category:basic,default:planning"`

	// InteractiveMode enables ask_question tool when a human is monitoring.
	// When false (default), the planner makes reasonable assumptions instead of
	// asking questions that would block without a human to answer.
	InteractiveMode bool `json:"interactive_mode" schema:"type:bool,description:Enable ask_question tool (requires human monitoring),category:advanced,default:false"`

	// SandboxURL is the URL of the sandbox container. When set, the planner
	// fetches a `git ls-files` snapshot of the project at dispatch time and
	// injects it into the user prompt as ground-truth file inventory. Without
	// this the planner can confidently hallucinate Go-idiomatic structures
	// (cmd/server/main.go) on revision rounds and fail to re-explore even
	// after the reviewer flags the path. Greenfield-safe: empty output is
	// skipped silently. Caught 2026-05-03 on openrouter @easy /health.
	SandboxURL string `json:"sandbox_url,omitempty" schema:"type:string,description:Sandbox URL for project file tree snapshot,category:advanced"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxGenerationRetries: 2,
		RetryBackoffMs:       200,
		DefaultCapability:    "planning",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
