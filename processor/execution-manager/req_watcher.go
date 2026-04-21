package executionmanager

// req_watcher.go — cross-component loop cancellation.
//
// Watches EXECUTION_STATES for req.> entries. When a parent requirement
// transitions to a terminal stage (failed/error/completed), we enumerate
// TaskExecution children owned by that requirement and cancel any that are
// still in flight:
//
//  1. Read the child's current-stage task ID (DeveloperTaskID /
//     ValidatorTaskID / ReviewerTaskID, via currentStageTaskID).
//  2. Look that task ID up in AGENT_LOOPS to find the agentic loop_id.
//  3. Publish an agentic SignalCancel to agent.signal.{loop_id} — the
//     agentic-loop processor receives it and exits its turn early.
//  4. Mark the TaskExecution as errored with reason
//     "parent_requirement_<stage>" so local state reflects the cancel.
//
// This is the "in-flight" half of cross-component cancellation. The
// parentRequirementTerminated gate covers the other half (no NEW dispatches
// after parent dies); together they stop work on orphaned requirements
// rather than burning LLM calls until the node timeout fires.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go/jetstream"
)

// signalMessage mirrors the on-the-wire shape consumed by
// agentic-loop on subject agent.signal.{loop_id}. Defined locally so
// execution-manager does not need to depend on the agentic-dispatch
// processor package for this one send-side path. Must stay in sync with
// semstreams/processor/agentic-dispatch/loop_tracker.go:SignalMessage.
type signalMessage struct {
	LoopID    string    `json:"loop_id"`
	Type      string    `json:"type"`   // pause, resume, cancel
	Reason    string    `json:"reason"` // optional reason
	Timestamp time.Time `json:"timestamp"`
}

// Validate implements message.Payload.
func (s *signalMessage) Validate() error { return nil }

// Schema implements message.Payload.
func (s *signalMessage) Schema() message.Type {
	return message.Type{
		Domain:   agentic.Domain,
		Category: agentic.CategorySignalMessage,
		Version:  agentic.SchemaVersion,
	}
}

// MarshalJSON implements json.Marshaler (required by message.Payload).
// Alias indirection avoids recursing through our own MarshalJSON.
func (s *signalMessage) MarshalJSON() ([]byte, error) {
	type alias signalMessage
	return json.Marshal((*alias)(s))
}

// UnmarshalJSON implements json.Unmarshaler (required by message.Payload).
func (s *signalMessage) UnmarshalJSON(data []byte) error {
	type alias signalMessage
	return json.Unmarshal(data, (*alias)(s))
}

// watchRequirementTermination watches EXECUTION_STATES req.> entries and
// cancels in-flight child TaskExecutions when the parent reaches a terminal
// stage. Runs until ctx is cancelled.
func (c *Component) watchRequirementTermination(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch EXECUTION_STATES for requirement termination: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "EXECUTION_STATES")
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — requirement termination watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "req.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES req.> — requirement termination watcher disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Requirement termination watcher started (watching EXECUTION_STATES req.>)")

	// Suppress cancellations during initial replay: we'd enumerate historical
	// terminal requirements and try to cancel long-gone children. Only live
	// transitions (post-sentinel) should trigger cancel fan-out.
	replayDone := false

	for entry := range watcher.Updates() {
		if entry == nil {
			replayDone = true
			c.logger.Info("EXECUTION_STATES req.> replay complete for requirement termination watcher")
			continue
		}
		if !replayDone {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleRequirementUpdate(ctx, entry)
	}
}

// handleRequirementUpdate inspects a single req.> KV entry. If the stage is
// terminal, fan out cancellation to in-flight child task executions.
func (c *Component) handleRequirementUpdate(ctx context.Context, entry jetstream.KeyValueEntry) {
	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
		return
	}
	if !workflow.IsTerminalReqStage(reqExec.Stage) {
		return
	}
	// "completed" is benign — its children successfully merged and are already
	// terminal. Skip to avoid pointless AGENT_LOOPS scans on the happy path.
	if reqExec.Stage == "completed" {
		return
	}

	reason := fmt.Sprintf("parent_requirement_%s", reqExec.Stage)
	c.cancelChildrenForRequirement(ctx, reqExec.Slug, reqExec.RequirementID, reason)
}

// cancelChildrenForRequirement walks activeExecs for TaskExecutions whose
// RequirementID matches, signals SignalCancel to their in-flight loop (if
// any), and marks the execution errored.
func (c *Component) cancelChildrenForRequirement(ctx context.Context, slug, requirementID, reason string) {
	if requirementID == "" {
		return
	}

	var cancelled, markedErrored int
	for _, entityID := range c.activeExecs.Keys() {
		exec, ok := c.activeExecs.Get(entityID)
		if !ok {
			continue
		}

		exec.mu.Lock()
		if exec.terminated {
			exec.mu.Unlock()
			continue
		}
		if exec.RequirementID != requirementID || exec.Slug != slug {
			exec.mu.Unlock()
			continue
		}

		// Publish SignalCancel to the active stage's loop, if one is running.
		if taskID := c.currentStageTaskID(exec); taskID != "" {
			if loopID, ok := c.findLoopIDForTask(ctx, taskID); ok {
				if err := c.publishCancelSignal(ctx, loopID, reason); err != nil {
					c.logger.Warn("Failed to publish cancel signal",
						"loop_id", loopID, "task_id", taskID, "error", err)
				} else {
					cancelled++
					c.logger.Info("Cancelled in-flight loop for orphan task",
						"loop_id", loopID, "task_id", taskID,
						"slug", slug, "requirement_id", requirementID, "reason", reason)
				}
			}
		}

		c.markErrorLocked(ctx, exec, reason)
		markedErrored++
		exec.mu.Unlock()
	}

	if markedErrored > 0 {
		c.logger.Info("Requirement termination cascade complete",
			"slug", slug, "requirement_id", requirementID,
			"loops_cancelled", cancelled, "tasks_errored", markedErrored, "reason", reason)
	}
}

// findLoopIDForTask scans AGENT_LOOPS for the loop entity whose TaskID
// matches the given taskID and returns its loop_id. Returns ok=false when
// the bucket is unavailable, the scan fails, no match exists, or the
// matching loop is already terminal (no point cancelling a done loop).
//
// This is O(n) over AGENT_LOOPS. For semspec's workload (tens of concurrent
// loops, cancellation fan-out runs only on requirement termination) that
// cost is acceptable. A future optimization could maintain a reverse index
// taskID→loopID during handleLoopEntityUpdate.
func (c *Component) findLoopIDForTask(ctx context.Context, taskID string) (string, bool) {
	if c.natsClient == nil {
		return "", false
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		return "", false
	}
	bucket, err := js.KeyValue(ctx, "AGENT_LOOPS")
	if err != nil {
		return "", false
	}
	keys, err := bucket.Keys(ctx)
	if err != nil {
		return "", false
	}
	for _, k := range keys {
		entry, err := bucket.Get(ctx, k)
		if err != nil {
			continue
		}
		var loop agentic.LoopEntity
		if err := json.Unmarshal(entry.Value(), &loop); err != nil {
			continue
		}
		if loop.TaskID != taskID {
			continue
		}
		if loop.State.IsTerminal() {
			return "", false // no point signalling
		}
		return loop.ID, true
	}
	return "", false
}

// publishCancelSignal publishes SignalCancel for a loop_id on
// agent.signal.{loop_id}. The agentic-loop processor consumes this and
// exits its current turn early with outcome=cancelled.
func (c *Component) publishCancelSignal(ctx context.Context, loopID, reason string) error {
	if c.natsClient == nil {
		return fmt.Errorf("nats client unavailable")
	}
	payload := &signalMessage{
		LoopID:    loopID,
		Type:      agentic.SignalCancel,
		Reason:    reason,
		Timestamp: time.Now(),
	}
	env := message.NewBaseMessage(payload.Schema(), payload, componentName)
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal signal: %w", err)
	}
	subject := "agent.signal." + loopID
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}
