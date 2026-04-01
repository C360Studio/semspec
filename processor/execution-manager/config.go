package executionmanager

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"

	"github.com/c360studio/semspec/workflow"
)

// executionOrchestratorSchema is the pre-generated schema for this component.
var executionOrchestratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// TeamsConfig configures the team-based execution mode. Teams are ON by
// default whenever the agent graph is available. Set Enabled to false as
// an explicit kill switch for debugging. When Roster is empty, a default
// two-team roster ("alpha", "bravo") is auto-generated from the component model.
type TeamsConfig struct {
	// Enabled is a kill switch. When explicitly set to false, team-based
	// execution is disabled. When nil (omitted from JSON), teams are ON.
	Enabled *bool `json:"enabled,omitempty" schema:"type:bool,description:Kill switch — set false to disable team-based execution,category:basic"`

	// Roster defines the teams and their member roles/models.
	// When empty, a default two-team roster is auto-generated.
	Roster []TeamRosterEntry `json:"roster,omitempty" schema:"type:array,description:Team definitions with member roles,category:basic"`
}

// TeamRosterEntry defines a single team in the roster.
type TeamRosterEntry struct {
	Name    string            `json:"name"`
	Members []TeamMemberEntry `json:"members"`
}

// TeamMemberEntry defines a single agent member of a team.
type TeamMemberEntry struct {
	Role    string                 `json:"role"`              // "tester", "builder", "reviewer"
	Model   string                 `json:"model"`             // model endpoint name
	Persona *workflow.AgentPersona `json:"persona,omitempty"` // optional persona config (ADR-030)
}

// Config holds the configuration for the execution-orchestrator component.
type Config struct {
	// MaxIterations is the maximum number of developer→validate→review cycles
	// before escalating to human review. This budget is shared across all
	// retry reasons (validation failure + code review rejection).
	MaxIterations int `json:"max_iterations" schema:"type:int,description:Maximum execution iterations before escalation,category:basic,default:3"`

	// TimeoutSeconds is the per-execution timeout in seconds (covers the
	// full develop→validate→review pipeline, not individual steps).
	TimeoutSeconds int `json:"timeout_seconds" schema:"type:int,description:Timeout per task execution in seconds,category:advanced,default:1800"`

	// SandboxURL is the base URL of the sandbox server for worktree isolation.
	// When empty, worktree lifecycle management is disabled and agents operate
	// directly on the host filesystem.
	SandboxURL string `json:"sandbox_url,omitempty" schema:"type:string,description:Sandbox server URL for worktree isolation (empty=disabled),category:advanced"`

	// GraphGatewayURL is the URL of the graph-gateway for indexing readiness checks.
	// When empty, the indexing gate is disabled (merge completes immediately without
	// waiting for semsource to index the commit).
	GraphGatewayURL string `json:"graph_gateway_url,omitempty" schema:"type:string,description:Graph gateway URL for indexing gate (empty=disabled),category:advanced"`

	// IndexingBudgetStr is the maximum time to wait for semsource to index a merge
	// commit before proceeding. Uses Go duration format (e.g. "60s", "90s").
	// When zero or empty, defaults to 60s.
	IndexingBudgetStr string `json:"indexing_budget,omitempty" schema:"type:string,description:Max wait for commit indexing after merge (e.g. 60s),category:advanced,default:60s"`

	// BenchingThreshold is the per-category error count that triggers agent
	// benching. Deprecated: use LessonThreshold instead.
	BenchingThreshold int `json:"benching_threshold,omitempty" schema:"type:int,description:Deprecated — use lesson_threshold,category:advanced,default:3"`

	// LessonThreshold is the per-role per-category error count that triggers
	// a recurring-error notification. When any single error category for a role
	// reaches this count, a warning is logged and a NATS event published.
	LessonThreshold int `json:"lesson_threshold,omitempty" schema:"type:int,description:Error count per role per category that triggers notification,category:advanced,default:2"`

	// Model is the model endpoint name passed through to dispatched agents.
	Model string `json:"model" schema:"type:string,description:Model endpoint name for agent tasks,category:basic,default:default"`

	// Ports contains the input and output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`

	// Teams configures team-based execution. When Teams.Enabled is false (default),
	// the pipeline uses the existing 4-stage individual agent mode.
	Teams *TeamsConfig `json:"teams,omitempty" schema:"type:object,description:Team-based execution configuration,category:basic"`

	// ExecutionStateBucket is the KV bucket name for execution state.
	// The write IS the event — downstream components watch this bucket.
	ExecutionStateBucket string `json:"execution_state_bucket,omitempty" schema:"type:string,description:KV bucket for execution state (observable twofer),category:advanced,default:EXECUTION_STATES"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxIterations:  3,
		TimeoutSeconds: 1800,
		Model:          "default",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "execution-trigger",
					Type:        "jetstream",
					Subject:     "workflow.trigger.task-execution-loop",
					StreamName:  "WORKFLOW",
					Description: "Receive task execution triggers from task-dispatcher",
					Required:    true,
				},
				{
					Name:        "loop-completions",
					Type:        "jetstream",
					Subject:     "agent.complete.>",
					StreamName:  "AGENT",
					Description: "Receive agentic loop completion events",
					Required:    true,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:        "entity-triples",
					Type:        "nats",
					Subject:     "graph.mutation.triple.add",
					Description: "Publish entity state triples",
					Required:    false,
				},
				{
					Name:        "agent-tasks",
					Type:        "nats",
					Subject:     "agent.task.>",
					Description: "Dispatch agent tasks for development and review",
					Required:    false,
				},
			},
		},
	}
}

// DefaultExecutionStateBucket is the default KV bucket name for execution state.
const DefaultExecutionStateBucket = "EXECUTION_STATES"

// DefaultLessonThreshold is the per-role per-category error count that triggers
// a recurring-error notification.
const DefaultLessonThreshold = 2

// withDefaults returns a copy of c with zero-value fields replaced by defaults.
func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.MaxIterations <= 0 {
		c.MaxIterations = d.MaxIterations
	}
	if c.ExecutionStateBucket == "" {
		c.ExecutionStateBucket = DefaultExecutionStateBucket
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = d.TimeoutSeconds
	}
	if c.BenchingThreshold <= 0 {
		c.BenchingThreshold = workflow.DefaultBenchingThreshold
	}
	if c.LessonThreshold <= 0 {
		c.LessonThreshold = DefaultLessonThreshold
	}
	if c.Model == "" {
		c.Model = d.Model
	}
	if c.Ports == nil {
		c.Ports = d.Ports
	}
	return c
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.MaxIterations <= 0 {
		return fmt.Errorf("max_iterations must be positive")
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	if c.IndexingBudgetStr != "" {
		if _, err := time.ParseDuration(c.IndexingBudgetStr); err != nil {
			return fmt.Errorf("invalid indexing_budget %q: %w", c.IndexingBudgetStr, err)
		}
	}
	// Validate explicitly provided roster entries. Empty roster is fine —
	// seedTeams auto-generates a default two-team roster.
	if c.Teams != nil {
		for i, team := range c.Teams.Roster {
			if len(team.Members) == 0 {
				return fmt.Errorf("teams.roster[%d] (%q) must have at least 1 member", i, team.Name)
			}
		}
	}
	return nil
}

// GetTimeout returns the execution timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// GetIndexingBudget returns the parsed indexing budget duration.
// Returns 0 if not configured (gate caller should use DefaultIndexingBudget).
func (c *Config) GetIndexingBudget() time.Duration {
	if c.IndexingBudgetStr == "" {
		return 0
	}
	d, err := time.ParseDuration(c.IndexingBudgetStr)
	if err != nil {
		return 0
	}
	return d
}
