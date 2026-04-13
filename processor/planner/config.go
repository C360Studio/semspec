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

	// DefaultCapability is the model capability to use for planning.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for planning,category:basic,default:planning"`

	// InteractiveMode enables ask_question tool when a human is monitoring.
	// When false (default), the planner makes reasonable assumptions instead of
	// asking questions that would block without a human to answer.
	InteractiveMode bool `json:"interactive_mode" schema:"type:bool,description:Enable ask_question tool (requires human monitoring),category:advanced,default:false"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxGenerationRetries: 2,
		DefaultCapability:    "planning",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return nil
}
