package structuralvalidator

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// structuralValidatorSchema defines the configuration schema.
var structuralValidatorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the structural-validator component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOWS"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:structural-validator"`

	// RepoPath is the repository root directory to run checks in.
	// When empty the component falls back to SEMSPEC_REPO_PATH then the working directory.
	RepoPath string `json:"repo_path" schema:"type:string,description:Repository root path,category:basic,default:"`

	// ChecklistPath is the path to checklist.json relative to the repo root.
	ChecklistPath string `json:"checklist_path" schema:"type:string,description:Path to checklist.json relative to repo root,category:basic,default:.semspec/checklist.json"`

	// DefaultTimeout is the fallback command execution timeout when a check has no timeout set.
	DefaultTimeout string `json:"default_timeout" schema:"type:string,description:Default command execution timeout (duration string),category:advanced,default:120s"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:     "WORKFLOWS",
		ConsumerName:   "structural-validator",
		ChecklistPath:  ".semspec/checklist.json",
		DefaultTimeout: "120s",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "validation-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.structural-validator",
					StreamName:  "WORKFLOWS",
					Description: "Receive structural validation triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "validation-results",
					Type:        "nats",
					Subject:     "workflow.result.structural-validator.>",
					Description: "Publish structural validation results",
					Required:    false,
				},
			},
		},
	}
}

// GetDefaultTimeout parses the default timeout duration.
// Returns 120 seconds if the field is empty or unparseable.
func (c *Config) GetDefaultTimeout() time.Duration {
	if c.DefaultTimeout == "" {
		return 120 * time.Second
	}
	d, err := time.ParseDuration(c.DefaultTimeout)
	if err != nil {
		return 120 * time.Second
	}
	return d
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.ChecklistPath == "" {
		return fmt.Errorf("checklist_path is required")
	}
	return nil
}
