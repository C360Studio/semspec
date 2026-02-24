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

	// ArtifactAPISubject is the NATS subject for the ObjectStore API that stores full LLM call artifacts.
	// When set, the /calls/ endpoint can retrieve complete CallRecords (with Messages and Response).
	ArtifactAPISubject string `json:"artifact_api_subject,omitempty" schema:"type:string,description:NATS subject for LLM artifact ObjectStore API,category:basic"`

	// RepoRoot is the repository root path for accessing plan data.
	// If empty, defaults to SEMSPEC_REPO_PATH env var or current working directory.
	RepoRoot string `json:"repo_root,omitempty" schema:"type:string,description:Repository root path for plan access,category:basic"`

	// GraphGatewayURL is the URL for the graph gateway service.
	// Used to query LLM call entities from the knowledge graph.
	GraphGatewayURL string `json:"graph_gateway_url,omitempty" schema:"type:string,description:Graph gateway URL for LLM call queries,category:basic,default:http://localhost:8082"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		LLMCallsBucket:  "LLM_CALLS",
		ToolCallsBucket: "TOOL_CALLS",
		LoopsBucket:     "AGENT_LOOPS",
		GraphGatewayURL: "http://localhost:8082",
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
