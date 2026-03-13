package executionorchestrator

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// executionOrchestratorSchema is the pre-generated schema for this component.
var executionOrchestratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the configuration for the execution-orchestrator component.
type Config struct {
	// MaxIterations is the maximum number of developer→validate→review cycles
	// before escalating to human review. This budget is shared across all
	// retry reasons (validation failure + code review rejection).
	MaxIterations int `json:"max_iterations" schema:"type:int,description:Maximum execution iterations before escalation,category:basic,default:3"`

	// TimeoutSeconds is the per-execution timeout in seconds (covers the
	// full develop→validate→review pipeline, not individual steps).
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Timeout per task execution in seconds,category:advanced,default:1800"`

	// Model is the model endpoint name passed through to dispatched agents.
	Model string `json:"model" schema:"type:string,description:Model endpoint name for agent tasks,category:basic,default:default"`

	// Ports contains the input and output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxIterations:  3,
		TimeoutSeconds: 1800,
		Model:          "default",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "execution-trigger",
					Type:        "jetstream",
					Subject:     "workflow.trigger.task-execution-loop",
					StreamName:  "WORKFLOW",
					Description: "Receive task execution triggers from task-dispatcher",
					Required:    true,
				},
				{
					Name:        "loop-completions",
					Type:        "jetstream",
					Subject:     "agentic.loop_completed.v1",
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
					Required:    false,
				},
				{
					Name:        "agent-tasks",
					Type:        "nats",
					Subject:     "agent.task.>",
					Description: "Dispatch agent tasks for development and review",
					Required:    false,
				},
			},
		},
	}
}

// withDefaults returns a copy of c with zero-value fields replaced by defaults.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.MaxIterations <= 0 {
		c.MaxIterations = d.MaxIterations
	}
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
	if c.MaxIterations <= 0 {
		return fmt.Errorf("max_iterations must be positive")
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	return nil
}

// GetTimeout returns the execution timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}
