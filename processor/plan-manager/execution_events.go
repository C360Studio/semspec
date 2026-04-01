package planmanager

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/nats-io/nats.go/jetstream"
)

// watchExecutionCompletions watches the EXECUTION_STATES KV bucket for
// requirement execution entries reaching terminal state. When all requirements
// for a plan reach terminal state, the plan transitions:
//   - all completed → StatusReviewingRollup (or StatusComplete as fallback)
//   - any failed    → stays in StatusImplementing (user decides: retry/complete/reject)
//
// On startup, the KV watcher replays all historical entries before a nil
// sentinel. Plan-level transitions are deferred until after replay completes
// to prevent stale pre-crash terminal entries from incorrectly killing plans.
func (c *Component) watchExecutionCompletions(ctx context.Context) {
	// Retry bucket acquisition — execution-manager may create the bucket
	// after plan-manager starts. Without retry, the watcher is permanently
	// disabled and plans never transition from implementing → complete.
	bucket, err := retry.DoWithResult(ctx, retry.Quick(), func() (jetstream.KeyValue, error) {
		return c.getExecBucket(ctx)
	})
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available after retries — plan completion watcher disabled",
			"error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "req.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES req entries — plan completion watcher disabled",
			"error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Plan completion watcher started (watching EXECUTION_STATES req.>)")

	replayDone := false
	for entry := range watcher.Updates() {
		if entry == nil {
			// End of initial KV replay. All historical entries have been
			// delivered; subsequent entries are live updates.
			replayDone = true
			c.logger.Info("EXECUTION_STATES replay complete, checking convergence")
			c.checkPostReplayConvergence(ctx, bucket)
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		c.handleRequirementStateChange(ctx, bucket, entry, replayDone)
	}
}

// handleRequirementStateChange processes a single EXECUTION_STATES KV update
// for a requirement entry (key: req.<slug>.<reqID>).
// During initial KV replay (replayDone=false), terminal entries are logged but
// do not trigger plan-level state transitions — those are deferred to
// checkPostReplayConvergence after the replay completes.
func (c *Component) handleRequirementStateChange(ctx context.Context, bucket jetstream.KeyValue, entry jetstream.KeyValueEntry, replayDone bool) {
	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
		c.logger.Debug("Failed to unmarshal requirement execution", "key", entry.Key(), "error", err)
		return
	}

	// Only act on terminal stages.
	if !isTerminalStage(reqExec.Stage) {
		return
	}

	slug := reqExec.Slug
	if slug == "" {
		// Parse slug from key: req.<slug>.<reqID>
		slug = slugFromKey(entry.Key())
	}
	if slug == "" {
		return
	}

	// During initial KV replay, log terminal entries but skip plan-level
	// transitions. checkPostReplayConvergence handles catch-up after replay.
	if !replayDone {
		c.logger.Debug("Replay: skipping plan transition for terminal requirement",
			"slug", slug, "key", entry.Key(), "stage", reqExec.Stage)
		return
	}

	c.checkPlanConvergence(ctx, bucket, slug)
}

// checkPlanConvergence evaluates whether a plan's requirements have all reached
// terminal state and takes the appropriate action. Called both from live KV
// updates and from post-replay convergence checks.
//
// Three outcomes:
//   - Not all terminal: log progress, return (no transition)
//   - All terminal, none failed: transition to reviewing_rollup
//   - All terminal, some failed: stay in implementing, log stall for user action
func (c *Component) checkPlanConvergence(ctx context.Context, bucket jetstream.KeyValue, slug string) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		return
	}

	plan, ok := ps.get(slug)
	if !ok {
		return
	}

	// Only transition from implementing.
	if plan.EffectiveStatus() != workflow.StatusImplementing {
		return
	}

	// Count terminal requirements by scanning the bucket.
	totalRequired := len(plan.Requirements)
	if totalRequired == 0 {
		return
	}

	completedCount, failedCount, err := c.countTerminalRequirements(ctx, bucket, slug)
	if err != nil {
		c.logger.Warn("Failed to count terminal requirements", "slug", slug, "error", err)
		return
	}

	terminalCount := completedCount + failedCount
	if terminalCount < totalRequired {
		c.logger.Debug("Requirements still in progress",
			"slug", slug,
			"completed", completedCount,
			"failed", failedCount,
			"total", totalRequired)
		return
	}

	// All requirements are terminal.
	if failedCount == 0 {
		c.logger.Info("All requirements completed — transitioning to rollup review",
			"slug", slug,
			"completed", completedCount)
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusReviewingRollup); err != nil {
			// Fall back to direct complete if rollup transition fails
			// (e.g., reviewing_rollup not configured).
			c.logger.Warn("Rollup transition failed, completing directly",
				"slug", slug, "error", err)
			if err := c.setPlanStatusCached(ctx, plan, workflow.StatusComplete); err != nil {
				c.logger.Error("Failed to complete plan", "slug", slug, "error", err)
			}
		}
		return
	}

	// Some requirements failed — don't auto-reject.
	// Stay in implementing and let the user decide: retry, complete partial, or reject.
	c.logger.Info("Requirements finished with failures — awaiting user decision",
		"slug", slug,
		"completed", completedCount,
		"failed", failedCount,
		"total", totalRequired)
}

// checkPostReplayConvergence runs after the initial EXECUTION_STATES KV replay
// completes. It checks all plans in StatusImplementing for convergence,
// catching the case where a plan legitimately completed before a crash but
// the status transition was never persisted.
func (c *Component) checkPostReplayConvergence(ctx context.Context, bucket jetstream.KeyValue) {
	c.mu.RLock()
	ps := c.plans
	c.mu.RUnlock()
	if ps == nil {
		return
	}

	for _, plan := range ps.list() {
		if plan.EffectiveStatus() != workflow.StatusImplementing {
			continue
		}
		c.checkPlanConvergence(ctx, bucket, plan.Slug)
	}
}

// countTerminalRequirements scans EXECUTION_STATES for all req.<slug>.* keys
// and counts entries in terminal stages.
func (c *Component) countTerminalRequirements(ctx context.Context, bucket jetstream.KeyValue, slug string) (completed, failed int, err error) {
	prefix := "req." + slug + "."
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		return 0, 0, err
	}

	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}
		var reqExec workflow.RequirementExecution
		if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
			continue
		}
		switch reqExec.Stage {
		case "completed":
			completed++
		case "failed", "error":
			failed++
		}
	}
	return completed, failed, nil
}

// isTerminalStage returns true for requirement execution stages that indicate
// the requirement will not progress further.
func isTerminalStage(stage string) bool {
	return stage == "completed" || stage == "failed" || stage == "error"
}

// slugFromKey extracts the plan slug from a KV key formatted as req.<slug>.<reqID>.
func slugFromKey(key string) string {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}
