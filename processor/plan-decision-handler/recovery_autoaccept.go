package changeproposalhandler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// recoveryAutoAcceptSubject is the plan-manager mutation we invoke to
// accept a recovery PlanDecision. Matches mutationPlanDecisionAccept
// in plan-manager/mutations.go — the two strings must stay in sync.
const recoveryAutoAcceptSubject = "plan.mutation.plan_decision.accept"

// alreadyAcceptedTTL bounds the per-decision dedup cache that prevents
// double-accept on KV watcher replays. PlanDecision IDs are stable
// per-plan; once we've issued an accept, we don't need to remember it
// forever — just long enough for any KV-watcher replay storm to settle.
const alreadyAcceptedTTL = 10 * time.Minute

// watchRecoveryProposals subscribes to PLAN_STATES KV changes and
// programmatically accepts proposed_by="recovery-agent" PlanDecisions
// via the plan.mutation.plan_decision.accept mutation. Gated by
// Config.AutoAcceptRecovery — off by default. ADR-037 stage-2 apply
// path; recovery diagnoses become actionable without requiring an
// operator click.
//
// Acceptance filter is narrow:
//
//	ProposedBy="recovery-agent"
//	Status="proposed"
//	Kind="requirement_change"   (cascade re-runs the affected req)
//	len(AffectedReqIDs) > 0      (apply has something to target)
//
// kind="execution_exhausted" is NOT auto-accepted — that's the terminal
// "escalate_human" / "mark_unrecoverable" shape, where the operator
// should see the diagnosis and decide. Only the recoverable actions
// get the auto-shortcut.
//
// Idempotency: a per-decision dedup cache prevents double-accept on
// watcher replays. KV watchers replay all entries on startup; without
// dedup we'd issue duplicate accepts (which the mutation rejects with
// "cannot accept in status accepted", so it's not load-bearing, but
// it's noisy in logs).
//
// Best-effort: a watch failure or mutation error logs warn but does
// not stop the cascade-trigger consumer. Recovery is observability
// when its apply path is broken; the diagnosis still landed.
func (c *Component) watchRecoveryProposals(ctx context.Context) {
	if c.natsClient == nil {
		return
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("auto-accept watcher: no JetStream", "error", err)
		return
	}
	bucket, err := workflow.WaitForKVBucket(ctx, js, "PLAN_STATES")
	if err != nil {
		c.logger.Warn("auto-accept watcher: PLAN_STATES bucket unavailable",
			"error", err)
		return
	}
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("auto-accept watcher: failed to watch PLAN_STATES",
			"error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Recovery auto-accept watcher started",
		"bucket", "PLAN_STATES",
		"filter", "proposed_by=recovery-agent + status=proposed + kind=requirement_change")

	// Dedup: decision-ID → time-accepted. Drop entries older than the
	// TTL on each pass to keep memory bounded.
	acceptedIDs := make(map[string]time.Time, 64)
	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		// Decode just the fields we need — partial unmarshaling keeps
		// this hot path from doing full Plan struct construction for
		// every KV change.
		var planView struct {
			Slug          string                  `json:"slug"`
			PlanDecisions []workflow.PlanDecision `json:"plan_decisions,omitempty"`
		}
		if err := json.Unmarshal(entry.Value(), &planView); err != nil {
			continue
		}
		if planView.Slug == "" || len(planView.PlanDecisions) == 0 {
			continue
		}

		now := time.Now()
		// Periodic dedup cleanup.
		for id, ts := range acceptedIDs {
			if now.Sub(ts) > alreadyAcceptedTTL {
				delete(acceptedIDs, id)
			}
		}

		for _, dec := range planView.PlanDecisions {
			if !shouldAutoAcceptRecovery(&dec) {
				continue
			}
			if _, seen := acceptedIDs[dec.ID]; seen {
				continue
			}
			acceptedIDs[dec.ID] = now
			c.invokeAccept(ctx, planView.Slug, dec.ID)
		}
	}
}

// shouldAutoAcceptRecovery is the gated filter — narrow on purpose.
// Returns true iff the proposal is a recovery-agent-emitted decision in
// proposed status with at least one affected req to target AND its Kind
// is in the auto-acceptable set:
//
//   - PlanDecisionKindRequirementChange — refine_prompt / narrow_scope /
//     split_req recovery actions (the existing path).
//   - PlanDecisionKindStoryReprepare — story_reprepare action (Train C
//     step 4). The cascade dirty-marks Stories + scenarios; plan-manager
//     drives stories_generated → preparing_stories so Sarah re-runs with
//     the diagnosis as Story.RecoveryHint.
//
// Other kinds (execution_exhausted terminal records, qa-reviewer
// proposals, human proposals) stay human-gated. AffectedReqIDs is the
// load-bearing predicate for both auto-acceptable kinds: it scopes the
// cascade target, and an empty list signals "the wedge isn't scoped to
// specific work — needs human triage."
func shouldAutoAcceptRecovery(dec *workflow.PlanDecision) bool {
	if dec == nil {
		return false
	}
	if dec.ProposedBy != "recovery-agent" {
		return false
	}
	if dec.Status != workflow.PlanDecisionStatusProposed {
		return false
	}
	switch dec.Kind {
	case workflow.PlanDecisionKindRequirementChange,
		workflow.PlanDecisionKindStoryReprepare:
		// auto-acceptable
	default:
		return false
	}
	if len(dec.AffectedReqIDs) == 0 {
		return false
	}
	return true
}

// invokeAccept fires plan.mutation.plan_decision.accept via NATS
// request/reply. Best-effort: failure logs warn; the operator can
// still manually accept via HTTP if needed.
func (c *Component) invokeAccept(ctx context.Context, slug, proposalID string) {
	req := struct {
		Slug       string `json:"slug"`
		ProposalID string `json:"proposal_id"`
		AcceptedBy string `json:"accepted_by"`
	}{Slug: slug, ProposalID: proposalID, AcceptedBy: "auto:recovery"}

	data, err := json.Marshal(req)
	if err != nil {
		c.logger.Warn("Failed to marshal auto-accept request",
			"slug", slug, "proposal_id", proposalID, "error", err)
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	respData, err := c.natsClient.RequestWithRetry(
		reqCtx,
		recoveryAutoAcceptSubject,
		data,
		10*time.Second,
		natsclient.DefaultRetryConfig(),
	)
	if err != nil {
		c.logger.Warn("Auto-accept mutation request failed",
			"slug", slug, "proposal_id", proposalID, "error", err)
		return
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil || !resp.Success {
		// Already-accepted is the common idempotent case from replay;
		// log at info level rather than warn so it doesn't look like
		// a real failure to operators.
		level := "warn"
		if resp.Error != "" && (containsAny(resp.Error, "cannot accept in status", "already accepted")) {
			level = "info"
		}
		if level == "info" {
			c.logger.Info("Auto-accept skipped (already accepted or wrong status)",
				"slug", slug, "proposal_id", proposalID, "error", resp.Error)
		} else {
			c.logger.Warn("Auto-accept mutation rejected",
				"slug", slug, "proposal_id", proposalID,
				"error", resp.Error, "unmarshal_err", err)
		}
		return
	}
	c.logger.Info("Auto-accepted recovery PlanDecision",
		"slug", slug, "proposal_id", proposalID)
}

// containsAny reports whether s contains any of the listed needles.
// Tiny helper to keep the log-level branch in invokeAccept readable.
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if len(n) == 0 {
			continue
		}
		if indexOf(s, n) >= 0 {
			return true
		}
	}
	return false
}

// indexOf is a strings.Index without the import — keeps this file's
// dependency surface minimal.
func indexOf(s, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(s) {
		return -1
	}
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
