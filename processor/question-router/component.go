// Package questionrouter provides a component that routes questions from agents
// to the appropriate answerer (LLM agent or human) based on topic patterns
// configured in answerers.json.
//
// Subscribes to question.ask.> on the AGENT stream. Questions can come from
// any stage (planning or execution). The router doesn't care — it just matches
// the topic pattern and dispatches.
package questionrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const componentName = "question-router"

// Config holds configuration for the question-router component.
type Config struct {
	StreamName   string `json:"stream_name" schema:"type:string,description:JetStream stream name,category:basic,default:AGENT"`
	ConsumerName string `json:"consumer_name" schema:"type:string,description:Durable consumer name,category:basic,default:question-router"`
	Subject      string `json:"subject" schema:"type:string,description:Subject to subscribe to,category:basic,default:question.ask.>"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		StreamName:   "AGENT",
		ConsumerName: "question-router",
		Subject:      "question.ask.>",
	}
}

// Component routes questions to answerers.
type Component struct {
	config       Config
	natsClient   *natsclient.Client
	logger       *slog.Logger
	router       *answerer.Router
	tripleWriter *graphutil.TripleWriter

	running bool
	mu      sync.RWMutex

	questionsRouted atomic.Int64
	routingFailed   atomic.Int64
}

// NewComponent creates a new question-router.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.StreamName == "" {
		cfg.StreamName = DefaultConfig().StreamName
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = DefaultConfig().ConsumerName
	}
	if cfg.Subject == "" {
		cfg.Subject = DefaultConfig().Subject
	}

	logger := deps.GetLogger()

	// Load answerer registry.
	registry, err := loadAnswererRegistry()
	if err != nil {
		return nil, fmt.Errorf("load answerer registry: %w", err)
	}

	return &Component{
		config:     cfg,
		natsClient: deps.NATSClient,
		logger:     logger.With("component", componentName),
		router:     answerer.NewRouter(registry, deps.NATSClient, logger),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: componentName,
		},
	}, nil
}

func loadAnswererRegistry() (*answerer.Registry, error) {
	for _, base := range []string{"/app", os.Getenv("SEMSPEC_REPO_PATH"), "."} {
		if base == "" {
			continue
		}
		for _, name := range []string{"answerers.json", "answerers.yaml"} {
			path := filepath.Join(base, "configs", name)
			r, err := answerer.LoadRegistry(path)
			if err == nil {
				return r, nil
			}
		}
	}
	return nil, fmt.Errorf("answerers config not found in any search path")
}

// Initialize prepares the component.
func (c *Component) Initialize() error { return nil }

// Start begins consuming question events.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.mu.Unlock()

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.Subject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, c.handleQuestion); err != nil {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return fmt.Errorf("consume questions: %w", err)
	}

	c.logger.Info("question-router started",
		"stream", c.config.StreamName,
		"subject", c.config.Subject)
	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	c.logger.Info("question-router stopped",
		"questions_routed", c.questionsRouted.Load(),
		"routing_failed", c.routingFailed.Load())
	return nil
}

// handleQuestion processes a question.ask.<id> event.
func (c *Component) handleQuestion(ctx context.Context, msg jetstream.Msg) {
	defer func() { _ = msg.Ack() }()

	// Parse the question event — simple JSON with question_id, question, context.
	var event struct {
		QuestionID string `json:"question_id"`
		Question   string `json:"question"`
		Context    string `json:"context"`
		Topic      string `json:"topic"`
	}
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		c.logger.Debug("Failed to parse question event", "error", err)
		return
	}

	if event.QuestionID == "" {
		c.logger.Debug("Question event missing question_id")
		return
	}

	// Build a minimal Question for routing (the router needs Topic for pattern matching).
	topic := event.Topic
	if topic == "" {
		// Extract topic from subject: question.ask.<id> → use "general" as fallback.
		topic = "general"
	}

	// Write question entity to graph (source of truth).
	entityID := "question." + event.QuestionID
	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.question.text", event.Question)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.question.context", event.Context)
	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.question.status", string(workflow.QuestionStatusPending))
	_ = c.tripleWriter.WriteTriple(ctx, entityID, "workflow.question.topic", topic)

	q := &workflow.Question{
		ID:       event.QuestionID,
		Topic:    topic,
		Question: event.Question,
		Context:  event.Context,
	}

	result, err := c.router.RouteQuestion(ctx, q)
	if err != nil {
		c.routingFailed.Add(1)
		c.logger.Warn("Failed to route question",
			"question_id", event.QuestionID, "error", err)
		return
	}

	c.questionsRouted.Add(1)
	c.logger.Info("Question routed",
		"question_id", event.QuestionID,
		"answerer", result.Route.Answerer,
		"message", result.Message)
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Routes agent questions to answerers based on topic patterns",
		Version:     "0.1.0",
	}
}

func (c *Component) InputPorts() []component.Port  { return nil }
func (c *Component) OutputPorts() []component.Port { return nil }
func (c *Component) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}

func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()
	status := "stopped"
	if running {
		status = "running"
	}
	return component.HealthStatus{
		Healthy: running,
		Status:  status,
	}
}

func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{}
}
