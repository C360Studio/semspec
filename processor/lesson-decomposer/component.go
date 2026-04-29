// Package lessondecomposer implements ADR-033 Phase 2+: a JetStream
// processor that consumes reviewer-rejection signals and produces
// evidence-cited Lesson entities via the shared lessons.Writer.
//
// Phase 2a (this commit) ships the wire end-to-end: the component
// subscribes to workflow.events.lesson.decompose.requested.>, parses the
// LessonDecomposeRequested payload, and logs receipts. No trajectory
// fetch and no LLM dispatch yet — those land in Phase 2b alongside the
// decomposer persona and prompt.
//
// The Enabled config flag keeps the component switchable per-deployment
// while the decomposer's quality is validated against real-LLM runs.
// When Enabled is false, messages are acked and skipped so producers
// never block on consumer state.
package lessondecomposer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the lesson-decomposer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Lifecycle.
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics.
	requestsReceived atomic.Int64
	requestsSkipped  atomic.Int64
	parseErrors      atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// NewComponent constructs a lesson-decomposer Component from raw JSON config
// and semstreams dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.FilterSubject == "" {
		config.FilterSubject = defaults.FilterSubject
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Component{
		name:       "lesson-decomposer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
	}, nil
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized lesson-decomposer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"filter", c.config.FilterSubject,
		"enabled", c.config.Enabled,
	)
	return nil
}

// Start begins consuming LessonDecomposeRequested messages from JetStream.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("component already running")
	}
	if c.natsClient == nil {
		c.mu.Unlock()
		return fmt.Errorf("NATS client required")
	}

	c.running = true
	c.startTime = time.Now()

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.FilterSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("consume decompose triggers: %w", err)
	}

	c.logger.Info("lesson-decomposer started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.FilterSubject,
		"enabled", c.config.Enabled,
	)
	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	c.logger.Info("lesson-decomposer stopped",
		"requests_received", c.requestsReceived.Load(),
		"requests_skipped", c.requestsSkipped.Load(),
		"parse_errors", c.parseErrors.Load(),
	)
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "lesson-decomposer",
		Type:        "processor",
		Description: "ADR-033 Phase 2+: produces evidence-cited lessons from reviewer-rejection trajectories",
		Version:     "0.1.0",
	}
}

// InputPorts returns the input port definitions.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "decompose-requests",
			Direction:   component.DirectionInput,
			Required:    true,
			Description: "Reviewer-rejection signals to decompose into evidence-cited lessons",
			Config:      component.NATSPort{Subject: c.config.FilterSubject},
		},
	}
}

// OutputPorts returns the output port definitions. Phase 2a has no outputs;
// Phase 2b emits Lesson triples via lessons.Writer (graph.mutation.triple.add).
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return lessonDecomposerSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	status := "stopped"
	if running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.parseErrors.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.getLastActivity(),
	}
}

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage parses a LessonDecomposeRequested payload and logs receipt.
// Phase 2a: skeleton — Phase 2b will fetch trajectory + dispatch decomposer LLM.
func (c *Component) handleMessage(_ context.Context, msg jetstream.Msg) {
	c.requestsReceived.Add(1)
	c.updateLastActivity()

	if !c.config.Enabled {
		c.requestsSkipped.Add(1)
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK skipped message", "error", ackErr)
		}
		return
	}

	var base message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &base); err != nil {
		c.parseErrors.Add(1)
		c.logger.Error("Failed to unmarshal BaseMessage envelope", "error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK malformed message", "error", ackErr)
		}
		return
	}

	req, ok := base.Payload().(*payloads.LessonDecomposeRequested)
	if !ok {
		c.parseErrors.Add(1)
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", base.Payload()))
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK wrong-type message", "error", ackErr)
		}
		return
	}

	if err := req.Validate(); err != nil {
		c.parseErrors.Add(1)
		c.logger.Error("Invalid LessonDecomposeRequested payload", "error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid payload", "error", ackErr)
		}
		return
	}

	// Phase 2a: log and ack. Phase 2b adds trajectory fetch + LLM dispatch.
	c.logger.Info("Lesson decompose request received (Phase 2a — log only)",
		"slug", req.Slug,
		"task_id", req.TaskID,
		"requirement_id", req.RequirementID,
		"scenario_id", req.ScenarioID,
		"loop_id", req.LoopID,
		"verdict", req.Verdict,
		"source", req.Source,
	)

	if ackErr := msg.Ack(); ackErr != nil {
		c.logger.Warn("Failed to ACK processed message", "error", ackErr)
	}
}

func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}
