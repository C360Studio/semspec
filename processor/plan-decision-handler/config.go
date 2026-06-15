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

	// AutoAcceptRecovery enables programmatic acceptance of proposed
	// PlanDecisions emitted by the recovery-agent. ADR-037 stage-1 design
	// note: recovery diagnoses are useful even without auto-action; this
	// knob unlocks the full apply→cascade→retry loop when operators have
	// validated the recovery agent's behaviour for their deployment.
	//
	// Gate is narrow: only PlanDecisions with ProposedBy="recovery-agent",
	// Status="proposed", Kind="requirement_change", and non-empty
	// AffectedReqIDs are auto-accepted. qa-reviewer + req-executor + human
	// proposals always require explicit human accept.
	//
	// Default false (Goodhart guard): operators opt into the safety-net
	// shortcut, they don't get it by default. Mock e2e flips this on so
	// the full apply path is exercised; production semspec.json leaves it
	// off so recovery stays diagnosis-only until the operator decides.
	AutoAcceptRecovery bool `json:"auto_accept_recovery" schema:"type:boolean,description:Auto-accept PlanDecisions from recovery-agent (ADR-037 stage-2 apply path),category:advanced,default:false"`

	// MaxAutoArchitectureRevises bounds how many architecture_revise recovery
	// PlanDecisions the watcher will auto-accept for a single plan. Each accepted
	// architecture_revise wipes Architecture + Stories + Scenarios and re-runs the
	// whole pipeline from the architect — the heaviest, most expensive recovery
	// action. Without a cap, a plan whose revised architecture still wedges would
	// loop (implement → wedge → architecture_revise → implement → …) burning a
	// full re-run each cycle. Once this many architecture_revise decisions are
	// already accepted on the plan, further ones stay proposed for human review.
	// The count is monotonic — PlanDecisions persist across the wipe. Default 1:
	// one automatic architecture revision, then a human must decide.
	MaxAutoArchitectureRevises int `json:"max_auto_architecture_revises" schema:"type:int,description:Max architecture_revise recovery decisions auto-accepted per plan before human review,category:advanced,default:1,min:0,max:10"`

	// MaxAutoStoryReprepares bounds how many story_reprepare recovery
	// PlanDecisions the watcher will auto-accept for a single plan. A
	// story_reprepare now abandons current requirement executions and re-runs
	// Sarah for affected Stories; without a cap, a non-converging story shape
	// can loop through full execution cycles. Default 1: one automatic Story
	// reprepare, then a human must decide.
	MaxAutoStoryReprepares int `json:"max_auto_story_reprepares" schema:"type:int,description:Max story_reprepare recovery decisions auto-accepted per plan before human review,category:advanced,default:1,min:0,max:10"`

	// Ports contains input/output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Input/output port definitions,category:basic"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		StreamName:                 "WORKFLOW",
		ConsumerName:               "plan-decision-handler",
		TriggerSubject:             "workflow.trigger.plan-decision-cascade",
		AcceptedSubject:            "workflow.events.plan-decision.accepted",
		TimeoutSeconds:             120,
		MaxAutoArchitectureRevises: 1,
		MaxAutoStoryReprepares:     1,
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
