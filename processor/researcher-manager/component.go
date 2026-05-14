// Package researchermanager owns the RESEARCH KV bucket and routes
// research requests from the developer agent to a researcher sub-agent.
//
// Flow (one cycle):
//
//  1. Developer's research() tool writes a pending Research record to
//     RESEARCH KV and publishes a ResearchRequestPayload to
//     agent.research.requested.<id>. The dev's tool blocks on a KV
//     watcher of the same record.
//  2. THIS COMPONENT consumes the request. It loads the Research from
//     KV, marks it in_progress, builds a researcher prompt from the
//     question + sources, and publishes an agentic.TaskMessage on
//     agent.task.research. agentic-loop picks it up and runs a
//     researcher sub-agent loop.
//  3. The researcher's terminal tool answer_research writes the answered
//     Research record (status=answered, answer, citations) back to KV.
//  4. The developer's KV watcher sees the answered state and unblocks
//     with the rendered answer + citations.
//
// This component does NOT track in-flight loops (unlike recovery-agent).
// The answer_research terminal owns the answer-write, and the asking
// dev's executor owns the watch — so the manager's job ends at dispatch.
// On dispatch failure we write Research.Status=error so the asking dev's
// watcher unblocks with a useful error message rather than waiting out
// the full timeout.
//
// See project_research_tool_plan_2026_05_14 for the full design.
package researchermanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/research"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	componentName = "researcher-manager"

	// agentTaskSubject is the JetStream subject the researcher's dispatch
	// TaskMessage publishes to. agentic-loop subscribes to agent.task.*
	// via its general consumer.
	agentTaskSubject = "agent.task.research"

	// stepResearch is the WorkflowStep label stamped on dispatched
	// researcher TaskMessages.
	stepResearch = "research"

	// filterSubjectAll matches every research request. R3 ships a single
	// consumer name, sufficient until we need per-deployment isolation.
	filterSubjectAll = "agent.research.requested.>"

	// defaultStreamName is the JetStream stream researcher requests flow
	// through. Matches semspec's AGENT stream config.
	defaultStreamName = "AGENT"
)

// Component owns the RESEARCH KV bucket and dispatches researcher loops.
type Component struct {
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	decoder       *message.Decoder
	assembler     *prompt.Assembler

	mu      sync.Mutex
	store   *workflow.ResearchStore
	cancel  context.CancelFunc
	running bool

	// Metrics (atomic — no labels, single consumer per counter).
	requestsReceived atomic.Int64
	dispatched       atomic.Int64
	dispatchFailures atomic.Int64
}

// NewComponent constructs a researcher-manager from raw JSON config + the
// usual component dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal researcher-manager config: %w", err)
		}
	}
	if cfg.Bucket == "" {
		cfg.Bucket = workflow.ResearchBucket
	}

	logger := deps.GetLogger()

	// Build the persona-fragment registry the researcher dispatches
	// through. Same shape as recovery-agent / planner / qa-reviewer.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))

	return &Component{
		config:        cfg,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		toolRegistry:  deps.ToolRegistry,
		decoder:       message.NewDecoder(deps.PayloadRegistry),
		assembler:     prompt.NewAssembler(registry),
	}, nil
}

// Initialize is a no-op; the bucket + consumer are created in Start.
func (c *Component) Initialize() error { return nil }

// Start creates the RESEARCH KV bucket, subscribes to research request
// events, and dispatches researcher loops on each request.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}
	if c.natsClient == nil {
		return fmt.Errorf("researcher-manager: NATS client is required")
	}

	store, err := workflow.NewResearchStore(c.natsClient)
	if err != nil {
		return fmt.Errorf("create research store: %w", err)
	}
	c.store = store

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	consumerCfg := natsclient.StreamConsumerConfig{
		StreamName:    defaultStreamName,
		ConsumerName:  componentName,
		FilterSubject: filterSubjectAll,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, consumerCfg, c.handleMessagePush); err != nil {
		cancel()
		return fmt.Errorf("consume research requests: %w", err)
	}

	c.running = true
	c.logger.Info("researcher-manager started",
		slog.String("bucket", c.config.Bucket),
		slog.String("stream", consumerCfg.StreamName),
		slog.String("filter", consumerCfg.FilterSubject),
	)
	return nil
}

// Stop gracefully halts the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.running = false
	return nil
}

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage parses a ResearchRequestPayload, loads the Research
// record from KV, dispatches a researcher loop, and acks the message.
//
// All paths ack — a publish/dispatch failure must not block the
// upstream pipeline. On dispatch failure we set Research.Status=error
// so the asking dev's KV watcher unblocks with a useful message rather
// than waiting out the full executor timeout.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.requestsReceived.Add(1)

	// Ack happens on every terminal disposition (success, idempotent skip,
	// permanent parse failure). Transient failures (KV unreachable, status-
	// flip CAS error) return WITHOUT acking so the JetStream redelivery
	// (MaxDeliver=3 / AckWait=30s) gets another chance.
	ackAndForget := func(reason string) {
		if err := msg.Ack(); err != nil {
			c.logger.Warn("research request ack failed",
				slog.String("subject", msg.Subject()),
				slog.String("reason", reason),
				slog.Any("error", err))
		}
	}

	researchID, err := c.parseRequestID(msg)
	if err != nil {
		// Permanent parse failure — ack so we don't redeliver garbage.
		c.logger.Warn("invalid research request envelope",
			slog.String("subject", msg.Subject()),
			slog.Any("error", err))
		ackAndForget("malformed envelope")
		return
	}

	r, err := c.store.Get(ctx, researchID)
	if err != nil {
		// The record may have TTL'd out before we processed the request, or
		// the publisher's KV write hasn't propagated yet. Don't ack — let
		// MaxDeliver give the propagation race time to settle.
		c.logger.Warn("research record not found for request",
			slog.String("research_id", researchID), slog.Any("error", err))
		return
	}

	if r.Status != workflow.ResearchStatusPending {
		// Idempotency: redelivery of an already-dispatched request. Ack
		// without re-dispatching.
		c.logger.Debug("skipping non-pending research request (idempotent redelivery)",
			slog.String("research_id", researchID),
			slog.String("status", string(r.Status)))
		ackAndForget("non-pending")
		return
	}

	// CAS transition pending → in_progress. If the record has advanced
	// concurrently (researcher already answered, OR another delivery flipped
	// it first), ErrResearchStaleStatus tells us to skip without re-dispatch.
	now := time.Now().UTC()
	r.Status = workflow.ResearchStatusInProgress
	r.DispatchedAt = &now
	if err := c.store.TransitionStatus(ctx, r, workflow.ResearchStatusPending); err != nil {
		if errors.Is(err, workflow.ErrResearchStaleStatus) {
			c.logger.Debug("research record advanced under us; skipping dispatch",
				slog.String("research_id", researchID), slog.Any("error", err))
			ackAndForget("stale-status")
			return
		}
		// Transient KV failure — don't ack, let JetStream redeliver.
		c.logger.Warn("failed to flip research to in_progress (transient — will retry)",
			slog.String("research_id", researchID), slog.Any("error", err))
		return
	}

	if err := c.dispatchResearcher(ctx, r); err != nil {
		c.dispatchFailures.Add(1)
		c.logger.Error("research dispatch failed",
			slog.String("research_id", researchID), slog.Any("error", err))
		c.markErrored(ctx, r, fmt.Sprintf("dispatch failed: %v", err))
		// Ack — dispatch failure is a permanent error from the manager's
		// perspective (redelivering won't help since we'd just fail the
		// assembler/marshal/publish again). The asking dev's watcher has
		// already been unblocked by markErrored.
		ackAndForget("dispatch-failed")
		return
	}

	c.dispatched.Add(1)
	ackAndForget("dispatched")
}

// parseRequestID extracts the ResearchID from an incoming
// agent.research.requested.<id> message. The research executor publishes
// a raw ResearchRequestPayload JSON body (not wrapped in a BaseMessage
// envelope, because ResearchRequestPayload doesn't implement the full
// message.Payload interface). Falls back to the subject suffix as a
// last resort so pathological encoding doesn't lose the routing key.
func (c *Component) parseRequestID(msg jetstream.Msg) (string, error) {
	// Try raw ResearchRequestPayload JSON (the executor's publish path).
	var payload workflow.ResearchRequestPayload
	if err := json.Unmarshal(msg.Data(), &payload); err == nil && payload.ResearchID != "" {
		return payload.ResearchID, nil
	}

	// Subject-suffix fallback: agent.research.requested.<id>
	subject := msg.Subject()
	if researchID := subjectSuffix(subject, research.SubjectResearchRequested); researchID != "" {
		return researchID, nil
	}

	return "", fmt.Errorf("could not extract research_id from message (subject=%s, body_bytes=%d)", subject, len(msg.Data()))
}

// dispatchResearcher builds a researcher TaskMessage from the loaded
// Research record and publishes it on agent.task.research. agentic-loop's
// general consumer picks it up and runs the researcher loop.
func (c *Component) dispatchResearcher(ctx context.Context, r *workflow.Research) error {
	if c.modelRegistry == nil {
		return fmt.Errorf("model registry not wired — cannot resolve research capability")
	}

	cap := string(model.CapabilityResearch)
	modelName := c.modelRegistry.Resolve(cap)
	if modelName == "" {
		// Fallback to "writing" (cheap synthesis class) if research isn't
		// configured for this deployment. Researcher still runs; operators
		// see the warn and configure on demand.
		modelName = c.modelRegistry.Resolve(string(model.CapabilityWriting))
		c.logger.Warn("research capability not configured; falling back",
			slog.String("research_id", r.ID),
			slog.String("fallback_model", modelName))
	}

	resCtx := &prompt.ResearcherPromptContext{
		ResearchID:     r.ID,
		Question:       r.Question,
		Sources:        r.Sources,
		AskingPlanSlug: r.PlanSlug,
		AskingTaskID:   r.TaskID,
	}

	var (
		endpoint  *ssmodel.EndpointConfig
		maxTokens int
	)
	if ep := c.modelRegistry.GetEndpoint(modelName); ep != nil {
		endpoint = ep
		maxTokens = ep.MaxTokens
	}

	availableTools := prompt.FilterTools(researcherAvailableToolNames(), prompt.RoleResearcher)
	asmCtx := &prompt.AssemblyContext{
		Role:              prompt.RoleResearcher,
		Provider:          resolveProvider(modelName),
		HasResponseFormat: terminal.EndpointSupportsResponseFormatGated(endpoint, nil),
		Domain:            "software",
		AvailableTools:    availableTools,
		SupportsTools:     true,
		MaxTokens:         maxTokens,
		Persona:           prompt.GlobalPersonas().ForRole(prompt.RoleResearcher),
		Vocabulary:        prompt.GlobalPersonas().Vocabulary(),
		Researcher:        resCtx,
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		return fmt.Errorf("assemble researcher prompt: %w", assembled.RenderError)
	}

	taskID := fmt.Sprintf("research-%s-%s", r.ID, uuid.New().String())
	task := &agentic.TaskMessage{
		TaskID: taskID,
		// agentic.RoleGeneral is the semstreams-side role enum — semspec's
		// fine-grained RoleResearcher persona is carried via Metadata["role"]
		// below and recovered by the assembler when the loop's prompt is
		// re-rendered. Same pattern as recovery-agent and other manager-
		// dispatched roles.
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForEndpoint(c.toolRegistry, "research", endpoint, availableTools...),
		ToolChoice:   prompt.ResolveToolChoice(prompt.RoleResearcher, availableTools),
		WorkflowSlug: workflow.WorkflowSlugResearch,
		WorkflowStep: stepResearch,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"research_id":    r.ID,
			"asking_loop_id": r.AskingLoopID,
			"asking_call_id": r.AskingCallID,
			"plan_slug":      r.PlanSlug,
			"task_id":        r.TaskID,
			"capability":     cap,
			"model":          modelName,
			"role":           string(prompt.RoleResearcher),
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task message: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, agentTaskSubject, data); err != nil {
		return fmt.Errorf("publish researcher task: %w", err)
	}

	c.logger.Info("Dispatched researcher",
		slog.String("research_id", r.ID),
		slog.String("task_id", taskID),
		slog.String("model", modelName),
		slog.Int("prompt_chars", len(assembled.UserMessage)),
	)
	return nil
}

// markErrored CAS-flips Research → status=error so the asking dev's KV
// watcher unblocks with a useful message rather than waiting out the
// full timeout. Transitions from pending OR in_progress only — if the
// record already advanced to answered, the answer wins and we leave it
// alone (the dispatch error log line remains the operational record).
// Best-effort — failure to mark is logged at Warn.
func (c *Component) markErrored(ctx context.Context, r *workflow.Research, reason string) {
	r.Status = workflow.ResearchStatusError
	r.Error = reason
	if err := c.store.TransitionStatus(ctx, r,
		workflow.ResearchStatusPending,
		workflow.ResearchStatusInProgress,
	); err != nil {
		if errors.Is(err, workflow.ErrResearchStaleStatus) {
			c.logger.Debug("research already terminal; leaving in place",
				slog.String("research_id", r.ID), slog.String("attempted_error", reason))
			return
		}
		c.logger.Warn("failed to mark research errored",
			slog.String("research_id", r.ID), slog.Any("error", err))
	}
}

// researcherAvailableToolNames is the canonical tool list for the
// researcher role. The actual wire palette goes through
// prompt.FilterTools(RoleResearcher) which enforces the allowlist
// declared in prompt/tool_filter.go. Keeping the list here is the same
// pattern as recovery-agent's recoveryAvailableToolNames.
func researcherAvailableToolNames() []string {
	return []string{
		"bash",
		"http_request",
		"web_search",
		"answer_research",
	}
}

// resolveProvider returns the prompt.Provider for a model name. Uses
// strings.Contains so OpenRouter-style routed names ("anthropic/claude-3.5-sonnet",
// "google/gemini-2.5-pro", "openrouter/qwen3-32b") match correctly,
// matching the established pattern in other components. Empty provider
// is the safe fallback — the assembler tolerates it by emitting
// un-templated prompts.
//
// TODO(post-MVP): three different `resolveProvider` implementations now
// exist (researcher-manager, recovery-agent, requirement-executor); the
// reviewer flagged them as drift candidates. Centralize into
// prompt.ResolveProviderFromModel in a follow-up.
func resolveProvider(modelName string) prompt.Provider {
	switch {
	case strings.Contains(modelName, "claude"):
		return prompt.ProviderAnthropic
	case strings.Contains(modelName, "gemini"):
		return prompt.ProviderGoogle
	case strings.Contains(modelName, "gpt-"):
		return prompt.ProviderOpenAI
	case strings.Contains(modelName, "qwen"), strings.Contains(modelName, "llama"):
		return prompt.ProviderOllama
	}
	return ""
}

// subjectSuffix extracts the substring after a known prefix. Used as a
// last-resort routing key extraction when payload parsing fails. Returns
// "" if the subject doesn't carry the expected prefix.
func subjectSuffix(subject, prefix string) string {
	if len(subject) <= len(prefix) || subject[:len(prefix)] != prefix {
		return ""
	}
	return subject[len(prefix):]
}

// Meta returns the component's discovery metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Owns RESEARCH KV and dispatches researcher sub-agent loops in response to developer research() tool calls",
		Version:     "0.2.0",
	}
}

// InputPorts/OutputPorts declare the message-flow contract.
// Subjects are documented in the descriptions since component.Port doesn't
// carry a typed subject field (Direction is the only structural metadata).
func (c *Component) InputPorts() []component.Port {
	return []component.Port{{
		Name:        "research_requested_in",
		Direction:   component.DirectionInput,
		Description: "ResearchRequestPayload events from the research dev tool, on JetStream subject " + filterSubjectAll,
	}}
}

func (c *Component) OutputPorts() []component.Port {
	return []component.Port{{
		Name:        "research_task_out",
		Direction:   component.DirectionOutput,
		Description: "agentic.TaskMessage for researcher sub-agent dispatch, published to JetStream subject " + agentTaskSubject,
	}}
}

// ConfigSchema returns the component's JSON-Schema description.
func (c *Component) ConfigSchema() component.ConfigSchema { return component.ConfigSchema{} }

// Health reports whether the component has started.
func (c *Component) Health() component.HealthStatus {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	status := "stopped"
	if running {
		status = "healthy"
	}
	return component.HealthStatus{
		Healthy:   running,
		Status:    status,
		LastCheck: time.Now().UTC(),
	}
}

// DataFlow reports the manager's basic counters.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{}
}
