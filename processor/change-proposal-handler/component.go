// Package changeproposalhandler provides the change-proposal-handler component.
// It reacts to accepted ChangeProposal events by running the cascade logic
// asynchronously — isolating dirty-marking and cancellation from the HTTP handler
// that manages the proposal lifecycle.
//
// Flow:
//  1. plan-api accepts a ChangeProposal and publishes a ChangeProposalCascadeRequest.
//  2. This component consumes the request from JetStream.
//  3. It loads the proposal, runs cascade.ChangeProposal, and publishes
//     a change_proposal.accepted event to JetStream.
//  4. For each affected scenario that has a running loop, it publishes a
//     cancellation Signal to agent.signal.cancel.<loopID> via Core NATS.
package changeproposalhandler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	wf "github.com/c360studio/semspec/vocabulary/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/cancellation"
	"github.com/c360studio/semspec/workflow/cascade"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the change-proposal-handler processor.
type Component struct {
	name         string
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	repoRoot     string
	tripleWriter *graphutil.TripleWriter
	kvStore      *natsclient.KVStore

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	requestsProcessed atomic.Int64
	requestsFailed    atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new change-proposal-handler processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for unset fields.
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.AcceptedSubject == "" {
		config.AcceptedSubject = defaults.AcceptedSubject
	}
	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = defaults.TimeoutSeconds
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	const name = "change-proposal-handler"
	logger := deps.GetLogger()
	return &Component{
		name:       name,
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		repoRoot:   repoRoot,
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: name,
		},
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("initialized change-proposal-handler",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins consuming cascade trigger messages.
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

	// Initialize ENTITY_STATES KV store for workflow Manager operations.
	if entityBucket, kvErr := c.natsClient.GetKeyValueBucket(ctx, "ENTITY_STATES"); kvErr != nil {
		c.logger.Warn("ENTITY_STATES bucket not available — workflow state operations will use disk fallback",
			"error", kvErr)
	} else {
		c.kvStore = c.natsClient.NewKVStore(entityBucket)
	}

	// Push-based consumption — messages arrive via callback, no polling delay.
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       c.config.GetTimeout() + 30*time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("consume cascade triggers: %w", err)
	}

	c.logger.Info("change-proposal-handler started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.TriggerSubject)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
// Messages arrive immediately when published — no polling delay.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage processes a single cascade trigger message.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.requestsProcessed.Add(1)
	c.updateLastActivity()

	// Unwrap BaseMessage envelope to reach the ChangeProposalCascadeRequest payload.
	var baseMsg struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Error("failed to parse BaseMessage envelope", "error", err)
		_ = msg.Term()
		return
	}

	var req payloads.ChangeProposalCascadeRequest
	if err := json.Unmarshal(baseMsg.Payload, &req); err != nil {
		c.logger.Error("failed to parse ChangeProposalCascadeRequest", "error", err)
		_ = msg.Term()
		return
	}

	if err := req.Validate(); err != nil {
		c.logger.Error("invalid cascade request", "error", err)
		_ = msg.Term()
		return
	}

	c.logger.Info("handling change proposal cascade",
		"proposal_id", req.ProposalID,
		"slug", req.Slug,
		"trace_id", req.TraceID)

	cascadeCtx, cancel := context.WithTimeout(ctx, c.config.GetTimeout())
	defer cancel()

	if err := c.handleCascadeRequest(cascadeCtx, &req); err != nil {
		c.logger.Error("cascade failed",
			"proposal_id", req.ProposalID,
			"slug", req.Slug,
			"error", err)
		c.requestsFailed.Add(1)
		_ = msg.Nak()
		return
	}

	if err := msg.Ack(); err != nil {
		c.logger.Warn("failed to ACK cascade message", "error", err)
	}

	c.logger.Info("cascade complete",
		"proposal_id", req.ProposalID,
		"slug", req.Slug)
}

// handleCascadeRequest executes the cascade and publishes the accepted event.
func (c *Component) handleCascadeRequest(ctx context.Context, req *payloads.ChangeProposalCascadeRequest) error {
	manager := workflow.NewManager(c.repoRoot, c.kvStore)

	// Load all proposals for the plan and find the one we need.
	proposals, err := workflow.LoadChangeProposals(ctx, c.kvStore, req.Slug)
	if err != nil {
		return fmt.Errorf("load change proposals for slug %q: %w", req.Slug, err)
	}

	var target *workflow.ChangeProposal
	for i := range proposals {
		if proposals[i].ID == req.ProposalID {
			target = &proposals[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("proposal %q not found in slug %q", req.ProposalID, req.Slug)
	}

	// Run the cascade: dirty-mark scenarios and tasks.
	result, err := cascade.ChangeProposal(ctx, manager, req.Slug, target)
	if err != nil {
		return fmt.Errorf("cascade change proposal: %w", err)
	}

	c.logger.Info("cascade complete",
		"proposal_id", req.ProposalID,
		"affected_requirements", len(result.AffectedRequirementIDs),
		"affected_scenarios", len(result.AffectedScenarioIDs))

	// Write cascade result to graph as entity triples.
	entityID := fmt.Sprintf("%s.exec.cascade.run.%s-%s", workflow.EntityPrefix(), req.Slug, req.ProposalID)
	if err := c.tripleWriter.WriteTriple(ctx, entityID, wf.Phase, "cascaded"); err != nil {
		c.logger.Error("Failed to write cascade phase triple", "entity_id", entityID, "error", err)
	}
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Type, "cascade")
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.Slug, req.Slug)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.TraceID, req.TraceID)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.CascadeAffectedRequirements, len(result.AffectedRequirementIDs))
	_ = c.tripleWriter.WriteTriple(ctx, entityID, wf.CascadeAffectedScenarios, len(result.AffectedScenarioIDs))

	// Publish full Graphable entity to graph-ingest for relationship tracking.
	entity := NewCascadeEntity(req.ProposalID, req.Slug, req.TraceID,
		len(result.AffectedRequirementIDs), len(result.AffectedScenarioIDs)).
		WithPhase("cascaded")
	c.publishEntity(ctx, entity)

	// Publish the accepted event to JetStream so downstream consumers can react.
	if err := c.publishAcceptedEvent(ctx, req, result); err != nil {
		// Log but do not fail — the cascade itself succeeded.
		c.logger.Warn("failed to publish accepted event",
			"proposal_id", req.ProposalID,
			"error", err)
	}

	// Send Core NATS cancellation signals for any running scenario loops.
	// These are ephemeral by design — delivery is best-effort.
	for _, scenarioID := range result.AffectedScenarioIDs {
		c.publishCancellationSignal(ctx, scenarioID, req.ProposalID)
	}

	return nil
}

// publishAcceptedEvent publishes a change_proposal.accepted event to JetStream.
func (c *Component) publishAcceptedEvent(ctx context.Context, req *payloads.ChangeProposalCascadeRequest, result *cascade.Result) error {
	evt := &payloads.ChangeProposalAcceptedEvent{
		ProposalID:             req.ProposalID,
		Slug:                   req.Slug,
		TraceID:                req.TraceID,
		AffectedRequirementIDs: result.AffectedRequirementIDs,
		AffectedScenarioIDs:    result.AffectedScenarioIDs,
	}

	baseMsg := message.NewBaseMessage(evt.Schema(), evt, "change-proposal-handler")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal accepted event: %w", err)
	}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	if _, err := js.Publish(ctx, c.config.AcceptedSubject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", c.config.AcceptedSubject, err)
	}

	return nil
}

// publishCancellationSignal sends a Core NATS cancellation signal to any loop
// running for the given scenarioID. The scenarioID is used as the loopID because
// scenario-execution-loop IDs are derived from scenario IDs (best-effort delivery).
func (c *Component) publishCancellationSignal(ctx context.Context, scenarioID, proposalID string) {
	sig := &cancellation.Signal{
		LoopID: scenarioID,
		Reason: fmt.Sprintf("change proposal %s accepted; scenario re-queued for execution", proposalID),
	}

	baseMsg := message.NewBaseMessage(sig.Schema(), sig, "change-proposal-handler")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Warn("failed to marshal cancellation signal",
			"scenario_id", scenarioID,
			"error", err)
		return
	}

	subject := fmt.Sprintf("agent.signal.cancel.%s", scenarioID)
	if err := c.natsClient.Publish(ctx, subject, data); err != nil {
		c.logger.Warn("failed to publish cancellation signal",
			"scenario_id", scenarioID,
			"subject", subject,
			"error", err)
	} else {
		c.logger.Debug("published cancellation signal",
			"scenario_id", scenarioID,
			"subject", subject)
	}
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.logger.Info("change-proposal-handler stopped",
		"requests_processed", c.requestsProcessed.Load(),
		"requests_failed", c.requestsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "change-proposal-handler",
		Type:        "processor",
		Description: "Handles accepted ChangeProposal cascade: dirty-marks affected scenarios and tasks, cancels running scenario loops",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return handlerSchema
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
		ErrorCount: int(c.requestsFailed.Load()),
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

// IsRunning returns whether the component is currently running.
func (c *Component) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
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
