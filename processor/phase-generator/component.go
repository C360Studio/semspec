// Package phasegenerator provides a processor that generates execution phases
// from approved plans using LLM, decomposing work into logical stages.
package phasegenerator

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
	contextbuilder "github.com/c360studio/semspec/processor/context-builder"
	"github.com/c360studio/semspec/processor/contexthelper"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/phases"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semspec/workflow/reactive"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// maxFormatRetries is the total number of LLM call attempts when the response
// isn't valid JSON. On each retry, the parse error is fed back to the LLM as a
// correction prompt so it can fix the output format.
const maxFormatRetries = 5

// llmCompleter is the subset of the LLM client used by phase-generator.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the phase-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient llmCompleter

	// Centralized context building via context-builder
	contextHelper *contexthelper.Helper

	// JetStream consumer
	consumer jetstream.Consumer
	stream   jetstream.Stream

	// KV bucket for workflow state (reactive engine state)
	stateBucket jetstream.KeyValue

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed atomic.Int64
	phasesGenerated   atomic.Int64
	generationsFailed atomic.Int64
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
	return "phase-review"
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.PhasesGenerated
}

// StateManager returns nil - this component updates state directly via KV bucket.
// The reactive engine manages state; we just update it on completion.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
}

// PhaseGeneratorResultType is the message type for phase generator results.
var PhaseGeneratorResultType = message.Type{Domain: "workflow", Category: "phase-generator-result", Version: "v1"}

// Result is the result payload for phase generation.
type Result struct {
	RequestID     string           `json:"request_id"`
	Slug          string           `json:"slug"`
	PhaseCount    int              `json:"phase_count"`
	Phases        []workflow.Phase `json:"phases"`
	Status        string           `json:"status"`
	LLMRequestIDs []string         `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *Result) Schema() message.Type {
	return PhaseGeneratorResultType
}

// Validate implements message.Payload.
func (r *Result) Validate() error {
	return nil
}

// MarshalJSON implements json.Marshaler.
func (r *Result) MarshalJSON() ([]byte, error) {
	type Alias Result
	return json.Marshal((*Alias)(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Result) UnmarshalJSON(data []byte) error {
	type Alias Result
	return json.Unmarshal(data, (*Alias)(r))
}

// NewComponent creates a new phase-generator processor.
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
	if config.ContextSubjectPrefix == "" {
		config.ContextSubjectPrefix = defaults.ContextSubjectPrefix
	}
	if config.ContextResponseBucket == "" {
		config.ContextResponseBucket = defaults.ContextResponseBucket
	}
	if config.ContextTimeout == "" {
		config.ContextTimeout = defaults.ContextTimeout
	}
	if config.StateBucket == "" {
		config.StateBucket = defaults.StateBucket
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	ctxHelper := contexthelper.New(deps.NATSClient, contexthelper.Config{
		SubjectPrefix:  config.ContextSubjectPrefix,
		ResponseBucket: config.ContextResponseBucket,
		Timeout:        config.GetContextTimeout(),
		SourceName:     "phase-generator",
	}, logger)

	return &Component{
		name:       "phase-generator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
			llm.WithCallStore(llm.GlobalCallStore()),
		),
		contextHelper: ctxHelper,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized phase-generator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing phase generation triggers.
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
	c.stream = stream

	// Get or create workflow state bucket
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
		AckWait:       180 * time.Second,
		MaxDeliver:    3,
	}

	consumer, err := stream.CreateOrUpdateConsumer(subCtx, consumerConfig)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("create consumer: %w", err)
	}
	c.consumer = consumer

	go c.consumeLoop(subCtx)

	c.logger.Info("phase-generator started",
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

// handleMessage processes a single phase generation trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	// Parse the reactive engine's BaseMessage-wrapped payload.
	trigger, err := reactive.ParseReactivePayload[reactive.PhaseGeneratorRequest](msg.Data())
	if err != nil {
		c.logger.Error("Failed to parse trigger", "error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := trigger.Validate(); err != nil {
		c.logger.Error("Invalid trigger payload", "error", err)
		// ACK invalid requests — they will not succeed on retry.
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK invalid message", "error", ackErr)
		}
		return
	}

	c.logger.Info("Processing phase generation trigger",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"trace_id", trigger.TraceID)

	// Signal in-progress to prevent redelivery during LLM operations.
	if err := msg.InProgress(); err != nil {
		c.logger.Debug("Failed to signal in-progress", "error", err)
	}

	// Inject trace context for LLM call tracking
	llmCtx := ctx
	if trigger.TraceID != "" || trigger.LoopID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
			LoopID:  trigger.LoopID,
		})
	}

	phases, llmRequestIDs, err := c.generatePhases(llmCtx, trigger)
	if err != nil {
		c.handleTriggerFailure(ctx, msg, trigger, "Failed to generate phases", err)
		return
	}

	if err := c.savePhases(ctx, trigger, phases); err != nil {
		c.handleTriggerFailure(ctx, msg, trigger, "Failed to save phases", err)
		return
	}

	if err := c.publishResult(ctx, trigger, phases, llmRequestIDs); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
	}

	c.phasesGenerated.Add(int64(len(phases)))

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Phases generated successfully",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"phase_count", len(phases))
}

// handleTriggerFailure handles a failed phase generation or save operation.
func (c *Component) handleTriggerFailure(ctx context.Context, msg jetstream.Msg, trigger *reactive.PhaseGeneratorRequest, operation string, err error) {
	c.generationsFailed.Add(1)
	c.logger.Error(operation,
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"error", err)

	// Transition workflow to failure state so the reactive engine can handle it
	if trigger.ExecutionID != "" {
		if transErr := c.transitionToFailure(ctx, trigger.ExecutionID, err.Error()); transErr != nil {
			c.logger.Error("Failed to transition to failure state", "error", transErr)
		}
	} else {
		c.logger.Debug("Skipping failure transition - no ExecutionID",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug)
	}
	if ackErr := msg.Ack(); ackErr != nil {
		c.logger.Warn("Failed to ACK message", "error", ackErr)
	}
}

// transitionToFailure transitions the workflow to the generator-failed phase.
func (c *Component) transitionToFailure(ctx context.Context, executionID string, cause string) error {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get workflow state: %w", err)
	}

	var state reactive.PhaseReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	state.Phase = phases.PhaseGeneratorFailed
	state.Error = cause
	state.UpdatedAt = time.Now()

	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, executionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Transitioned workflow to failure state",
		"execution_id", executionID,
		"phase", phases.PhaseGeneratorFailed,
		"cause", cause)
	return nil
}

// generatePhases calls the LLM to generate phases from the plan.
func (c *Component) generatePhases(ctx context.Context, trigger *reactive.PhaseGeneratorRequest) ([]workflow.Phase, []string, error) {
	prompt := trigger.Prompt
	if prompt == "" {
		return nil, nil, fmt.Errorf("no prompt provided in trigger")
	}

	// Build context via context-builder (graph-first)
	var graphContext string
	var sopRequirements []string
	resp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType:   contextbuilder.TaskTypePlanning,
		Topic:      trigger.Title,
		Capability: c.config.DefaultCapability,
	})
	if resp != nil {
		graphContext = contexthelper.FormatContextResponse(resp)
		sopRequirements = resp.SOPRequirements
		c.logger.Info("Built phase generation context via context-builder",
			"title", trigger.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"sop_requirements", len(sopRequirements),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"title", trigger.Title)
	}

	// Enrich prompt with graph context and SOP requirements
	if graphContext != "" {
		prompt = fmt.Sprintf("%s\n\n## Codebase Context\n\nThe following context from the knowledge graph provides information about the existing codebase structure:\n\n%s", prompt, graphContext)
	}
	if len(sopRequirements) > 0 {
		prompt = prompt + "\n\n" + prompts.FormatSOPRequirements(sopRequirements)
	}

	// Call LLM with format retry loop
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	temperature := 0.7
	messages := []llm.Message{{Role: "user", Content: prompt}}
	var lastErr error
	var phases []workflow.Phase
	var llmRequestIDs []string

	for attempt := range maxFormatRetries {
		llmResp, err := c.llmClient.Complete(ctx, llm.Request{
			Capability:  capability,
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			return nil, llmRequestIDs, fmt.Errorf("LLM completion: %w", err)
		}

		llmRequestIDs = append(llmRequestIDs, llmResp.RequestID)

		c.logger.Debug("LLM response received",
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"has_graph_context", graphContext != "",
			"attempt", attempt+1)

		parsedPhases, parseErr := c.parsePhasesFromResponse(llmResp.Content, trigger.Slug)
		if parseErr == nil {
			phases = parsedPhases
			break
		}

		lastErr = parseErr

		if attempt+1 >= maxFormatRetries {
			break
		}

		c.logger.Warn("Phase generator LLM format retry",
			"attempt", attempt+1,
			"error", parseErr)

		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: phaseFormatCorrectionPrompt(parseErr)},
		)
	}

	if phases == nil {
		return nil, llmRequestIDs, fmt.Errorf("parse phases from response after %d attempts: %w", maxFormatRetries, lastErr)
	}

	return phases, llmRequestIDs, nil
}

// parsePhasesFromResponse extracts phases from the LLM response content.
func (c *Component) parsePhasesFromResponse(content, slug string) ([]workflow.Phase, error) {
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var resp prompts.PhaseGeneratorResponse
	if err := json.Unmarshal([]byte(jsonContent), &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	if len(resp.Phases) < 2 {
		return nil, fmt.Errorf("expected at least 2 phases, got %d", len(resp.Phases))
	}

	// Validate no circular dependencies
	if err := validatePhaseDependencies(resp.Phases); err != nil {
		return nil, err
	}

	// Convert to workflow.Phase
	planID := workflow.PlanEntityID(slug)
	now := time.Now()
	phases := make([]workflow.Phase, len(resp.Phases))

	for i, genPhase := range resp.Phases {
		seq := i + 1
		phases[i] = workflow.Phase{
			ID:               workflow.PhaseEntityID(slug, seq),
			PlanID:           planID,
			Sequence:         seq,
			Name:             genPhase.Name,
			Description:      genPhase.Description,
			DependsOn:        normalizePhaseDependsOn(genPhase.DependsOn, slug),
			Status:           workflow.PhaseStatusPending,
			RequiresApproval: genPhase.RequiresApproval,
			CreatedAt:        now,
		}
	}

	return phases, nil
}

// validatePhaseDependencies checks for circular dependencies in the phase list.
func validatePhaseDependencies(phases []prompts.GeneratedPhase) error {
	n := len(phases)
	for i, p := range phases {
		for _, dep := range p.DependsOn {
			if dep < 1 || dep > n {
				return fmt.Errorf("phase %d has invalid dependency %d (valid range: 1-%d)", i+1, dep, n)
			}
			if dep > i+1 {
				return fmt.Errorf("phase %d depends on later phase %d (forward dependency not allowed)", i+1, dep)
			}
			if dep == i+1 {
				return fmt.Errorf("phase %d depends on itself", i+1)
			}
		}
	}
	return nil
}

// normalizePhaseDependsOn converts 1-based sequence numbers to phase entity IDs.
func normalizePhaseDependsOn(deps []int, slug string) []string {
	if len(deps) == 0 {
		return []string{}
	}
	result := make([]string, len(deps))
	for i, seq := range deps {
		result[i] = workflow.PhaseEntityID(slug, seq)
	}
	return result
}

// phaseFormatCorrectionPrompt builds a correction message for the LLM when
// the phase generation response isn't valid JSON.
func phaseFormatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON object matching this exact structure:\n"+
			"```json\n"+
			"{\n"+
			"  \"phases\": [\n"+
			"    {\n"+
			"      \"name\": \"Phase 1: Foundation\",\n"+
			"      \"description\": \"Set up base types and infrastructure\",\n"+
			"      \"depends_on\": [],\n"+
			"      \"requires_approval\": false\n"+
			"    },\n"+
			"    {\n"+
			"      \"name\": \"Phase 2: Implementation\",\n"+
			"      \"description\": \"Implement core business logic\",\n"+
			"      \"depends_on\": [1],\n"+
			"      \"requires_approval\": false\n"+
			"    }\n"+
			"  ]\n"+
			"}\n"+
			"```\n\n"+
			"Rules:\n"+
			"- At least 2 phases required\n"+
			"- depends_on uses 1-based sequence numbers\n"+
			"- No forward or circular dependencies\n"+
			"- Return ONLY the JSON object",
		err.Error(),
	)
}

// savePhases saves the generated phases to the plan's phases.json file.
func (c *Component) savePhases(ctx context.Context, trigger *reactive.PhaseGeneratorRequest, phases []workflow.Phase) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	manager := workflow.NewManager(repoRoot)
	if err := manager.SavePhases(ctx, phases, trigger.Slug); err != nil {
		return err
	}

	// Update plan status to phases_generated
	plan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		c.logger.Warn("Failed to load plan after saving phases — status not updated",
			"slug", trigger.Slug, "error", err)
		return nil
	}
	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusPhasesGenerated); err != nil {
		c.logger.Warn("Failed to update plan status to phases_generated",
			"slug", trigger.Slug, "error", err)
	}
	return nil
}

// publishResult updates the workflow state with the generated phases.
// This transitions the workflow to the phases-generated phase, which triggers
// the reactive engine to advance to the next step.
func (c *Component) publishResult(ctx context.Context, trigger *reactive.PhaseGeneratorRequest, generatedPhases []workflow.Phase, llmRequestIDs []string) error {
	// Check if this is a workflow-dispatched request (has ExecutionID)
	if trigger.ExecutionID == "" {
		c.logger.Warn("No ExecutionID - cannot update workflow state",
			"slug", trigger.Slug,
			"request_id", trigger.RequestID)
		return nil
	}

	// Get current state from KV
	entry, err := c.stateBucket.Get(ctx, trigger.ExecutionID)
	if err != nil {
		return fmt.Errorf("get workflow state %s: %w", trigger.ExecutionID, err)
	}

	// Deserialize the typed state
	var state reactive.PhaseReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	// Marshal phases to JSON for storage
	phasesJSON, err := json.Marshal(generatedPhases)
	if err != nil {
		return fmt.Errorf("marshal phases: %w", err)
	}

	// Update state with results
	state.PhasesContent = phasesJSON
	state.PhaseCount = len(generatedPhases)
	state.LLMRequestIDs = llmRequestIDs
	state.Phase = phases.PhasesGenerated
	state.UpdatedAt = time.Now()

	// Write back to KV
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, trigger.ExecutionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Updated workflow state with generated phases",
		"slug", trigger.Slug,
		"execution_id", trigger.ExecutionID,
		"phase", phases.PhasesGenerated,
		"phase_count", len(generatedPhases))
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

	c.logger.Info("phase-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"phases_generated", c.phasesGenerated.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "phase-generator",
		Type:        "processor",
		Description: "Generates execution phases from approved plans using LLM",
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
	return phaseGeneratorSchema
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
		ErrorCount: int(c.generationsFailed.Load()),
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
