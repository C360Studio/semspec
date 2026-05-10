package executionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// recoveryCompleteConsumerName is the durable consumer name execution-
// manager uses to subscribe to RecoveryComplete events. Distinct from
// plan-manager's consumer so both components receive every message even
// though they filter to non-overlapping subsets in the handler
// (execution-phase recoveries are scoped by a non-empty requirement_id).
const recoveryCompleteConsumerName = "execution-manager-recovery"

// startRecoveryCompleteWatcher subscribes to recovery.complete.> events
// and reacts to execution-phase recoveries — those whose underlying
// RecoveryRequested carried a task_id (i.e. the recovery was triggered
// by a markEscalatedLocked path). Plan-phase recoveries are owned by
// plan-manager's watcher.
//
// Best-effort: subscription failure is logged at warn but does not
// block component startup. Recovery is observability/diagnosis-surface
// in stage 1, not a hard dependency.
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
		c.logger.Warn("execution-manager recovery-complete watcher failed to start",
			"subject", cfg.FilterSubject, "error", err)
		return
	}
	c.logger.Info("execution-manager recovery-complete watcher started",
		"subject", cfg.FilterSubject)
}

// handleRecoveryCompletePush is the push callback for ConsumeStreamWithConfig.
func (c *Component) handleRecoveryCompletePush(ctx context.Context, msg jetstream.Msg) {
	c.handleRecoveryComplete(ctx, msg)
}

// handleRecoveryComplete decodes a RecoveryComplete event and writes
// the diagnosis as triples on the wedged task-execution entity.
//
// Stage 1 is observability-shaped: the diagnosis surfaces via the SKG
// and logs; automatic re-dispatch on refine_prompt requires reviving
// the discarded worktree + re-creating loop state, which is deferred
// to stage 2 alongside the coordinator wiring.
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

	// Distinguish execution-phase from plan-phase by reading the matching
	// RecoveryRequested record from RECOVERY_STATES KV. The wire
	// RecoveryComplete deliberately omits requirement_id/task_id — the
	// routing key is layer+slug — but the request shape carries them and
	// we persisted both at recovery-agent completion time.
	taskID, requirementID, ok := c.lookupRecoveryScope(ctx, complete.RecoveryID)
	if !ok {
		// No record (transient KV miss / older recovery without a record)
		// → skip silently. Recovery is best-effort here.
		return
	}
	if taskID == "" {
		// Plan-phase recovery — plan-manager's watcher handles this case.
		return
	}

	c.writeRecoveryTriples(ctx, taskID, requirementID, complete)
	c.logger.Info("Recovery decision recorded for task execution",
		"slug", complete.Slug,
		"recovery_id", complete.RecoveryID,
		"task_id", taskID,
		"requirement_id", requirementID,
		"action", complete.Action,
		"recovery_succeeded", complete.RecoverySucceeded,
		"diagnosis", complete.Diagnosis)
}

// lookupRecoveryScope reads the RECOVERY_STATES KV record for a recovery
// ID and returns the wedged (task_id, requirement_id) pair. Missing or
// unreadable records return ok=false; the caller treats that as "skip."
func (c *Component) lookupRecoveryScope(ctx context.Context, recoveryID string) (taskID, requirementID string, ok bool) {
	if c.natsClient == nil {
		return "", "", false
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		return "", "", false
	}
	kv, err := js.KeyValue(ctx, payloads.RecoveryStatesBucket)
	if err != nil {
		return "", "", false
	}
	entry, err := kv.Get(ctx, recoveryID)
	if err != nil {
		return "", "", false
	}
	var record struct {
		Requested *payloads.RecoveryRequested `json:"requested"`
	}
	if err := json.Unmarshal(entry.Value(), &record); err != nil || record.Requested == nil {
		return "", "", false
	}
	return record.Requested.TaskID, record.Requested.RequirementID, true
}

// writeRecoveryTriples writes the recovery action + diagnosis to the
// graph as triples on the task-execution entity. Mirrors plan-manager's
// writeRecoveryTriples but scoped to TaskExecutionEntityID's prefix.
func (c *Component) writeRecoveryTriples(ctx context.Context, taskID, _ string, complete *payloads.RecoveryComplete) {
	if c.tripleWriter == nil {
		return
	}
	entityID := fmt.Sprintf("task.%s.%s", complete.Slug, taskID)
	triples := map[string]string{
		wf.RecoveryID:        complete.RecoveryID,
		wf.RecoveryAction:    string(complete.Action),
		wf.RecoveryDiagnosis: complete.Diagnosis,
	}
	for pred, val := range triples {
		if val == "" {
			continue
		}
		if err := c.tripleWriter.WriteTriple(ctx, entityID, pred, val); err != nil {
			c.logger.Warn("Failed to write recovery triple",
				"entity", entityID, "predicate", pred, "error", err)
		}
	}
}
