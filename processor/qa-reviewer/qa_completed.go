package qareviewer

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

// qaCompletedConsumerName is the durable JetStream consumer name. One consumer
// shared across restarts so pending events redeliver after a crash.
const qaCompletedConsumerName = "qa-reviewer-qa-completed"

// startQACompletedConsumer subscribes qa-reviewer to QACompletedEvent. The
// consumer is durable so events redeliver on restart — which removes the
// core-NATS race where qa-reviewer previously missed the event by subscribing
// after the executor published. On each event it loads the plan, validates
// state, and calls processReview which publishes plan.mutation.qa.start to
// claim the plan for review.
//
// The subscription-scoped ctx is captured in the handler closure and used as
// the parent for spawned goroutines. This keeps long-running work (mutation
// request + LLM dispatch) tied to the component lifecycle (canceled by Stop)
// rather than the per-message handler context (canceled on handler return).
func (c *Component) startQACompletedConsumer(ctx context.Context) error {
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    "WORKFLOW",
		ConsumerName:  qaCompletedConsumerName,
		FilterSubject: "workflow.events.qa.completed",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		// Matches the work here: load plan + publish mutation + dispatch LLM.
		// LLM dispatch itself is non-blocking (fire-and-forget to agent queue).
		AckWait: 30 * time.Second,
	}
	handler := func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleQACompleted(ctx, msgCtx, msg)
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, handler); err != nil {
		return fmt.Errorf("consume qa-completed events: %w", err)
	}
	c.logger.Info("qa-completed consumer started",
		"stream", cfg.StreamName, "consumer", cfg.ConsumerName)
	return nil
}

// handleQACompleted processes one QACompletedEvent.
//
// lifecycleCtx is the subscription-scoped context (canceled by Stop) used for
// spawned goroutines that outlive this callback. msgCtx is the per-message
// handler context (canceled on return) used for the synchronous KV read and
// ack/nak so a stopping consumer doesn't hang on a dying broker.
func (c *Component) handleQACompleted(lifecycleCtx, msgCtx context.Context, msg jetstream.Msg) {
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
		c.logger.Error("Failed to parse QACompleted BaseMessage envelope", "error", err)
		_ = msg.Term()
		return
	}

	var evt workflow.QACompletedEvent
	if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
		c.logger.Error("Failed to parse QACompletedEvent payload", "error", err)
		_ = msg.Term()
		return
	}

	p := &payloads.QACompletedPayload{QACompletedEvent: evt}
	if err := p.Validate(); err != nil {
		c.logger.Error("Invalid QACompletedEvent", "slug", evt.Slug, "error", err)
		_ = msg.Term()
		return
	}

	plan, err := c.loadPlanFromKV(msgCtx, evt.Slug)
	if err != nil {
		c.logger.Warn("QACompletedEvent for unknown plan — dropping",
			"slug", evt.Slug, "run_id", evt.RunID, "error", err)
		_ = msg.Ack()
		return
	}

	current := plan.EffectiveStatus()
	if current != workflow.StatusReadyForQA {
		c.logger.Info("QACompletedEvent for plan not in ready_for_qa — dropping",
			"slug", evt.Slug, "current", current, "run_id", evt.RunID)
		_ = msg.Ack()
		return
	}

	qaRun := &workflow.QARun{
		RunID:       evt.RunID,
		Passed:      evt.Passed,
		Failures:    evt.Failures,
		Artifacts:   evt.Artifacts,
		DurationMs:  evt.DurationMs,
		RunnerError: evt.RunnerError,
		TraceID:     evt.TraceID,
		CompletedAt: time.Now(),
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parent the goroutine on the subscription-scoped context so Stop() cancels
	// it; msgCtx would expire as soon as this handler returns.
	go c.processReview(lifecycleCtx, plan, qaRun)
	_ = msg.Ack()
}
