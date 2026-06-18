package requirementexecutor

// ADR-037 race-closure between req-executor's synchronous markFailedLocked
// and recovery-agent's async PlanDecision emit. When
// Config.DeferTerminalOnRecovery is true, exhaustion call sites that have
// already published RecoveryRequested transition the exec to
// phaseAwaitingRecovery and arm a timer instead of immediately
// terminal-failing. The accepted-PlanDecision watcher
// (workflow.events.plan-decision.accepted) resumes the exec; the timer
// terminal-fails it on no-accept.
//
// Take 8 (2026-05-11 gemini @hard) surfaced this race: req-executor
// terminal-failed in <1ms while recovery's accepted PlanDecision landed
// ~14s later. By then, the req was gone and cascade dirty-marks hit a
// graveyard. This file closes the gap so accepted recovery PlanDecisions
// can actually revive the wedged req.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// planDecisionAcceptedConsumer is the durable consumer name. One per
// req-executor process so events redeliver after a crash; consumer is
// shared with restarts.
const planDecisionAcceptedConsumer = "requirement-executor-plan-decision-accepted"

// recoveryDeferEnabled reports whether the ADR-037 race-closure path is
// active. Centralizing the gate keeps the call sites tidy and the
// disabled-path tests straightforward.
func (c *Component) recoveryDeferEnabled() bool {
	return c.config.DeferTerminalOnRecovery
}

// recoveryTimeout returns the configured awaiting-recovery deadline.
func (c *Component) recoveryTimeout() time.Duration {
	if c.config.RecoveryTimeoutSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.config.RecoveryTimeoutSeconds) * time.Second
}

// maxRecoveryRestarts returns the configured upper bound on resume
// attempts. Defaulted in config.withDefaults.
func (c *Component) maxRecoveryRestarts() int {
	return c.config.MaxRecoveryRestarts
}

// deferToAwaitingRecoveryLocked transitions the exec to
// phaseAwaitingRecovery and arms the recoveryTimer. The caller MUST
// have just published a RecoveryRequested (or be in a code path that
// is known to have triggered one upstream — e.g. the tdd-exhaustion
// path where execution-manager publishes recovery before req-executor
// receives the escalated signal).
//
// Returns true when the defer succeeded (caller skips markFailedLocked).
// Returns false when defer was not applicable (caller falls through to
// markFailedLocked unchanged):
//   - Feature disabled (DeferTerminalOnRecovery=false)
//   - Already terminated (idempotent guard)
//   - Recovery restart budget exhausted (Goodhart guard)
//
// Caller must hold exec.mu.
func (c *Component) deferToAwaitingRecoveryLocked(ctx context.Context, exec *requirementExecution, reason string) bool {
	if !c.recoveryDeferEnabled() {
		return false
	}
	if exec.terminated {
		return false
	}
	if exec.recoveryRestarts >= c.maxRecoveryRestarts() {
		c.logger.Info("Recovery restart budget exhausted — falling through to markFailedLocked",
			"entity_id", exec.EntityID,
			"requirement_id", exec.RequirementID,
			"recovery_restarts", exec.recoveryRestarts,
			"max_recovery_restarts", c.maxRecoveryRestarts(),
		)
		return false
	}

	// Stop any existing recovery timer (idempotency: a second defer of the
	// same exec re-arms a fresh timer).
	if exec.recoveryTimer != nil {
		exec.recoveryTimer.stop()
		exec.recoveryTimer = nil
	}

	exec.awaitingRecovery = true
	exec.recoveryReason = reason

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseAwaitingRecovery, map[string]any{
		"awaiting_recovery_reason": reason,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for awaiting-recovery",
			"stage", phaseAwaitingRecovery, "error", err)
	}

	c.publishEntity(context.Background(),
		NewRequirementExecutionEntity(exec).WithPhase(phaseAwaitingRecovery))

	timeout := c.recoveryTimeout()
	timer := time.AfterFunc(timeout, func() {
		c.handleRecoveryTimeout(exec, timeout)
	})
	exec.recoveryTimer = &timeoutHandle{stop: func() { timer.Stop() }}

	c.logger.Info("Deferred terminal-fail; awaiting recovery PlanDecision",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"reason", reason,
		"timeout", timeout,
	)
	return true
}

// handleRecoveryTimeout fires after the awaiting-recovery deadline with
// no accepted PlanDecision. It re-acquires the exec lock and
// terminal-fails using the captured reason, matching the pre-defer
// behavior. Idempotent against accept-races: if resumeFromRecoveryLocked
// already cleared awaitingRecovery, this is a no-op.
func (c *Component) handleRecoveryTimeout(exec *requirementExecution, timeout time.Duration) {
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if !exec.awaitingRecovery {
		return
	}
	if c.hasPendingRecoveryDecision(context.Background(), exec.Slug, exec.RequirementID) {
		nextTimeout := c.recoveryTimeout()
		timer := time.AfterFunc(nextTimeout, func() {
			c.handleRecoveryTimeout(exec, nextTimeout)
		})
		exec.recoveryTimer = &timeoutHandle{stop: func() { timer.Stop() }}
		c.logger.Warn("Recovery deadline expired but a PlanDecision is still pending; extending awaiting-recovery instead of terminal-failing",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"reason", exec.recoveryReason,
			"expired_timeout", timeout,
			"next_timeout", nextTimeout,
		)
		return
	}
	c.logger.Warn("Recovery deadline expired; terminal-failing",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"reason", exec.recoveryReason,
		"timeout", timeout,
	)
	exec.awaitingRecovery = false
	exec.recoveryTimer = nil
	c.markFailedLocked(context.Background(), exec, exec.recoveryReason)
}

func (c *Component) hasPendingRecoveryDecision(ctx context.Context, slug, requirementID string) bool {
	if c.pendingRecoveryDecisionChecker != nil {
		return c.pendingRecoveryDecisionChecker(ctx, slug, requirementID)
	}
	if c.natsClient == nil {
		return false
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Debug("Recovery timeout: JetStream unavailable while checking pending PlanDecisions",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return false
	}
	bucket, err := js.KeyValue(ctx, "PLAN_STATES")
	if err != nil {
		c.logger.Debug("Recovery timeout: PLAN_STATES unavailable while checking pending PlanDecisions",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return false
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		c.logger.Debug("Recovery timeout: plan unavailable while checking pending PlanDecisions",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return false
	}
	var plan struct {
		PlanDecisions []workflow.PlanDecision `json:"plan_decisions"`
	}
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		c.logger.Debug("Recovery timeout: plan JSON unmarshal failed while checking pending PlanDecisions",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return false
	}
	for _, dec := range plan.PlanDecisions {
		if isPendingRecoveryDecisionForRequirement(dec, requirementID) {
			return true
		}
	}
	return false
}

func isPendingRecoveryDecisionForRequirement(dec workflow.PlanDecision, requirementID string) bool {
	if dec.Status != workflow.PlanDecisionStatusProposed &&
		dec.Status != workflow.PlanDecisionStatusUnderReview {
		return false
	}
	switch dec.Kind {
	case workflow.PlanDecisionKindRequirementChange,
		workflow.PlanDecisionKindStoryReprepare,
		workflow.PlanDecisionKindArchitectureRevise,
		workflow.PlanDecisionKindScopeIncomplete:
		// recoverable or waiting decisions
	default:
		return false
	}
	if len(dec.AffectedReqIDs) == 0 {
		return true
	}
	for _, affected := range dec.AffectedReqIDs {
		if affected == requirementID {
			return true
		}
	}
	return false
}

// resumeFromRecoveryLocked re-runs a wedged execution after a recovery
// PlanDecision has been accepted. Strategy: full re-decompose
// (restructure-equivalent reset). The req.RecoveryHint set by
// plan-manager.applyRecoveryHint propagates to the next developer
// dispatch via execution-manager.lookupRecoveryHint, so the manager
// guidance reaches the wedged role through the existing channel.
//
// Two callers as of 2026-05-28:
//   - handlePlanDecisionAccepted via the existing iteration-exhaustion /
//     dev-gate fast-fail path (exec was mid-cycle when markEscalatedLocked
//     deferred it — ADR-049 Move-3 lands here too).
//   - resumeTerminalForRecoveryLocked via the QA-recovery path (exec was
//     terminal-stage in KV when the plan-level QA verdict rejected; the
//     wrapper re-marks it awaiting before calling here).
//
// The function is identical for both callers — the only difference upstream
// is how the exec got into activeExecs (cache-resident vs. KV-loaded then
// reinserted).
//
// Story reset (ADR-049 / #167/#168 analogue for the dev-gate path): this
// function calls reopenOwnedStoriesForRecoveryLocked at the TOP — before
// branch recreation and re-decompose — so ANY owned story left in a non-
// ready state (Complete, Executing, Failed, Pending) is walked back to Ready.
// This mirrors the lesson learned on the QA-recovery path: without the reopen,
// dispatchCurrentStoryLocked's ClaimStoryStatus(→Executing) is rejected for
// a story not at Ready (invalid transition), the executor treats the reservation
// as unavailable and now fails closed unless completed owner evidence exists.
// The dev-gate fast-fail (ADR-049 Move-3) is the primary producer of
// Executing-stranded stories.
//
// If the reopen was attempted (natsClient present) but not all owned stories
// reached Ready (transient NATS error, partial walk), this function re-arms
// awaiting-recovery and returns WITHOUT incrementing recoveryRestarts — so the
// next PlanDecision-accepted event retries the walk from wherever the story
// currently sits (self-healing across cycles). The outer recovery timer still
// bounds total wait time.
//
// Caller must hold exec.mu.
func (c *Component) resumeFromRecoveryLocked(ctx context.Context, exec *requirementExecution) {
	// Reset any owned stories that are not in a dispatchable state BEFORE the
	// branch recreation and re-decompose. This must happen first so the reset
	// stories are visible in PLAN_STATES when dispatchSynthesizerLocked reloads
	// the plan. Both the dev-gate / iteration-exhaustion path (new) and the
	// QA-recovery path (existing via resumeTerminalForRecoveryLocked) need this.
	// The call is idempotent: stories already at Ready are excluded.
	reopenResult := c.reopenOwnedStoriesForRecoveryLocked(ctx, exec)

	// C1: if the walk was attempted but did not fully land, do NOT proceed to
	// dispatchSynthesizerLocked — dispatching now would produce the same
	// false-green pressure this function exists to prevent (story still non-Ready,
	// ClaimStoryStatus(→Executing) rejected, executor cannot attach completed
	// owner evidence). Instead, re-arm awaiting-recovery WITHOUT burning a
	// recoveryRestarts slot so the next cycle retries the walk from the story's
	// current status (self-healing). The outer recovery timer still fires if no
	// progress is made, so this path cannot loop forever within a single exec.
	//
	// natsClient==nil means unit-test / no-NATS mode: reopenResult.candidates==0
	// regardless of story states, so we always proceed — keeping the nil-client
	// path a clean no-op and not disrupting existing unit tests.
	if c.natsClient != nil && !reopenResult.allReady() {
		// Re-mark awaiting-recovery (without incrementing recoveryRestarts — this
		// was not a real restart attempt, only a story-walk attempt). Re-arm the
		// timer so the outer budget fires if the walk keeps failing.
		exec.awaitingRecovery = true
		timeout := c.recoveryTimeout()
		timer := time.AfterFunc(timeout, func() {
			c.handleRecoveryTimeout(exec, timeout)
		})
		exec.recoveryTimer = &timeoutHandle{stop: func() { timer.Stop() }}
		c.logger.Warn("recovery: story walk incomplete — re-arming awaiting-recovery to retry on next PlanDecision",
			"entity_id", exec.EntityID,
			"slug", exec.Slug,
			"requirement_id", exec.RequirementID,
			"reopened", reopenResult.reopened,
			"candidates", reopenResult.candidates,
		)
		return
	}

	if exec.recoveryTimer != nil {
		exec.recoveryTimer.stop()
		exec.recoveryTimer = nil
	}
	exec.awaitingRecovery = false
	exec.terminated = false
	exec.recoveryRestarts++

	// Reset DAG state AND per-recovery retry budget. Each recovery
	// resumption is a fresh shot at the requirement — re-decompose, retry
	// up to MaxRequirementRetries again. recoveryRestarts is the outer cap
	// (incremented above); RetryCount is the inner per-recovery budget.
	//
	// Before issue #36 fix (2026-05-31): RetryCount was preserved across
	// resumptions, which meant every recovery resume immediately exhausted
	// on the first req-review attempt (RetryCount=2 >= MaxRetries=2 fired
	// at L1650 of component.go before any meaningful work) and deferred
	// back to recovery — wasting an outer budget slot per cycle. Each
	// recovery then yielded only 1 round of (impl+test+req-review) before
	// the next defer, defeating the point of giving the requirement
	// another chance.
	//
	// Reset semantics matches restructure (startRestructureRetryLocked at
	// component.go:1835), which already starts fresh; recovery resumption
	// is conceptually identical (wipe everything, start over with feedback)
	// except that recovery is human/auto-decision-driven rather than
	// reviewer-verdict-driven.
	//
	// Worst-case cycle math after this fix:
	//   - Per recovery: (1 initial + MaxRequirementRetries fixable retries)
	//     × max_tdd_cycles per node = 3 × 5 = 15 dev dispatches with
	//     default config
	//   - Across recoveries: × (1 + MaxRecoveryRestarts) = × 3 = 45
	//     dev dispatches lifetime max
	// Operators concerned about worst-case spend should lower
	// max_recovery_restarts (default 1; hybrid-gpt5 uses 2). A future
	// PR may add a stuck-pattern detector (consecutive identical
	// reviewer verdicts → human gate) as defense-in-depth, but the
	// underlying non-convergence is fixed structurally by ADR-041 — the
	// req-reviewer's tier-aware contract makes the run-#3-shape
	// failure mode impossible.
	exec.DAG = nil
	exec.SortedNodeIDs = nil
	exec.NodeIndex = nil
	exec.CurrentNodeIdx = -1
	exec.CurrentNodeTaskID = ""
	exec.VisitedNodes = make(map[string]bool)
	exec.NodeResults = nil
	// Re-run the requirement's Stories from the start on recovery resume. Without
	// this, a multi-Story requirement keeps a stale CurrentStoryIdx and re-runs
	// only a subset; clearing SortedStoryIDs makes dispatchSynthesizerLocked
	// re-derive the story topo order from current plan Stories. Applies to both
	// QA-recovery (completed req) and iteration-exhaustion resumes.
	exec.CurrentStoryIdx = 0
	exec.SortedStoryIDs = nil
	// KV NodeResults is append-only via handleReqNodeMutation; the in-memory
	// wipe above must be mirrored to KV or the stale entries reappear on
	// rebuildExecFromKV after the next restart. Closes Pass-1 H4 for the
	// recovery-resume path. Best-effort: a wipe failure logs but doesn't
	// abort the resume (the recovery branch creation already succeeded).
	if err := c.sendReqResetNodeResults(ctx, exec.storeKey); err != nil {
		c.logger.Warn("Failed to wipe KV NodeResults on recovery resume",
			"entity_id", exec.EntityID, "error", err)
	}
	exec.DirtyNodeIDs = nil
	exec.ReviewVerdict = ""
	exec.ReviewFeedback = ""
	exec.ReviewRetryCount = 0
	exec.RetryCount = 0
	exec.ScenarioVerdicts = nil

	if c.sandbox != nil && exec.RequirementBranch != "" {
		// Recreate from the DependsOn-derived base, not "HEAD": a reopened
		// mid-chain requirement must re-fork from its prerequisite's branch so it
		// re-inherits the prereq edits it was derived from. Empty base (a DAG
		// root) falls back to HEAD — the prior behavior, unchanged.
		recreateBase := selectReqBranchBase("", exec.BaseBranch)
		if err := c.sandbox.DeleteBranch(ctx, exec.RequirementBranch); err != nil {
			c.logger.Warn("Failed to delete old requirement branch on recovery resume",
				"branch", exec.RequirementBranch, "error", err)
		}
		if err := c.sandbox.CreateBranch(ctx, exec.RequirementBranch, recreateBase); err != nil {
			c.logger.Warn("Failed to recreate requirement branch on recovery resume",
				"branch", exec.RequirementBranch, "base", recreateBase, "error", err)
		}
	}

	if err := c.sendReqPhase(ctx, exec.storeKey, phaseDecomposing, map[string]any{
		"recovery_restart": exec.recoveryRestarts,
		"resumed_from":     phaseAwaitingRecovery,
	}); err != nil {
		c.logger.Warn("Failed to send req.phase mutation for recovery resume", "error", err)
	}

	c.publishEntity(context.Background(),
		NewRequirementExecutionEntity(exec).WithPhase(phaseDecomposing))

	c.logger.Info("Resuming requirement from awaiting-recovery — re-decomposing",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"recovery_restart", exec.recoveryRestarts,
		"max_recovery_restarts", c.maxRecoveryRestarts(),
	)

	c.dispatchSynthesizerLocked(ctx, exec)
}

// findAwaitingByRequirement locates an active exec in
// phaseAwaitingRecovery for the given slug+requirementID. Returns nil
// when no match. Cache iteration is bounded by the small number of
// concurrent executions per slug; this hot path is fine.
func (c *Component) findAwaitingByRequirement(slug, requirementID string) *requirementExecution {
	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok || exec == nil {
			continue
		}
		// Read awaitingRecovery under the exec's lock to avoid a race with
		// timer or resume that flips the field.
		exec.mu.Lock()
		match := exec.awaitingRecovery && exec.Slug == slug && exec.RequirementID == requirementID
		exec.mu.Unlock()
		if match {
			return exec
		}
	}
	return nil
}

// abandonExecsForSlug tears down every active requirement execution for a slug.
// It is the conservative fallback for older/unscoped planning re-entry events.
// Scoped architecture_revise/story_reprepare events normally call
// abandonExecsForRequirements instead. For each exec it stops timers, marks it
// terminated (so any late cross-component callback — a task completion that
// lands after the wipe — hits the terminated guard and no-ops instead of
// writing to the deterministic req.<slug>.<reqID> key the fresh exec will
// reuse), and removes it from activeExecs via cleanupExecutionLocked.
//
// Returns the count abandoned. The in-flight agentic loops themselves are not
// force-cancelled here (req-executor has no cancellation channel to them); they
// run to completion and their terminal writes are absorbed by the terminated
// guard. EXECUTION_STATES rows were already deleted by plan-manager's reset.
// abandonLiveExecForRequirement tears down any active exec for slug+reqID —
// marking it terminated and running cleanupExecutionLocked so it leaves
// activeExecs. Used by the recovery dependent-subtree invalidation so a
// mid-execution dependent on a stale base does not block the fresh re-dispatch
// via the req_watcher EntityID duplicate guard. Returns whether one was found.
//
// Lock ordering: the caller (resetDependentBranchSubtree) holds the REOPENED
// owner's exec.mu; here we lock the DEPENDENT's exec.mu — a distinct exec
// (different requirement ID), and no path locks a dependent then its
// prerequisite, so owner→dependent ordering cannot deadlock.
func (c *Component) abandonLiveExecForRequirement(slug, reqID string) bool {
	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok || exec == nil {
			continue
		}
		exec.mu.Lock()
		if exec.Slug != slug || exec.RequirementID != reqID {
			exec.mu.Unlock()
			continue
		}
		exec.terminated = true
		exec.awaitingRecovery = false
		c.cleanupExecutionLocked(exec, false)
		exec.mu.Unlock()
		return true
	}
	return false
}

func (c *Component) abandonExecsForSlug(slug string) int {
	abandoned := 0
	for _, key := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(key)
		if !ok || exec == nil {
			continue
		}
		exec.mu.Lock()
		if exec.Slug != slug {
			exec.mu.Unlock()
			continue
		}
		exec.terminated = true
		exec.awaitingRecovery = false
		c.cleanupExecutionLocked(exec, false)
		exec.mu.Unlock()
		abandoned++
	}
	return abandoned
}

func (c *Component) abandonExecsForRequirements(slug string, reqIDs []string) int {
	if len(reqIDs) == 0 {
		return c.abandonExecsForSlug(slug)
	}
	abandoned := 0
	seen := make(map[string]struct{}, len(reqIDs))
	for _, reqID := range reqIDs {
		if reqID == "" {
			continue
		}
		if _, ok := seen[reqID]; ok {
			continue
		}
		seen[reqID] = struct{}{}
		if c.abandonLiveExecForRequirement(slug, reqID) {
			abandoned++
		}
	}
	return abandoned
}

// startPlanDecisionAcceptedConsumer subscribes req-executor to
// workflow.events.plan-decision.accepted so the awaiting-recovery resume
// path can fire on auto-accept (or human-accept). Best-effort: a watch
// failure here doesn't stop the component; it just means the timer is
// the only recovery resolution channel.
func (c *Component) startPlanDecisionAcceptedConsumer(ctx context.Context) error {
	if c.natsClient == nil {
		return nil
	}
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  planDecisionAcceptedConsumer,
		FilterSubject: c.config.PlanDecisionAcceptedSubject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	handler := func(msgCtx context.Context, msg jetstream.Msg) {
		c.handlePlanDecisionAccepted(ctx, msgCtx, msg)
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, handler); err != nil {
		return fmt.Errorf("consume plan-decision-accepted events: %w", err)
	}
	c.logger.Info("plan-decision-accepted consumer started",
		"stream", cfg.StreamName, "consumer", cfg.ConsumerName)
	return nil
}

// handlePlanDecisionAccepted resolves the awaiting-recovery state for
// any affected requirement carried by an accepted PlanDecision. Each
// affected req is matched against the activeExecs cache; matches are
// resumed via resumeFromRecoveryLocked, non-matches are silently
// ignored (the event also fires for non-recovery proposals).
//
// lifecycleCtx is used for the resume's dispatch context so it
// outlives the per-message handler.
func (c *Component) handlePlanDecisionAccepted(lifecycleCtx, msgCtx context.Context, msg jetstream.Msg) {
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
		c.logger.Error("Failed to parse PlanDecisionAccepted BaseMessage envelope", "error", err)
		_ = msg.Term()
		return
	}
	var evt payloads.PlanDecisionAcceptedEvent
	if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
		c.logger.Error("Failed to parse PlanDecisionAcceptedEvent payload", "error", err)
		_ = msg.Term()
		return
	}
	if err := evt.Validate(); err != nil {
		c.logger.Warn("Invalid PlanDecisionAcceptedEvent", "error", err)
		_ = msg.Ack()
		return
	}

	// architecture_revise and story_reprepare restart the plan from a planning
	// phase; scope_incomplete restarts execution from plan-manager's retry path.
	// In all three cases plan-manager has already reset/requeued the relevant
	// work. The wedged exec(s) must NOT be resumed here — resuming would
	// re-decompose against stale context and race the regenerated execution.
	// Abandon the event's scoped affected requirements (expanded by
	// plan-decision-handler where needed); fall back to every active exec for the
	// slug only when older/unscoped events do not carry affected requirement IDs.
	if evt.Kind == workflow.PlanDecisionKindArchitectureRevise ||
		evt.Kind == workflow.PlanDecisionKindStoryReprepare ||
		evt.Kind == workflow.PlanDecisionKindScopeIncomplete {
		abandoned := c.abandonExecsForRequirements(evt.Slug, evt.AffectedRequirementIDs)
		c.logger.Info("Abandoned in-flight execs on plan-manager-owned recovery accept",
			"slug", evt.Slug, "proposal_id", evt.ProposalID, "kind", evt.Kind,
			"affected_requirements", len(evt.AffectedRequirementIDs), "abandoned", abandoned)
		_ = msg.Ack()
		return
	}

	// When one recovery event affects BOTH a prerequisite and a requirement that
	// derives from it (the normal needs_changes case — every active req is
	// affected), do NOT directly resume the dependent: resuming it would recreate
	// its branch from the prerequisite's PRE-rebuild base (the prereq isn't
	// reopened yet) and launch a dev loop the prereq's cascade then orphans. Skip
	// it here; the prerequisite's resumeTerminalForRecoveryLocked cascade resets
	// it, and the orchestrator re-dispatches it fresh with a re-resolved base.
	skipAsDependent := c.coAffectedDependents(msgCtx, evt.Slug, evt.AffectedRequirementIDs)

	for _, reqID := range evt.AffectedRequirementIDs {
		if skipAsDependent[reqID] {
			c.logger.Info("Skipping direct resume of a co-affected branch dependent — its prerequisite's cascade will reset and re-derive it",
				"slug", evt.Slug, "requirement_id", reqID)
			continue
		}
		// First, the existing awaiting-recovery resume path: covers the
		// iteration-exhaustion wedge (exec was mid-cycle, got marked
		// awaiting_recovery by markEscalatedLocked, now we resume it).
		if exec := c.findAwaitingByRequirement(evt.Slug, reqID); exec != nil {
			c.logger.Info("Resuming exec on accepted PlanDecision",
				"slug", evt.Slug,
				"requirement_id", reqID,
				"proposal_id", evt.ProposalID,
			)
			exec.mu.Lock()
			c.resumeFromRecoveryLocked(lifecycleCtx, exec)
			exec.mu.Unlock()
			continue
		}

		// Second, the QA-recovery path: covers the case where the
		// requirement's per-req gates approved (dev + reviewer signed off,
		// exec reached completed) but the plan-level QA verdict rejected.
		// The completed exec was removed from activeExecs at cleanup, so
		// findAwaitingByRequirement misses it. Without this branch, the
		// recovery chain hangs at "PlanDecision accepted, nothing happens
		// downstream" — empirically caught 2026-05-28 on gemini mavlink-
		// decode run #4 where qa-reviewer rejected for flaky time.Sleep
		// timing, recovery-agent emitted a refined_prompt PlanDecision,
		// auto-accept watcher accepted it, but the plan stayed at
		// `rejected` because no exec was in awaiting_recovery to resume.
		exec, err := c.loadTerminalReqExecFromKV(msgCtx, evt.Slug, reqID)
		if err != nil {
			c.logger.Warn("Failed to load terminal req exec from KV for QA-recovery",
				"slug", evt.Slug, "requirement_id", reqID, "error", err)
			continue
		}
		if exec == nil {
			// No match anywhere — the event also fires for non-recovery
			// proposals and for proposals affecting reqs we never tracked.
			continue
		}

		// Budget gate. recoveryRestarts is not persisted on
		// workflow.RequirementExecution, so rebuildExecFromKV always sets
		// it to zero — the per-exec gate in deferToAwaitingRecoveryLocked
		// can never bite on the QA-recovery path. Count accepted
		// recovery-agent PlanDecisions for this req on the plan instead;
		// the just-accepted proposal is one of them, so the gate fires
		// when subsequent retries try to land on top of an exhausted
		// budget.
		cycles := c.countAcceptedRecoveryCyclesForReq(msgCtx, evt.Slug, reqID)
		if cycles > c.maxRecoveryRestarts() {
			c.logger.Warn("QA-recovery budget exhausted; refusing to resume completed requirement",
				"slug", evt.Slug,
				"requirement_id", reqID,
				"proposal_id", evt.ProposalID,
				"cycles_observed", cycles,
				"max_recovery_restarts", c.maxRecoveryRestarts(),
			)
			continue
		}

		c.activeExecs.Set(exec.EntityID, exec)
		exec.mu.Lock()
		c.resumeTerminalForRecoveryLocked(lifecycleCtx, exec, evt.ProposalID)
		exec.mu.Unlock()
	}
	_ = msg.Ack()
}
