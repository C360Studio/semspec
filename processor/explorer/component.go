// Package explorer provides a processor that generates exploration content
// (Goal, Context, Questions, Next Steps) using LLM.
package explorer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the explorer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry *model.Registry
	httpClient    *http.Client

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed   atomic.Int64
	explorationsCreated atomic.Int64
	explorationsFailed  atomic.Int64
	lastActivityMu      sync.RWMutex
	lastActivity        time.Time
}

// NewComponent creates a new explorer processor.
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
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
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

	return &Component{
		name:          "explorer",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        deps.GetLogger(),
		modelRegistry: model.NewDefaultRegistry(),
		httpClient:    &http.Client{},
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized explorer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing explorer triggers.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("explorer already running")
	}
	if c.natsClient == nil {
		c.mu.Unlock()
		return fmt.Errorf("NATS client required")
	}
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	js, err := c.natsClient.JetStream()
	if err != nil {
		c.rollbackStart(nil)
		return fmt.Errorf("get jetstream: %w", err)
	}

	stream, err := js.Stream(ctx, c.config.StreamName)
	if err != nil {
		c.rollbackStart(nil)
		return fmt.Errorf("get stream %s: %w", c.config.StreamName, err)
	}
	c.stream = stream

	consumerConfig := jetstream.ConsumerConfig{
		Durable:       c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       180 * time.Second,
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		c.rollbackStart(nil)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	// Start message consumption
	consumeCtx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	go c.consumeMessages(consumeCtx)

	c.logger.Info("Explorer component started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)

	return nil
}

// rollbackStart reverts the running state when Start() fails partway through.
func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// consumeMessages consumes messages from the JetStream consumer.
func (c *Component) consumeMessages(ctx context.Context) {
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
			continue
		}

		for msg := range msgs.Messages() {
			c.handleMessage(ctx, msg)
		}

		if msgs.Error() != nil && ctx.Err() == nil {
			c.logger.Debug("Fetch error", "error", msgs.Error())
		}
	}
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

	c.logger.Info("Explorer component stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"explorations_created", c.explorationsCreated.Load(),
		"explorations_failed", c.explorationsFailed.Load())

	return nil
}

// handleMessage processes a trigger message.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	// Check for context cancellation before expensive operations
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the message
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.logger.Error("Failed to unmarshal message", "error", err)
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK malformed message", "error", err)
		}
		return
	}

	// Extract trigger payload
	var trigger workflow.WorkflowTriggerPayload
	payloadBytes, err := json.Marshal(baseMsg.Payload())
	if err != nil {
		c.logger.Error("Failed to marshal payload", "error", err)
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}
	if err := json.Unmarshal(payloadBytes, &trigger); err != nil {
		c.logger.Error("Failed to unmarshal trigger payload", "error", err)
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing exploration trigger",
		"slug", trigger.Data.Slug,
		"title", trigger.Data.Title,
		"request_id", trigger.RequestID)

	// Generate exploration using LLM
	exploration, err := c.generateExploration(ctx, &trigger)
	if err != nil {
		c.logger.Error("Failed to generate exploration",
			"slug", trigger.Data.Slug,
			"error", err)
		c.explorationsFailed.Add(1)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Save the exploration
	if err := c.saveExploration(ctx, &trigger, exploration); err != nil {
		c.logger.Error("Failed to save exploration",
			"slug", trigger.Data.Slug,
			"error", err)
		c.explorationsFailed.Add(1)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Publish result notification
	if err := c.publishResult(ctx, &trigger, exploration); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"slug", trigger.Data.Slug,
			"error", err)
		// Don't fail - the exploration was saved successfully
	}

	c.explorationsCreated.Add(1)
	c.logger.Info("Exploration generated successfully",
		"slug", trigger.Data.Slug,
		"has_goal", exploration.Goal != "",
		"questions_count", len(exploration.Questions))

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}
}

// updateLastActivity updates the last activity timestamp.
func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

// ExplorationContent represents the LLM-generated exploration content.
type ExplorationContent struct {
	Goal      string   `json:"goal"`
	Context   string   `json:"context"`
	Questions []string `json:"questions,omitempty"`
	Scope     struct {
		Include    []string `json:"include,omitempty"`
		Exclude    []string `json:"exclude,omitempty"`
		DoNotTouch []string `json:"do_not_touch,omitempty"`
	} `json:"scope,omitempty"`
	NextSteps []string `json:"next_steps,omitempty"`
}

// generateExploration uses the LLM to generate exploration content.
func (c *Component) generateExploration(ctx context.Context, trigger *workflow.WorkflowTriggerPayload) (*ExplorationContent, error) {
	// Build prompt
	prompt := trigger.Prompt
	if prompt == "" {
		prompt = prompts.ExplorerPromptWithTopic(trigger.Data.Title)
	}

	// Resolve model
	capability := model.Capability(c.config.DefaultCapability)
	modelName := c.modelRegistry.Resolve(capability)
	endpoint := c.modelRegistry.GetEndpoint(modelName)

	if endpoint == nil {
		return nil, fmt.Errorf("no endpoint found for model %s", modelName)
	}

	// Determine API URL
	apiURL := endpoint.URL
	if apiURL == "" {
		apiURL = os.Getenv("LLM_API_URL")
		if apiURL == "" {
			apiURL = "http://localhost:11434"
		}
		apiURL = strings.TrimSuffix(apiURL, "/") + "/v1"
	}
	apiURL = strings.TrimSuffix(apiURL, "/") + "/chat/completions"

	// Build request
	systemPrompt := prompts.ExplorerSystemPrompt()
	reqBody := map[string]any{
		"model": endpoint.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.7,
		"max_tokens":  4096,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", apiURL, bytes.NewReader(reqData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM request failed: %s (status %d)", string(body), resp.StatusCode)
	}

	// Parse response
	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := llmResp.Choices[0].Message.Content

	// Parse the exploration from the response
	return c.parseExplorationFromResponse(content)
}

// parseExplorationFromResponse extracts exploration content from LLM response.
func (c *Component) parseExplorationFromResponse(content string) (*ExplorationContent, error) {
	exploration := &ExplorationContent{}

	// Try to extract JSON from the response
	jsonContent := extractExplorationJSON(content)
	if jsonContent != "" {
		if err := json.Unmarshal([]byte(jsonContent), exploration); err == nil {
			// Validate required fields
			if exploration.Goal != "" {
				return exploration, nil
			}
		}
	}

	// Fallback: parse structured sections from the response
	return c.parseExplorationSections(content)
}

// parseExplorationSections parses exploration content from structured text.
func (c *Component) parseExplorationSections(content string) (*ExplorationContent, error) {
	exploration := &ExplorationContent{}

	// Parse Goal section
	goalRegex := regexp.MustCompile(`(?i)(?:^|\n)##?\s*goal[:\s]*\n?([\s\S]*?)(?:\n##|\n\*\*|$)`)
	if matches := goalRegex.FindStringSubmatch(content); len(matches) > 1 {
		exploration.Goal = strings.TrimSpace(matches[1])
	}

	// Parse Context section
	contextRegex := regexp.MustCompile(`(?i)(?:^|\n)##?\s*context[:\s]*\n?([\s\S]*?)(?:\n##|\n\*\*|$)`)
	if matches := contextRegex.FindStringSubmatch(content); len(matches) > 1 {
		exploration.Context = strings.TrimSpace(matches[1])
	}

	// Parse Questions section
	questionsRegex := regexp.MustCompile(`(?i)(?:^|\n)##?\s*questions?[:\s]*\n?([\s\S]*?)(?:\n##|\n\*\*|$)`)
	if matches := questionsRegex.FindStringSubmatch(content); len(matches) > 1 {
		questionLines := strings.Split(matches[1], "\n")
		for _, line := range questionLines {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimPrefix(line, "•")
			line = strings.TrimSpace(line)
			// Remove numbered prefix like "1." or "1)"
			if len(line) > 2 && (line[1] == '.' || line[1] == ')') && line[0] >= '0' && line[0] <= '9' {
				line = strings.TrimSpace(line[2:])
			}
			if line != "" {
				exploration.Questions = append(exploration.Questions, line)
			}
		}
	}

	// Parse Next Steps section
	nextStepsRegex := regexp.MustCompile(`(?i)(?:^|\n)##?\s*next\s*steps?[:\s]*\n?([\s\S]*?)(?:\n##|\n\*\*|$)`)
	if matches := nextStepsRegex.FindStringSubmatch(content); len(matches) > 1 {
		stepLines := strings.Split(matches[1], "\n")
		for _, line := range stepLines {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimPrefix(line, "•")
			line = strings.TrimSpace(line)
			if len(line) > 2 && (line[1] == '.' || line[1] == ')') && line[0] >= '0' && line[0] <= '9' {
				line = strings.TrimSpace(line[2:])
			}
			if line != "" {
				exploration.NextSteps = append(exploration.NextSteps, line)
			}
		}
	}

	// If we didn't find structured sections, use the whole content as context
	if exploration.Goal == "" && exploration.Context == "" {
		// Take the first paragraph as goal
		paragraphs := strings.Split(content, "\n\n")
		if len(paragraphs) > 0 {
			exploration.Goal = strings.TrimSpace(paragraphs[0])
		}
		if len(paragraphs) > 1 {
			exploration.Context = strings.TrimSpace(strings.Join(paragraphs[1:], "\n\n"))
		}
	}

	if exploration.Goal == "" {
		return nil, fmt.Errorf("exploration missing 'goal' field")
	}

	return exploration, nil
}

// extractExplorationJSON attempts to extract JSON from the response.
func extractExplorationJSON(content string) string {
	// Look for JSON code block
	codeBlockRegex := regexp.MustCompile("```(?:json)?\\s*\\n?([\\s\\S]*?)\\n?```")
	if matches := codeBlockRegex.FindStringSubmatch(content); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// Look for bare JSON object
	jsonRegex := regexp.MustCompile(`\{[\s\S]*"goal"[\s\S]*\}`)
	if matches := jsonRegex.FindString(content); matches != "" {
		return matches
	}

	return ""
}

// saveExploration saves the exploration content to the plan file.
func (c *Component) saveExploration(ctx context.Context, trigger *workflow.WorkflowTriggerPayload, exploration *ExplorationContent) error {
	// Get repo root from environment
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	manager := workflow.NewManager(repoRoot)

	// Load existing plan
	plan, err := manager.LoadPlan(ctx, trigger.Data.Slug)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}

	// Update plan with exploration content
	plan.Goal = exploration.Goal
	plan.Context = exploration.Context

	// Update scope if provided
	if len(exploration.Scope.Include) > 0 {
		plan.Scope.Include = exploration.Scope.Include
	}
	if len(exploration.Scope.Exclude) > 0 {
		plan.Scope.Exclude = exploration.Scope.Exclude
	}
	if len(exploration.Scope.DoNotTouch) > 0 {
		plan.Scope.DoNotTouch = exploration.Scope.DoNotTouch
	}

	// Store questions as execution steps for now
	if len(exploration.Questions) > 0 {
		var questionSteps []string
		for i, q := range exploration.Questions {
			questionSteps = append(questionSteps, fmt.Sprintf("%d. %s", i+1, q))
		}
		plan.Execution = "Questions to explore:\n" + strings.Join(questionSteps, "\n")
	}

	// Add next steps
	if len(exploration.NextSteps) > 0 {
		var steps []string
		for i, s := range exploration.NextSteps {
			steps = append(steps, fmt.Sprintf("%d. %s", i+1, s))
		}
		if plan.Execution != "" {
			plan.Execution += "\n\nNext steps:\n" + strings.Join(steps, "\n")
		} else {
			plan.Execution = "Next steps:\n" + strings.Join(steps, "\n")
		}
	}

	// Save plan
	return manager.SavePlan(ctx, plan)
}

// ExplorerResult represents the result of an exploration generation.
type ExplorerResult struct {
	RequestID string              `json:"request_id"`
	Slug      string              `json:"slug"`
	Content   *ExplorationContent `json:"content"`
	Status    string              `json:"status"`
}

// Schema implements message.Payload.
func (r *ExplorerResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *ExplorerResult) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *ExplorerResult) MarshalJSON() ([]byte, error) {
	type Alias ExplorerResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *ExplorerResult) UnmarshalJSON(data []byte) error {
	type Alias ExplorerResult
	return json.Unmarshal(data, (*Alias)(r))
}

// publishResult publishes a success notification for the exploration generation.
func (c *Component) publishResult(ctx context.Context, trigger *workflow.WorkflowTriggerPayload, exploration *ExplorationContent) error {
	result := &ExplorerResult{
		RequestID: trigger.RequestID,
		Slug:      trigger.Data.Slug,
		Content:   exploration,
		Status:    "completed",
	}

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "result", Version: "v1"},
		result,
		"explorer",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	subject := fmt.Sprintf("workflow.result.explorer.%s", trigger.Data.Slug)
	return c.natsClient.Publish(ctx, subject, data)
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "explorer",
		Type:        "processor",
		Description: "Generates exploration content (Goal/Context/Questions) using LLM",
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
	return explorerSchema
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
		ErrorCount: int(c.explorationsFailed.Load()),
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

func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}
