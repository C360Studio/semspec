package scenarioexecutor

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// scenarioExecutorSchema is the pre-generated schema for this component.
var scenarioExecutorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the configuration for the scenario-executor component.
type Config struct {
	// TimeoutSeconds is the per-scenario timeout in seconds (covers the full
	// decompose → serial-execute pipeline).
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Timeout per scenario execution in seconds,category:advanced,default:3600"`

	// Model is the model endpoint name passed through to dispatched agents.
	Model string `json:"model" schema:"type:string,description:Model endpoint name for agent tasks,category:basic,default:default"`

	// SandboxURL is the base URL of the sandbox server. When set, the
	// scenario-executor creates per-scenario branches for worktree isolation.
	SandboxURL string `json:"sandbox_url" schema:"type:string,description:Sandbox server URL for branch management,category:advanced"`

	// Ports contains the input and output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TimeoutSeconds: 3600,
		Model:          "default",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "scenario-trigger",
					Type:        "jetstream",
					Subject:     subjectScenarioTrigger,
					StreamName:  "WORKFLOW",
					Description: "Receive scenario execution triggers from scenario-orchestrator",
					Required:    true,
				},
				{
					Name:        "loop-completions",
					Type:        "jetstream",
					Subject:     subjectLoopCompleted,
					StreamName:  "AGENT",
					Description: "Receive agentic loop completion events",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "entity-triples",
					Type:        "nats",
					Subject:     "graph.mutation.triple.add",
					Description: "Publish entity state triples",
				},
				{
					Name:        "decomposer-task",
					Type:        "jetstream",
					Subject:     "agent.task.development",
					StreamName:  "AGENT",
					Description: "Dispatch decomposer agent tasks",
				},
				{
					Name:        "execution-trigger",
					Type:        "jetstream",
					Subject:     "workflow.trigger.task-execution-loop",
					StreamName:  "WORKFLOW",
					Description: "Dispatch DAG nodes to execution-orchestrator for TDD pipeline",
				},
			},
		},
	}
}

// withDefaults returns a copy of c with zero-value fields replaced by defaults.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	if c.Model == "" {
		c.Model = d.Model
	}
	if c.Ports == nil {
		c.Ports = d.Ports
	}
	return c
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	return nil
}

// GetTimeout returns the execution timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 60 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}
