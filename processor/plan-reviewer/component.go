// Package planreviewer provides a processor that reviews plans against SOPs
// before approval using LLM analysis.
package planreviewer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	"github.com/c360studio/semspec/processor/context-builder/strategies"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the plan-reviewer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry *model.Registry
	httpClient    *http.Client

	// Graph-first context building
	graphGatherer *gatherers.GraphGatherer

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	reviewsProcessed atomic.Int64
	reviewsApproved  atomic.Int64
	reviewsRejected  atomic.Int64
	reviewsFailed    atomic.Int64
	lastActivityMu   sync.RWMutex
	lastActivity     time.Time
}

// NewComponent creates a new plan-reviewer processor.
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
	if config.ResultSubjectPrefix == "" {
		config.ResultSubjectPrefix = defaults.ResultSubjectPrefix
	}
	if config.ContextBuildTimeout == "" {
		config.ContextBuildTimeout = defaults.ContextBuildTimeout
	}
	if config.LLMTimeout == "" {
		config.LLMTimeout = defaults.LLMTimeout
	}
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.GraphGatewayURL == "" {
		config.GraphGatewayURL = defaults.GraphGatewayURL
	}
	if config.ContextTokenBudget == 0 {
		config.ContextTokenBudget = defaults.ContextTokenBudget
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Component{
		name:          "plan-reviewer",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        deps.GetLogger(),
		modelRegistry: model.Global(),
		graphGatherer: gatherers.NewGraphGatherer(config.GraphGatewayURL),
		httpClient: &http.Client{
			Timeout: 180 * time.Second, // Allow time for LLM responses
		},
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized plan-reviewer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing plan review triggers.
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
		FilterSubject: c.config.TriggerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       180 * time.Second, // Allow time for LLM
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

	c.logger.Info("plan-reviewer started",
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

// PlanReviewTrigger is the trigger payload for plan review.
type PlanReviewTrigger struct {
	RequestID     string   `json:"request_id"`
	Slug          string   `json:"slug"`
	ProjectID     string   `json:"project_id"`
	PlanContent   string   `json:"plan_content"`
	ScopePatterns []string `json:"scope_patterns"`
	SOPContext    string   `json:"sop_context,omitempty"` // Pre-built SOP context
}

// Schema implements message.Payload.
func (t *PlanReviewTrigger) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "trigger", Version: "v1"}
}

// Validate implements message.Payload.
func (t *PlanReviewTrigger) Validate() error {
	if t.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if t.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if t.PlanContent == "" {
		return fmt.Errorf("plan_content is required")
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (t *PlanReviewTrigger) MarshalJSON() ([]byte, error) {
	type Alias PlanReviewTrigger
	return json.Marshal((*Alias)(t))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *PlanReviewTrigger) UnmarshalJSON(data []byte) error {
	type Alias PlanReviewTrigger
	return json.Unmarshal(data, (*Alias)(t))
}

// handleMessage processes a single plan review trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.reviewsProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to parse message", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Extract trigger payload
	var trigger PlanReviewTrigger
	payloadBytes, err := json.Marshal(baseMsg.Payload())
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to marshal payload", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}
	if err := json.Unmarshal(payloadBytes, &trigger); err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to unmarshal trigger", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger", "error", err)
		// ACK invalid requests - they won't succeed on retry
		if err := msg.Ack(); err != nil {
			c.logger.Warn("Failed to ACK message", "error", err)
		}
		return
	}

	c.logger.Info("Processing plan review trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug)

	// Perform the review using LLM
	result, err := c.reviewPlan(ctx, &trigger)
	if err != nil {
		c.reviewsFailed.Add(1)
		c.logger.Error("Failed to review plan",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
		// NAK for retry
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message", "error", err)
		}
		return
	}

	// Track metrics
	if result.IsApproved() {
		c.reviewsApproved.Add(1)
	} else {
		c.reviewsRejected.Add(1)
	}

	// Publish result
	if err := c.publishResult(ctx, &trigger, result); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
		// Don't fail - review was successful
	}

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Plan review completed",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"verdict", result.Verdict,
		"findings_count", len(result.Findings))
}

// reviewPlan calls the LLM to review the plan against SOPs.
// It follows the graph-first pattern by enriching context with graph data.
func (c *Component) reviewPlan(ctx context.Context, trigger *PlanReviewTrigger) (*prompts.PlanReviewResult, error) {
	// Check context cancellation before expensive operations
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Step 1: Enrich SOPContext with graph data (graph-first)
	enrichedContext := trigger.SOPContext
	graphContext, err := c.buildReviewContext(ctx, trigger)
	if err != nil {
		c.logger.Warn("Failed to build review context from graph, proceeding without",
			"slug", trigger.Slug,
			"error", err)
	} else if graphContext != "" {
		c.logger.Info("Enriched review context from graph",
			"slug", trigger.Slug,
			"context_length", len(graphContext))
		if enrichedContext != "" {
			enrichedContext = enrichedContext + "\n\n## Additional Context from Knowledge Graph\n\n" + graphContext
		} else {
			enrichedContext = graphContext
		}
	}

	// Build prompts with enriched context
	systemPrompt := prompts.PlanReviewerSystemPrompt()
	userPrompt := prompts.PlanReviewerUserPrompt(trigger.Slug, trigger.PlanContent, enrichedContext)

	// If no context at all, return approved automatically
	if enrichedContext == "" {
		return &prompts.PlanReviewResult{
			Verdict:  "approved",
			Summary:  "No plan-scope SOPs or relevant context found. Plan approved by default.",
			Findings: nil,
		}, nil
	}

	// Resolve model and endpoint based on capability
	capability := c.config.DefaultCapability
	cap := model.ParseCapability(capability)
	if cap == "" {
		cap = model.CapabilityReviewing
	}
	modelName := c.modelRegistry.Resolve(cap)

	// Get endpoint configuration for the model
	endpoint := c.modelRegistry.GetEndpoint(modelName)
	if endpoint == nil {
		return nil, fmt.Errorf("no endpoint configured for model %s", modelName)
	}

	// Build the full endpoint URL
	if endpoint.URL == "" {
		return nil, fmt.Errorf("no URL configured for model %s", modelName)
	}

	// Parse and construct URL properly for OpenAI-compatible API
	u, err := url.Parse(endpoint.URL)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint URL: %w", err)
	}
	u.Path = path.Join(u.Path, "chat/completions")
	llmURL := u.String()

	c.logger.Debug("Using LLM endpoint",
		"capability", capability,
		"model_name", modelName,
		"model", endpoint.Model,
		"url", llmURL)

	// Build request for OpenAI-compatible API
	reqBody := map[string]any{
		"model": endpoint.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.3, // Lower temperature for more consistent reviews
		"max_tokens":  4096,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create request with timeout context
	llmTimeout, err := time.ParseDuration(c.config.LLMTimeout)
	if err != nil {
		llmTimeout = 120 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, llmURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request to %s: %w", llmURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(body))
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
		return nil, fmt.Errorf("no choices in LLM response")
	}

	content := llmResp.Choices[0].Message.Content

	// Parse review result from response
	result, err := c.parseReviewFromResponse(content)
	if err != nil {
		return nil, fmt.Errorf("parse review from response: %w", err)
	}

	return result, nil
}

// Pre-compiled regex pattern for JSON extraction.
var jsonBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*?\\})\\s*```")

// parseReviewFromResponse extracts the review result from the LLM response.
func (c *Component) parseReviewFromResponse(content string) (*prompts.PlanReviewResult, error) {
	// Extract JSON from the response (may be wrapped in markdown code blocks)
	jsonContent := extractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var result prompts.PlanReviewResult
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	// Validate verdict
	if result.Verdict != "approved" && result.Verdict != "needs_changes" {
		return nil, fmt.Errorf("invalid verdict: %s (expected approved or needs_changes)", result.Verdict)
	}

	return &result, nil
}

// extractJSON extracts JSON content from a string, handling markdown code blocks.
func extractJSON(content string) string {
	// Try to find JSON code block
	if matches := jsonBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		return matches[1]
	}

	// Try to find raw JSON object starting with {
	start := -1
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
				return content[start : i+1]
			}
		}
	}

	return ""
}

// buildReviewContext queries the knowledge graph to build additional context for plan review.
// This implements the graph-first pattern by enriching the review context with relevant codebase information.
func (c *Component) buildReviewContext(ctx context.Context, trigger *PlanReviewTrigger) (string, error) {
	var contextParts []string
	estimator := strategies.NewTokenEstimator()
	budget := strategies.NewBudgetAllocation(c.config.ContextTokenBudget)

	// Extract topics from plan content for relevance matching
	topics := extractTopicsFromPlan(trigger.PlanContent)

	// Step 1: Find relevant existing specs and plans
	if len(topics) > 0 && budget.Remaining() > strategies.MinTokensForPatterns {
		for _, topic := range topics {
			if budget.Remaining() < strategies.MinTokensForPartial {
				break
			}

			topicLower := strings.ToLower(topic)

			// Search for existing proposals
			proposals, err := c.graphGatherer.QueryEntitiesByPredicate(ctx, "semspec.plan")
			if err == nil {
				for _, e := range proposals {
					if budget.Remaining() < strategies.MinTokensForPartial {
						break
					}

					idLower := strings.ToLower(e.ID)
					if strings.Contains(idLower, topicLower) {
						content, err := c.graphGatherer.HydrateEntity(ctx, e.ID, 1)
						if err != nil {
							continue
						}

						tokens := estimator.Estimate(content)
						if budget.CanFit(tokens) {
							if err := budget.Allocate("plan:"+e.ID, tokens); err == nil {
								contextParts = append(contextParts, "### Related Plan: "+e.ID+"\n\n"+content)
							}
						}
					}
				}
			}
		}
	}

	// Step 2: Find architecture patterns relevant to scope
	if len(topics) > 0 && budget.Remaining() > strategies.MinTokensForPatterns {
		prefixes := []string{"code.function", "code.type", "code.interface"}
		for _, prefix := range prefixes {
			if budget.Remaining() < strategies.MinTokensForPartial {
				break
			}

			entities, err := c.graphGatherer.QueryEntitiesByPredicate(ctx, prefix)
			if err != nil {
				continue
			}

			matchCount := 0
			for _, e := range entities {
				if matchCount >= 2 { // Limit per type for reviews
					break
				}
				if budget.Remaining() < strategies.MinTokensForPartial {
					break
				}

				idLower := strings.ToLower(e.ID)
				matched := false
				for _, topic := range topics {
					if strings.Contains(idLower, strings.ToLower(topic)) {
						matched = true
						break
					}
				}

				if matched {
					content, err := c.graphGatherer.HydrateEntity(ctx, e.ID, 1)
					if err != nil {
						continue
					}

					tokens := estimator.Estimate(content)
					if budget.CanFit(tokens) {
						if err := budget.Allocate("pattern:"+e.ID, tokens); err == nil {
							contextParts = append(contextParts, "### Code Pattern: "+e.ID+"\n\n"+content)
							matchCount++
						}
					}
				}
			}
		}
	}

	c.logger.Debug("Built review context from graph",
		"parts", len(contextParts),
		"budget_used", budget.Allocated,
		"budget_total", budget.Total)

	if len(contextParts) == 0 {
		return "", nil
	}

	return strings.Join(contextParts, "\n\n---\n\n"), nil
}

// extractTopicsFromPlan extracts key topics from plan content for relevance matching.
func extractTopicsFromPlan(planContent string) []string {
	// Simple keyword extraction from plan JSON
	// Look for words in goal, context, and scope fields
	var topics []string

	// Extract words that look like identifiers or technical terms
	words := strings.FieldsFunc(planContent, func(r rune) bool {
		return r == ' ' || r == '"' || r == ':' || r == ',' || r == '[' || r == ']' || r == '{' || r == '}'
	})

	seen := make(map[string]bool)
	for _, word := range words {
		word = strings.TrimSpace(word)
		// Look for camelCase, PascalCase, or snake_case words
		if len(word) >= 4 && !seen[word] {
			// Skip common JSON keys and values
			if word == "goal" || word == "context" || word == "scope" || word == "include" ||
				word == "exclude" || word == "true" || word == "false" || word == "null" {
				continue
			}
			seen[word] = true
			topics = append(topics, word)
		}
	}

	// Limit to top topics
	if len(topics) > 10 {
		topics = topics[:10]
	}

	return topics
}

// PlanReviewResult is the result payload for plan review.
type PlanReviewResult struct {
	RequestID string                      `json:"request_id"`
	Slug      string                      `json:"slug"`
	Verdict   string                      `json:"verdict"`
	Summary   string                      `json:"summary"`
	Findings  []prompts.PlanReviewFinding `json:"findings"`
	Status    string                      `json:"status"`
}

// Schema implements message.Payload.
func (r *PlanReviewResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *PlanReviewResult) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *PlanReviewResult) MarshalJSON() ([]byte, error) {
	type Alias PlanReviewResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *PlanReviewResult) UnmarshalJSON(data []byte) error {
	type Alias PlanReviewResult
	return json.Unmarshal(data, (*Alias)(r))
}

// publishResult publishes a result notification for the plan review.
// Uses JetStream publish for ordering guarantees on workflow results.
func (c *Component) publishResult(ctx context.Context, trigger *PlanReviewTrigger, result *prompts.PlanReviewResult) error {
	payload := &PlanReviewResult{
		RequestID: trigger.RequestID,
		Slug:      trigger.Slug,
		Verdict:   result.Verdict,
		Summary:   result.Summary,
		Findings:  result.Findings,
		Status:    "completed",
	}

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "result", Version: "v1"},
		payload,
		"plan-reviewer",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	// Use JetStream publish for durable workflow results
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream: %w", err)
	}

	subject := fmt.Sprintf("%s.%s", c.config.ResultSubjectPrefix, trigger.Slug)
	if _, err := js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("publish result: %w", err)
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
	c.logger.Info("plan-reviewer stopped",
		"reviews_processed", c.reviewsProcessed.Load(),
		"reviews_approved", c.reviewsApproved.Load(),
		"reviews_rejected", c.reviewsRejected.Load(),
		"reviews_failed", c.reviewsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "plan-reviewer",
		Type:        "processor",
		Description: "Reviews plans against SOPs before approval using LLM analysis",
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
	return planReviewerSchema
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
