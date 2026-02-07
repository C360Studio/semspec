package workflowdocuments

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// workflowDocumentsSchema defines the configuration schema.
var workflowDocumentsSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the workflow-documents output component.
type Config struct {
	Ports   *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	BaseDir string                `json:"base_dir" schema:"type:string,description:Base directory for document output (defaults to SEMSPEC_REPO_PATH or current directory),category:basic"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Ports == nil {
		return fmt.Errorf("ports configuration required")
	}
	if len(c.Ports.Inputs) == 0 {
		return fmt.Errorf("at least one input port required")
	}
	return nil
}

// DefaultConfig returns the default configuration for workflow-documents.
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "documents_in",
					Type:        "jetstream",
					Subject:     "output.workflow.documents",
					StreamName:  "WORKFLOW",
					Required:    true,
					Description: "Workflow document output messages for file export",
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "documents_written",
					Type:        "nats",
					Subject:     "workflow.documents.written",
					Required:    false,
					Description: "Notification when documents are written to disk",
				},
			},
		},
		BaseDir: "",
	}
}
