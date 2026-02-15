// Package questionanswerer provides a processor that answers questions
// using LLM agents based on topic and capability routing.
package questionanswerer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the question-answerer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry *model.Registry
	questionStore *workflow.QuestionStore
	httpClient    *http.Client

	// Centralized context building via context-builder
	contextHelper *contexthelper.Helper

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

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
	if config.ContextSubjectPrefix == "" {
		config.ContextSubjectPrefix = defaults.ContextSubjectPrefix
	}
	if config.ContextResponseBucket == "" {
		config.ContextResponseBucket = defaults.ContextResponseBucket
	}
	if config.ContextTimeout == "" {
		config.ContextTimeout = defaults.ContextTimeout
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

	logger := deps.GetLogger()

	// Initialize context helper for centralized context building
	ctxHelper := contexthelper.New(deps.NATSClient, contexthelper.Config{
		SubjectPrefix:  config.ContextSubjectPrefix,
		ResponseBucket: config.ContextResponseBucket,
		Timeout:        config.GetContextTimeout(),
		SourceName:     "question-answerer",
	}, logger)

	return &Component{
		name:          "question-answerer",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: model.NewDefaultRegistry(),
		questionStore: store,
		httpClient: &http.Client{}, // Timeout controlled per-request via context
		contextHelper: ctxHelper,
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

	// Get JetStream context
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	// Get stream
	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}
	c.stream = stream

	// Create or get consumer
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.TaskSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       120 * time.Second,
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	// Start consuming messages
	go c.consumeLoop(subCtx)

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

// consumeLoop continuously consumes messages from the JetStream consumer.
func (c *Component) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch messages with a timeout
		msgs, err := c.consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug("Fetch timeout or error", "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			c.handleMessage(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "error", msgs.Error())
		}
	}
}

// handleMessage processes a single question-answering task.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.tasksProcessed.Add(1)
	c.updateLastActivity()

	// Parse the task
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Error("Failed to parse message", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Extract task payload
	var task answerer.QuestionAnswerTask
	payloadBytes, err := json.Marshal(baseMsg.Payload())
	if err != nil {
		c.logger.Error("Failed to marshal payload", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}
	if err := json.Unmarshal(payloadBytes, &task); err != nil {
		c.logger.Error("Failed to unmarshal task", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing question-answering task",
		"task_id", task.TaskID,
		"question_id", task.QuestionID,
		"topic", task.Topic,
		"capability", task.Capability)

	// Generate answer using LLM
	answer, err := c.generateAnswer(ctx, &task)
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
	if err := c.publishAnswer(ctx, &task, answer); err != nil {
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

	// Update question store
	if err := c.updateQuestionStore(ctx, &task, answer); err != nil {
		c.logger.Warn("Failed to update question store",
			"question_id", task.QuestionID,
			"error", err)
		// Don't fail - answer was published successfully
	}

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
// It follows the graph-first pattern by requesting context from the
// centralized context-builder before making the LLM call.
func (c *Component) generateAnswer(ctx context.Context, task *answerer.QuestionAnswerTask) (string, error) {
	// Step 1: Request question context from centralized context-builder (graph-first)
	var graphContext string
	resp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType: contextbuilder.TaskTypeQuestion,
		Topic:    task.Topic + " " + task.Question, // Combine for better keyword matching
	})
	if resp != nil {
		graphContext = contexthelper.FormatContextResponse(resp)
		c.logger.Info("Built question context via context-builder",
			"topic", task.Topic,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"topic", task.Topic)
	}

	// Step 2: Build the prompt with graph context
	prompt := c.buildPromptWithContext(task, graphContext)

	// Step 3: Resolve model and endpoint based on capability
	capability := task.Capability
	if capability == "" {
		capability = c.config.DefaultCapability
	}

	// Get model name for capability
	cap := model.ParseCapability(capability)
	if cap == "" {
		cap = model.CapabilityPlanning // Default capability
	}
	modelName := c.modelRegistry.Resolve(cap)

	// Get endpoint configuration for the model
	endpoint := c.modelRegistry.GetEndpoint(modelName)
	if endpoint == nil {
		return "", fmt.Errorf("no endpoint configured for model %s", modelName)
	}

	// Build the full endpoint URL
	llmURL := endpoint.URL
	if llmURL == "" {
		return "", fmt.Errorf("no URL configured for model %s", modelName)
	}
	// Ensure URL ends with /chat/completions for OpenAI-compatible API
	if llmURL[len(llmURL)-1] != '/' {
		llmURL += "/"
	}
	llmURL += "chat/completions"

	c.logger.Debug("Using LLM endpoint",
		"capability", capability,
		"model_name", modelName,
		"model", endpoint.Model,
		"url", llmURL,
		"has_graph_context", graphContext != "")

	// Step 4: Build request for OpenAI-compatible API
	reqBody := map[string]any{
		"model": endpoint.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful technical expert. Answer questions clearly and concisely. If you're uncertain, explain what additional information would help. Use the provided codebase context to give accurate, specific answers."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.7,
		"max_tokens":  2048,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	// Create request with timeout context
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, llmURL, bytes.NewReader(reqBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request to %s: %w", llmURL, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return "", fmt.Errorf("LLM API error (status %d): %s", httpResp.StatusCode, string(body))
	}

	// Parse response
	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(httpResp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in LLM response")
	}

	return llmResp.Choices[0].Message.Content, nil
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
	payload := &workflow.AnswerPayload{
		QuestionID:   task.QuestionID,
		AnsweredBy:   fmt.Sprintf("agent/%s", task.AgentName),
		AnswererType: "agent",
		Answer:       answer,
		Confidence:   "medium", // Could be determined from LLM response
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

	return nil
}

// updateQuestionStore updates the question in the KV store with the answer.
func (c *Component) updateQuestionStore(ctx context.Context, task *answerer.QuestionAnswerTask, answer string) error {
	// Get the question
	q, err := c.questionStore.Get(ctx, task.QuestionID)
	if err != nil {
		return fmt.Errorf("get question: %w", err)
	}

	// Update with answer
	now := time.Now()
	q.Answer = answer
	q.AnsweredBy = fmt.Sprintf("agent/%s", task.AgentName)
	q.AnsweredAt = &now
	q.Status = workflow.QuestionStatusAnswered

	// Store updated question
	if err := c.questionStore.Store(ctx, q); err != nil {
		return fmt.Errorf("store question: %w", err)
	}

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
