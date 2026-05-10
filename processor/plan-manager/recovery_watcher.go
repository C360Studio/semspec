package planmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// recoveryCompleteConsumerName is the durable consumer name plan-manager
// uses to subscribe to RecoveryComplete events. Distinct from execution-
// manager's consumer so both components receive every message even though
// they filter to non-overlapping subsets in the handler (plan-phase
// recoveries have RequirementID="").
const recoveryCompleteConsumerName = "plan-manager-recovery"

// startRecoveryCompleteWatcher subscribes to recovery.complete.> events
// and reacts to plan-phase recoveries (RequirementID==""). Execution-phase
// recoveries are owned by execution-manager's own watcher.
//
// Best-effort: a subscription error logs but does not block component
// startup — recovery is observability/diagnosis-surface in stage 1, not
// a hard dependency for the rest of plan-manager's work.
func (c *Component) startRecoveryCompleteWatcher(ctx context.Context) {
	if c.natsClient == nil {
		return
	}
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  recoveryCompleteConsumerName,
		FilterSubject: payloads.RecoveryCompleteSubjectPrefix + ">",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, c.handleRecoveryCompletePush); err != nil {
		c.logger.Warn("plan-manager recovery-complete watcher failed to start",
			"subject", cfg.FilterSubject, "error", err)
		return
	}
	c.logger.Info("plan-manager recovery-complete watcher started",
		"subject", cfg.FilterSubject)
}

// handleRecoveryCompletePush is the push callback for ConsumeStreamWithConfig.
func (c *Component) handleRecoveryCompletePush(ctx context.Context, msg jetstream.Msg) {
	c.handleRecoveryComplete(ctx, msg)
}

// handleRecoveryComplete decodes a RecoveryComplete event, filters out
// execution-phase recoveries (owned by execution-manager), and writes the
// diagnosis as triples on the wedged plan entity. Stage 1 is observability-
// shaped: the diagnosis surfaces via the SKG and logs; automatic re-
// dispatch on refine_prompt is deferred to stage 2 when the coordinator
// landings give us the rollback semantics to do it safely.
func (c *Component) handleRecoveryComplete(ctx context.Context, msg jetstream.Msg) {
	defer func() {
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK RecoveryComplete", "error", err)
		}
	}()

	if c.decoder == nil {
		c.logger.Warn("RecoveryComplete decoder not initialised; payload registry missing")
		return
	}
	base, err := c.decoder.Decode(msg.Data())
	if err != nil {
		c.logger.Warn("Failed to decode RecoveryComplete envelope", "error", err)
		return
	}
	complete, ok := base.Payload().(*payloads.RecoveryComplete)
	if !ok {
		c.logger.Warn("Unexpected RecoveryComplete payload type",
			"type", fmt.Sprintf("%T", base.Payload()))
		return
	}

	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	plan, found := ps.get(complete.Slug)
	if !found {
		// No plan: either a foreign event for a slug we don't own, or the
		// plan was purged. Ack-and-skip — the recovery record is durable
		// in RECOVERY_STATES regardless.
		c.logger.Debug("RecoveryComplete for unknown plan; skipping",
			"slug", complete.Slug, "recovery_id", complete.RecoveryID)
		return
	}

	// Plan-phase recoveries only. plan-manager escalates revision-loop
	// exhaustion to StatusRejected; that's the wedge shape this watcher
	// reconciles. Execution-phase events arrive on the same subject but
	// the plan stays mid-flight (StatusImplementing etc.) and execution-
	// manager's watcher handles them.
	if plan.EffectiveStatus() != workflow.StatusRejected {
		c.logger.Debug("RecoveryComplete for non-rejected plan; skipping (execution-manager handles this case)",
			"slug", complete.Slug, "recovery_id", complete.RecoveryID, "status", plan.EffectiveStatus())
		return
	}

	// Write diagnosis triples onto the plan entity so the SKG surfaces the
	// recovery decision. The plan stays StatusRejected; stage 2 will wire
	// programmatic re-dispatch when coordinator-layer recovery lands.
	c.writeRecoveryTriples(ctx, complete)

	c.logger.Info("Recovery decision recorded for plan",
		"slug", complete.Slug,
		"recovery_id", complete.RecoveryID,
		"action", complete.Action,
		"recovery_succeeded", complete.RecoverySucceeded,
		"diagnosis", complete.Diagnosis)
}

// writeRecoveryTriples writes the recovery action + diagnosis to the
// graph as triples on the plan entity. Best-effort — a write failure
// logs at warn but does not break the watcher.
func (c *Component) writeRecoveryTriples(ctx context.Context, complete *payloads.RecoveryComplete) {
	c.mu.RLock()
	tw := c.tripleWriter
	c.mu.RUnlock()
	if tw == nil {
		return
	}
	entityID := fmt.Sprintf("plan.%s", complete.Slug)
	triples := map[string]string{
		wf.RecoveryID:        complete.RecoveryID,
		wf.RecoveryAction:    string(complete.Action),
		wf.RecoveryDiagnosis: complete.Diagnosis,
	}
	for pred, val := range triples {
		if val == "" {
			continue
		}
		if err := tw.WriteTriple(ctx, entityID, pred, val); err != nil {
			c.logger.Warn("Failed to write recovery triple",
				"entity", entityID, "predicate", pred, "error", err)
		}
	}
}

