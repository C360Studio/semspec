package workflowvalidator

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// workflowValidatorSchema defines the configuration schema.
var workflowValidatorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the workflow-validator processor.
type Config struct {
	Ports       *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	BaseDir     string                `json:"base_dir" schema:"type:string,description:Base directory for document paths (defaults to SEMSPEC_REPO_PATH or current directory),category:basic"`
	TimeoutSecs int                   `json:"timeout_secs" schema:"type:integer,description:Request timeout in seconds,category:basic,default:30"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.TimeoutSecs < 0 {
		return fmt.Errorf("timeout_secs must be non-negative")
	}
	return nil
}

// DefaultConfig returns the default configuration for workflow-validator.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "validate_requests",
					Type:        "nats",
					Subject:     "workflow.validate.*",
					Required:    true,
					Description: "Validation request/reply subject (wildcard for document type)",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "validation_events",
					Type:        "nats",
					Subject:     "workflow.validation.events",
					Required:    false,
					Description: "Validation event notifications",
				},
			},
		},
		BaseDir:     "",
		TimeoutSecs: 30,
	}
}
