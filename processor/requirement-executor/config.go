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

	// Model is an OPTIONAL hard override propagated to req-executor's own
	// dispatches (via exec.Model on the in-memory state). Empty signals
	// "let each downstream dispatch resolve via capability registry."
	// Distinct from ReviewerModel which overrides the req-executor's own
	// role-specific dispatches. Do NOT default-fill; empty is load-bearing
	// for the capability-resolution path.
	Model string `json:"model,omitempty" schema:"type:string,description:Optional override propagated to req-executor dispatches (empty = use capability registry),category:basic"`

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

	// MaxReviewRetries is the maximum number of times to re-dispatch the
	// requirement reviewer when its verdict is empty or unparseable. Independent
	// of requirement-level retries. Default: 3.
	MaxReviewRetries int `json:"max_review_retries" schema:"type:int,description:Max reviewer re-dispatches on parse/verdict failure,category:advanced,default:3,min:0,max:5"`

	// DeferTerminalOnRecovery opts the requirement-executor into ADR-037's
	// race-closure path. When true, exhaustion call sites that just
	// published RecoveryRequested transition to phaseAwaitingRecovery and
	// arm a timer instead of immediately calling markFailedLocked. The
	// awaiting exec is resumed by the accepted-PlanDecision watcher
	// (workflow.events.plan-decision.accepted) or terminal-failed on
	// timeout. Operationally meaningful only when plan-decision-handler
	// has AutoAcceptRecovery=true — otherwise every recovery would burn
	// the full timeout. Default false to preserve existing behavior;
	// production gemini @hard runs set this true alongside auto-accept.
	DeferTerminalOnRecovery bool `json:"defer_terminal_on_recovery" schema:"type:boolean,description:Defer terminal markFailed when recovery is in flight (ADR-037 stage-2 race closure),category:advanced,default:false"`

	// RecoveryTimeoutSeconds bounds the phaseAwaitingRecovery wait. Real-LLM
	// recovery diagnosis on gemini-pro lands in 10-20s, so 60s gives a
	// comfortable margin without blocking too long when recovery silently
	// fails. Only used when DeferTerminalOnRecovery is true.
	RecoveryTimeoutSeconds int `json:"recovery_timeout_seconds" schema:"type:int,description:Seconds to wait in phaseAwaitingRecovery before terminal-failing,category:advanced,default:60,min:5,max:600"`

	// PlanDecisionAcceptedSubject is the JetStream subject on which
	// plan-decision-handler publishes accepted-PlanDecision events. Must
	// match the AcceptedSubject configured on plan-decision-handler.
	// Default mirrors plan-decision-handler's default and the publisher's
	// hardcoded cascade trigger. The legacy "change-proposal" subject
	// names were retired 2026-05-11 after a real-LLM run surfaced a
	// publisher-vs-consumer subject mismatch (publishers hardcoded the
	// new name; configs overrode consumers to the legacy name; cascade
	// messages silently dropped).
	PlanDecisionAcceptedSubject string `json:"plan_decision_accepted_subject" schema:"type:string,description:Subject for accepted-PlanDecision events (must match plan-decision-handler.accepted_subject),category:advanced,default:workflow.events.plan-decision.accepted"`

	// MaxRecoveryRestarts bounds how many times an exec can be resumed from
	// awaiting-recovery in its lifetime. Goodhart guard: prevents a recovery
	// agent from looping a wedge by repeatedly emitting accepted
	// PlanDecisions. Default 1 — one restart out-of-band of the normal
	// retry budget. 0 disables resumption entirely (recovery would still
	// defer, but every defer would resolve via timer-fail).
	MaxRecoveryRestarts int `json:"max_recovery_restarts" schema:"type:int,description:Max times an exec can be resumed from awaiting-recovery,category:advanced,default:1,min:0,max:5"`

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

// DefaultConfig returns a Config with sensible defaults. Model fields
// are left empty on purpose — empty signals "use capability registry
// resolution" per model.ResolveModel.
func DefaultConfig() Config {
	return Config{
		TimeoutSeconds: 3600,
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
	// Intentionally NOT auto-defaulting Model / ReviewerModel. Empty fields
	// signal "use capability registry resolution" — see model.ResolveModel.
	// Auto-injecting "default" here would short-circuit ResolveModel and
	// route every dispatch to registry defaults.Model instead of the role's
	// capability. Caught 2026-05-08 take 8 trajectory inspection.
	if c.MaxRequirementRetries < 0 {
		c.MaxRequirementRetries = 2
	}
	if c.MaxReviewRetries == 0 {
		c.MaxReviewRetries = 3
	}
	if c.RecoveryTimeoutSeconds == 0 {
		c.RecoveryTimeoutSeconds = 60
	}
	if c.PlanDecisionAcceptedSubject == "" {
		c.PlanDecisionAcceptedSubject = "workflow.events.plan-decision.accepted"
	}
	// MaxRecoveryRestarts: 0 is a valid "disable resumption" value, so
	// only default when the JSON omitted it. Reflect via JSON-default
	// rules: an absent field arrives as 0, but operators who explicitly
	// pass 0 also get 0 — accept the small ambiguity since 0 is the
	// safest behavior either way.
	if c.MaxRecoveryRestarts == 0 {
		c.MaxRecoveryRestarts = 1
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
