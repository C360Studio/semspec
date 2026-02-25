// Package plancoordinator provides a processor that coordinates concurrent planners
// for parallel plan generation using LLM-driven focus area selection.
package plancoordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/llm"
	"github.com/c360studio/semspec/model"
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/retry"
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// llmCompleter is the subset of the LLM client used by plan-coordinator.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the plan-coordinator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient llmCompleter

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

// ---------------------------------------------------------------------------
// Participant interface (plan-coordinator is an entry point, not inside a workflow)
// ---------------------------------------------------------------------------

// Compile-time check that Component implements Participant interface.
var _ semstreamsWorkflow.Participant = (*Component)(nil)

// WorkflowID returns the workflow this component participates in.
// Plan-coordinator is an entry point, so this is for interface completeness.
func (c *Component) WorkflowID() string {
	return "plan-coordination"
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.CoordinationCoordinated
}

// StateManager returns nil - plan-coordinator is an entry point, not inside a reactive workflow.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
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
		llmClient:     llm.NewClient(model.Global(), llm.WithLogger(logger), llm.WithCallStore(llm.GlobalCallStore())),
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

	// Parse the trigger (handles both BaseMessage-wrapped and raw JSON from
	// workflow-processor publish_async). Use ParseNATSMessage to preserve TraceID
	// which gets lost when going through the semstreams message registry.
	trigger, err := workflow.ParseNATSMessage[workflow.PlanCoordinatorTrigger](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse trigger", "error", err)
		if err := msg.Term(); err != nil {
			c.logger.Warn("Failed to Term message", "error", err)
		}
		return
	}

	c.logger.Info("Processing plan coordinator trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"max_planners", trigger.MaxPlanners,
		"explicit_focuses", trigger.Focuses,
		"trace_id", trigger.TraceID)

	// Inject trace context for LLM call tracking
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	// Coordinate planning
	if err := c.coordinatePlanning(llmCtx, trigger); err != nil {
		c.sessionsFailed.Add(1)
		c.logger.Error("Failed to coordinate planning",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
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
		"slug", trigger.Slug)
}

// coordinatePlanning orchestrates the multi-planner planning process.
func (c *Component) coordinatePlanning(ctx context.Context, trigger *workflow.PlanCoordinatorTrigger) error {
	sessionID := uuid.New().String()
	now := time.Now()

	// Create session
	session := &workflow.PlanSession{
		SessionID: sessionID,
		Slug:      trigger.Slug,
		Title:     trigger.Title,
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
	plannerResults, plannerRequestIDs, err := c.runPlanners(ctx, trigger, session, sessionID, focuses)
	if err != nil {
		return err
	}

	c.logger.Info("Planner results collected",
		"session_id", sessionID,
		"result_count", len(plannerResults),
		"slug", trigger.Slug)
	for i, r := range plannerResults {
		goalPreview := r.Goal
		if len(goalPreview) > 100 {
			goalPreview = goalPreview[:100] + "..."
		}
		c.logger.Info("Planner result",
			"index", i,
			"focus", r.FocusArea,
			"goal_length", len(r.Goal),
			"goal_preview", goalPreview,
			"context_length", len(r.Context),
			"scope_include", len(r.Scope.Include),
			"scope_exclude", len(r.Scope.Exclude))
	}

	// Step 3: Synthesize results
	session.Status = "synthesizing"
	synthesized, synthesisRequestID, err := c.synthesizeResults(ctx, trigger, plannerResults)
	if err != nil {
		return fmt.Errorf("synthesize results: %w", err)
	}

	// Collect all LLM request IDs from planners and synthesis
	var allLLMRequestIDs []string
	allLLMRequestIDs = append(allLLMRequestIDs, plannerRequestIDs...)
	if synthesisRequestID != "" {
		allLLMRequestIDs = append(allLLMRequestIDs, synthesisRequestID)
	}

	synthGoalPreview := synthesized.Goal
	if len(synthGoalPreview) > 200 {
		synthGoalPreview = synthGoalPreview[:200] + "..."
	}
	c.logger.Info("Plan synthesized",
		"session_id", sessionID,
		"slug", trigger.Slug,
		"goal_length", len(synthesized.Goal),
		"goal_preview", synthGoalPreview,
		"context_length", len(synthesized.Context),
		"scope_include", len(synthesized.Scope.Include))

	// Step 4: Save the plan
	if err := c.savePlan(ctx, trigger, synthesized); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	c.logger.Info("Plan saved to disk",
		"session_id", sessionID,
		"slug", trigger.Slug)

	// Publish result notification
	if err := c.publishResult(ctx, trigger, synthesized, len(plannerResults), allLLMRequestIDs); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"error", err)
	}

	session.Status = "complete"
	completedAt := time.Now()
	session.CompletedAt = &completedAt

	return nil
}

// plannerOutcome bundles a planner result with the LLM request ID used.
type plannerOutcome struct {
	result       *workflow.PlannerResult
	llmRequestID string
}

// runPlanners spawns planners concurrently and collects their results.
// It returns the planner results and all LLM request IDs generated during planning.
func (c *Component) runPlanners(
	ctx context.Context,
	trigger *workflow.PlanCoordinatorTrigger,
	session *workflow.PlanSession,
	sessionID string,
	focuses []*FocusArea,
) ([]workflow.PlannerResult, []string, error) {
	outcomes := make(chan plannerOutcome, len(focuses))
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
			result, llmRequestID, err := c.spawnPlanner(ctx, trigger, sessionID, pID, f)
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
			case outcomes <- plannerOutcome{result: result, llmRequestID: llmRequestID}:
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
	var llmRequestIDs []string

	for i := 0; i < len(focuses); i++ {
		select {
		case outcome := <-outcomes:
			plannerResults = append(plannerResults, *outcome.result)
			if outcome.llmRequestID != "" {
				llmRequestIDs = append(llmRequestIDs, outcome.llmRequestID)
			}
		case err := <-errors:
			plannerErrors = append(plannerErrors, err)
		case <-deadline:
			return nil, llmRequestIDs, fmt.Errorf("planner timeout after %v", timeout*time.Duration(len(focuses)))
		case <-ctx.Done():
			return nil, llmRequestIDs, ctx.Err()
		}
	}

	if len(plannerErrors) > 0 {
		c.logger.Warn("Some planners failed",
			"session_id", sessionID,
			"error_count", len(plannerErrors),
			"success_count", len(plannerResults))
		for i, err := range plannerErrors {
			c.logger.Warn("Planner error detail",
				"session_id", sessionID,
				"error_index", i,
				"error", err.Error())
		}
	}

	if len(plannerResults) == 0 {
		return nil, llmRequestIDs, fmt.Errorf("all planners failed: %v", plannerErrors)
	}

	c.logger.Info("All planners completed",
		"session_id", sessionID,
		"success_count", len(plannerResults),
		"error_count", len(plannerErrors),
		"total_focuses", len(focuses))

	return plannerResults, llmRequestIDs, nil
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
		Topic:    trigger.Title,
	})
	if resp != nil {
		graphContext = contexthelper.FormatContextResponse(resp)
		c.logger.Info("Built coordination context via context-builder",
			"title", trigger.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"title", trigger.Title)
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
`+"```", trigger.Title, trigger.Description, graphContext)
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
`+"```", trigger.Title, trigger.Description)
	}

	content, _, err := c.callLLM(ctx, systemPrompt, userPrompt)
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
	jsonContent := llm.ExtractJSON(content)
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
// It returns the planner result and the LLM request ID used for the call.
func (c *Component) spawnPlanner(
	ctx context.Context,
	trigger *workflow.PlanCoordinatorTrigger,
	sessionID, plannerID string,
	focus *FocusArea,
) (*workflow.PlannerResult, string, error) {
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
		trigger.Title,
		focus.Hints,
		toContextInfo(focus.Context),
	)

	// Call LLM directly (simpler than publishing to planner processor)
	content, llmRequestID, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		c.logger.Warn("Planner LLM call failed",
			"planner_id", plannerID,
			"focus", focus.Area,
			"error", err)
		c.markPlannerFailed(sessionID, plannerID, err.Error())
		return nil, "", err
	}

	contentPreview := content
	if len(contentPreview) > 300 {
		contentPreview = contentPreview[:300] + "..."
	}
	c.logger.Info("Planner LLM response received",
		"planner_id", plannerID,
		"focus", focus.Area,
		"response_length", len(content),
		"response_preview", contentPreview)

	// Parse result
	result, err := c.parsePlannerResult(content, plannerID, focus.Area)
	if err != nil {
		c.logger.Warn("Failed to parse planner result",
			"planner_id", plannerID,
			"focus", focus.Area,
			"error", err,
			"raw_content_length", len(content))
		c.markPlannerFailed(sessionID, plannerID, err.Error())
		return nil, llmRequestID, err
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

	return result, llmRequestID, nil
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
	jsonContent := llm.ExtractJSON(content)
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
// It returns the synthesized plan and the LLM request ID used for synthesis (empty if no LLM call was made).
func (c *Component) synthesizeResults(
	ctx context.Context,
	_ *workflow.PlanCoordinatorTrigger,
	results []workflow.PlannerResult,
) (*SynthesizedPlan, string, error) {
	// If only one result, use it directly (no LLM call needed)
	if len(results) == 1 {
		return &SynthesizedPlan{
			Goal:    results[0].Goal,
			Context: results[0].Context,
			Scope:   results[0].Scope,
		}, "", nil
	}

	// Use LLM to synthesize multiple results
	systemPrompt := "You are synthesizing multiple planning perspectives into a unified development plan."
	userPrompt := prompts.PlanCoordinatorSynthesisPrompt(results)

	content, llmRequestID, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		c.logger.Warn("Synthesis LLM call failed, falling back to simple merge", "error", err)
		return c.simpleMerge(results), "", nil
	}

	synthPreview := content
	if len(synthPreview) > 300 {
		synthPreview = synthPreview[:300] + "..."
	}
	c.logger.Info("Synthesis LLM response received",
		"response_length", len(content),
		"response_preview", synthPreview)

	// Parse synthesized result
	synthesized, err := c.parseSynthesizedPlan(content)
	if err != nil {
		c.logger.Warn("Synthesis parse failed, falling back to simple merge",
			"error", err,
			"raw_content_length", len(content))
		return c.simpleMerge(results), llmRequestID, nil
	}

	// Guard against empty synthesis — fall back to simple merge if LLM returned vacuous content
	if synthesized.Goal == "" {
		c.logger.Warn("Synthesis returned empty goal, falling back to simple merge")
		return c.simpleMerge(results), llmRequestID, nil
	}

	return synthesized, llmRequestID, nil
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
	jsonContent := llm.ExtractJSON(content)
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

	c.logger.Info("Saving plan to disk",
		"slug", trigger.Slug,
		"repo_root", repoRoot,
		"goal_length", len(plan.Goal),
		"context_length", len(plan.Context),
		"goal_empty", plan.Goal == "",
		"context_empty", plan.Context == "")

	manager := workflow.NewManager(repoRoot)

	// Load existing plan
	existingPlan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		// Plan not found is non-retryable
		return retry.NonRetryable(fmt.Errorf("load plan: %w", err))
	}

	c.logger.Info("Loaded existing plan for update",
		"slug", trigger.Slug,
		"existing_goal_length", len(existingPlan.Goal),
		"existing_id", existingPlan.ID,
		"project_id", existingPlan.ProjectID)

	// Update with synthesized content
	existingPlan.Goal = plan.Goal
	existingPlan.Context = plan.Context
	existingPlan.Scope = plan.Scope

	// Record trace ID for trajectory tracking
	if trigger.TraceID != "" && !slices.Contains(existingPlan.ExecutionTraceIDs, trigger.TraceID) {
		existingPlan.ExecutionTraceIDs = append(existingPlan.ExecutionTraceIDs, trigger.TraceID)
	}

	// Save the updated plan
	if err := manager.SavePlan(ctx, existingPlan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	c.logger.Info("Plan written successfully",
		"slug", trigger.Slug,
		"final_goal_length", len(existingPlan.Goal),
		"final_context_length", len(existingPlan.Context))

	return nil
}

// callLLM makes an LLM API call using the centralized llm.Client.
// It returns the response content, the LLM request ID for trajectory tracking, and any error.
func (c *Component) callLLM(ctx context.Context, systemPrompt, userPrompt string) (string, string, error) {
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
		return "", "", fmt.Errorf("LLM completion: %w", err)
	}

	c.logger.Debug("LLM response received",
		"model", resp.Model,
		"tokens_used", resp.TokensUsed)

	return resp.Content, resp.RequestID, nil
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

// CoordinatorResultType is the message type for coordinator results.
var CoordinatorResultType = message.Type{Domain: "workflow", Category: "coordinator-result", Version: "v1"}

// CoordinatorResult is the result payload for plan coordination.
type CoordinatorResult struct {
	RequestID     string   `json:"request_id"`
	TraceID       string   `json:"trace_id,omitempty"`
	Slug          string   `json:"slug"`
	PlannerCount  int      `json:"planner_count"`
	Status        string   `json:"status"`
	LLMRequestIDs []string `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *CoordinatorResult) Schema() message.Type {
	return CoordinatorResultType
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
// Result is published to workflow.result.plan-coordinator.<slug> for observability.
func (c *Component) publishResult(ctx context.Context, trigger *workflow.PlanCoordinatorTrigger, _ *SynthesizedPlan, plannerCount int, llmRequestIDs []string) error {
	result := &CoordinatorResult{
		RequestID:     trigger.RequestID,
		TraceID:       trigger.TraceID,
		Slug:          trigger.Slug,
		PlannerCount:  plannerCount,
		Status:        "completed",
		LLMRequestIDs: llmRequestIDs,
	}

	// Wrap in BaseMessage and publish to well-known subject for observability
	baseMsg := message.NewBaseMessage(result.Schema(), result, c.name)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	resultSubject := fmt.Sprintf("workflow.result.plan-coordinator.%s", trigger.Slug)
	if err := c.natsClient.Publish(ctx, resultSubject, data); err != nil {
		return fmt.Errorf("publish result: %w", err)
	}
	c.logger.Info("Published plan-coordinator result",
		"slug", trigger.Slug,
		"request_id", trigger.RequestID,
		"trace_id", trigger.TraceID,
		"subject", resultSubject,
		"planner_count", plannerCount)
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
