package planmanager

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// watchExecutionCompletions watches the EXECUTION_STATES KV bucket for
// requirement execution entries reaching terminal state. When all requirements
// for a plan reach terminal state, the plan transitions:
//   - all completed → StatusComplete
//   - any failed    → StatusRejected
//
// This is the MVP plan completion handler. Future: insert a reviewing_rollup
// stage here (plan-level red team writes integration tests) before completing.
func (c *Component) watchExecutionCompletions(ctx context.Context) {
	bucket, err := c.getExecBucket(ctx)
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — plan completion watcher disabled",
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

	for entry := range watcher.Updates() {
		if entry == nil {
			// End of initial replay — ignore bootstrap entries.
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		c.handleRequirementStateChange(ctx, bucket, entry)
	}
}

// handleRequirementStateChange processes a single EXECUTION_STATES KV update
// for a requirement entry (key: req.<slug>.<reqID>).
func (c *Component) handleRequirementStateChange(ctx context.Context, bucket jetstream.KeyValue, entry jetstream.KeyValueEntry) {
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

	// All requirements are terminal — transition the plan.
	if failedCount > 0 {
		c.logger.Info("All requirements terminal, some failed — rejecting plan",
			"slug", slug,
			"completed", completedCount,
			"failed", failedCount)
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusRejected); err != nil {
			c.logger.Error("Failed to reject plan", "slug", slug, "error", err)
		}
	} else {
		c.logger.Info("All requirements completed — completing plan",
			"slug", slug,
			"completed", completedCount)
		if err := c.setPlanStatusCached(ctx, plan, workflow.StatusComplete); err != nil {
			c.logger.Error("Failed to complete plan", "slug", slug, "error", err)
		}
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
