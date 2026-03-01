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
	"github.com/c360studio/semspec/workflow/reactive"
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
//
// The plan-coordinator participates in the coordination-loop reactive workflow.
// It handles three dispatch subjects:
//   - workflow.async.coordination-focus — focus area determination + planner dispatch
//   - workflow.async.coordination-planner — individual planner LLM execution
//   - workflow.async.coordination-synthesis — result synthesis + plan saving
//
// State is KV-backed via the reactive engine. The engine is the single KV
// writer for planner results (no CAS conflicts). The focus and synthesis
// handlers update KV directly (single writer per execution step).
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient llmCompleter

	// Centralized context building via context-builder
	contextHelper *contexthelper.Helper

	// JetStream
	stream            jetstream.Stream
	focusConsumer     jetstream.Consumer
	plannerConsumer   jetstream.Consumer
	synthesisConsumer jetstream.Consumer

	// KV-backed state (shared with reactive engine)
	stateBucket jetstream.KeyValue

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
// Participant interface
// ---------------------------------------------------------------------------

// Compile-time check that Component implements Participant interface.
var _ semstreamsWorkflow.Participant = (*Component)(nil)

// WorkflowID returns the workflow this component participates in.
func (c *Component) WorkflowID() string {
	return reactive.CoordinationLoopWorkflowID
}

// Phase returns the completion phase this component represents.
func (c *Component) Phase() string {
	return phases.CoordinationSynthesized
}

// StateManager returns nil - this component updates state directly via KV bucket.
// The reactive engine manages state; we update it on completion of each step.
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
	if config.StateBucket == "" {
		config.StateBucket = defaults.StateBucket
	}
	if config.FocusSubject == "" {
		config.FocusSubject = defaults.FocusSubject
	}
	if config.PlannerSubject == "" {
		config.PlannerSubject = defaults.PlannerSubject
	}
	if config.SynthesisSubject == "" {
		config.SynthesisSubject = defaults.SynthesisSubject
	}
	if config.PlannerResultSubject == "" {
		config.PlannerResultSubject = defaults.PlannerResultSubject
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
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized plan-coordinator",
		"stream", c.config.StreamName,
		"state_bucket", c.config.StateBucket,
		"max_concurrent_planners", c.config.MaxConcurrentPlanners)
	return nil
}

// Start begins processing coordination dispatch messages.
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

	// Get state bucket (shared with reactive engine)
	stateBucket, err := js.KeyValue(subCtx, c.config.StateBucket)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get state bucket %s: %w", c.config.StateBucket, err)
	}
	c.stateBucket = stateBucket

	// Create consumers for each dispatch subject
	focusConsumer, err := stream.CreateOrUpdateConsumer(subCtx, jetstream.ConsumerConfig{
		Durable:       "plan-coordinator-focus",
		FilterSubject: c.config.FocusSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       300 * time.Second,
		MaxDeliver:    3,
	})
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create focus consumer: %w", err)
	}
	c.focusConsumer = focusConsumer

	plannerConsumer, err := stream.CreateOrUpdateConsumer(subCtx, jetstream.ConsumerConfig{
		Durable:       "plan-coordinator-planner",
		FilterSubject: c.config.PlannerSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       300 * time.Second,
		MaxDeliver:    3,
	})
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create planner consumer: %w", err)
	}
	c.plannerConsumer = plannerConsumer

	synthesisConsumer, err := stream.CreateOrUpdateConsumer(subCtx, jetstream.ConsumerConfig{
		Durable:       "plan-coordinator-synthesis",
		FilterSubject: c.config.SynthesisSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       300 * time.Second,
		MaxDeliver:    3,
	})
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create synthesis consumer: %w", err)
	}
	c.synthesisConsumer = synthesisConsumer

	// Start consuming from all three subjects
	go c.consumeLoop(subCtx, c.focusConsumer, "focus", c.handleFocusMessage)
	go c.consumeLoop(subCtx, c.plannerConsumer, "planner", c.handlePlannerMessage)
	go c.consumeLoop(subCtx, c.synthesisConsumer, "synthesis", c.handleSynthesisMessage)

	c.logger.Info("plan-coordinator started",
		"stream", c.config.StreamName,
		"state_bucket", c.config.StateBucket,
		"focus_subject", c.config.FocusSubject,
		"planner_subject", c.config.PlannerSubject,
		"synthesis_subject", c.config.SynthesisSubject)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// consumeLoop continuously consumes messages from a JetStream consumer.
func (c *Component) consumeLoop(ctx context.Context, consumer jetstream.Consumer, name string, handler func(context.Context, jetstream.Msg)) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Debug("Fetch timeout or error", "consumer", name, "error", err)
			continue
		}

		for msg := range msgs.Messages() {
			handler(ctx, msg)
		}

		if msgs.Error() != nil && msgs.Error() != context.DeadlineExceeded {
			c.logger.Warn("Message fetch error", "consumer", name, "error", msgs.Error())
		}
	}
}

// ---------------------------------------------------------------------------
// Focus handler — determines focus areas and dispatches planner messages
// ---------------------------------------------------------------------------

// handleFocusMessage processes a focus determination dispatch from the reactive engine.
// It determines focus areas (via LLM or explicit), dispatches N planner messages,
// and transitions state to planners_dispatched.
func (c *Component) handleFocusMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK focus message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := reactive.ParseReactivePayload[reactive.PlanCoordinatorRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse focus dispatch", "error", err)
		if err := msg.Term(); err != nil {
			c.logger.Warn("Failed to Term message", "error", err)
		}
		return
	}

	c.logger.Info("Processing focus determination",
		"execution_id", trigger.ExecutionID,
		"slug", trigger.Slug,
		"explicit_focuses", trigger.FocusAreas)

	// Inject trace context
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	// Step 1: Determine focus areas
	focuses, err := c.determineFocusAreas(llmCtx, trigger)
	if err != nil {
		c.logger.Error("Focus determination failed",
			"execution_id", trigger.ExecutionID,
			"slug", trigger.Slug,
			"error", err)
		c.transitionToFailure(ctx, trigger.ExecutionID, phases.CoordinationFocusFailed, err.Error())
		if retry.IsNonRetryable(err) {
			if termErr := msg.Term(); termErr != nil {
				c.logger.Warn("Failed to Term message", "error", termErr)
			}
		} else {
			if nakErr := msg.Nak(); nakErr != nil {
				c.logger.Warn("Failed to NAK message", "error", nakErr)
			}
		}
		return
	}

	c.logger.Info("Determined focus areas",
		"execution_id", trigger.ExecutionID,
		"focus_count", len(focuses),
		"focuses", focusAreas(focuses))

	// Step 2: Dispatch N planner messages
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream for planner dispatch", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	for _, focus := range focuses {
		plannerID := uuid.New().String()
		plannerMsg := &reactive.CoordinationPlannerMessage{
			ExecutionID:      trigger.ExecutionID,
			PlannerID:        plannerID,
			Slug:             trigger.Slug,
			Title:            trigger.Title,
			FocusArea:        focus.Area,
			FocusDescription: focus.Description,
			Hints:            focus.Hints,
			TraceID:          trigger.TraceID,
			LoopID:           trigger.LoopID,
		}
		baseMsg := message.NewBaseMessage(plannerMsg.Schema(), plannerMsg, c.name)
		data, marshalErr := json.Marshal(baseMsg)
		if marshalErr != nil {
			c.logger.Error("Failed to marshal planner message", "error", marshalErr)
			continue
		}
		if _, pubErr := js.Publish(ctx, c.config.PlannerSubject, data); pubErr != nil {
			c.logger.Error("Failed to publish planner message",
				"planner_id", plannerID,
				"focus", focus.Area,
				"error", pubErr)
		}
	}

	// Step 3: Update state with focuses and transition to planners_dispatched
	c.transitionToPlannersDispatched(ctx, trigger.ExecutionID, focuses, len(focuses))

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK focus message", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Planner handler — executes individual planner LLM call and publishes result
// ---------------------------------------------------------------------------

// handlePlannerMessage processes an individual planner dispatch.
// It calls the LLM and publishes the result to the engine's planner-result subject.
func (c *Component) handlePlannerMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK planner message during shutdown", "error", err)
		}
		return
	}

	c.updateLastActivity()

	plannerMsg, err := reactive.ParseReactivePayload[reactive.CoordinationPlannerMessage](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse planner dispatch", "error", err)
		if err := msg.Term(); err != nil {
			c.logger.Warn("Failed to Term message", "error", err)
		}
		return
	}

	c.logger.Info("Processing planner",
		"execution_id", plannerMsg.ExecutionID,
		"planner_id", plannerMsg.PlannerID,
		"focus", plannerMsg.FocusArea)

	// Inject trace context
	llmCtx := ctx
	if plannerMsg.TraceID != "" || plannerMsg.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: plannerMsg.TraceID,
			LoopID:  plannerMsg.LoopID,
		})
	}

	// Build focused prompt and call LLM
	focus := &FocusArea{
		Area:        plannerMsg.FocusArea,
		Description: plannerMsg.FocusDescription,
		Hints:       plannerMsg.Hints,
	}
	result, llmRequestID, planErr := c.executePlanner(llmCtx, plannerMsg, focus)

	// Build planner result — the engine merges this into state
	plannerResult := &reactive.CoordinationPlannerResult{
		ExecutionID:  plannerMsg.ExecutionID,
		PlannerID:    plannerMsg.PlannerID,
		Slug:         plannerMsg.Slug,
		FocusArea:    plannerMsg.FocusArea,
		LLMRequestID: llmRequestID,
	}

	if planErr != nil {
		plannerResult.Status = "failed"
		plannerResult.Error = planErr.Error()
		c.logger.Warn("Planner failed",
			"planner_id", plannerMsg.PlannerID,
			"focus", plannerMsg.FocusArea,
			"error", planErr)
	} else {
		plannerResult.Status = "completed"
		resultJSON, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			plannerResult.Status = "failed"
			plannerResult.Error = fmt.Sprintf("marshal result: %v", marshalErr)
		} else {
			plannerResult.Result = resultJSON
		}
	}

	// Publish result for engine merge (single KV writer pattern)
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream for planner result", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	baseMsg := message.NewBaseMessage(plannerResult.Schema(), plannerResult, c.name)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal planner result", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	resultSubject := fmt.Sprintf("%s.%s", c.config.PlannerResultSubject, plannerMsg.Slug)
	if _, err := js.Publish(ctx, resultSubject, data); err != nil {
		c.logger.Error("Failed to publish planner result",
			"planner_id", plannerMsg.PlannerID,
			"error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK planner message", "error", err)
	}

	c.logger.Info("Planner result published",
		"planner_id", plannerMsg.PlannerID,
		"focus", plannerMsg.FocusArea,
		"status", plannerResult.Status,
		"subject", resultSubject)
}

// executePlanner calls the LLM with a focused prompt and parses the result.
func (c *Component) executePlanner(
	ctx context.Context,
	plannerMsg *reactive.CoordinationPlannerMessage,
	focus *FocusArea,
) (*workflow.PlannerResult, string, error) {
	systemPrompt := prompts.PlannerFocusedSystemPrompt(focus.Area)
	userPrompt := prompts.PlannerFocusedPrompt(
		focus.Area,
		focus.Description,
		plannerMsg.Title,
		focus.Hints,
		nil, // No additional context info in coordination path
	)

	content, llmRequestID, err := c.callLLM(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, "", err
	}

	result, err := c.parsePlannerResult(content, plannerMsg.PlannerID, focus.Area)
	if err != nil {
		return nil, llmRequestID, err
	}

	return result, llmRequestID, nil
}

// ---------------------------------------------------------------------------
// Synthesis handler — synthesizes planner results and saves the plan
// ---------------------------------------------------------------------------

// handleSynthesisMessage processes a synthesis dispatch from the reactive engine.
// It reads planner results from KV state, synthesizes them, saves the plan,
// publishes a result notification, and transitions to synthesized.
func (c *Component) handleSynthesisMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK synthesis message during shutdown", "error", err)
		}
		return
	}

	c.updateLastActivity()

	synthReq, err := reactive.ParseReactivePayload[reactive.CoordinationSynthesisRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse synthesis dispatch", "error", err)
		if err := msg.Term(); err != nil {
			c.logger.Warn("Failed to Term message", "error", err)
		}
		return
	}

	c.logger.Info("Processing synthesis",
		"execution_id", synthReq.ExecutionID,
		"slug", synthReq.Slug)

	// Inject trace context
	llmCtx := ctx
	if synthReq.TraceID != "" || synthReq.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: synthReq.TraceID,
			LoopID:  synthReq.LoopID,
		})
	}

	// Read current state to get planner results
	entry, err := c.stateBucket.Get(ctx, synthReq.ExecutionID)
	if err != nil {
		c.logger.Error("Failed to get coordination state for synthesis",
			"execution_id", synthReq.ExecutionID,
			"error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	var state reactive.CoordinationState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		c.logger.Error("Failed to unmarshal coordination state", "error", err)
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}

	// Convert planner results to workflow.PlannerResult for synthesis
	var plannerResults []workflow.PlannerResult
	for _, outcome := range state.PlannerResults {
		if outcome.Status == "completed" && len(outcome.Result) > 0 {
			var parsed workflow.PlannerResult
			if parseErr := json.Unmarshal(outcome.Result, &parsed); parseErr != nil {
				c.logger.Warn("Failed to parse planner result for synthesis",
					"planner_id", outcome.PlannerID,
					"error", parseErr)
				continue
			}
			plannerResults = append(plannerResults, parsed)
		}
	}

	if len(plannerResults) == 0 {
		c.logger.Error("No planner results available for synthesis",
			"execution_id", synthReq.ExecutionID)
		c.transitionToFailure(ctx, synthReq.ExecutionID, phases.CoordinationSynthesisFailed, "no planner results for synthesis")
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}

	// Synthesize results
	synthesized, synthesisRequestID, err := c.synthesizeResults(llmCtx, plannerResults)
	if err != nil {
		c.logger.Error("Synthesis failed",
			"execution_id", synthReq.ExecutionID,
			"error", err)
		c.transitionToFailure(ctx, synthReq.ExecutionID, phases.CoordinationSynthesisFailed, err.Error())
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}

	// Save the plan
	if err := c.savePlanFromSynthesis(ctx, &state, synthesized); err != nil {
		c.logger.Error("Failed to save plan",
			"execution_id", synthReq.ExecutionID,
			"slug", synthReq.Slug,
			"error", err)
		c.transitionToFailure(ctx, synthReq.ExecutionID, phases.CoordinationSynthesisFailed, err.Error())
		if termErr := msg.Term(); termErr != nil {
			c.logger.Warn("Failed to Term message", "error", termErr)
		}
		return
	}

	// Publish result notification
	if err := c.publishResultFromState(ctx, &state, synthesized, synthesisRequestID); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"execution_id", synthReq.ExecutionID,
			"error", err)
	}

	// Transition to synthesized — the engine completes the workflow
	c.transitionToSynthesized(ctx, synthReq.ExecutionID, synthesized, synthesisRequestID)

	c.sessionsCompleted.Add(1)

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK synthesis message", "error", err)
	}

	c.logger.Info("Synthesis completed",
		"execution_id", synthReq.ExecutionID,
		"slug", synthReq.Slug)
}

// ---------------------------------------------------------------------------
// KV state transitions
// ---------------------------------------------------------------------------

// transitionToPlannersDispatched updates state with focus areas and
// transitions to planners_dispatched phase.
func (c *Component) transitionToPlannersDispatched(ctx context.Context, executionID string, focuses []*FocusArea, plannerCount int) {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		c.logger.Error("Failed to get state for planners-dispatched transition",
			"execution_id", executionID, "error", err)
		return
	}

	var state reactive.CoordinationState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		c.logger.Error("Failed to unmarshal state", "execution_id", executionID, "error", err)
		return
	}

	// Store focus areas
	state.Focuses = make([]reactive.CoordinationFocus, len(focuses))
	for i, f := range focuses {
		state.Focuses[i] = reactive.CoordinationFocus{
			Area:        f.Area,
			Description: f.Description,
			Hints:       f.Hints,
		}
	}
	state.PlannerCount = plannerCount
	if state.PlannerResults == nil {
		state.PlannerResults = make(map[string]*reactive.PlannerOutcome)
	}
	state.Phase = phases.CoordinationPlannersDispatched
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		c.logger.Error("Failed to marshal state", "execution_id", executionID, "error", err)
		return
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		c.logger.Error("Failed to update state to planners_dispatched",
			"execution_id", executionID, "error", err)
	}
}

// transitionToSynthesized updates state and transitions to synthesized phase.
func (c *Component) transitionToSynthesized(ctx context.Context, executionID string, synthesized *SynthesizedPlan, synthesisRequestID string) {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		c.logger.Error("Failed to get state for synthesized transition",
			"execution_id", executionID, "error", err)
		return
	}

	var state reactive.CoordinationState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		c.logger.Error("Failed to unmarshal state", "execution_id", executionID, "error", err)
		return
	}

	synthJSON, _ := json.Marshal(synthesized)
	state.SynthesizedPlan = synthJSON
	if synthesisRequestID != "" {
		state.LLMRequestIDs = append(state.LLMRequestIDs, synthesisRequestID)
	}
	state.Phase = phases.CoordinationSynthesized
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		c.logger.Error("Failed to marshal state", "execution_id", executionID, "error", err)
		return
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		c.logger.Error("Failed to update state to synthesized",
			"execution_id", executionID, "error", err)
	}
}

// transitionToFailure updates state to a failure phase.
func (c *Component) transitionToFailure(ctx context.Context, executionID, phase, errMsg string) {
	if executionID == "" {
		return
	}

	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		c.logger.Error("Failed to get state for failure transition",
			"execution_id", executionID, "error", err)
		return
	}

	var state reactive.CoordinationState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		c.logger.Error("Failed to unmarshal state", "execution_id", executionID, "error", err)
		return
	}

	state.Phase = phase
	state.Error = errMsg
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		c.logger.Error("Failed to marshal state", "execution_id", executionID, "error", err)
		return
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		c.logger.Error("Failed to update state to failure",
			"execution_id", executionID, "phase", phase, "error", err)
	}

	c.sessionsFailed.Add(1)
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
func (c *Component) determineFocusAreas(ctx context.Context, trigger *reactive.PlanCoordinatorRequest) ([]*FocusArea, error) {
	// If explicit focuses provided, use them
	if len(trigger.FocusAreas) > 0 {
		focuses := make([]*FocusArea, len(trigger.FocusAreas))
		for i, f := range trigger.FocusAreas {
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

// savePlanFromSynthesis saves a synthesized plan using data from the coordination state.
func (c *Component) savePlanFromSynthesis(ctx context.Context, state *reactive.CoordinationState, plan *SynthesizedPlan) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return retry.NonRetryable(fmt.Errorf("get working directory: %w", err))
		}
	}

	c.logger.Info("Saving plan from synthesis",
		"slug", state.Slug,
		"repo_root", repoRoot)

	manager := workflow.NewManager(repoRoot)

	existingPlan, err := manager.LoadPlan(ctx, state.Slug)
	if err != nil {
		return retry.NonRetryable(fmt.Errorf("load plan: %w", err))
	}

	existingPlan.Goal = plan.Goal
	existingPlan.Context = plan.Context
	existingPlan.Scope = plan.Scope

	if state.TraceID != "" && !slices.Contains(existingPlan.ExecutionTraceIDs, state.TraceID) {
		existingPlan.ExecutionTraceIDs = append(existingPlan.ExecutionTraceIDs, state.TraceID)
	}

	if err := manager.SavePlan(ctx, existingPlan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	c.logger.Info("Plan written from synthesis",
		"slug", state.Slug,
		"goal_length", len(existingPlan.Goal))

	return nil
}

// publishResultFromState publishes a success notification using coordination state data.
func (c *Component) publishResultFromState(ctx context.Context, state *reactive.CoordinationState, _ *SynthesizedPlan, synthesisRequestID string) error {
	llmRequestIDs := state.LLMRequestIDs
	if synthesisRequestID != "" && !slices.Contains(llmRequestIDs, synthesisRequestID) {
		llmRequestIDs = append(llmRequestIDs, synthesisRequestID)
	}

	result := &CoordinatorResult{
		RequestID:     state.RequestID,
		TraceID:       state.TraceID,
		Slug:          state.Slug,
		PlannerCount:  state.PlannerCount,
		Status:        "completed",
		LLMRequestIDs: llmRequestIDs,
	}

	baseMsg := message.NewBaseMessage(result.Schema(), result, c.name)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	resultSubject := fmt.Sprintf("workflow.result.plan-coordinator.%s", state.Slug)
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get jetstream for result: %w", err)
	}
	if _, err := js.Publish(ctx, resultSubject, data); err != nil {
		return fmt.Errorf("publish result: %w", err)
	}

	c.logger.Info("Published plan-coordinator result",
		"slug", state.Slug,
		"subject", resultSubject,
		"planner_count", state.PlannerCount)
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
