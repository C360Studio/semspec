// Package plancoordinator provides a processor that coordinates concurrent planners
// for parallel plan generation using LLM-driven focus area selection.
package plancoordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the plan-coordinator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient *llm.Client

	// Centralized context building via context-builder
	contextHelper *contexthelper.Helper

	// JetStream
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// Session tracking
	sessions   map[string]*workflow.PlanSession
	sessionsMu sync.RWMutex

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed atomic.Int64
	sessionsCompleted atomic.Int64
	sessionsFailed    atomic.Int64
	lastActivityMu    sync.RWMutex
	lastActivity      time.Time
}

// NewComponent creates a new plan-coordinator processor.
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
	if config.SessionsBucket == "" {
		config.SessionsBucket = defaults.SessionsBucket
	}
	if config.MaxConcurrentPlanners == 0 {
		config.MaxConcurrentPlanners = defaults.MaxConcurrentPlanners
	}
	if config.PlannerTimeout == "" {
		config.PlannerTimeout = defaults.PlannerTimeout
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

	logger := deps.GetLogger()

	// Initialize context helper for centralized context building
	ctxHelper := contexthelper.New(deps.NATSClient, contexthelper.Config{
		SubjectPrefix:  config.ContextSubjectPrefix,
		ResponseBucket: config.ContextResponseBucket,
		Timeout:        config.GetContextTimeout(),
		SourceName:     "plan-coordinator",
	}, logger)

	return &Component{
		name:          "plan-coordinator",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		llmClient:     llm.NewClient(model.Global(), llm.WithLogger(logger)),
		contextHelper: ctxHelper,
		sessions:      make(map[string]*workflow.PlanSession),
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized plan-coordinator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject,
		"max_concurrent_planners", c.config.MaxConcurrentPlanners)
	return nil
}

// Start begins processing coordinator triggers.
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
		AckWait:       300 * time.Second, // Allow time for coordination
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

	c.logger.Info("plan-coordinator started",
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

// handleMessage processes a single coordinator trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the trigger
	var baseMsg message.BaseMessage
	if err := json.Unmarshal(msg.Data(), &baseMsg); err != nil {
		// Parse errors are non-retryable - bad message format
		c.logger.Error("Failed to parse message", "error", err)
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}

	// Extract trigger payload
	var trigger workflow.PlanCoordinatorTrigger
	payloadBytes, err := json.Marshal(baseMsg.Payload())
	if err != nil {
		// Marshal errors are non-retryable - programming error
		c.logger.Error("Failed to marshal payload", "error", err)
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}
	if err := json.Unmarshal(payloadBytes, &trigger); err != nil {
		// Unmarshal errors are non-retryable - bad payload
		c.logger.Error("Failed to unmarshal trigger", "error", err)
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}

	c.logger.Info("Processing plan coordinator trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Data.Slug,
		"max_planners", trigger.MaxPlanners,
		"explicit_focuses", trigger.Focuses)

	// Coordinate planning
	if err := c.coordinatePlanning(ctx, &trigger); err != nil {
		c.sessionsFailed.Add(1)
		c.logger.Error("Failed to coordinate planning",
			"request_id", trigger.RequestID,
			"slug", trigger.Data.Slug,
			"error", err)

		// Check if error is non-retryable
		if retry.IsNonRetryable(err) {
			if termErr := msg.Term(); termErr != nil {
				c.logger.Warn("Failed to Term message", "error", termErr)
			}
		} else {
			// Retryable error - NAK for retry
			if nakErr := msg.Nak(); nakErr != nil {
				c.logger.Warn("Failed to NAK message", "error", nakErr)
			}
		}
		return
	}

	c.sessionsCompleted.Add(1)

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Plan coordination completed",
		"request_id", trigger.RequestID,
		"slug", trigger.Data.Slug)
}

// coordinatePlanning orchestrates the multi-planner planning process.
func (c *Component) coordinatePlanning(ctx context.Context, trigger *workflow.PlanCoordinatorTrigger) error {
	sessionID := uuid.New().String()
	now := time.Now()

	// Create session
	session := &workflow.PlanSession{
		SessionID: sessionID,
		Slug:      trigger.Data.Slug,
		Title:     trigger.Data.Title,
		Status:    "coordinating",
		Planners:  make(map[string]*workflow.PlannerState),
		CreatedAt: now,
	}

	c.sessionsMu.Lock()
	c.sessions[sessionID] = session
	c.sessionsMu.Unlock()

	defer func() {
		c.sessionsMu.Lock()
		delete(c.sessions, sessionID)
		c.sessionsMu.Unlock()
	}()

	// Step 1: Use LLM to decide focus areas (or use explicit focuses)
	focuses, err := c.determineFocusAreas(ctx, trigger)
	if err != nil {
		return fmt.Errorf("determine focus areas: %w", err)
	}

	c.logger.Info("Determined focus areas",
		"session_id", sessionID,
		"focus_count", len(focuses),
		"focuses", focusAreas(focuses))

	// Step 2: Spawn planners concurrently and collect results
	session.Status = "planning"
	plannerResults, err := c.runPlanners(ctx, trigger, session, sessionID, focuses)
	if err != nil {
		return err
	}

	// Step 3: Synthesize results
	session.Status = "synthesizing"
	synthesized, err := c.synthesizeResults(ctx, trigger, plannerResults)
	if err != nil {
		return fmt.Errorf("synthesize results: %w", err)
	}

	// Step 4: Save the plan
	if err := c.savePlan(ctx, trigger, synthesized); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	// Publish result notification
	if err := c.publishResult(ctx, trigger, synthesized, len(plannerResults)); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"error", err)
	}

	session.Status = "complete"
	completedAt := time.Now()
	session.CompletedAt = &completedAt

	return nil
}

// runPlanners spawns planners concurrently and collects their results.
func (c *Component) runPlanners(
	ctx context.Context,
	trigger *workflow.PlanCoordinatorTrigger,
	session *workflow.PlanSession,
	sessionID string,
	focuses []*FocusArea,
) ([]workflow.PlannerResult, error) {
	results := make(chan *workflow.PlannerResult, len(focuses))
	errors := make(chan error, len(focuses))

	for _, focus := range focuses {
		plannerID := uuid.New().String()
		state := &workflow.PlannerState{
			ID:        plannerID,
			FocusArea: focus.Area,
			Status:    "pending",
		}
		session.Planners[plannerID] = state

		go func(f *FocusArea, pID string) {
			result, err := c.spawnPlanner(ctx, trigger, sessionID, pID, f)
			if err != nil {
				// Use select with context check to prevent goroutine leak
				// if receiver exits early (timeout/cancellation)
				select {
				case errors <- fmt.Errorf("planner %s (%s): %w", pID, f.Area, err):
				case <-ctx.Done():
				}
				return
			}
			select {
			case results <- result:
			case <-ctx.Done():
			}
		}(focus, plannerID)
	}

	// Collect results with timeout
	timeout, err := time.ParseDuration(c.config.PlannerTimeout)
	if err != nil {
		timeout = 120 * time.Second
	}
	deadline := time.After(timeout * time.Duration(len(focuses)))

	var plannerResults []workflow.PlannerResult
	var plannerErrors []error

	for i := 0; i < len(focuses); i++ {
		select {
		case result := <-results:
			plannerResults = append(plannerResults, *result)
		case err := <-errors:
			plannerErrors = append(plannerErrors, err)
		case <-deadline:
			return nil, fmt.Errorf("planner timeout after %v", timeout*time.Duration(len(focuses)))
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if len(plannerErrors) > 0 {
		c.logger.Warn("Some planners failed",
			"session_id", sessionID,
			"error_count", len(plannerErrors),
			"success_count", len(plannerResults))
	}

	if len(plannerResults) == 0 {
		return nil, fmt.Errorf("all planners failed: %v", plannerErrors)
	}

	return plannerResults, nil
}

// FocusArea represents a planning focus area determined by the coordinator.
type FocusArea struct {
	Area        string
	Description string
	Hints       []string
	Context     *workflow.PlannerContext
}

// focusAreas returns a slice of focus area names.
func focusAreas(focuses []*FocusArea) []string {
	result := make([]string, len(focuses))
	for i, f := range focuses {
		result[i] = f.Area
	}
	return result
}

// determineFocusAreas decides what focus areas to use for planning.
// It follows the graph-first pattern by requesting context from the centralized context-builder.
func (c *Component) determineFocusAreas(ctx context.Context, trigger *workflow.PlanCoordinatorTrigger) ([]*FocusArea, error) {
	// If explicit focuses provided, use them
	if len(trigger.Focuses) > 0 {
		focuses := make([]*FocusArea, len(trigger.Focuses))
		for i, f := range trigger.Focuses {
			focuses[i] = &FocusArea{
				Area:        f,
				Description: fmt.Sprintf("Analyze from %s perspective", f),
			}
		}
		return focuses, nil
	}

	// Step 1: Request coordination context from centralized context-builder (graph-first)
	var graphContext string
	resp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType: contextbuilder.TaskTypePlanning, // Coordination is part of planning
		Topic:    trigger.Data.Title,
	})
	if resp != nil {
		graphContext = contexthelper.FormatContextResponse(resp)
		c.logger.Info("Built coordination context via context-builder",
			"title", trigger.Data.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"title", trigger.Data.Title)
	}

	// Step 2: Use LLM to determine focus areas
	systemPrompt := c.loadPrompt(
		c.config.Prompts.GetCoordinatorSystem(),
		prompts.PlanCoordinatorSystemPrompt(),
	)

	var userPrompt string
	if graphContext != "" {
		userPrompt = fmt.Sprintf(`Analyze this task and determine the optimal focus areas for planning:

**Title:** %s
**Description:** %s

## Codebase Context

The following context from the knowledge graph provides information about the existing codebase structure:

%s

Based on the task complexity and codebase context, decide:
1. How many planners to spawn (1-3)
2. What focus areas each should cover

Respond with a JSON object:
`+"```json"+`
{
  "focus_areas": [
    {
      "area": "focus area name",
      "description": "what to analyze",
      "hints": ["file patterns", "keywords"]
    }
  ]
}
`+"```", trigger.Data.Title, trigger.Data.Description, graphContext)
	} else {
		userPrompt = fmt.Sprintf(`Analyze this task and determine the optimal focus areas for planning:

**Title:** %s
**Description:** %s

Based on the task complexity, decide:
1. How many planners to spawn (1-3)
2. What focus areas each should cover

Respond with a JSON object:
`+"```json"+`
{
  "focus_areas": [
    {
      "area": "focus area name",
      "description": "what to analyze",
      "hints": ["file patterns", "keywords"]
    }
  ]
}
`+"```", trigger.Data.Title, trigger.Data.Description)
	}

	content, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		// Fall back to single planner
		c.logger.Warn("Failed to determine focus areas via LLM, falling back to single planner",
			"error", err)
		return []*FocusArea{{
			Area:        "general",
			Description: "General analysis of the task",
		}}, nil
	}

	// Parse focus areas from response
	focuses, err := c.parseFocusAreas(content)
	if err != nil {
		c.logger.Warn("Failed to parse focus areas, falling back to single planner",
			"error", err)
		return []*FocusArea{{
			Area:        "general",
			Description: "General analysis of the task",
		}}, nil
	}

	// Limit to max concurrent planners
	maxPlanners := c.config.MaxConcurrentPlanners
	if trigger.MaxPlanners > 0 && trigger.MaxPlanners < maxPlanners {
		maxPlanners = trigger.MaxPlanners
	}
	if len(focuses) > maxPlanners {
		focuses = focuses[:maxPlanners]
	}

	return focuses, nil
}

// parseFocusAreas extracts focus areas from LLM response.
func (c *Component) parseFocusAreas(content string) ([]*FocusArea, error) {
	jsonContent := extractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var resp struct {
		FocusAreas []struct {
			Area        string   `json:"area"`
			Description string   `json:"description"`
			Hints       []string `json:"hints,omitempty"`
		} `json:"focus_areas"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if len(resp.FocusAreas) == 0 {
		return nil, fmt.Errorf("no focus areas in response")
	}

	focuses := make([]*FocusArea, len(resp.FocusAreas))
	for i, fa := range resp.FocusAreas {
		focuses[i] = &FocusArea{
			Area:        fa.Area,
			Description: fa.Description,
			Hints:       fa.Hints,
		}
	}

	return focuses, nil
}

// spawnPlanner spawns a focused planner and waits for its result.
func (c *Component) spawnPlanner(
	ctx context.Context,
	trigger *workflow.PlanCoordinatorTrigger,
	sessionID, plannerID string,
	focus *FocusArea,
) (*workflow.PlannerResult, error) {
	// Update planner state
	c.sessionsMu.Lock()
	if session, ok := c.sessions[sessionID]; ok {
		if state, ok := session.Planners[plannerID]; ok {
			state.Status = "running"
			now := time.Now()
			state.StartedAt = &now
		}
	}
	c.sessionsMu.Unlock()

	// Build focused prompt
	systemPrompt := prompts.PlannerFocusedSystemPrompt(focus.Area)
	userPrompt := prompts.PlannerFocusedPrompt(
		focus.Area,
		focus.Description,
		trigger.Data.Title,
		focus.Hints,
		toContextInfo(focus.Context),
	)

	// Call LLM directly (simpler than publishing to planner processor)
	content, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		c.markPlannerFailed(sessionID, plannerID, err.Error())
		return nil, err
	}

	// Parse result
	result, err := c.parsePlannerResult(content, plannerID, focus.Area)
	if err != nil {
		c.markPlannerFailed(sessionID, plannerID, err.Error())
		return nil, err
	}

	// Update planner state
	c.sessionsMu.Lock()
	if session, ok := c.sessions[sessionID]; ok {
		if state, ok := session.Planners[plannerID]; ok {
			state.Status = "completed"
			state.Result = result
			now := time.Now()
			state.CompletedAt = &now
		}
	}
	c.sessionsMu.Unlock()

	return result, nil
}

// toContextInfo converts PlannerContext to prompt context info.
func toContextInfo(ctx *workflow.PlannerContext) *prompts.PlannerContextInfo {
	if ctx == nil {
		return nil
	}
	return &prompts.PlannerContextInfo{
		Entities: ctx.Entities,
		Files:    ctx.Files,
		Summary:  ctx.Summary,
	}
}

// markPlannerFailed updates a planner's state to failed.
func (c *Component) markPlannerFailed(sessionID, plannerID, errMsg string) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	if session, ok := c.sessions[sessionID]; ok {
		if state, ok := session.Planners[plannerID]; ok {
			state.Status = "failed"
			state.Error = errMsg
			now := time.Now()
			state.CompletedAt = &now
		}
	}
}

// parsePlannerResult extracts a planner result from LLM response.
func (c *Component) parsePlannerResult(content, plannerID, focusArea string) (*workflow.PlannerResult, error) {
	jsonContent := extractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var parsed struct {
		Goal    string `json:"goal"`
		Context string `json:"context"`
		Scope   struct {
			Include    []string `json:"include,omitempty"`
			Exclude    []string `json:"exclude,omitempty"`
			DoNotTouch []string `json:"do_not_touch,omitempty"`
		} `json:"scope"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &parsed); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	if parsed.Goal == "" {
		return nil, fmt.Errorf("missing goal in response")
	}

	return &workflow.PlannerResult{
		PlannerID: plannerID,
		FocusArea: focusArea,
		Goal:      parsed.Goal,
		Context:   parsed.Context,
		Scope: workflow.Scope{
			Include:    parsed.Scope.Include,
			Exclude:    parsed.Scope.Exclude,
			DoNotTouch: parsed.Scope.DoNotTouch,
		},
	}, nil
}

// synthesizeResults combines multiple planner results into a unified plan.
func (c *Component) synthesizeResults(
	ctx context.Context,
	_ *workflow.PlanCoordinatorTrigger,
	results []workflow.PlannerResult,
) (*SynthesizedPlan, error) {
	// If only one result, use it directly
	if len(results) == 1 {
		return &SynthesizedPlan{
			Goal:    results[0].Goal,
			Context: results[0].Context,
			Scope:   results[0].Scope,
		}, nil
	}

	// Use LLM to synthesize multiple results
	systemPrompt := "You are synthesizing multiple planning perspectives into a unified development plan."
	userPrompt := prompts.PlanCoordinatorSynthesisPrompt(results)

	content, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		// Fall back to simple merge
		return c.simpleMerge(results), nil
	}

	// Parse synthesized result
	synthesized, err := c.parseSynthesizedPlan(content)
	if err != nil {
		return c.simpleMerge(results), nil
	}

	return synthesized, nil
}

// SynthesizedPlan is the final merged plan from multiple planners.
type SynthesizedPlan struct {
	Goal    string
	Context string
	Scope   workflow.Scope
}

// simpleMerge performs a basic merge of planner results.
func (c *Component) simpleMerge(results []workflow.PlannerResult) *SynthesizedPlan {
	var goals, contexts []string
	var include, exclude, doNotTouch []string

	for _, r := range results {
		goals = append(goals, fmt.Sprintf("[%s] %s", r.FocusArea, r.Goal))
		if r.Context != "" {
			contexts = append(contexts, fmt.Sprintf("[%s] %s", r.FocusArea, r.Context))
		}
		include = append(include, r.Scope.Include...)
		exclude = append(exclude, r.Scope.Exclude...)
		doNotTouch = append(doNotTouch, r.Scope.DoNotTouch...)
	}

	return &SynthesizedPlan{
		Goal:    joinWithNewlines(goals),
		Context: joinWithNewlines(contexts),
		Scope: workflow.Scope{
			Include:    unique(include),
			Exclude:    unique(exclude),
			DoNotTouch: unique(doNotTouch),
		},
	}
}

// parseSynthesizedPlan extracts a synthesized plan from LLM response.
func (c *Component) parseSynthesizedPlan(content string) (*SynthesizedPlan, error) {
	jsonContent := extractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var parsed struct {
		Goal    string `json:"goal"`
		Context string `json:"context"`
		Scope   struct {
			Include    []string `json:"include,omitempty"`
			Exclude    []string `json:"exclude,omitempty"`
			DoNotTouch []string `json:"do_not_touch,omitempty"`
		} `json:"scope"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &parsed); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return &SynthesizedPlan{
		Goal:    parsed.Goal,
		Context: parsed.Context,
		Scope: workflow.Scope{
			Include:    parsed.Scope.Include,
			Exclude:    parsed.Scope.Exclude,
			DoNotTouch: parsed.Scope.DoNotTouch,
		},
	}, nil
}

// savePlan saves the synthesized plan to the plan.json file.
func (c *Component) savePlan(ctx context.Context, trigger *workflow.PlanCoordinatorTrigger, plan *SynthesizedPlan) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			// Filesystem errors are non-retryable
			return retry.NonRetryable(fmt.Errorf("get working directory: %w", err))
		}
	}

	manager := workflow.NewManager(repoRoot)

	// Load existing plan
	existingPlan, err := manager.LoadPlan(ctx, trigger.Data.Slug)
	if err != nil {
		// Plan not found is non-retryable
		return retry.NonRetryable(fmt.Errorf("load plan: %w", err))
	}

	// Update with synthesized content
	existingPlan.Goal = plan.Goal
	existingPlan.Context = plan.Context
	existingPlan.Scope = plan.Scope

	// Save the updated plan
	return manager.SavePlan(ctx, existingPlan)
}

// callLLM makes an LLM API call using the centralized llm.Client.
func (c *Component) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	temperature := 0.7
	resp, err := c.llmClient.Complete(ctx, llm.Request{
		Capability: capability,
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: &temperature,
		MaxTokens:   4096,
	})
	if err != nil {
		return "", fmt.Errorf("LLM completion: %w", err)
	}

	c.logger.Debug("LLM response received",
		"model", resp.Model,
		"tokens_used", resp.TokensUsed)

	return resp.Content, nil
}

// loadPrompt loads a custom prompt from file or returns the default.
func (c *Component) loadPrompt(configPath, defaultPrompt string) string {
	if configPath == "" {
		return defaultPrompt
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		c.logger.Warn("Failed to load custom prompt, using default",
			"path", configPath, "error", err)
		return defaultPrompt
	}
	return string(content)
}

// CoordinatorResult is the result payload for plan coordination.
type CoordinatorResult struct {
	RequestID    string `json:"request_id"`
	Slug         string `json:"slug"`
	PlannerCount int    `json:"planner_count"`
	Status       string `json:"status"`
}

// Schema implements message.Payload.
func (r *CoordinatorResult) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "result", Version: "v1"}
}

// Validate implements message.Payload.
func (r *CoordinatorResult) Validate() error { return nil }

// MarshalJSON implements json.Marshaler.
func (r *CoordinatorResult) MarshalJSON() ([]byte, error) {
	type Alias CoordinatorResult
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *CoordinatorResult) UnmarshalJSON(data []byte) error {
	type Alias CoordinatorResult
	return json.Unmarshal(data, (*Alias)(r))
}

// publishResult publishes a success notification for the coordination.
func (c *Component) publishResult(ctx context.Context, trigger *workflow.PlanCoordinatorTrigger, _ *SynthesizedPlan, plannerCount int) error {
	result := &CoordinatorResult{
		RequestID:    trigger.RequestID,
		Slug:         trigger.Data.Slug,
		PlannerCount: plannerCount,
		Status:       "completed",
	}

	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "result", Version: "v1"},
		result,
		"plan-coordinator",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	subject := fmt.Sprintf("workflow.result.plan-coordinator.%s", trigger.Data.Slug)
	return c.natsClient.Publish(ctx, subject, data)
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

	c.logger.Info("plan-coordinator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"sessions_completed", c.sessionsCompleted.Load(),
		"sessions_failed", c.sessionsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "plan-coordinator",
		Type:        "processor",
		Description: "Coordinates concurrent planners for parallel plan generation",
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
	return configSchema
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
		ErrorCount: int(c.sessionsFailed.Load()),
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

// GetCoordinatorSystem returns the coordinator system prompt path.
func (p *PromptsConfig) GetCoordinatorSystem() string {
	if p == nil {
		return ""
	}
	return p.CoordinatorSystem
}

// Pre-compiled regex patterns for JSON extraction.
var (
	jsonBlockPattern  = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(\\{.*?\\})\\s*```")
	jsonObjectPattern = regexp.MustCompile(`(?s)\{[\s\S]*\}`)
)

// extractJSON extracts JSON content from a string, handling markdown code blocks.
func extractJSON(content string) string {
	// Try to find JSON code block
	if matches := jsonBlockPattern.FindStringSubmatch(content); len(matches) > 1 {
		return matches[1]
	}

	// Try to find raw JSON object
	if matches := jsonObjectPattern.FindString(content); matches != "" {
		return matches
	}

	return ""
}

// joinWithNewlines joins strings with double newlines.
func joinWithNewlines(strs []string) string {
	return strings.Join(strs, "\n\n")
}

// unique returns unique strings from a slice.
func unique(strs []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
