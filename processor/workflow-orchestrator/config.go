package workfloworchestrator

import (
	"fmt"

	"github.com/c360studio/semstreams/component"
)

// Config holds configuration for the workflow orchestrator component.
type Config struct {
	// RulesPath is the path to the workflow rules YAML file.
	RulesPath string `json:"rules_path,omitempty"`

	// LoopsBucket is the KV bucket name to watch for loop completions.
	LoopsBucket string `json:"loops_bucket"`

	// StreamName is the JetStream stream for publishing tasks.
	StreamName string `json:"stream_name"`

	// RepoPath is the path to the repository for reading generated documents.
	RepoPath string `json:"repo_path,omitempty"`

	// Validation configuration
	Validation *ValidationConfig `json:"validation,omitempty"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty"`
}

// ValidationConfig holds validation and retry configuration.
type ValidationConfig struct {
	// Enabled controls whether document validation is enabled.
	Enabled bool `json:"enabled"`

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `json:"max_retries"`

	// BackoffBaseSeconds is the base backoff duration in seconds.
	BackoffBaseSeconds int `json:"backoff_base_seconds"`

	// BackoffMultiplier is the exponential backoff multiplier.
	BackoffMultiplier float64 `json:"backoff_multiplier"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		RulesPath:   "configs/workflow-rules.yaml",
		LoopsBucket: "AGENT_LOOPS",
		StreamName:  "AGENT",
		Validation: &ValidationConfig{
			Enabled:            true,
			MaxRetries:         3,
			BackoffBaseSeconds: 5,
			BackoffMultiplier:  2.0,
		},
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "loop-completions",
					Type:        "kv",
					Description: "Watch for loop completion events",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "workflow-tasks",
					Type:        "jetstream",
					Subject:     "agent.task.workflow",
					StreamName:  "AGENT",
					Description: "Publish workflow tasks to trigger next steps",
					Required:    true,
				},
				{
					Name:        "user-responses",
					Type:        "jetstream",
					Subject:     "user.response.>",
					StreamName:  "USER",
					Description: "Publish user notifications",
					Required:    false,
				},
			},
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.LoopsBucket == "" {
		return fmt.Errorf("loops_bucket is required")
	}
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	return nil
}
