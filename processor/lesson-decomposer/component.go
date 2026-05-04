// Package lessondecomposer implements ADR-033 Phase 2+: a JetStream
// processor that consumes reviewer-rejection signals and produces
// evidence-cited Lesson entities via the shared lessons.Writer.
//
// Phase 2a (skeleton) shipped the wire end-to-end without an LLM call.
// Phase 2b (this file) wires:
//
//   - Trajectory fetch via NATS request/reply on agentic.query.trajectory
//     (see trajectory.go).
//   - One-shot LLM dispatch as an agentic.TaskMessage published on
//     agent.task.lesson-decomposition. The agentic-loop processor runs
//     the loop and writes terminal state to AGENT_LOOPS KV.
//   - Loop-completion watcher that parses the submit_work output, builds
//     a workflow.Lesson with Source="decomposer", and writes it via
//     lessons.Writer. The keyword-classifier lesson keeps writing in
//     parallel until Phase 3.
//
// The Enabled config flag short-circuits the consumer so the wire can be
// disabled per-deployment without reverting publishers.
package lessondecomposer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/c360studio/semspec/agentgraph"
	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// agentTaskSubject is the NATS subject the agentic-loop processor
	// listens on for our dispatched LLM task. Single-segment after
	// agent.task. matches the agentic-loop's "agent.task.*" subscription.
	agentTaskSubject = "agent.task.lesson-decomposition"

	// stepDecompose is the WorkflowStep stamped on dispatched TaskMessages
	// and matched in the AGENT_LOOPS watcher. There's only one step in
	// the decomposer's flow today; defining it as a constant keeps the
	// publish + subscribe sides in sync.
	stepDecompose = "decompose"

	// trajectoryStepLimit caps the number of trajectory steps rendered
	// into the decomposer prompt. The decomposer's input budget is small
	// per ADR-033 (~4-7K tokens) — full trajectories from real-LLM runs
	// can blow past that. 80 steps with one-line summaries fits the
	// budget with room for the rest of the prompt.
	trajectoryStepLimit = 80

	// existingLessonsLimit caps the number of role-scoped lessons rendered
	// into the prompt. The decomposer reads these to avoid duplicating
	// existing lessons; more than 10 is rarely useful and just chews
	// prompt budget.
	existingLessonsLimit = 10
)

// Component implements the lesson-decomposer processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Dispatch dependencies (Phase 2b). All optional at boot — when any
	// is missing the component logs the receipt and skips dispatch
	// rather than crashing, so misconfigured deployments don't take down
	// the rejection path.
	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer
	decoder       *message.Decoder

	// inFlight maps a dispatched TaskID → the dispatch context (request +
	// resolved model name). Populated when handleMessage publishes a
	// TaskMessage; consumed by the AGENT_LOOPS watcher when the loop
	// reaches terminal state. A nil map is treated as no in-flight
	// dispatches (test-only path with no dispatch wiring). Carrying the
	// model in the entry lets logRejection attribute every dropped
	// lesson to a (model, prompt_version) tuple — the partition key the
	// named-quirks list (audit sites A.1/A.3/D.6) reads from.
	inFlightMu sync.Mutex
	inFlight   map[string]*inFlightDispatch

	// Lifecycle.
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics — non-rejection counters stay atomic.Int64 (out of
	// audit-035 scope; one-call-site each, no per-label discrimination
	// required).
	requestsReceived atomic.Int64
	requestsSkipped  atomic.Int64
	parseErrors      atomic.Int64
	dispatched       atomic.Int64
	dispatchFailures atomic.Int64
	lessonsRecorded  atomic.Int64

	// Per-rejection-reason counters. ADR-035 audit site B.3 + the
	// Prometheus migration. The CounterVec is the metric exposed at
	// /metrics under semspec_lesson_decomposer_rejections_total{reason=...};
	// the four cached Counter handles below let logRejection
	// dispatch a single .Inc() per fire without a per-call
	// WithLabelValues map lookup. Both shapes are populated by
	// NewComponent so logRejection sees non-nil counters whether or
	// not deps.MetricsRegistry was provided (registration only governs
	// /metrics exposure).
	rejectionsCounter         *prometheus.CounterVec
	parseErrorRejections      prometheus.Counter
	missingFieldsRejections   prometheus.Counter
	missingEvidenceRejections prometheus.Counter
	emptyEvidenceRejections   prometheus.Counter

	lastActivityMu sync.RWMutex
	lastActivity   time.Time
}

// NewComponent constructs a lesson-decomposer Component from raw JSON config
// and semstreams dependencies.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.FilterSubject == "" {
		config.FilterSubject = defaults.FilterSubject
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	// Initialize prompt assembler with software domain.
	registry := prompt.NewRegistry()
	registry.RegisterAll(promptdomain.Software()...)
	registry.Register(prompt.ToolGuidanceFragment(prompt.DefaultToolGuidance()))
	registry.Register(prompt.GraphManifestFragment(workflowtools.RegistrySummaryFetchFn()))
	assembler := prompt.NewAssembler(registry)

	tw := &graphutil.TripleWriter{
		NATSClient:    deps.NATSClient,
		Logger:        logger,
		ComponentName: "lesson-decomposer",
	}

	// Build the per-rejection-reason CounterVec and pre-warm its four
	// children so logRejection's switch dispatches to a non-nil counter
	// whether or not the metrics registry is available. Registration
	// against /metrics happens below, gated on deps.MetricsRegistry.
	rejectionsCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "semspec_lesson_decomposer_rejections_total",
			Help: "Total fires of lesson-decomposer rejection classes (ADR-035 audit B.3). Labeled by reason: parse_error / missing_fields / missing_evidence / empty_evidence.",
		},
		[]string{"reason"},
	)

	c := &Component{
		name:                      "lesson-decomposer",
		config:                    config,
		natsClient:                deps.NATSClient,
		logger:                    logger,
		modelRegistry:             deps.ModelRegistry,
		toolRegistry:              deps.ToolRegistry,
		assembler:                 assembler,
		lessonWriter:              &lessons.Writer{TW: tw, Logger: logger},
		decoder:                   message.NewDecoder(deps.PayloadRegistry),
		inFlight:                  make(map[string]*inFlightDispatch),
		rejectionsCounter:         rejectionsCounter,
		parseErrorRejections:      rejectionsCounter.WithLabelValues(string(rejectionParseError)),
		missingFieldsRejections:   rejectionsCounter.WithLabelValues(string(rejectionMissingFields)),
		missingEvidenceRejections: rejectionsCounter.WithLabelValues(string(rejectionMissingEvidence)),
		emptyEvidenceRejections:   rejectionsCounter.WithLabelValues(string(rejectionEmptyEvidence)),
	}

	// Register the CounterVec with the metrics registry so per-reason
	// fires surface at /metrics. Idempotent — the registry handles
	// duplicate registration. Nil-safe — tests construct Components
	// without deps and skip registration; .Inc() still works on the
	// unregistered counter.
	if deps.MetricsRegistry != nil {
		if err := deps.MetricsRegistry.RegisterCounterVec("lesson-decomposer", "rejections_total", rejectionsCounter); err != nil {
			return nil, fmt.Errorf("register lesson-decomposer rejection counters: %w", err)
		}
	}

	return c, nil
}

// Initialize prepares the component for startup.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized lesson-decomposer",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"filter", c.config.FilterSubject,
		"enabled", c.config.Enabled,
	)
	return nil
}

// Start begins consuming LessonDecomposeRequested messages from JetStream
// and watching AGENT_LOOPS for our loop completions.
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

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.FilterSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("consume decompose triggers: %w", err)
	}

	// Watch AGENT_LOOPS for terminal state on our dispatched tasks.
	go c.watchLoopCompletions(subCtx)

	c.logger.Info("lesson-decomposer started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.FilterSubject,
		"enabled", c.config.Enabled,
	)
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

	c.logger.Info("lesson-decomposer stopped",
		"requests_received", c.requestsReceived.Load(),
		"requests_skipped", c.requestsSkipped.Load(),
		"parse_errors", c.parseErrors.Load(),
		"dispatched", c.dispatched.Load(),
		"dispatch_failures", c.dispatchFailures.Load(),
		"lessons_recorded", c.lessonsRecorded.Load(),
		"parse_error_rejections", int64(testutil.ToFloat64(c.parseErrorRejections)),
		"missing_fields_rejections", int64(testutil.ToFloat64(c.missingFieldsRejections)),
		"missing_evidence_rejections", int64(testutil.ToFloat64(c.missingEvidenceRejections)),
		"empty_evidence_rejections", int64(testutil.ToFloat64(c.emptyEvidenceRejections)),
	)
	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "lesson-decomposer",
		Type:        "processor",
		Description: "ADR-033 Phase 2+: produces evidence-cited lessons from reviewer-rejection trajectories",
		Version:     "0.2.0",
	}
}

// InputPorts returns the input port definitions.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "decompose-requests",
			Direction:   component.DirectionInput,
			Required:    true,
			Description: "Reviewer-rejection signals to decompose into evidence-cited lessons",
			Config:      component.NATSPort{Subject: c.config.FilterSubject},
		},
	}
}

// OutputPorts returns the output port definitions.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "decomposer-task",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "Dispatches one-shot decomposer agent tasks via agentic-loop",
			Config:      component.NATSPort{Subject: agentTaskSubject},
		},
	}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return lessonDecomposerSchema
}

// rejectionCount sums the four per-reason rejection counter handles.
// Nil-safe so a zero-valued Component (used by tests that construct
// `&Component{}` literals to verify lifecycle defaults) doesn't panic
// when Health() runs before NewComponent populates the handles.
func (c *Component) rejectionCount() int {
	var total int
	if c.parseErrorRejections != nil {
		total += int(testutil.ToFloat64(c.parseErrorRejections))
	}
	if c.missingFieldsRejections != nil {
		total += int(testutil.ToFloat64(c.missingFieldsRejections))
	}
	if c.missingEvidenceRejections != nil {
		total += int(testutil.ToFloat64(c.missingEvidenceRejections))
	}
	if c.emptyEvidenceRejections != nil {
		total += int(testutil.ToFloat64(c.emptyEvidenceRejections))
	}
	return total
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

	// parseErrors counts envelope-decode failures at request intake;
	// parseErrorRejections counts loop-result JSON-parse failures at
	// completion. Distinct buckets that look near-collision-y when
	// grepping logs — both are summed into ErrorCount but stay separate
	// in their per-reason readers.
	return component.HealthStatus{
		Healthy:   running,
		LastCheck: time.Now(),
		ErrorCount: int(c.parseErrors.Load()) + int(c.dispatchFailures.Load()) +
			c.rejectionCount(),
		Uptime: time.Since(startTime),
		Status: status,
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

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage parses a LessonDecomposeRequested payload, fetches the
// developer's trajectory, builds the decomposer prompt, and dispatches
// a one-shot LLM task. Result is consumed asynchronously by the
// AGENT_LOOPS watcher.
//
// All branches ack the message — failure paths must not block the
// rejection flow that produced the request.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	c.requestsReceived.Add(1)
	c.updateLastActivity()

	if !c.config.Enabled {
		c.requestsSkipped.Add(1)
		c.ackOrWarn(msg, "skipped (disabled)")
		return
	}

	req, ok := c.parseRequest(msg)
	if !ok {
		return
	}

	if err := c.dispatchDecomposer(ctx, req); err != nil {
		c.dispatchFailures.Add(1)
		c.logger.Error("Lesson decompose dispatch failed",
			"slug", req.Slug, "task_id", req.TaskID, "error", err)
	}

	c.ackOrWarn(msg, "processed")
}

// parseRequest unwraps the BaseMessage envelope and returns the typed
// payload. Any failure increments parseErrors, acks the message (so it
// isn't redelivered forever), and returns ok=false. Uses message.Decoder
// so the payload registry resolves *LessonDecomposeRequested from the
// envelope's type fields — raw json.Unmarshal cannot do this without the
// registry hook.
func (c *Component) parseRequest(msg jetstream.Msg) (*payloads.LessonDecomposeRequested, bool) {
	if c.decoder == nil {
		c.parseErrors.Add(1)
		c.logger.Error("Decoder not initialised — payload registry missing")
		c.ackOrWarn(msg, "no decoder")
		return nil, false
	}
	base, err := c.decoder.Decode(msg.Data())
	if err != nil {
		c.parseErrors.Add(1)
		c.logger.Error("Failed to decode BaseMessage envelope", "error", err)
		c.ackOrWarn(msg, "malformed envelope")
		return nil, false
	}
	req, ok := base.Payload().(*payloads.LessonDecomposeRequested)
	if !ok {
		c.parseErrors.Add(1)
		c.logger.Error("Unexpected payload type", "type", fmt.Sprintf("%T", base.Payload()))
		c.ackOrWarn(msg, "wrong type")
		return nil, false
	}
	if err := req.Validate(); err != nil {
		c.parseErrors.Add(1)
		c.logger.Error("Invalid LessonDecomposeRequested payload", "error", err)
		c.ackOrWarn(msg, "invalid payload")
		return nil, false
	}
	return req, true
}

// dispatchDecomposer is the load-bearing path: fetch trajectory, build
// prompt, publish TaskMessage. Returns the first error encountered;
// caller decides whether to log/retry.
func (c *Component) dispatchDecomposer(ctx context.Context, req *payloads.LessonDecomposeRequested) error {
	if c.modelRegistry == nil || c.assembler == nil {
		c.logger.Warn("Decomposer dispatch skipped: dispatch dependencies not wired",
			"slug", req.Slug, "model_registry", c.modelRegistry != nil, "assembler", c.assembler != nil)
		return nil
	}

	devLoopID := req.DeveloperLoopID
	if devLoopID == "" {
		devLoopID = req.LoopID
	}

	devSteps := c.fetchSteps(ctx, devLoopID, "developer")
	revSteps := c.fetchSteps(ctx, req.ReviewerLoopID, "reviewer")
	existing := c.fetchExistingLessons(ctx, "developer")

	// Phase 6: any verdict that isn't a rejection is treated as a
	// positive-lesson dispatch. Today only "approved" arrives that way
	// (gated by execution-manager's EnablePositiveLessons flag); future
	// producers can extend the surface.
	positive := req.Verdict == "approved"

	promptCtx := &prompt.LessonDecomposerPromptContext{
		Verdict:         req.Verdict,
		Feedback:        req.Feedback,
		Source:          req.Source,
		TargetRole:      "developer",
		Positive:        positive,
		DeveloperLoopID: devLoopID,
		ReviewerLoopID:  req.ReviewerLoopID,
		DeveloperSteps:  devSteps,
		ReviewerSteps:   revSteps,
		ExistingLessons: existing,
		Scenario: &prompt.DecomposerScenarioContext{
			ID: req.ScenarioID,
		},
	}

	modelName := c.modelRegistry.Resolve(string(model.CapabilityLessonDecomposition))
	if modelName == "" {
		// Capability not configured by deployment — fall back to reviewing
		// since the decomposer is a reviewer-shaped read-and-judge task.
		modelName = c.modelRegistry.Resolve(string(model.CapabilityReviewing))
	}
	provider := resolveProvider(c.modelRegistry, modelName)
	maxTokens := resolveMaxTokens(c.modelRegistry, modelName)

	asmCtx := &prompt.AssemblyContext{
		Role:                   prompt.RoleLessonDecomposer,
		Provider:               provider,
		Domain:                 "software",
		AvailableTools:         prompt.FilterTools(c.availableToolNames(), prompt.RoleLessonDecomposer),
		SupportsTools:          true,
		MaxTokens:              maxTokens,
		Persona:                prompt.GlobalPersonas().ForRole(prompt.RoleLessonDecomposer),
		Vocabulary:             prompt.GlobalPersonas().Vocabulary(),
		LessonDecomposerPrompt: promptCtx,
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		return fmt.Errorf("prompt render: %w", assembled.RenderError)
	}

	taskID := fmt.Sprintf("decompose-%s-%s", req.Slug, uuid.New().String())
	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForDeliverable(c.toolRegistry, "lesson", c.availableToolNames()...),
		ToolChoice:   &agentic.ToolChoice{Mode: "required"},
		WorkflowSlug: workflow.WorkflowSlugLessonDecomposition,
		WorkflowStep: stepDecompose,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":         req.Slug,
			"task_id":           req.TaskID,
			"requirement_id":    req.RequirementID,
			"scenario_id":       req.ScenarioID,
			"developer_loop_id": devLoopID,
			"reviewer_loop_id":  req.ReviewerLoopID,
			"deliverable_type":  "lesson",
			"trigger_source":    req.Source,
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "lesson-decomposer")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task message: %w", err)
	}

	c.trackInFlight(taskID, req, modelName)

	if err := c.natsClient.PublishToStream(ctx, agentTaskSubject, data); err != nil {
		c.untrackInFlight(taskID)
		return fmt.Errorf("publish task: %w", err)
	}

	c.dispatched.Add(1)
	c.logger.Info("Dispatched lesson-decomposer agent",
		"slug", req.Slug,
		"task_id", taskID,
		"model", modelName,
		"developer_loop_id", devLoopID,
		"developer_steps", len(devSteps),
		"system_chars", assembled.SystemMessageChars,
	)
	return nil
}

// inFlightDispatch carries a tracked dispatch's originating request plus
// the resolved model name. The model is captured at dispatch time so
// post-dispatch log lines can attribute rejections to a (model,
// prompt_version) tuple — the partition key the named-quirks list
// (audit sites A.1/A.3/D.6) reads from. ADR-035 audit site B.3.
type inFlightDispatch struct {
	Req   *payloads.LessonDecomposeRequested
	Model string
}

// rejectionReason classifies why a lesson-decomposer dispatch's output
// was rejected. Used as the structured `reason` field on Warn logs and
// to discriminate per-reason counters. Cardinality is bounded — adding
// a new class requires both a new constant here and a corresponding
// counter on Component (so operators can grep logs by reason and read
// counters by reason).
type rejectionReason string

const (
	rejectionParseError      rejectionReason = "parse_error"
	rejectionMissingFields   rejectionReason = "missing_fields"
	rejectionMissingEvidence rejectionReason = "missing_evidence"
	rejectionEmptyEvidence   rejectionReason = "empty_evidence"

	// rejectionRawHeadBytes caps the raw-response excerpt logged on every
	// rejection. The head is sufficient to characterize most quirks
	// (markdown fences, prose prefixes, malformed openings) without
	// blowing log volume on long agent outputs.
	rejectionRawHeadBytes = 512
)

// rejectionRawHead truncates raw response output to a fixed cap so the
// log line stays bounded but still captures the head of the model's
// output for quirk characterization.
func rejectionRawHead(s string) string {
	if len(s) <= rejectionRawHeadBytes {
		return s
	}
	return s[:rejectionRawHeadBytes]
}

// classifyBuildLessonError maps a buildLesson error to a rejectionReason
// using the sentinels from result.go. Falls back to
// rejectionMissingFields when no sentinel matches — that's the safest
// default for an unrecognized buildLesson failure (the verb "missing"
// still applies to whatever required field tripped it). errors.Is
// ordering matters: empty-evidence is checked before no-evidence
// because in principle a future error could wrap both, and empty-
// evidence is the more specific signal.
func classifyBuildLessonError(err error) rejectionReason {
	switch {
	case errors.Is(err, errLessonEmptyEvidence):
		return rejectionEmptyEvidence
	case errors.Is(err, errLessonNoEvidence):
		return rejectionMissingEvidence
	case errors.Is(err, errLessonNilResult), errors.Is(err, errLessonMissingFields):
		return rejectionMissingFields
	default:
		return rejectionMissingFields
	}
}

// logRejection emits a uniform structured Warn for every lesson-
// decomposer rejection class and increments the per-reason counter.
//
// ADR-035 audit site B.3: makes silent drops greppable so operators
// can characterize quirks from logs alone, before the named-quirks
// list (audit sites A.1/A.3/D.6) lands its formal incident-triple
// emission. Once that infrastructure is in place this helper is the
// natural integration point — the per-reason counter and structured
// fields here become the inputs to the parse-incident triple.
//
// Attempt is currently always 1 — the lesson-decomposer is one-shot
// per request today. The field is retained on the log line so a future
// retry-with-hint port (deferred per ADR-035 step 3) doesn't need to
// change the log shape, only the value.
func (c *Component) logRejection(reason rejectionReason, loop *agentic.LoopEntity, disp *inFlightDispatch, err error) {
	switch reason {
	case rejectionParseError:
		c.parseErrorRejections.Inc()
	case rejectionMissingFields:
		c.missingFieldsRejections.Inc()
	case rejectionMissingEvidence:
		c.missingEvidenceRejections.Inc()
	case rejectionEmptyEvidence:
		c.emptyEvidenceRejections.Inc()
	}
	c.logger.Warn("Lesson-decomposer rejection",
		"reason", string(reason),
		"slug", disp.Req.Slug,
		"task_id", loop.TaskID,
		"loop_id", loop.ID,
		"model", disp.Model,
		"error", err,
		"raw_head", rejectionRawHead(loop.Result),
		"attempt", 1,
	)
}

func (c *Component) trackInFlight(taskID string, req *payloads.LessonDecomposeRequested, modelName string) {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	if c.inFlight == nil {
		c.inFlight = make(map[string]*inFlightDispatch)
	}
	c.inFlight[taskID] = &inFlightDispatch{Req: req, Model: modelName}
}

func (c *Component) untrackInFlight(taskID string) *inFlightDispatch {
	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	if c.inFlight == nil {
		return nil
	}
	disp := c.inFlight[taskID]
	delete(c.inFlight, taskID)
	return disp
}

// availableToolNames returns the tool names registered with the component.
// Empty slice when the registry is nil — callers handle that as "no
// tools" via terminal.ToolsForDeliverable's allowlist semantics.
func (c *Component) availableToolNames() []string {
	if c.toolRegistry == nil {
		return nil
	}
	tools := c.toolRegistry.ListTools()
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// fetchSteps pulls a trajectory by loop ID and pre-summarises each step
// for the prompt builder. Returns an empty slice on any failure — the
// decomposer can still produce a useful lesson from feedback alone, and
// the prompt renderer surfaces a "trajectory unavailable" notice when
// the loop ID was set but no steps came back.
func (c *Component) fetchSteps(ctx context.Context, loopID, label string) []prompt.TrajectoryStepSummary {
	if loopID == "" {
		return nil
	}
	traj, err := fetchTrajectory(ctx, c.natsClient, loopID, trajectoryStepLimit)
	if err != nil {
		c.logger.Warn("Trajectory fetch failed",
			"loop_id", loopID, "label", label, "error", err)
		return nil
	}
	out := make([]prompt.TrajectoryStepSummary, 0, len(traj.Steps))
	for i, step := range traj.Steps {
		out = append(out, prompt.TrajectoryStepSummary{
			Index:   i,
			Summary: summarizeStep(step, 200),
		})
	}
	return out
}

// fetchExistingLessons reads the role-scoped lessons already in the
// graph so the decomposer can avoid duplicating them. Bounded by
// existingLessonsLimit. Soft-fails to nil on any error.
func (c *Component) fetchExistingLessons(ctx context.Context, role string) []string {
	if c.lessonWriter == nil {
		return nil
	}
	lessonsList, err := c.lessonWriter.ListLessonsForRole(ctx, role, existingLessonsLimit)
	if err != nil {
		c.logger.Debug("Existing-lessons fetch failed",
			"role", role, "error", err)
		return nil
	}
	out := make([]string, 0, len(lessonsList))
	for _, l := range lessonsList {
		summary := l.Summary
		if l.InjectionForm != "" {
			summary = l.InjectionForm
		}
		if summary == "" {
			continue
		}
		out = append(out, summary)
	}
	return out
}

// resolveProvider maps the resolved model name to a provider for prompt
// formatting. Falls back to ProviderOllama (markdown style) when the
// registry can't classify the endpoint.
func resolveProvider(reg ssmodel.RegistryReader, modelName string) prompt.Provider {
	if reg == nil || modelName == "" {
		return prompt.ProviderOllama
	}
	ep := reg.GetEndpoint(modelName)
	if ep == nil {
		return prompt.ProviderOllama
	}
	switch ep.Provider {
	case "anthropic":
		return prompt.ProviderAnthropic
	case "openai":
		return prompt.ProviderOpenAI
	case "google":
		return prompt.ProviderGoogle
	default:
		return prompt.ProviderOllama
	}
}

func resolveMaxTokens(reg ssmodel.RegistryReader, modelName string) int {
	if reg == nil || modelName == "" {
		return 0
	}
	if ep := reg.GetEndpoint(modelName); ep != nil {
		return ep.MaxTokens
	}
	return 0
}

// ackOrWarn acks the JetStream message and logs a warning on failure
// (the warning includes the disposition so logs still tell the story).
func (c *Component) ackOrWarn(msg jetstream.Msg, disposition string) {
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "disposition", disposition, "error", err)
	}
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for terminal
// loop entries with our WorkflowSlug and consumes them via
// handleLoopCompletion. Replay entries that were already processed by a
// previous incarnation are skipped — the inFlight map starts empty on
// every Start, so any pre-replay entry has no matching dispatch context.
func (c *Component) watchLoopCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot watch AGENT_LOOPS: no JetStream", "error", err)
		return
	}
	bucket, err := workflow.WaitForKVBucket(ctx, js, "AGENT_LOOPS")
	if err != nil {
		c.logger.Warn("AGENT_LOOPS bucket not available — loop completion watcher disabled", "error", err)
		return
	}
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch AGENT_LOOPS", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for lesson-decomposition)")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		var loop agentic.LoopEntity
		if err := json.Unmarshal(entry.Value(), &loop); err != nil {
			continue
		}
		if !loop.State.IsTerminal() {
			continue
		}
		if loop.WorkflowSlug != workflow.WorkflowSlugLessonDecomposition {
			continue
		}
		c.handleLoopCompletion(ctx, &loop)
	}
}

// handleLoopCompletion is the consumer side of the dispatch flow.
// Looks up the dispatching request, parses the agent's submit_work
// output, and writes the resulting Lesson via lessons.Writer.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity) {
	disp := c.untrackInFlight(loop.TaskID)
	if disp == nil {
		// Either replay before we tracked it, or a foreign loop with
		// our slug. Skip silently — alerting on replay would just be
		// noise, and a misrouted loop is the agentic-loop processor's
		// to surface.
		return
	}

	c.updateLastActivity()

	if loop.Outcome != agentic.OutcomeSuccess {
		c.dispatchFailures.Add(1)
		c.logger.Warn("Lesson-decomposer loop did not succeed",
			"slug", disp.Req.Slug,
			"task_id", loop.TaskID,
			"loop_id", loop.ID,
			"model", disp.Model,
			"outcome", loop.Outcome,
			"error", loop.Error,
		)
		return
	}

	parsed, err := parseDecomposerResult(loop.Result)
	if err != nil {
		c.logRejection(rejectionParseError, loop, disp, err)
		return
	}

	scenarioID := disp.Req.ScenarioID
	positive := disp.Req.Verdict == "approved"
	lesson, err := buildLesson(parsed, scenarioID, "developer", positive)
	if err != nil {
		c.logRejection(classifyBuildLessonError(err), loop, disp, err)
		return
	}

	// Generate a stable ID up-front so the log line ties the dispatch
	// task back to the persisted lesson. lessonWriter would generate
	// one anyway when ID is empty, but we want to emit it.
	if lesson.ID == "" {
		lesson.ID = uuid.New().String()
	}

	if c.lessonWriter == nil {
		c.logger.Warn("Lesson skipped — writer not wired",
			"slug", disp.Req.Slug, "lesson_id", lesson.ID)
		return
	}

	// Use WithoutCancel so the write isn't aborted when the watcher's
	// context cancels mid-shutdown — best-effort durable write to the
	// graph is the right shape for an audit log.
	graphCtx := context.WithoutCancel(ctx)
	if err := c.lessonWriter.RecordLesson(graphCtx, lesson); err != nil {
		c.dispatchFailures.Add(1)
		c.logger.Error("Failed to record decomposer lesson",
			"slug", disp.Req.Slug,
			"lesson_id", lesson.ID,
			"error", err,
		)
		return
	}

	c.lessonsRecorded.Add(1)
	c.logger.Info("Decomposer lesson recorded",
		"slug", disp.Req.Slug,
		"task_id", loop.TaskID,
		"loop_id", loop.ID,
		"lesson_id", lesson.ID,
		"role", lesson.Role,
		"root_cause_role", lesson.RootCauseRole,
		"evidence_steps", len(lesson.EvidenceSteps),
		"evidence_files", len(lesson.EvidenceFiles),
		"entity_id", agentgraph.LessonEntityID(lesson.ID),
	)
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
