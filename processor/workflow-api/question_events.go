package workflowapi

import (
	"context"
	"encoding/json"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// handleQuestionUpdates watches the QUESTIONS KV bucket for mutations and publishes
// question entities to the graph. This catches creation, answer, timeout, and escalation
// all in one place without wiring into each individual code path.
func (c *Component) handleQuestionUpdates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := js.KeyValue(ctx, workflow.QuestionsBucket)
	if err != nil {
		c.logger.Warn("QUESTIONS bucket not found, question graph publishing disabled",
			"error", err)
		return
	}

	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch QUESTIONS bucket, question graph publishing disabled",
			"error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Question graph publisher started")

	for entry := range watcher.Updates() {
		// nil entry signals end of initial values replay
		if entry == nil {
			continue
		}

		// Skip deletes/purges
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}

		var q workflow.Question
		if err := json.Unmarshal(entry.Value(), &q); err != nil {
			c.logger.Warn("Failed to unmarshal question from KV",
				"key", entry.Key(),
				"error", err)
			continue
		}

		if err := c.publishQuestionEntity(ctx, &q); err != nil {
			c.logger.Warn("Failed to publish question entity to graph",
				"question_id", q.ID,
				"error", err)
			// Best-effort: don't affect question lifecycle
		}
	}
}
