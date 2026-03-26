// Package questionanswerer provides a processor that answers questions
// using LLM agents based on topic and capability routing.
package questionanswerer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// llmCompleter is the subset of the LLM client used by the question-answerer.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the question-answerer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient    llmCompleter
	tripleWriter *graphutil.TripleWriter

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	tasksProcessed   atomic.Int64
	answersGenerated atomic.Int64
	answersFailed    atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// NewComponent creates a new question-answerer processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.TaskSubject == "" {
		config.TaskSubject = defaults.TaskSubject
	}
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	return &Component{
		name:       "question-answerer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient:  llm.NewClient(model.Global(), llm.WithLogger(logger)),
		tripleWriter: &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        logger,
			ComponentName: "question-answerer",
		},
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized question-answerer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"task_subject", c.config.TaskSubject)
	return nil
}

// Start begins processing question-answering tasks.
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

	// Push-based consumption — messages arrive via callback, no polling delay.
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.TaskSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       120 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("consume question tasks: %w", err)
	}

	c.logger.Info("question-answerer started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.TaskSubject)

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

// handleMessage processes a single question-answering task.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.tasksProcessed.Add(1)
	c.updateLastActivity()

	// Parse the task using reactive payload parser.
	task, err := payloads.ParseReactivePayload[answerer.QuestionAnswerTask](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse task", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing question-answering task",
		"task_id", task.TaskID,
		"question_id", task.QuestionID,
		"topic", task.Topic,
		"capability", task.Capability,
		"trace_id", task.TraceID)

	// Signal in-progress to prevent redelivery during LLM operations.
	if err := msg.InProgress(); err != nil {
		c.logger.Debug("Failed to signal in-progress", "error", err)
	}

	// Inject trace context for LLM call tracking
	llmCtx := ctx
	if task.TraceID != "" || task.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: task.TraceID,
			LoopID:  task.LoopID,
		})
	}

	// Generate answer using LLM
	answer, err := c.generateAnswer(llmCtx, task)
	if err != nil {
		c.answersFailed.Add(1)
		c.logger.Error("Failed to generate answer",
			"task_id", task.TaskID,
			"question_id", task.QuestionID,
			"error", err)
		// NAK for retry
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Publish answer
	if err := c.publishAnswer(ctx, task, answer); err != nil {
		c.answersFailed.Add(1)
		c.logger.Error("Failed to publish answer",
			"task_id", task.TaskID,
			"question_id", task.QuestionID,
			"error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Graph triples for the answer are written in publishAnswer (source of truth).

	c.answersGenerated.Add(1)

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Question answered successfully",
		"task_id", task.TaskID,
		"question_id", task.QuestionID,
		"topic", task.Topic)
}

// generateAnswer calls the LLM to generate an answer.
func (c *Component) generateAnswer(ctx context.Context, task *answerer.QuestionAnswerTask) (string, error) {
	// Use task.Capability if provided, otherwise fall back to config default.
	capability := task.Capability
	if capability == "" {
		capability = c.config.DefaultCapability
	}
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	// Build the prompt from the task fields directly.
	prompt := c.buildPromptWithContext(task, "")

	temperature := 0.7
	llmResp, err := c.llmClient.Complete(ctx, llm.Request{
		Capability: capability,
		Messages: []llm.Message{
			{Role: "system", Content: "You are a helpful technical expert. Answer questions clearly and concisely. If you're uncertain, explain what additional information would help. Use the provided codebase context to give accurate, specific answers."},
			{Role: "user", Content: prompt},
		},
		Temperature: &temperature,
		MaxTokens:   2048,
	})
	if err != nil {
		return "", fmt.Errorf("LLM completion: %w", err)
	}

	c.logger.Debug("LLM response received",
		"model", llmResp.Model,
		"tokens_used", llmResp.TokensUsed)

	return llmResp.Content, nil
}

// buildPromptWithContext constructs the prompt including graph context.
func (c *Component) buildPromptWithContext(task *answerer.QuestionAnswerTask, graphContext string) string {
	var prompt strings.Builder

	prompt.WriteString(fmt.Sprintf("Topic: %s\n\n", task.Topic))
	prompt.WriteString(fmt.Sprintf("Question: %s\n\n", task.Question))

	// Include any provided context from the task
	if task.Context != "" {
		prompt.WriteString(fmt.Sprintf("Provided Context:\n%s\n\n", task.Context))
	}

	// Include graph context
	if graphContext != "" {
		prompt.WriteString("## Codebase Context\n\n")
		prompt.WriteString("The following context from the knowledge graph provides relevant information:\n\n")
		prompt.WriteString(graphContext)
		prompt.WriteString("\n\n")
	}

	prompt.WriteString("Please provide a clear, actionable answer based on the codebase context above. If you are uncertain about any aspect, explain what additional information would help.")

	return prompt.String()
}

// publishAnswer publishes the answer to the question.answer subject.
func (c *Component) publishAnswer(ctx context.Context, task *answerer.QuestionAnswerTask, answer string) error {
	answeredBy := fmt.Sprintf("agent/%s", task.AgentName)

	// Write answer triples to graph (source of truth).
	questionEntityID := "question." + task.QuestionID
	_ = c.tripleWriter.WriteTriple(ctx, questionEntityID, "workflow.question.status", string(workflow.QuestionStatusAnswered))
	_ = c.tripleWriter.WriteTriple(ctx, questionEntityID, "workflow.question.answer", answer)
	_ = c.tripleWriter.WriteTriple(ctx, questionEntityID, "workflow.question.answered_by", answeredBy)

	// Publish answer event so the execution-orchestrator can resume.
	payload := &workflow.AnswerPayload{
		QuestionID:   task.QuestionID,
		AnsweredBy:   answeredBy,
		AnswererType: "agent",
		Answer:       answer,
		Confidence:   "medium",
		Sources:      "LLM generation",
	}

	baseMsg := message.NewBaseMessage(workflow.AnswerType, payload, "question-answerer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal answer: %w", err)
	}

	subject := fmt.Sprintf("question.answer.%s", task.QuestionID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}

	c.logger.Info("Answer published",
		"question_id", task.QuestionID,
		"answered_by", answeredBy,
		"subject", subject)

	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	// Copy cancel function and clear state before releasing lock
	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	// Cancel context after releasing lock to avoid potential deadlock
	if cancel != nil {
		cancel()
	}

	c.logger.Info("question-answerer stopped",
		"tasks_processed", c.tasksProcessed.Load(),
		"answers_generated", c.answersGenerated.Load(),
		"answers_failed", c.answersFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "question-answerer",
		Type:        "processor",
		Description: "Answers questions using LLM agents based on topic and capability",
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
	return answererSchema
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
		ErrorCount: int(c.answersFailed.Load()),
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
