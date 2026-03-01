package plancoordinator

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// configSchema defines the configuration schema.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan-coordinator processor component.
type Config struct {
	// StreamName is the JetStream stream for consuming dispatches and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow dispatches,category:basic,default:WORKFLOW"`

	// StateBucket is the KV bucket for reactive workflow state (shared with engine).
	StateBucket string `json:"state_bucket" schema:"type:string,description:KV bucket for reactive workflow state,category:basic,default:REACTIVE_STATE"`

	// FocusSubject is the subject for focus determination dispatch.
	FocusSubject string `json:"focus_subject" schema:"type:string,description:Subject for focus dispatch,category:basic,default:workflow.async.coordination-focus"`

	// PlannerSubject is the subject for individual planner dispatch.
	PlannerSubject string `json:"planner_subject" schema:"type:string,description:Subject for planner dispatch,category:basic,default:workflow.async.coordination-planner"`

	// SynthesisSubject is the subject for synthesis dispatch.
	SynthesisSubject string `json:"synthesis_subject" schema:"type:string,description:Subject for synthesis dispatch,category:basic,default:workflow.async.coordination-synthesis"`

	// PlannerResultSubject is the subject prefix for planner result publishing.
	PlannerResultSubject string `json:"planner_result_subject" schema:"type:string,description:Subject prefix for planner results,category:basic,default:workflow.result.coordination-planner"`

	// MaxConcurrentPlanners is the maximum number of concurrent planners (1-3).
	MaxConcurrentPlanners int `json:"max_concurrent_planners" schema:"type:int,description:Maximum concurrent planners,category:advanced,default:3,min:1,max:3"`

	// PlannerTimeout is the timeout for each planner to complete.
	PlannerTimeout string `json:"planner_timeout" schema:"type:string,description:Timeout for planner completion,category:advanced,default:120s"`

	// DefaultCapability is the model capability to use for coordination.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for coordination,category:basic,default:planning"`

	// ContextSubjectPrefix is the subject prefix for context build requests.
	ContextSubjectPrefix string `json:"context_subject_prefix" schema:"type:string,description:Subject prefix for context build requests,category:advanced,default:context.build"`

	// ContextResponseBucket is the KV bucket for context responses.
	ContextResponseBucket string `json:"context_response_bucket" schema:"type:string,description:KV bucket for context responses,category:advanced,default:CONTEXT_RESPONSES"`

	// ContextTimeout is the timeout for context building.
	ContextTimeout string `json:"context_timeout" schema:"type:string,description:Timeout for context building,category:advanced,default:30s"`

	// Prompts contains optional custom prompt file paths.
	Prompts *PromptsConfig `json:"prompts,omitempty" schema:"type:object,description:Custom prompt file paths,category:advanced"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// PromptsConfig contains optional paths to custom prompt files.
type PromptsConfig struct {
	// CoordinatorSystem is the path to the coordinator system prompt.
	CoordinatorSystem string `json:"coordinator_system,omitempty"`

	// CoordinatorSynthesis is the path to the coordinator synthesis prompt.
	CoordinatorSynthesis string `json:"coordinator_synthesis,omitempty"`

	// PlannerFocusedSystem is the path to the focused planner system prompt.
	PlannerFocusedSystem string `json:"planner_focused_system,omitempty"`

	// PlannerFocusedUser is the path to the focused planner user prompt.
	PlannerFocusedUser string `json:"planner_focused_user,omitempty"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:            "WORKFLOW",
		StateBucket:           "REACTIVE_STATE",
		FocusSubject:          "workflow.async.coordination-focus",
		PlannerSubject:        "workflow.async.coordination-planner",
		SynthesisSubject:      "workflow.async.coordination-synthesis",
		PlannerResultSubject:  "workflow.result.coordination-planner",
		MaxConcurrentPlanners: 3,
		PlannerTimeout:        "120s",
		DefaultCapability:     "planning",
		ContextSubjectPrefix:  "context.build",
		ContextResponseBucket: "CONTEXT_RESPONSES",
		ContextTimeout:        "30s",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "coordination-focus",
					Type:        "jetstream",
					Subject:     "workflow.async.coordination-focus",
					StreamName:  "WORKFLOW",
					Description: "Focus determination dispatch from reactive engine",
					Required:    true,
				},
				{
					Name:        "coordination-planners",
					Type:        "jetstream",
					Subject:     "workflow.async.coordination-planner",
					StreamName:  "WORKFLOW",
					Description: "Individual planner dispatch",
					Required:    true,
				},
				{
					Name:        "coordination-synthesis",
					Type:        "jetstream",
					Subject:     "workflow.async.coordination-synthesis",
					StreamName:  "WORKFLOW",
					Description: "Synthesis dispatch from reactive engine",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "planner-results",
					Type:        "jetstream",
					Subject:     "workflow.result.coordination-planner.>",
					StreamName:  "WORKFLOW",
					Description: "Planner results for engine merge",
					Required:    true,
				},
				{
					Name:        "coordinator-results",
					Type:        "jetstream",
					Subject:     "workflow.result.plan-coordinator.>",
					Description: "Final coordinator results for observability",
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
	if c.StateBucket == "" {
		return fmt.Errorf("state_bucket is required")
	}
	if c.MaxConcurrentPlanners < 1 || c.MaxConcurrentPlanners > 3 {
		return fmt.Errorf("max_concurrent_planners must be 1-3")
	}
	return nil
}

// GetContextTimeout parses the context timeout duration.
func (c *Config) GetContextTimeout() time.Duration {
	if c.ContextTimeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(c.ContextTimeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}
