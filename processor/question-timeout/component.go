// Package questiontimeout provides a processor that monitors question SLAs
// and triggers escalation when questions are not answered in time.
package questiontimeout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// Component implements the question-timeout processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	registry      *answerer.Registry
	questionStore *workflow.QuestionStore

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	checksPerformed      atomic.Int64
	timeoutsDetected     atomic.Int64
	escalationsTriggered atomic.Int64
	lastCheckMu          sync.RWMutex
	lastCheck            time.Time
}

// NewComponent creates a new question-timeout processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.CheckInterval == 0 {
		config.CheckInterval = defaults.CheckInterval
	}
	if config.DefaultSLA == 0 {
		config.DefaultSLA = defaults.DefaultSLA
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create question store
	store, err := workflow.NewQuestionStore(deps.NATSClient)
	if err != nil {
		return nil, fmt.Errorf("create question store: %w", err)
	}

	// Load answerer registry
	registry := answerer.NewRegistry()
	if config.AnswererConfigPath != "" {
		loaded, err := answerer.LoadRegistryFromDir(config.AnswererConfigPath)
		if err != nil {
			deps.GetLogger().Warn("Failed to load answerer registry, using defaults",
				"path", config.AnswererConfigPath,
				"error", err)
		} else {
			registry = loaded
		}
	} else {
		// Try default path
		repoPath := os.Getenv("SEMSPEC_REPO_PATH")
		if repoPath == "" {
			repoPath, _ = os.Getwd()
		}
		if repoPath != "" {
			loaded, err := answerer.LoadRegistryFromDir(repoPath)
			if err == nil {
				registry = loaded
			}
		}
	}

	return &Component{
		name:          "question-timeout",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        deps.GetLogger(),
		registry:      registry,
		questionStore: store,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized question-timeout",
		"check_interval", c.config.CheckInterval,
		"default_sla", c.config.DefaultSLA)
	return nil
}

// Start begins monitoring question timeouts.
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

	// Start the check loop
	go c.checkLoop(subCtx)

	c.logger.Info("question-timeout started",
		"check_interval", c.config.CheckInterval,
		"default_sla", c.config.DefaultSLA)

	return nil
}

// checkLoop periodically checks for timed-out questions.
func (c *Component) checkLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	// Run immediately on start
	c.checkTimeouts(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkTimeouts(ctx)
		}
	}
}

// checkTimeouts finds overdue questions and escalates them.
func (c *Component) checkTimeouts(ctx context.Context) {
	c.checksPerformed.Add(1)
	c.updateLastCheck()

	// Get all pending questions
	questions, err := c.questionStore.List(ctx, workflow.QuestionStatusPending)
	if err != nil {
		c.logger.Error("Failed to list pending questions", "error", err)
		return
	}

	c.logger.Debug("Checking for timeouts", "pending_questions", len(questions))

	for _, q := range questions {
		if err := c.checkQuestion(ctx, q); err != nil {
			c.logger.Warn("Failed to check question timeout",
				"question_id", q.ID,
				"error", err)
		}
	}
}

// checkQuestion checks if a single question has exceeded its SLA.
func (c *Component) checkQuestion(ctx context.Context, q *workflow.Question) error {
	// Get the route for this question's topic
	route := c.registry.Match(q.Topic)

	// Determine the SLA
	sla := c.config.DefaultSLA
	if route.SLA.Duration() > 0 {
		sla = route.SLA.Duration()
	}

	// Check if SLA exceeded
	age := time.Since(q.CreatedAt)
	if age <= sla {
		return nil // Not yet timed out
	}

	c.timeoutsDetected.Add(1)

	c.logger.Info("Question SLA exceeded",
		"question_id", q.ID,
		"topic", q.Topic,
		"age", age,
		"sla", sla)

	// Publish timeout event
	if err := c.publishTimeoutEvent(ctx, q, age, sla); err != nil {
		c.logger.Warn("Failed to publish timeout event",
			"question_id", q.ID,
			"error", err)
	}

	// Check if escalation is configured
	if route.EscalateTo != "" {
		if err := c.escalateQuestion(ctx, q, route); err != nil {
			return fmt.Errorf("escalate question: %w", err)
		}
	}

	return nil
}

// publishTimeoutEvent publishes a timeout event for a question.
func (c *Component) publishTimeoutEvent(ctx context.Context, q *workflow.Question, age, sla time.Duration) error {
	event := TimeoutEvent{
		QuestionID: q.ID,
		Topic:      q.Topic,
		Age:        age,
		SLA:        sla,
		Timestamp:  time.Now(),
	}

	baseMsg := message.NewBaseMessage(TimeoutEventType, &event, "question-timeout")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	subject := fmt.Sprintf("question.timeout.%s", q.ID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}

	return nil
}

// escalateQuestion escalates a question to the next answerer.
func (c *Component) escalateQuestion(ctx context.Context, q *workflow.Question, route *answerer.Route) error {
	c.escalationsTriggered.Add(1)

	c.logger.Info("Escalating question",
		"question_id", q.ID,
		"from", route.Answerer,
		"to", route.EscalateTo)

	// Update question with escalation info
	q.AssignedTo = route.EscalateTo
	q.AssignedAt = time.Now()

	if err := c.questionStore.Store(ctx, q); err != nil {
		return fmt.Errorf("update question: %w", err)
	}

	// Publish escalation event
	event := EscalationEvent{
		QuestionID:   q.ID,
		Topic:        q.Topic,
		FromAnswerer: route.Answerer,
		ToAnswerer:   route.EscalateTo,
		Reason:       "SLA exceeded",
		Timestamp:    time.Now(),
	}

	baseMsg := message.NewBaseMessage(EscalationEventType, &event, "question-timeout")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	subject := fmt.Sprintf("question.escalate.%s", q.ID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}

	// If escalation target is an agent, route it
	escalationRoute := c.registry.Match(q.Topic)
	if escalationRoute.Answerer == route.EscalateTo {
		// Same route matched, need to find escalation target route
		// For now, we'll just update the assignment
		// A more sophisticated implementation would have a separate escalation routing
	}

	return nil
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
	c.logger.Info("question-timeout stopped",
		"checks_performed", c.checksPerformed.Load(),
		"timeouts_detected", c.timeoutsDetected.Load(),
		"escalations_triggered", c.escalationsTriggered.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "question-timeout",
		Type:        "processor",
		Description: "Monitors question SLAs and triggers escalation",
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
	return timeoutSchema
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
		ErrorCount: 0,
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
		LastActivity:      c.getLastCheck(),
	}
}

func (c *Component) updateLastCheck() {
	c.lastCheckMu.Lock()
	c.lastCheck = time.Now()
	c.lastCheckMu.Unlock()
}

func (c *Component) getLastCheck() time.Time {
	c.lastCheckMu.RLock()
	defer c.lastCheckMu.RUnlock()
	return c.lastCheck
}

// TimeoutEvent represents a timeout event for a question.
type TimeoutEvent struct {
	QuestionID string        `json:"question_id"`
	Topic      string        `json:"topic"`
	Age        time.Duration `json:"age"`
	SLA        time.Duration `json:"sla"`
	Timestamp  time.Time     `json:"timestamp"`
}

// Schema returns the message type for this payload.
func (e *TimeoutEvent) Schema() message.Type {
	return TimeoutEventType
}

// Validate validates the event.
func (e *TimeoutEvent) Validate() error {
	if e.QuestionID == "" {
		return fmt.Errorf("question_id is required")
	}
	return nil
}

// MarshalJSON marshals the event to JSON.
func (e *TimeoutEvent) MarshalJSON() ([]byte, error) {
	type Alias TimeoutEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON unmarshals the event from JSON.
func (e *TimeoutEvent) UnmarshalJSON(data []byte) error {
	type Alias TimeoutEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// TimeoutEventType is the message type for timeout events.
var TimeoutEventType = message.Type{
	Domain:   "question",
	Category: "timeout",
	Version:  "v1",
}

// EscalationEvent represents an escalation event for a question.
type EscalationEvent struct {
	QuestionID   string    `json:"question_id"`
	Topic        string    `json:"topic"`
	FromAnswerer string    `json:"from_answerer"`
	ToAnswerer   string    `json:"to_answerer"`
	Reason       string    `json:"reason"`
	Timestamp    time.Time `json:"timestamp"`
}

// Schema returns the message type for this payload.
func (e *EscalationEvent) Schema() message.Type {
	return EscalationEventType
}

// Validate validates the event.
func (e *EscalationEvent) Validate() error {
	if e.QuestionID == "" {
		return fmt.Errorf("question_id is required")
	}
	return nil
}

// MarshalJSON marshals the event to JSON.
func (e *EscalationEvent) MarshalJSON() ([]byte, error) {
	type Alias EscalationEvent
	return json.Marshal((*Alias)(e))
}

// UnmarshalJSON unmarshals the event from JSON.
func (e *EscalationEvent) UnmarshalJSON(data []byte) error {
	type Alias EscalationEvent
	return json.Unmarshal(data, (*Alias)(e))
}

// EscalationEventType is the message type for escalation events.
var EscalationEventType = message.Type{
	Domain:   "question",
	Category: "escalation",
	Version:  "v1",
}
