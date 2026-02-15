package plancoordinator

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// configSchema defines the configuration schema.
var configSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan-coordinator processor component.
type Config struct {
	// StreamName is the JetStream stream for consuming triggers and publishing results.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for workflow triggers,category:basic,default:WORKFLOWS"`

	// ConsumerName is the durable consumer name for trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name for trigger consumption,category:basic,default:plan-coordinator"`

	// TriggerSubject is the subject pattern for plan coordinator triggers.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject pattern for coordinator triggers,category:basic,default:workflow.trigger.plan-coordinator"`

	// SessionsBucket is the KV bucket for storing plan sessions.
	SessionsBucket string `json:"sessions_bucket" schema:"type:string,description:KV bucket for plan sessions,category:basic,default:PLAN_SESSIONS"`

	// MaxConcurrentPlanners is the maximum number of concurrent planners (1-3).
	MaxConcurrentPlanners int `json:"max_concurrent_planners" schema:"type:int,description:Maximum concurrent planners,category:advanced,default:3,min:1,max:3"`

	// PlannerTimeout is the timeout for each planner to complete.
	PlannerTimeout string `json:"planner_timeout" schema:"type:string,description:Timeout for planner completion,category:advanced,default:120s"`

	// DefaultCapability is the model capability to use for coordination.
	DefaultCapability string `json:"default_capability" schema:"type:string,description:Default model capability for coordination,category:basic,default:planning"`

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
		StreamName:            "WORKFLOWS",
		ConsumerName:          "plan-coordinator",
		TriggerSubject:        "workflow.trigger.plan-coordinator",
		SessionsBucket:        "PLAN_SESSIONS",
		MaxConcurrentPlanners: 3,
		PlannerTimeout:        "120s",
		DefaultCapability:     "planning",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "coordinator-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.plan-coordinator",
					StreamName:  "WORKFLOWS",
					Description: "Receive plan coordinator triggers",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "coordinator-results",
					Type:        "nats",
					Subject:     "workflow.result.plan-coordinator.>",
					Description: "Publish coordinator results",
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
	if c.MaxConcurrentPlanners < 1 || c.MaxConcurrentPlanners > 3 {
		return fmt.Errorf("max_concurrent_planners must be 1-3")
	}
	return nil
}
