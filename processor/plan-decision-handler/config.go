package changeproposalhandler

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

// handlerSchema defines the configuration schema for the plan-decision-handler.
var handlerSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the plan-decision-handler component.
type Config struct {
	// StreamName is the JetStream stream for consuming cascade trigger messages.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for cascade trigger messages,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name for cascade trigger consumption.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:plan-decision-handler"`

	// TriggerSubject is the subject on which PlanDecisionCascadeRequests arrive.
	TriggerSubject string `json:"trigger_subject" schema:"type:string,description:Subject for cascade trigger messages,category:basic,default:workflow.trigger.plan-decision-cascade"`

	// AcceptedSubject is the subject to which the accepted event is published after cascade.
	AcceptedSubject string `json:"accepted_subject" schema:"type:string,description:Subject for publishing accepted events after cascade,category:advanced,default:workflow.events.plan-decision.accepted"`

	// TimeoutSeconds is the maximum seconds allowed for a single cascade run.
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Cascade timeout in seconds,category:advanced,default:120,min:10,max:600"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:      "WORKFLOW",
		ConsumerName:    "plan-decision-handler",
		TriggerSubject:  "workflow.trigger.plan-decision-cascade",
		AcceptedSubject: "workflow.events.plan-decision.accepted",
		TimeoutSeconds:  120,
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "cascade-triggers",
					Type:        "jetstream",
					Subject:     "workflow.trigger.plan-decision-cascade",
					StreamName:  "WORKFLOW",
					Description: "Receive cascade requests when a PlanDecision is accepted",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "accepted-events",
					Type:        "nats",
					Subject:     "workflow.events.plan-decision.accepted",
					Description: "Publish accepted event with cascade summary after dirty marking",
					Required:    false,
				},
				{
					Name:        "cancellation-signals",
					Type:        "nats",
					Subject:     "agent.signal.cancel.*",
					Description: "Publish cancellation signals to running scenario loops affected by the cascade",
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
	if c.AcceptedSubject == "" {
		return fmt.Errorf("accepted_subject is required")
	}
	if c.TimeoutSeconds < 10 {
		return fmt.Errorf("timeout_seconds must be at least 10")
	}
	if c.TimeoutSeconds > 600 {
		return fmt.Errorf("timeout_seconds cannot exceed 600")
	}
	return nil
}

// GetTimeout returns the cascade timeout duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}
