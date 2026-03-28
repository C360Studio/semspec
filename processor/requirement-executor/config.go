package requirementexecutor

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"

	executionmanager "github.com/c360studio/semspec/processor/execution-manager"
)

// TeamsConfig is an alias for the execution-orchestrator TeamsConfig so that
// requirement-executor can share the same team roster format.
type TeamsConfig = executionmanager.TeamsConfig

// requirementExecutorSchema is the pre-generated schema for this component.
var requirementExecutorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds the configuration for the requirement-executor component.
type Config struct {
	// TimeoutSeconds is the per-requirement timeout in seconds (covers the full
	// decompose → serial-execute pipeline).
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Timeout per requirement execution in seconds,category:advanced,default:3600"`

	// Model is the model endpoint name passed through to dispatched agents.
	Model string `json:"model" schema:"type:string,description:Model endpoint name for agent tasks,category:basic,default:default"`

	// DecomposerModel is the model endpoint for the decomposer agent. When empty,
	// falls back to Model. Separate model allows independent mock fixtures.
	DecomposerModel string `json:"decomposer_model" schema:"type:string,description:Model endpoint for decomposer agent,category:advanced"`

	// SandboxURL is the base URL of the sandbox server. When set, the
	// requirement-executor creates per-requirement branches for worktree isolation.
	SandboxURL string `json:"sandbox_url" schema:"type:string,description:Sandbox server URL for branch management,category:advanced"`

	// MaxRequirementRetries is the maximum number of requirement-level retries after
	// reviewer rejection. On "fixable" rejection, only dirty nodes (whose scenarios
	// failed) are re-run. On "restructure" rejection, the entire DAG is re-decomposed.
	// 0 disables retries (current behavior). Default: 2.
	MaxRequirementRetries int `json:"max_requirement_retries" schema:"type:int,description:Max requirement-level retries on reviewer rejection,category:advanced,default:2,min:0,max:5"`

	// Teams configures requirement-level team-based review. When Teams.Enabled is true
	// and at least two teams are defined, a red team challenge runs after all DAG
	// nodes complete before the reviewer makes a final verdict.
	Teams *TeamsConfig `json:"teams,omitempty" schema:"type:object,description:Team-based execution configuration,category:advanced"`

	// Ports contains the input and output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TimeoutSeconds: 3600,
		Model:          "default",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "requirement-trigger",
					Type:        "jetstream",
					Subject:     subjectRequirementTrigger,
					StreamName:  "WORKFLOW",
					Description: "Receive requirement execution triggers from scenario-orchestrator",
					Required:    true,
				},
				{
					Name:        "execution-states",
					Type:        "kv",
					Subject:     "req.>",
					StreamName:  "EXECUTION_STATES",
					Description: "Watch requirement executions for completion signals (KV Twofer)",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "entity-triples",
					Type:        "nats",
					Subject:     "graph.mutation.triple.add",
					Description: "Publish entity state triples",
				},
				{
					Name:        "decomposer-task",
					Type:        "jetstream",
					Subject:     "agent.task.development",
					StreamName:  "AGENT",
					Description: "Dispatch decomposer agent tasks",
				},
				{
					Name:        "execution-trigger",
					Type:        "jetstream",
					Subject:     "workflow.trigger.task-execution-loop",
					StreamName:  "WORKFLOW",
					Description: "Dispatch DAG nodes to execution-orchestrator for TDD pipeline",
				},
			},
		},
	}
}

// withDefaults returns a copy of c with zero-value fields replaced by defaults.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	if c.Model == "" {
		c.Model = d.Model
	}
	if c.MaxRequirementRetries < 0 {
		c.MaxRequirementRetries = 2
	}
	if c.Ports == nil {
		c.Ports = d.Ports
	}
	return c
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	if c.Teams != nil && c.Teams.Enabled {
		if len(c.Teams.Roster) < 2 {
			return fmt.Errorf("teams.roster must contain at least 2 teams when teams.enabled is true (need blue + red), got %d", len(c.Teams.Roster))
		}
	}
	return nil
}

// GetTimeout returns the execution timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 60 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}
