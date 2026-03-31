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
	BaseDir         string `json:"base_dir" schema:"type:string,description:Base directory for document output (defaults to SEMSPEC_REPO_PATH or current directory),category:basic"`
	PlanStateBucket string `json:"plan_state_bucket" schema:"type:string,description:KV bucket to watch for plan state changes,category:basic,default:PLAN_STATES"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.PlanStateBucket == "" {
		return fmt.Errorf("plan_state_bucket is required")
	}
	return nil
}

// DefaultConfig returns the default configuration for workflow-documents.
func DefaultConfig() Config {
	return Config{
		PlanStateBucket: "PLAN_STATES",
	}
}
