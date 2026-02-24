package workflowapi

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// workflowAPISchema defines the configuration schema.
var workflowAPISchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the workflow-api component.
type Config struct {
	// ExecutionBucketName is the KV bucket name for workflow executions.
	ExecutionBucketName string `json:"execution_bucket_name" schema:"type:string,description:KV bucket for workflow executions,category:basic,default:WORKFLOW_EXECUTIONS"`

	// EventStreamName is the JetStream stream for workflow events (plan_approved, etc.).
	EventStreamName string `json:"event_stream_name" schema:"type:string,description:JetStream stream for workflow events,category:basic,default:WORKFLOW"`

	// UserStreamName is the JetStream stream for user signals (escalation, errors).
	UserStreamName string `json:"user_stream_name" schema:"type:string,description:JetStream stream for user signals,category:basic,default:USER"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		ExecutionBucketName: "WORKFLOW_EXECUTIONS",
		EventStreamName:     "WORKFLOW",
		UserStreamName:      "USER",
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.ExecutionBucketName == "" {
		return fmt.Errorf("execution_bucket_name is required")
	}
	if c.EventStreamName == "" {
		return fmt.Errorf("event_stream_name is required")
	}
	return nil
}
