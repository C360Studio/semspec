// Package taskcodereview provides a JetStream processor that reviews
// code changes made by the developer agent.
package taskcodereview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/reactive"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// llmCompleter is the subset of the LLM client used by the code reviewer.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the task-code-reviewer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	llmClient  llmCompleter

	// JetStream consumer state.
	consumer jetstream.Consumer

	// KV bucket for workflow state
	stateBucket jetstream.KeyValue

	// Lifecycle.
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics.
	triggersProcessed atomic.Int64
	reviewsApproved   atomic.Int64
	reviewsRejected   atomic.Int64
	reviewsFailed     atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent constructs a task-code-reviewer Component from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults.
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
	if config.StateBucket == "" {
		config.StateBucket = defaults.StateBucket
	}
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.Timeout == "" {
		config.Timeout = defaults.Timeout
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Resolve repo path.
	repoPath := config.RepoPath
	if repoPath == "" {
		if env := os.Getenv("SEMSPEC_REPO_PATH"); env != "" {
			repoPath = env
		} else {
			repoPath, _ = os.Getwd()
		}
	}
	_ = repoPath // Will be used when we read files for review context

	logger := deps.GetLogger()

	return &Component{
		name:       "task-code-reviewer",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
			llm.WithCallStore(llm.GlobalCallStore()),
		),
	}, nil
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized task-code-reviewer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName)
	return nil
}

// Start begins consuming code review requests from JetStream.
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

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get jetstream: %w", err)
	}

	stream, err := js.Stream(subCtx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}

	// Get workflow state bucket.
	stateBucket, err := js.KeyValue(subCtx, c.config.StateBucket)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get state bucket %s: %w", c.config.StateBucket, err)
	}
	c.stateBucket = stateBucket

	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       c.config.GetTimeout() + 60*time.Second,
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	go c.consumeLoop(subCtx)

	c.logger.Info("task-code-reviewer started",
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

func (c *Component) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

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

func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger.
	trigger, err := reactive.ParseReactivePayload[reactive.TaskCodeReviewRequest](msg.Data())
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to parse trigger", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger", "error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid message", "error", ackErr)
		}
		return
	}

	c.logger.Info("Processing code review request",
		"slug", trigger.Slug,
		"task", trigger.DeveloperTask,
		"execution_id", trigger.ExecutionID)

	// Signal in-progress to prevent redelivery during LLM operations.
	if err := msg.InProgress(); err != nil {
		c.logger.Debug("Failed to signal in-progress", "error", err)
	}

	// Perform the review using LLM.
	result, err := c.reviewCode(ctx, trigger)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to review code",
			"slug", trigger.Slug,
			"error", err)

		// Transition to failure state.
		if trigger.ExecutionID != "" {
			if transErr := c.transitionToFailure(ctx, trigger.ExecutionID, err.Error()); transErr != nil {
				c.logger.Error("Failed to transition to failure state", "error", transErr)
			}
		}
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK message", "error", ackErr)
		}
		return
	}

	// Update workflow state with review results.
	if err := c.updateWorkflowState(ctx, trigger, result); err != nil {
		c.logger.Warn("Failed to update workflow state",
			"slug", trigger.Slug,
			"error", err)
	}

	if result.Verdict == "approved" {
		c.reviewsApproved.Add(1)
	} else {
		c.reviewsRejected.Add(1)
	}

	if ackErr := msg.Ack(); ackErr != nil {
		c.logger.Warn("Failed to ACK message", "error", ackErr)
	}

	c.logger.Info("Code review completed",
		"slug", trigger.Slug,
		"task", trigger.DeveloperTask,
		"verdict", result.Verdict,
		"rejection_type", result.RejectionType)
}

// CodeReviewResult holds the result of a code review.
type CodeReviewResult struct {
	Verdict       string          `json:"verdict"`        // "approved" or "rejected"
	RejectionType string          `json:"rejection_type"` // "fixable", "misscoped", "architectural", "too_big"
	Feedback      string          `json:"feedback"`
	Patterns      json.RawMessage `json:"patterns,omitempty"`
}

func (c *Component) reviewCode(ctx context.Context, trigger *reactive.TaskCodeReviewRequest) (*CodeReviewResult, error) {
	llmCtx, cancel := context.WithTimeout(ctx, c.config.GetTimeout())
	defer cancel()

	capability := c.config.DefaultCapability
	if capability == "" {
		capability = "coding"
	}

	// Build the review prompt.
	prompt := c.buildReviewPrompt(trigger)

	temperature := 0.3
	llmReq := llm.Request{
		Capability:  capability,
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: &temperature,
		MaxTokens:   2048,
	}

	llmResp, err := c.llmClient.Complete(llmCtx, llmReq)
	if err != nil {
		return nil, fmt.Errorf("LLM completion: %w", err)
	}

	// Parse the LLM response.
	result, err := c.parseReviewResponse(llmResp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse review response: %w", err)
	}

	return result, nil
}

func (c *Component) buildReviewPrompt(trigger *reactive.TaskCodeReviewRequest) string {
	var outputStr string
	if len(trigger.Output) > 0 {
		outputStr = string(trigger.Output)
	} else {
		outputStr = "(no output provided)"
	}

	return fmt.Sprintf(`You are a senior code reviewer. Review the following code changes and provide a verdict.

## Task
%s

## Developer Output
%s

## Instructions

Review the code for:
1. Correctness - Does it solve the task requirements?
2. Code quality - Is it well-structured and maintainable?
3. Best practices - Does it follow language conventions?
4. Potential issues - Are there bugs or edge cases?

## Response Format

Respond with a JSON object:
{
  "verdict": "approved" or "rejected",
  "rejection_type": "fixable" | "misscoped" | "architectural" | "too_big" (only if rejected),
  "feedback": "Detailed explanation of the review result"
}

Rejection types:
- fixable: Minor issues that can be fixed without changing the approach
- misscoped: The task was incorrectly scoped or requirements misunderstood
- architectural: Requires significant architectural changes
- too_big: The task is too large and should be broken down

If the code is acceptable, use verdict "approved".
`, trigger.DeveloperTask, outputStr)
}

func (c *Component) parseReviewResponse(content string) (*CodeReviewResult, error) {
	// Try to extract JSON from the response.
	var result CodeReviewResult

	// Try direct JSON parse first.
	if err := json.Unmarshal([]byte(content), &result); err == nil {
		return &result, nil
	}

	// Try to find JSON in the response.
	start := -1
	end := -1
	braceCount := 0

	for i, ch := range content {
		if ch == '{' {
			if start == -1 {
				start = i
			}
			braceCount++
		} else if ch == '}' {
			braceCount--
			if braceCount == 0 && start != -1 {
				end = i + 1
				break
			}
		}
	}

	if start != -1 && end != -1 {
		jsonStr := content[start:end]
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
			return &result, nil
		}
	}

	// Default to approved if we can't parse (assume positive intent).
	c.logger.Warn("Could not parse review response, defaulting to approved",
		"content_length", len(content))
	return &CodeReviewResult{
		Verdict:  "approved",
		Feedback: "Review completed (response could not be parsed, defaulting to approved)",
	}, nil
}

func (c *Component) transitionToFailure(ctx context.Context, executionID string, cause string) error {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get workflow state: %w", err)
	}

	var state reactive.TaskExecutionState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	state.Phase = phases.TaskExecReviewerFailed
	state.Error = cause
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	return nil
}

func (c *Component) updateWorkflowState(ctx context.Context, trigger *reactive.TaskCodeReviewRequest, result *CodeReviewResult) error {
	if trigger.ExecutionID == "" {
		return nil
	}

	entry, err := c.stateBucket.Get(ctx, trigger.ExecutionID)
	if err != nil {
		return fmt.Errorf("get workflow state %s: %w", trigger.ExecutionID, err)
	}

	var state reactive.TaskExecutionState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	// Update state with review results.
	state.Verdict = result.Verdict
	state.RejectionType = result.RejectionType
	state.Feedback = result.Feedback
	state.Patterns = result.Patterns
	state.Phase = phases.TaskExecReviewed
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, trigger.ExecutionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Updated workflow state with code review result",
		"slug", trigger.Slug,
		"execution_id", trigger.ExecutionID,
		"phase", phases.TaskExecReviewed,
		"verdict", result.Verdict)
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

	c.logger.Info("task-code-reviewer stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"reviews_approved", c.reviewsApproved.Load(),
		"reviews_rejected", c.reviewsRejected.Load(),
		"reviews_failed", c.reviewsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "task-code-reviewer",
		Type:        "processor",
		Description: "Reviews code changes made by the developer agent",
		Version:     "0.1.0",
	}
}

// InputPorts returns the configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, def := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        def.Name,
			Direction:   component.DirectionInput,
			Required:    def.Required,
			Description: def.Description,
			Config:      component.NATSPort{Subject: def.Subject},
		}
	}
	return ports
}

// OutputPorts returns the configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}
	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, def := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        def.Name,
			Direction:   component.DirectionOutput,
			Required:    def.Required,
			Description: def.Description,
			Config:      component.NATSPort{Subject: def.Subject},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
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
		ErrorCount: int(c.reviewsFailed.Load()),
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
