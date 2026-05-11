package prompt

// RecoveryPromptContext carries everything the recovery-agent user-prompt
// fragment needs to render. The recovery-agent component constructs one of
// these from the inbound RecoveryRequested payload + fetched trajectory
// before calling Assembler.Assemble; keeping the shape on prompt/ (instead
// of importing workflow/payloads) avoids the payloads → prompt → payloads
// dependency cycle.
//
// Mirrors the fields the legacy hand-rolled recoveryPromptInput carried.
// Porting it to a typed AssemblyContext field is the wiring fix that lets
// the persona system (system-base + closed-action-set + rules + lessons-
// learned + tool guidance + response_format gating) contribute to the
// dispatch the same way every other reasoning role does.
type RecoveryPromptContext struct {
	// Layer is the recovery layer (phase-local / coordinator). Surfaces in
	// the prompt header so the agent knows whether this is the first
	// recovery attempt or a coordinator-layer retry.
	Layer string

	// Slug is the plan slug for the wedged work.
	Slug string

	// RequirementID is set for execution-layer recoveries; empty for plan-
	// layer recoveries (planner / req-gen / scen-gen / arch-gen wedges).
	RequirementID string

	// TaskID is set when the wedge is at the per-task level (a developer
	// TDD-budget exhaustion); empty for higher-layer wedges.
	TaskID string

	// LoopID identifies the wedged agent's agentic-loop. The component
	// fetches this loop's trajectory and pre-summarises it into
	// TrajectorySteps below.
	LoopID string

	// PriorRecoveryID is the prior recovery attempt's ID when this dispatch
	// is a coordinator-layer retry. Empty on first recovery. Renderer
	// surfaces this so the agent knows to pick a different action shape
	// than the prior layer attempted.
	PriorRecoveryID string

	// EscalationReason is the wedged component's failure summary. Required.
	EscalationReason string

	// LastFailureFeedback is the most-recent reviewer / verdict feedback
	// the wedged agent saw before escalating. Empty when the wedge wasn't
	// review-driven (e.g. a parse-error escalation has no reviewer
	// feedback).
	LastFailureFeedback string

	// TrajectorySteps are pre-summarised step lines (clipped to ~200 chars
	// each per trajectory.SummarizeStep). Empty when the request had no
	// loop_id (plan-layer wedges today) or the trajectory fetch failed —
	// the renderer surfaces "trajectory unavailable" in that case.
	TrajectorySteps []string
}
