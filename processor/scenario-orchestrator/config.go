package scenarioorchestrator

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// orchestratorSchema defines the configuration schema for the scenario-orchestrator.
var orchestratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the scenario-orchestrator component.
type Config struct {
	// StreamName is the JetStream stream for consuming orchestration triggers.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for orchestration triggers,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:scenario-orchestrator"`

	// TriggerSubject is the subject pattern for orchestration triggers.
	// Supports wildcards: scenario.orchestrate.* to handle per-plan orchestration.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for orchestration triggers,category:basic,default:scenario.orchestrate.*"`

	// WorkflowTriggerSubject is the subject for triggering requirement-execution-loop workflows.
	WorkflowTriggerSubject string `json:"workflow_trigger_subject" schema:"type:string,description:Subject for triggering requirement execution workflows,category:advanced,default:workflow.trigger.requirement-execution-loop"`

	// ExecutionTimeout is the maximum time allowed for a single orchestration cycle.
	ExecutionTimeout string `json:"execution_timeout" schema:"type:string,description:Timeout for a single orchestration cycle,category:advanced,default:120s"`

	// MaxConcurrent limits parallel scenario executions triggered per cycle.
	MaxConcurrent int `json:"max_concurrent" schema:"type:int,description:Maximum parallel scenario executions per cycle,category:advanced,default:5,min:1,max:20"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:             "WORKFLOW",
		ConsumerName:           "scenario-orchestrator",
		TriggerSubject:         "scenario.orchestrate.*",
		WorkflowTriggerSubject: "workflow.trigger.requirement-execution-loop",
		ExecutionTimeout:       "120s",
		MaxConcurrent:          5,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "orchestration-triggers",
					Type:        "jetstream",
					Subject:     "scenario.orchestrate.*",
					StreamName:  "WORKFLOW",
					Description: "Receive scenario orchestration triggers (one per plan)",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "requirement-execution-triggers",
					Type:        "nats",
					Subject:     "workflow.trigger.requirement-execution-loop",
					Description: "Trigger requirement-execution-loop for each ready requirement",
					Required:    false,
				},
			},
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.TriggerSubject == "" {
		return fmt.Errorf("trigger_subject is required")
	}
	if c.MaxConcurrent < 1 {
		return fmt.Errorf("max_concurrent must be at least 1")
	}
	if c.MaxConcurrent > 20 {
		return fmt.Errorf("max_concurrent cannot exceed 20")
	}
	if c.ExecutionTimeout != "" {
		if _, err := time.ParseDuration(c.ExecutionTimeout); err != nil {
			return fmt.Errorf("invalid execution_timeout: %w", err)
		}
	}
	return nil
}

// GetExecutionTimeout returns the execution timeout duration.
// Returns default 120s if parsing fails.
func (c *Config) GetExecutionTimeout() time.Duration {
	if c.ExecutionTimeout == "" {
		return 120 * time.Second
	}
	d, err := time.ParseDuration(c.ExecutionTimeout)
	if err != nil || d <= 0 {
		return 120 * time.Second
	}
	return d
}
