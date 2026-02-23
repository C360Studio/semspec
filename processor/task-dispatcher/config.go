package taskdispatcher

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// taskDispatcherSchema defines the configuration schema.
var taskDispatcherSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the task-dispatcher component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:task-dispatcher"`

	// TriggerSubject is the subject pattern for task dispatch triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for batch task triggers,category:basic,default:workflow.trigger.task-dispatcher"`

	// OutputSubject is the subject prefix for publishing execution results.
	OutputSubject string `json:"output_subject" schema:"type:string,description:Subject prefix for execution results,category:basic,default:workflow.result.task-dispatcher"`

	// MaxConcurrent limits parallel task executions.
	MaxConcurrent int `json:"max_concurrent" schema:"type:int,description:Maximum parallel task executions,category:advanced,default:3,min:1,max:10"`

	// ContextTimeout is the timeout for context building per task.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Timeout for context building per task,category:advanced,default:30s"`

	// ExecutionTimeout is the timeout for task execution (including agent work).
	ExecutionTimeout string `json:"execution_timeout" schema:"type:string,description:Timeout for task execution,category:advanced,default:300s"`

	// ContextSubjectPrefix is the subject prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Subject prefix for context build requests,category:advanced,default:context.build"`

	// ContextResponseBucket is the KV bucket for context responses.
	ContextResponseBucket string `json:"context_response_bucket" schema:"type:string,description:KV bucket name for context responses,category:advanced,default:CONTEXT_RESPONSES"`

	// WorkflowTriggerSubject is the subject for triggering the task-execution-loop workflow.
	WorkflowTriggerSubject string `json:"workflow_trigger_subject" schema:"type:string,description:Subject for triggering task execution workflow,category:advanced,default:workflow.trigger.task-execution-loop"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:            "WORKFLOW",
		ConsumerName:          "task-dispatcher",
		TriggerSubject:        "workflow.trigger.task-dispatcher",
		OutputSubject:         "workflow.result.task-dispatcher",
		MaxConcurrent:         3,
		ContextTimeout:        "30s",
		ExecutionTimeout:      "300s",
		ContextSubjectPrefix:  "context.build",
		ContextResponseBucket: "CONTEXT_RESPONSES",
		WorkflowTriggerSubject: "workflow.trigger.task-execution-loop",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "batch-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.task-dispatcher",
					StreamName:  "WORKFLOW",
					Description: "Receive batch task dispatch triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "dispatch-results",
					Type:        "nats",
					Subject:     "workflow.result.task-dispatcher.>",
					Description: "Publish task dispatch results",
					Required:    false,
				},
				{
					Name:        "context-requests",
					Type:        "nats",
					Subject:     "context.build.>",
					Description: "Request context building for tasks",
					Required:    false,
				},
				{
					Name:        "workflow-triggers",
					Type:        "nats",
					Subject:     "workflow.trigger.task-execution-loop",
					Description: "Trigger task execution workflows",
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
	if c.MaxConcurrent > 10 {
		return fmt.Errorf("max_concurrent cannot exceed 10")
	}

	// Validate timeout durations
	if c.ContextTimeout != "" {
		if _, err := time.ParseDuration(c.ContextTimeout); err != nil {
			return fmt.Errorf("invalid context_timeout: %w", err)
		}
	}
	if c.ExecutionTimeout != "" {
		if _, err := time.ParseDuration(c.ExecutionTimeout); err != nil {
			return fmt.Errorf("invalid execution_timeout: %w", err)
		}
	}

	return nil
}

// GetContextTimeout returns the context timeout duration.
// Returns default 30s if parsing fails.
func (c *Config) GetContextTimeout() time.Duration {
	if c.ContextTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ContextTimeout)
	if err != nil || d <= 0 {
		return 30 * time.Second
	}
	return d
}

// GetExecutionTimeout returns the execution timeout duration.
// Returns default 300s if parsing fails.
func (c *Config) GetExecutionTimeout() time.Duration {
	if c.ExecutionTimeout == "" {
		return 300 * time.Second
	}
	d, err := time.ParseDuration(c.ExecutionTimeout)
	if err != nil || d <= 0 {
		return 300 * time.Second
	}
	return d
}
