package payloads

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/message"
)

// RecoveryActionKind enumerates the closed action set a wedge-recovery agent
// can return per ADR-037. Adding a new kind requires an ADR addendum + payload
// re-registration; the closed set keeps recovery agents inside an approved
// blast radius and prevents arbitrary state mutation.
type RecoveryActionKind string

const (
	// RecoveryActionRefinePrompt — rewrite the wedged agent's task prompt with
	// explicit context the agent missed (e.g. "graph_search showed
	// `[project] org.sensorhub`; use that"). Recovery's first reach when the
	// trajectory shows the agent had the answer in front of it but didn't act.
	RecoveryActionRefinePrompt RecoveryActionKind = "refine_prompt"

	// RecoveryActionNarrowScope — reduce the task's scope (e.g. split a
	// multi-file change into one file at a time). Used when the trajectory
	// shows the agent thrashing across files because the task was too broad.
	RecoveryActionNarrowScope RecoveryActionKind = "narrow_scope"

	// RecoveryActionSplitReq — decompose the requirement into smaller
	// requirements. Heavier action than narrow_scope; mutates plan structure.
	// Reserved for cases where the plan-level decomposition was clearly wrong.
	RecoveryActionSplitReq RecoveryActionKind = "split_req"

	// RecoveryActionEscalateHuman — recovery agent has analysed the wedge
	// and produced a diagnosis but cannot pick a programmatic action. Surfaces
	// in the UI with the diagnosis text. The good failure mode — analysis is
	// the deliverable.
	RecoveryActionEscalateHuman RecoveryActionKind = "escalate_human"

	// RecoveryActionMarkUnrecoverable — recovery agent has determined the
	// wedge cannot succeed from current state regardless of refinements
	// (e.g. upstream artifact doesn't exist, fixture is malformed). Plan
	// continues with reduced scope or fails cleanly with diagnostic.
	RecoveryActionMarkUnrecoverable RecoveryActionKind = "mark_unrecoverable"

	// RecoveryActionStoryReprepare — wedge analysis points at Sarah's
	// Story-shaping (ADR-043 Move 3) as the source. The plan-time DAG is
	// wrong: a Story's tasks don't cover the work, files_owned misses a
	// path the dev needed, or the components selected don't match the
	// implementation. Reaches back to story-preparer for a re-prep cycle
	// on the affected requirement (cascade dirty-marks the requirement →
	// plan-manager transitions back to preparing_stories → Sarah runs
	// again with the recovery diagnosis as RecoveryHint).
	//
	// Distinct from split_req: split_req mutates plan structure at the
	// requirement layer (one big req → two smaller reqs); story_reprepare
	// keeps the requirements as authored and asks Sarah to re-shard.
	// Distinct from narrow_scope: narrow_scope reduces the task's file
	// surface; story_reprepare re-authors the task DAG itself.
	//
	// ADR-043 PR 4i lands the action vocabulary; callers materialize once
	// execution dispatches per-Story (PR 4h). Until then this action is
	// reserved infrastructure — the closed-set parser accepts it and the
	// PlanDecision routing maps it, but no recovery dispatch currently
	// emits it.
	RecoveryActionStoryReprepare RecoveryActionKind = "story_reprepare"
)

// RecoveryLayer identifies which recovery layer attempted the action. Per
// ADR-037 three-guardrails: phase-local recovery gets one shot, then
// coordinator gets one shot, then human. The next layer keys off the prior
// layer's record to avoid attempting the same recovery shape twice.
type RecoveryLayer string

const (
	// RecoveryLayerPhaseLocal — recovery dispatched by the same component
	// whose escalation triggered it (plan-manager for plan-phase wedges,
	// execution-manager for TDD-cycle wedges).
	RecoveryLayerPhaseLocal RecoveryLayer = "phase_local"

	// RecoveryLayerCoordinator — recovery dispatched by the dedicated
	// coordinator component (Stage 2) when phase-local recovery exhausted
	// or when the wedge crosses phase boundaries (QA failures, cross-req
	// contract mismatches).
	RecoveryLayerCoordinator RecoveryLayer = "coordinator"
)

// RecoveryRequestedType is the message type emitted on
// recovery.requested.<slug> when an escalating component asks for recovery.
// Async dispatch per ADR-037 stage-1 design lock — the escalating component
// emits this and continues, listens for RecoveryComplete on
// recovery.complete.<slug> via the standard reconciliation path.
var RecoveryRequestedType = message.Type{
	Domain:   "workflow",
	Category: "recovery-requested",
	Version:  "v1",
}

// RecoveryRequested carries the wedge context a recovery agent needs to
// diagnose. Per ADR-037 stage-1 design lock #2: full trajectory (capped at
// 80 steps via internal/trajectory.DefaultLogStepLimit) + plan/req state +
// last-failure feedback + referenced graph entity IDs.
//
// Wire semantics:
//   - Published on recovery.requested.<slug> via the WORKFLOW JetStream
//     stream. Subject family chosen to mirror existing workflow.events.>
//     conventions; recovery is itself a workflow event class.
//   - Consumed by phase-local recovery (when wired into plan-manager and
//     execution-manager Stage-1 escalation paths) or by the coordinator
//     component (Stage 2). The Layer field disambiguates.
type RecoveryRequested struct {
	// RecoveryID is a UUID generated by the escalating component, used as
	// the primary key in RECOVERY_STATES KV. The matching RecoveryComplete
	// message echoes this back so the watcher can reconcile.
	RecoveryID string `json:"recovery_id"`

	// Layer identifies the recovery layer this request targets — phase-local
	// for first-attempt recovery, coordinator for second-attempt or
	// cross-phase wedges.
	Layer RecoveryLayer `json:"layer"`

	// Slug is the plan slug — routing key and trace-deep-link.
	Slug string `json:"slug"`

	// RequirementID is set when the wedge is requirement-scoped to a single
	// requirement (e.g., execution-manager iteration exhaustion on one TDD
	// task). Empty for plan-phase wedges.
	RequirementID string `json:"requirement_id,omitempty"`

	// AffectedRequirementIDs lists the requirement IDs the wedge implicates
	// when there is more than one. Populated by plan-manager on QA verdict
	// wedges where the qa-reviewer's verdict applies to all assembled
	// requirements (e.g., "the implementation is flaky across the plan").
	// When set, supersedes RequirementID — recovery-agent's emitPlanDecision
	// uses this list to populate PlanDecision.AffectedReqIDs, which is what
	// the existing auto-accept watcher
	// (plan-decision-handler/recovery_autoaccept.go) requires non-empty
	// before firing without operator intervention.
	//
	// Empty (the default) preserves pre-2026-05-28 behavior: the single
	// RequirementID is used if set, else PlanDecision.AffectedReqIDs is
	// empty and the auto-accept watcher leaves the decision for human
	// review. That fallback remains correct for plan-review revision-cap
	// wedges where the PLAN itself is wrong and human gating is the right
	// outcome.
	AffectedRequirementIDs []string `json:"affected_requirement_ids,omitempty"`

	// AffectedStoryIDs lists Story IDs the wedge implicates when recovery
	// reaches Sarah's layer (ADR-043). Populated by requirement-executor
	// from the wedged exec's SortedStoryIDs at the time the
	// RecoveryRequested fires. The recovery-agent threads this list into
	// PlanDecision.AffectedStoryIDs so the cascade can dirty-mark the
	// specific Stories rather than the whole Requirement. Empty for
	// recovery requests whose wedge is requirement-scoped (refine_prompt,
	// narrow_scope) — the absence signals the recovery-agent should not
	// propose story_reprepare. Train C step 2.
	AffectedStoryIDs []string `json:"affected_story_ids,omitempty"`

	// TaskID identifies the specific TDD task that wedged. Empty for
	// plan-phase wedges.
	TaskID string `json:"task_id,omitempty"`

	// LoopID is the agentic-loop ID of the wedged agent. Strongly recommended
	// — the recovery agent fetches the full trajectory via internal/trajectory
	// when set, which is the load-bearing input per ADR-037 design lock #2.
	// Optional because plan-phase wedges don't yet plumb the wedged generator's
	// loop ID through RevisionMutationRequest (TODO: track on workflow.Plan).
	// When empty, the recovery agent falls back to feedback + findings only.
	// For execution-phase wedges this is exec.DeveloperLoopID (always set).
	LoopID string `json:"loop_id,omitempty"`

	// EscalationReason is the human-readable reason the escalating component
	// recorded (e.g. "fixable rejections exceeded TDD cycle budget", "plan
	// review revision cap reached"). Carries the wedge classification at
	// the source rather than asking the recovery agent to re-derive it.
	EscalationReason string `json:"escalation_reason"`

	// LastFailureFeedback is the most recent reviewer/validator feedback
	// shown to the wedged agent before escalation. Recovery agent reads this
	// alongside the trajectory to understand what the agent was responding
	// to (or failing to respond to). Empty when the wedge wasn't review-
	// driven (e.g. iter=N tool exhaustion).
	LastFailureFeedback string `json:"last_failure_feedback,omitempty"`

	// PriorRecoveryID is set when this recovery request is the second-layer
	// (coordinator) attempt after a phase-local recovery already failed.
	// Coordinator agent reads the prior recovery record from RECOVERY_STATES
	// to avoid attempting the same action shape twice.
	PriorRecoveryID string `json:"prior_recovery_id,omitempty"`

	// TraceID for end-to-end correlation across the recovery flow.
	TraceID string `json:"trace_id,omitempty"`
}

// Schema implements message.Payload.
func (r *RecoveryRequested) Schema() message.Type { return RecoveryRequestedType }

// Validate implements message.Payload.
func (r *RecoveryRequested) Validate() error {
	if r.RecoveryID == "" {
		return fmt.Errorf("recovery_id is required")
	}
	if r.Layer == "" {
		return fmt.Errorf("layer is required")
	}
	if r.Layer != RecoveryLayerPhaseLocal && r.Layer != RecoveryLayerCoordinator {
		return fmt.Errorf("layer must be phase_local or coordinator, got %q", r.Layer)
	}
	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if r.EscalationReason == "" {
		return fmt.Errorf("escalation_reason is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *RecoveryRequested) MarshalJSON() ([]byte, error) {
	type Alias RecoveryRequested
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *RecoveryRequested) UnmarshalJSON(data []byte) error {
	type Alias RecoveryRequested
	return json.Unmarshal(data, (*Alias)(r))
}

// RecoveryRequestedSubjectPrefix is the NATS subject the escalating
// component publishes RecoveryRequested on. Recovery-agent consumes,
// dispatches a manager-role agent, and surfaces the result via a
// PlanDecision through the standard plan.mutation.plan_decision.add
// wire (qa-reviewer + req-executor use the same shape).
//
// History — original ADR-037 stage 1 had a parallel recovery.complete.<slug>
// subject family + RECOVERY_STATES KV for recovery results. That parallel
// pipeline was retired in favour of unifying recovery output with the
// existing PlanDecision lifecycle (qa-reviewer's pattern). Cascade,
// auto-accept config, history, audit, UI surface — all reuse the
// change-proposal-handler infrastructure rather than maintain a parallel
// stack. See project_recovery_to_plan_decision_unification memory entry.
const RecoveryRequestedSubjectPrefix = "recovery.requested."
