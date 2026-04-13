package scenarioorchestrator

// req_completion_watcher.go — KV watcher for requirement execution completions.
//
// Watches EXECUTION_STATES req.> for terminal stages and caches completion data
// for prereq context enrichment. Replaces the JetStream stream consumer that
// previously consumed RequirementExecutionCompleteEvent messages.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// watchReqCompletions watches EXECUTION_STATES for req.> entries reaching
// terminal stages and caches the results for prereq context.
func (c *Component) watchReqCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch EXECUTION_STATES for req completions: no JetStream", "error", err)
		return
	}

	bucket, err := workflow.WaitForKVBucket(ctx, js, "EXECUTION_STATES")
	if err != nil {
		c.logger.Warn("EXECUTION_STATES bucket not available — req completion watcher disabled", "error", err)
		return
	}

	watcher, err := bucket.Watch(ctx, "req.>")
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES req.> — req completion watcher disabled", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Req completion watcher started (watching EXECUTION_STATES req.>)")

	for entry := range watcher.Updates() {
		if entry == nil {
			c.logger.Info("EXECUTION_STATES req.> replay complete for scenario-orchestrator")
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		c.handleReqCompletionFromKV(entry)
	}
}

// handleReqCompletionFromKV processes a single EXECUTION_STATES req.> KV update.
// Caches terminal requirement executions for prereq context.
func (c *Component) handleReqCompletionFromKV(entry jetstream.KeyValueEntry) {
	var reqExec workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value(), &reqExec); err != nil {
		return
	}

	if !workflow.IsTerminalReqStage(reqExec.Stage) {
		return
	}

	// Only cache completed (not failed/error) for prereq context.
	if reqExec.Stage != "completed" {
		return
	}

	// Synthesize completion event from KV data — same logic as
	// reconcileCompletedRequirements.
	var filesModified []string
	var summaries []string
	for _, nr := range reqExec.NodeResults {
		filesModified = append(filesModified, nr.FilesModified...)
		if nr.Summary != "" {
			summaries = append(summaries, nr.Summary)
		}
	}

	evt := &workflow.RequirementExecutionCompleteEvent{
		Slug:          reqExec.Slug,
		RequirementID: reqExec.RequirementID,
		Title:         reqExec.Title,
		Description:   reqExec.Description,
		ProjectID:     reqExec.ProjectID,
		TraceID:       reqExec.TraceID,
		Outcome:       "completed",
		NodeCount:     reqExec.NodeCount,
		FilesModified: filesModified,
		Summary:       strings.Join(summaries, "; "),
	}

	c.completedReqs.Set(reqExec.RequirementID, evt) //nolint:errcheck
	c.logger.Debug("Cached requirement completion from KV",
		"requirement_id", reqExec.RequirementID,
		"slug", reqExec.Slug)
}
