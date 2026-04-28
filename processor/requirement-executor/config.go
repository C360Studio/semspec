package requirementexecutor

import (
	"fmt"
	"reflect"
	"time"

	"github.com/c360studio/semstreams/component"
)

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

	// ReviewerModel is the model endpoint for the post-merge requirement
	// reviewer agent (semantic completeness + scenario coverage check). When
	// empty, falls back to Model. Distinct from execution-manager's
	// CodeReviewerModel (in-TDD-cycle code review) so production configs can
	// match each role to its best-suited model — e.g. a reasoning-heavy model
	// for the broader semantic requirement review and a fast, code-aware model
	// for the tight TDD code-review loop.
	ReviewerModel string `json:"reviewer_model" schema:"type:string,description:Model endpoint for post-merge requirement reviewer agent,category:advanced"`

	// SandboxURL is the base URL of the sandbox server. When set, the
	// requirement-executor creates per-requirement branches for worktree isolation.
	SandboxURL string `json:"sandbox_url" schema:"type:string,description:Sandbox server URL for branch management,category:advanced"`

	// MaxRequirementRetries is the maximum number of requirement-level retries after
	// reviewer rejection. On "fixable" rejection, only dirty nodes (whose scenarios
	// failed) are re-run. On "restructure" rejection, the entire DAG is re-decomposed.
	// 0 disables retries (current behavior). Default: 2.
	MaxRequirementRetries int `json:"max_requirement_retries" schema:"type:int,description:Max requirement-level retries on reviewer rejection,category:advanced,default:2,min:0,max:5"`

	// MaxDecomposerRetries is the maximum number of times to re-dispatch the
	// decomposer agent when its output cannot be parsed or produces an invalid
	// DAG (e.g., empty nodes array from an under-powered model). The previous
	// error is appended to the prompt as feedback so the LLM can correct.
	// 0 disables retries. Default: 2.
	MaxDecomposerRetries int `json:"max_decomposer_retries" schema:"type:int,description:Max retries when decomposer output fails to parse or produces an invalid DAG,category:advanced,default:2,min:0,max:5"`

	// MaxReviewRetries is the maximum number of times to re-dispatch the
	// requirement reviewer when its verdict is empty or unparseable. Independent
	// of requirement-level retries. Default: 3.
	MaxReviewRetries int `json:"max_review_retries" schema:"type:int,description:Max reviewer re-dispatches on parse/verdict failure,category:advanced,default:3,min:0,max:5"`

	// EnforceScenarioCoverage rejects decomposer output whose DAG does not carry
	// every input scenario ID on at least one node's scenario_ids. When false,
	// coverage gaps are warn-only and the DAG proceeds (legacy behavior).
	// Real-LLM runs should keep this on — it catches decomposer bugs early and
	// gives actionable feedback. Mock-LLM runs must set it false until fixtures
	// are updated to cite runtime-generated scenario IDs.
	EnforceScenarioCoverage *bool `json:"enforce_scenario_coverage,omitempty" schema:"type:bool,description:Reject decomposer output that leaves input scenarios uncovered,category:advanced,default:true"`

	// RequireCommitObservation gates markCompletedLocked on every NodeResult
	// that claimed FilesModified having a non-empty CommitSHA. Sibling guard
	// to execution-manager's claim/observation cross-check (bug #9): even if a
	// reviewer somehow approves work that never reached main, the requirement
	// will fail rather than be silently completed. Default true now that the
	// upstream wiring is in place — execution-manager records MergeCommit on
	// the task execution, req-executor surfaces it via the synthetic completion
	// event Result, and handleNodeCompleteLocked populates NodeResult.CommitSHA
	// from the parsed payload. Set to false only for tests/fixtures that
	// deliberately bypass the wiring.
	RequireCommitObservation *bool `json:"require_commit_observation,omitempty" schema:"type:bool,description:Fail requirement-completion when any node claimed files but produced no commit observation,category:advanced,default:true"`

	// Ports contains the input and output port definitions.
	Ports *component.PortConfig `json:"ports,omitempty" schema:"type:ports,description:Port configuration,category:basic"`
}

// enforceScenarioCoverage returns true when the gate is on. Defaults to true
// when unset — primary enforcement is the stated design, opt-out is for mock
// fixtures that can't cite runtime scenario IDs yet.
func (c *Config) enforceScenarioCoverage() bool {
	if c.EnforceScenarioCoverage == nil {
		return true
	}
	return *c.EnforceScenarioCoverage
}

// requireCommitObservation returns true when the claim/observation gate is on.
// Defaults to true when unset — production has the upstream wiring (execution-
// manager → workflow.TaskExecution.MergeCommit → req-executor synthetic event
// Result → NodeResult.CommitSHA). Set to false only for tests/fixtures that
// bypass the wiring.
func (c *Config) requireCommitObservation() bool {
	if c.RequireCommitObservation == nil {
		return true
	}
	return *c.RequireCommitObservation
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TimeoutSeconds: 3600,
		Model:          "default",
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:        "execution-states",
					Type:        "kv",
					Subject:     "req.>",
					StreamName:  "EXECUTION_STATES",
					Description: "Watch requirement executions for pending triggers and completion signals (KV self-trigger)",
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
					Name:        "execution-states",
					Type:        "kv",
					Subject:     "task.>",
					StreamName:  "EXECUTION_STATES",
					Description: "Write task execution states to trigger execution-manager via KV watcher",
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
	if c.MaxDecomposerRetries == 0 {
		c.MaxDecomposerRetries = 2
	}
	if c.MaxReviewRetries == 0 {
		c.MaxReviewRetries = 3
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
	return nil
}

// GetTimeout returns the execution timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 60 * time.Minute
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}
