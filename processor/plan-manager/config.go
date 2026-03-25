package planmanager

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// workflowAPISchema defines the configuration schema.
var workflowAPISchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan-api component.
type Config struct {
	// ExecutionBucketName is the KV bucket name for workflow executions.
	ExecutionBucketName string `json:"execution_bucket_name" schema:"type:string,description:KV bucket for workflow executions,category:basic,default:WORKFLOW_EXECUTIONS"`

	// EventStreamName is the JetStream stream for workflow events (plan_approved, etc.).
	EventStreamName string `json:"event_stream_name" schema:"type:string,description:JetStream stream for workflow events,category:basic,default:WORKFLOW"`

	// UserStreamName is the JetStream stream for user signals (escalation, errors).
	UserStreamName string `json:"user_stream_name" schema:"type:string,description:JetStream stream for user signals,category:basic,default:USER"`

	// SandboxURL is the base URL of the sandbox server for workspace browsing.
	// When empty, workspace endpoints return 503.
	SandboxURL string `json:"sandbox_url,omitempty" schema:"type:string,description:Sandbox server URL for workspace browser (empty=disabled),category:advanced"`

	// --- Coordinator fields (embedded plan-coordinator config) ---

	// MaxConcurrentPlanners is the maximum number of concurrent planners (1-10).
	MaxConcurrentPlanners int `json:"max_concurrent_planners" schema:"type:int,description:Maximum concurrent planners,category:basic,default:3,min:1,max:10"`

	// TimeoutSeconds is the per-coordination timeout in seconds.
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Timeout per coordination in seconds,category:advanced,default:1800"`

	// MaxReviewIterations is the maximum number of revision cycles before escalating.
	MaxReviewIterations int `json:"max_review_iterations" schema:"type:int,description:Max review revision cycles,category:basic,default:3,min:1,max:10"`

	// AutoApprove skips the human approval gate after reviewer approves.
	// Pointer type so JSON `false` is distinguishable from "not set" (nil → default true).
	AutoApprove *bool `json:"auto_approve" schema:"type:bool,description:Skip human approval gate,category:basic,default:true"`

	// Model is the model endpoint name passed through to dispatched planner agents.
	Model string `json:"model" schema:"type:string,description:Model endpoint name for planner agent tasks,category:basic,default:default"`

	// DefaultCapability is the model capability to use for coordination LLM calls.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for coordination,category:basic,default:planning"`

	// RepoPath is the workspace/repository root path. Defaults to SEMSPEC_REPO_PATH env var or ".".
	RepoPath string `json:"repo_path,omitempty" schema:"type:string,description:Repository root path,category:advanced"`

	// SemsourceReadinessBudget is the max time to wait for semsource readiness.
	SemsourceReadinessBudget string `json:"semsource_readiness_budget" schema:"type:string,description:Max semsource readiness wait,category:advanced,default:2s"`

	// Prompts contains optional custom prompt file paths.
	Prompts *PromptsConfig `json:"prompts,omitempty" schema:"type:object,description:Custom prompt file paths,category:advanced"`
}

// PromptsConfig contains optional paths to custom prompt files.
type PromptsConfig struct {
	CoordinatorSystem    string `json:"coordinator_system,omitempty"`
	CoordinatorSynthesis string `json:"coordinator_synthesis,omitempty"`
}

// IsAutoApprove returns whether the human approval gate should be skipped.
// Defaults to true when not explicitly configured.
func (c *Config) IsAutoApprove() bool {
	if c.AutoApprove == nil {
		return true
	}
	return *c.AutoApprove
}

// coordinatorConfig extracts the CoordinatorConfig from this Config.
func (c *Config) coordinatorConfig() CoordinatorConfig {
	return CoordinatorConfig{
		MaxConcurrentPlanners:    c.MaxConcurrentPlanners,
		TimeoutSeconds:           c.TimeoutSeconds,
		MaxReviewIterations:      c.MaxReviewIterations,
		AutoApprove:              c.AutoApprove,
		Model:                    c.Model,
		DefaultCapability:        c.DefaultCapability,
		RepoPath:                 c.RepoPath,
		SemsourceReadinessBudget: c.SemsourceReadinessBudget,
		Prompts:                  c.Prompts,
	}
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	defaultTrue := true
	return Config{
		ExecutionBucketName:   "WORKFLOW_EXECUTIONS",
		EventStreamName:       "WORKFLOW",
		UserStreamName:        "USER",
		MaxConcurrentPlanners: 3,
		TimeoutSeconds:        1800,
		MaxReviewIterations:   3,
		AutoApprove:           &defaultTrue,
		Model:                 "default",
		DefaultCapability:     "planning",
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
	if c.MaxConcurrentPlanners < 0 || c.MaxConcurrentPlanners > 10 {
		return fmt.Errorf("max_concurrent_planners must be 0-10 (0 = use default)")
	}
	return nil
}
