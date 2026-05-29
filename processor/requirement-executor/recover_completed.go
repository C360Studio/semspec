package requirementexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// loadTerminalReqExecFromKV fetches a terminal-state requirement execution
// for the given slug + requirement ID directly via the deterministic KV
// key. Used by handlePlanDecisionAccepted when findAwaitingByRequirement
// returns nil — covers the QA-rejection wedge shape where the
// requirement's per-req gates (dev + reviewer) approved but the plan-level
// QA verdict rejected.
//
// Returns nil + nil when no matching entry exists or stage is non-
// terminal. Returns nil + error only on KV transport failure so the
// caller can log and skip; non-fatal because the broader chain has other
// failure surfaces (timeout fallbacks in plan-decision-handler).
//
// The returned *requirementExecution is rebuilt via rebuildExecFromKV — it
// carries DAG, node index, retry counters, branch name, and the storeKey
// the resume path needs. Caller MUST insert it into c.activeExecs before
// calling resumeFromRecoveryLocked; the resume path expects the exec to
// be cache-resident so the dispatched decomposer's completion callback
// can route back.
func (c *Component) loadTerminalReqExecFromKV(ctx context.Context, slug, requirementID string) (*requirementExecution, error) {
	if c.natsClient == nil {
		return nil, nil
	}

	bucket, err := c.natsClient.GetKeyValueBucket(ctx, "EXECUTION_STATES")
	if err != nil {
		return nil, fmt.Errorf("get EXECUTION_STATES bucket: %w", err)
	}
	kvStore := c.natsClient.NewKVStore(bucket)

	key := workflow.RequirementExecutionKey(slug, requirementID)
	entry, err := kvStore.Get(ctx, key)
	if err != nil {
		// Distinguish "no entry" from "transport failure". natsclient maps the
		// NATS KV not-found path to a wrapped jetstream.ErrKeyNotFound; treat
		// any not-found-shaped error as a soft miss.
		if isKVNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}

	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value, &reqExec); err != nil {
		c.logger.Debug("Skipping corrupt EXECUTION_STATES entry during QA-recovery load",
			"key", key, "error", err)
		return nil, nil
	}
	if !workflow.IsTerminalReqStage(reqExec.Stage) {
		// Caller already tried the awaiting path. If we're here and the KV
		// entry is mid-flight, the activeExecs cache lookup just hadn't
		// landed yet — better to no-op than race the lifecycle.
		return nil, nil
	}
	return c.rebuildExecFromKV(key, &reqExec), nil
}

// countAcceptedRecoveryCyclesForReq returns how many recovery-agent
// PlanDecisions have already been accepted for this requirement on this
// plan. Used as a defense-in-depth budget gate for the QA-recovery path —
// the per-exec recoveryRestarts counter resets on every KV reload (it's
// not persisted in workflow.RequirementExecution), so without this check
// a thrashing QA verdict (needs_changes → recover → still flaky → repeat)
// could loop indefinitely.
//
// Returns 0 on any load/parse failure so a transient KV error doesn't
// false-fail the recovery — the outer Playwright/operator budget is the
// last-line guard.
func (c *Component) countAcceptedRecoveryCyclesForReq(ctx context.Context, slug, requirementID string) int {
	if c.natsClient == nil {
		return 0
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Debug("QA-recovery budget: jetstream unavailable; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	bucket, err := js.KeyValue(ctx, "PLAN_STATES")
	if err != nil {
		c.logger.Debug("QA-recovery budget: PLAN_STATES bucket unavailable; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		c.logger.Debug("QA-recovery budget: plan not in PLAN_STATES; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	var plan struct {
		PlanDecisions []workflow.PlanDecision `json:"plan_decisions"`
	}
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		c.logger.Debug("QA-recovery budget: plan JSON unmarshal failed; allowing resume",
			"slug", slug, "requirement_id", requirementID, "error", err)
		return 0
	}
	count := 0
	for _, dec := range plan.PlanDecisions {
		if dec.ProposedBy != "recovery-agent" {
			continue
		}
		if dec.Status != workflow.PlanDecisionStatusAccepted {
			continue
		}
		for _, affected := range dec.AffectedReqIDs {
			if affected == requirementID {
				count++
				break
			}
		}
	}
	return count
}

// resumeTerminalForRecoveryLocked transitions a terminal requirement
// execution back through the recovery flow. Mirrors the awaiting_recovery
// shape: marks the exec awaiting + records the proposal context, then
// defers to the existing resumeFromRecoveryLocked which does the actual
// DAG reset, branch reset, and decomposer dispatch.
//
// The caller must hold exec.mu and have inserted exec into c.activeExecs.
// Splitting this out keeps the new code path's diff small and makes the
// "state mark before resume" the only NEW behavior — the resume logic
// itself is unchanged.
func (c *Component) resumeTerminalForRecoveryLocked(ctx context.Context, exec *requirementExecution, proposalID string) {
	exec.awaitingRecovery = true
	exec.recoveryReason = fmt.Sprintf("QA-recovery: completed req re-dispatched (proposal %s)", proposalID)
	c.logger.Info("Resuming completed requirement from QA-recovery",
		"entity_id", exec.EntityID,
		"slug", exec.Slug,
		"requirement_id", exec.RequirementID,
		"prior_stage", "terminal",
		"proposal_id", proposalID,
	)
	c.resumeFromRecoveryLocked(ctx, exec)
}

// isKVNotFound returns true when err is the soft "no entry" shape returned
// by NATS KV Get. Mirrors the existing pattern in question-manager
// (component.go:258, 310): accept both the jetstream sentinel and the
// string-flattened variant some wrappers emit.
func isKVNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return true
	}
	return strings.Contains(err.Error(), "key not found")
}
