// Package taskcodereview provides a JetStream processor that reviews
// code changes made by the developer agent.
package taskcodereview

import (
	"fmt"
	"time"

	"github.com/c360studio/semstreams/component"
)

// Config holds configuration for the task-code-reviewer component.
type Config struct {
	// StreamName is the JetStream stream to consume from.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:task-code-reviewer"`

	// TriggerSubject is the subject pattern for code review triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for code review triggers,category:basic,default:workflow.async.task-code-reviewer"`

	// StateBucket is the KV bucket for workflow state.
	StateBucket string `json:"state_bucket" schema:"type:string,description:KV bucket for workflow state,category:advanced,default:REACTIVE_STATE"`

	// DefaultCapability is the LLM capability to use.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:LLM capability for code review,category:basic,default:coding"`

	// Timeout is the maximum duration for a review request.
	Timeout string `json:"timeout" schema:"type:string,description:Maximum duration for review,category:advanced,default:5m"`

	// RepoPath is the repository path for file access.
	RepoPath string `json:"repo_path" schema:"type:string,description:Repository path for file access,category:basic"`

	// Ports defines the component's input/output ports.
	Ports *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:        "WORKFLOW",
		ConsumerName:      "task-code-reviewer",
		TriggerSubject:    "workflow.async.task-code-reviewer",
		StateBucket:       "REACTIVE_STATE",
		DefaultCapability: "coding",
		Timeout:           "5m",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "trigger",
					Subject:     "workflow.async.task-code-reviewer",
					Required:    true,
					Description: "Code review request trigger",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "result",
					Subject:     "workflow.result.task-code-reviewer.*",
					Required:    false,
					Description: "Code review result",
				},
			},
		},
	}
}

// Validate checks if the configuration is valid.
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
	return nil
}

// GetTimeout parses the timeout duration.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
