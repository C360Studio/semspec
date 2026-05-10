package recoveryagent

import (
	"fmt"
	"reflect"

	"github.com/c360studio/semstreams/component"
)

// recoveryAgentSchema defines the configuration schema.
var recoveryAgentSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for the recovery-agent component.
//
// Stage 1 wires phase-local recovery for plan-manager and execution-manager
// escalations. The component dispatches a one-shot LLM agent task with the
// appropriate capability (plan_wedge_recovery / execution_wedge_recovery)
// and publishes the chosen RecoveryAction back on recovery.complete.<slug>.
//
// The Enabled flag is the per-deployment kill switch: when false, requests
// are acked and skipped without dispatch. Default true so the wire is
// observable on every deployment.
type Config struct {
	// Enabled controls whether the recovery-agent dispatches on incoming
	// RecoveryRequested. When false, the consumer acks and skips.
	Enabled bool `json:"enabled" schema:"type:boolean,description:Whether the recovery-agent dispatches; false acks-and-skips,category:basic,default:true"`

	// StreamName is the JetStream stream for consuming recovery requests.
	StreamName string `json:"stream_name" schema:"type:string,description:JetStream stream for recovery requests,category:basic,default:WORKFLOW"`

	// ConsumerName is the durable consumer name.
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:recovery-agent"`

	// FilterSubject is the subject pattern this component subscribes to.
	FilterSubject string `json:"filter_subject" schema:"type:string,description:NATS subject pattern for recovery requests,category:basic,default:recovery.requested.>"`

	// TrajectoryStepLimit caps how many trajectory steps are rendered into
	// the recovery prompt. ADR-037 design lock #2 fixed this at 80; the
	// config knob is here so an operator can drop it for context-window
	// constrained deployments. Falls back to the default when ≤0.
	TrajectoryStepLimit int `json:"trajectory_step_limit" schema:"type:integer,description:Maximum trajectory steps included in recovery prompt; ADR-037 cap is 80,category:basic,default:80"`
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:             true,
		StreamName:          "WORKFLOW",
		ConsumerName:        "recovery-agent",
		FilterSubject:       "recovery.requested.>",
		TrajectoryStepLimit: 80,
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.StreamName == "" {
		return fmt.Errorf("stream_name is required")
	}
	if c.ConsumerName == "" {
		return fmt.Errorf("consumer_name is required")
	}
	if c.FilterSubject == "" {
		return fmt.Errorf("filter_subject is required")
	}
	return nil
}
