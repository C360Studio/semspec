package trajectoryapi

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// trajectoryAPISchema defines the configuration schema.
var trajectoryAPISchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the trajectory-api component.
type Config struct {
	// LLMCallsBucket is the KV bucket name for LLM call records.
	LLMCallsBucket string `json:"llm_calls_bucket" schema:"type:string,description:KV bucket for LLM call records,category:basic,default:LLM_CALLS"`

	// ToolCallsBucket is the KV bucket name for tool call records.
	ToolCallsBucket string `json:"tool_calls_bucket" schema:"type:string,description:KV bucket for tool call records,category:basic,default:TOOL_CALLS"`

	// LoopsBucket is the KV bucket name for agent loop state.
	LoopsBucket string `json:"loops_bucket" schema:"type:string,description:KV bucket for agent loop state,category:basic,default:AGENT_LOOPS"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		LLMCallsBucket:  "LLM_CALLS",
		ToolCallsBucket: "TOOL_CALLS",
		LoopsBucket:     "AGENT_LOOPS",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.LLMCallsBucket == "" {
		return fmt.Errorf("llm_calls_bucket is required")
	}
	if c.ToolCallsBucket == "" {
		return fmt.Errorf("tool_calls_bucket is required")
	}
	if c.LoopsBucket == "" {
		return fmt.Errorf("loops_bucket is required")
	}
	return nil
}
