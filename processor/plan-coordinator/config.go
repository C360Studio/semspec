package plancoordinator

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// configSchema is the pre-generated schema for this component.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan-coordinator processor component.
type Config struct {
	// MaxConcurrentPlanners is the maximum number of concurrent planners (1-10).
	MaxConcurrentPlanners int `json:"max_concurrent_planners" schema:"type:int,description:Maximum concurrent planners,category:basic,default:3,min:1,max:10"`

	// TimeoutSeconds is the per-coordination timeout in seconds (covers the
	// full focus → plan → synthesize pipeline).
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Timeout per coordination in seconds,category:advanced,default:1800"`

	// Model is the model endpoint name passed through to dispatched planner agents.
	Model string `json:"model" schema:"type:string,description:Model endpoint name for planner agent tasks,category:basic,default:default"`

	// DefaultCapability is the model capability to use for coordination LLM calls
	// (focus determination and synthesis).
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for coordination,category:basic,default:planning"`

	// ContextSubjectPrefix is the subject prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Subject prefix for context build requests,category:advanced,default:context.build"`

	// ContextTimeout is the timeout for context building.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Timeout for context building,category:advanced,default:30s"`

	// RepoPath is the workspace/repository root path. Defaults to SEMSPEC_REPO_PATH env var or ".".
	RepoPath string `json:"repo_path,omitempty" schema:"type:string,description:Repository root path,category:advanced"`

	// Prompts contains optional custom prompt file paths.
	Prompts *PromptsConfig `json:"prompts,omitempty" schema:"type:object,description:Custom prompt file paths,category:advanced"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// PromptsConfig contains optional paths to custom prompt files.
type PromptsConfig struct {
	CoordinatorSystem    string `json:"coordinator_system,omitempty"`
	CoordinatorSynthesis string `json:"coordinator_synthesis,omitempty"`
}

// GetCoordinatorSystem returns the coordinator system prompt path.
func (p *PromptsConfig) GetCoordinatorSystem() string {
	if p == nil {
		return ""
	}
	return p.CoordinatorSystem
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		MaxConcurrentPlanners: 3,
		TimeoutSeconds:        1800,
		Model:                 "default",
		DefaultCapability:     "planning",
		ContextSubjectPrefix:  "context.build",
		ContextTimeout:        "30s",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "coordination-trigger",
					Type:        "jetstream",
					Subject:     subjectCoordinationTrigger,
					StreamName:  "WORKFLOW",
					Description: "Receive plan coordination triggers",
					Required:    true,
				},
				{
					Name:        "loop-completions",
					Type:        "jetstream",
					Subject:     "agent.complete.>",
					StreamName:  "AGENT",
					Description: "Receive agent completion events (LoopCompletedEvent)",
					Required:    true,
				},
				{
					Name:        "requirements-generated",
					Type:        "jetstream",
					Subject:     "workflow.events.requirements.generated",
					StreamName:  "WORKFLOW",
					Description: "Receive requirement generator completion events",
					Required:    true,
				},
				{
					Name:        "scenarios-generated",
					Type:        "jetstream",
					Subject:     "workflow.events.scenarios.generated",
					StreamName:  "WORKFLOW",
					Description: "Receive scenario generator completion events",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "entity-triples",
					Type:        "nats",
					Subject:     "graph.mutation.triple.add",
					Description: "Publish entity state triples",
					Required:    false,
				},
				{
					Name:        "agent-tasks",
					Type:        "nats",
					Subject:     "agent.task.>",
					Description: "Dispatch planner agent tasks",
					Required:    false,
				},
			},
		},
	}
}

// withDefaults returns a copy of c with zero-value fields replaced by defaults.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.MaxConcurrentPlanners <= 0 {
		c.MaxConcurrentPlanners = d.MaxConcurrentPlanners
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	if c.Model == "" {
		c.Model = d.Model
	}
	if c.DefaultCapability == "" {
		c.DefaultCapability = d.DefaultCapability
	}
	if c.ContextSubjectPrefix == "" {
		c.ContextSubjectPrefix = d.ContextSubjectPrefix
	}
	if c.ContextTimeout == "" {
		c.ContextTimeout = d.ContextTimeout
	}
	if c.RepoPath == "" {
		c.RepoPath = os.Getenv("SEMSPEC_REPO_PATH")
		if c.RepoPath == "" {
			c.RepoPath = "."
		}
	}
	if c.Ports == nil {
		c.Ports = d.Ports
	}
	return c
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.MaxConcurrentPlanners < 1 || c.MaxConcurrentPlanners > 10 {
		return fmt.Errorf("max_concurrent_planners must be 1-10")
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	return nil
}

// GetTimeout returns the coordination timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// GetContextTimeout parses the context timeout duration.
func (c *Config) GetContextTimeout() time.Duration {
	if c.ContextTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ContextTimeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}
