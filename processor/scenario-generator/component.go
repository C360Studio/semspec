// Package scenariogenerator provides a processor that generates BDD scenarios
// from plan requirements by dispatching a scenario-generator agent through
// agentic-dispatch.
//
// Each requirement dispatches ONE task. Completions arrive independently via
// AGENT_LOOPS KV watch, and each sends its own per-requirement mutation to
// plan-manager. Plan-manager handles convergence (detecting when all
// requirements have scenarios). This component does no convergence tracking.
package scenariogenerator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/prompt"
	promptdomain "github.com/c360studio/semspec/prompt/domain"
	"github.com/c360studio/semspec/tools/terminal"
	workflowtools "github.com/c360studio/semspec/tools/workflow"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/dispatchretry"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/lessons"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	ssmodel "github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// workflowSlugPlanning identifies planning workflows in agent TaskMessages.
	// Shared with the planner and requirement-generator — all belong to semspec-planning.
	workflowSlugPlanning = "semspec-planning"

	// stepScenarioGeneration is the workflow step for scenario generation.
	stepScenarioGeneration = "scenario-generation"

	// subjectScenarioGeneratorTask is the NATS subject for scenario-generator agent tasks.
	subjectScenarioGeneratorTask = "agent.task.scenario-generation"
)

// scenarioRetryPayload is the per-key context retryOrFail needs to re-dispatch
// scenario generation: the original request and reviewFindings (preserved
// across error retries per ADR-029 H1). Stored as the Payload field of a
// dispatchretry.Entry under the composite "slug/requirementID" key.
type scenarioRetryPayload struct {
	req            *payloads.ScenarioGeneratorRequest
	reviewFindings string
}

// Component implements the scenario-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// retry tracks per-requirement attempts. Keyed by "slug/requirementID";
	// payload is *scenarioRetryPayload.
	retry *dispatchretry.State

	// Metrics
	triggersProcessed  atomic.Int64
	scenariosGenerated atomic.Int64
	generationsFailed  atomic.Int64
	lastActivityMu     sync.RWMutex
	lastActivity       time.Time
}

// ---------------------------------------------------------------------------
// Result payload
// ---------------------------------------------------------------------------

// ScenarioGeneratorResultType is the message type for scenario generator results.
var ScenarioGeneratorResultType = message.Type{
	Domain:   "workflow",
	Category: "scenario-generator-result",
	Version:  "v1",
}

// Result is the result payload for scenario generation.
type Result struct {
	RequirementID string              `json:"requirement_id"`
	Slug          string              `json:"slug"`
	ScenarioCount int                 `json:"scenario_count"`
	Scenarios     []workflow.Scenario `json:"scenarios"`
	Status        string              `json:"status"`
}

// Schema implements message.Payload.
func (r *Result) Schema() message.Type { return ScenarioGeneratorResultType }

// Validate implements message.Payload.
func (r *Result) Validate() error { return nil }

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

// ---------------------------------------------------------------------------
// LLM response shape (kept for parsing loop results)
// ---------------------------------------------------------------------------

// llmScenario is the raw JSON shape returned by the agent before we assign IDs.
type llmScenario struct {
	Given string   `json:"given"`
	When  string   `json:"when"`
	Then  []string `json:"then"`
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewComponent creates a new scenario-generator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for any zero-value fields.
	defaults := DefaultConfig()
	if config.DefaultCapability == "" {
		config.DefaultCapability = defaults.DefaultCapability
	}
	if config.PlanStateBucket == "" {
		config.PlanStateBucket = defaults.PlanStateBucket
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	if config.MaxGenerationRetries == 0 {
		config.MaxGenerationRetries = defaults.MaxGenerationRetries
	}
	if config.RetryBackoffMs <= 0 {
		config.RetryBackoffMs = defaults.RetryBackoffMs
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
		ComponentName: "scenario-generator",
	}

	return &Component{
		name:          "scenario-generator",
		config:        config,
		natsClient:    deps.NATSClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		toolRegistry:  deps.ToolRegistry,
		assembler:     assembler,
		lessonWriter:  &lessons.Writer{TW: tw, Logger: logger},
		retry: dispatchretry.New(dispatchretry.Config{
			MaxRetries: config.MaxGenerationRetries,
			BackoffMs:  config.RetryBackoffMs,
		}),
	}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized scenario-generator",
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins processing scenario generation triggers.
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

	// Watch PLAN_STATES for plans reaching "requirements_generated" status.
	// This is the KV twofer — the write IS the trigger.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot start PLAN_STATES watcher: no JetStream", "error", err)
	} else {
		go c.watchPlanStates(subCtx, js)
	}

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

	c.logger.Info("scenario-generator started",
		"plan_state_bucket", c.config.PlanStateBucket)

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

	c.logger.Info("scenario-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"scenarios_generated", c.scenariosGenerated.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// ---------------------------------------------------------------------------
// Agent dispatch
// ---------------------------------------------------------------------------

// dispatchScenarioGenerator dispatches a scenario-generator agent loop via
// agentic-dispatch for a single requirement. previousError is non-empty on
// retry attempts and is appended to the prompt so the LLM can self-correct.
func (c *Component) dispatchScenarioGenerator(ctx context.Context, req *payloads.ScenarioGeneratorRequest, previousError string, reviewFindings ...string) {
	c.updateLastActivity()

	taskID := fmt.Sprintf("scengen-%s-%s-%s", req.Slug, req.RequirementID, uuid.New().String())
	c.retry.SetActiveLoop(req.Slug+"/"+req.RequirementID, taskID)

	scenCtx := &prompt.ScenarioGeneratorPromptContext{
		PlanGoal:               req.PlanGoal,
		RequirementID:          req.RequirementID,
		RequirementTitle:       req.RequirementTitle,
		RequirementDescription: req.RequirementDescription,
		ArchitectureContext:    req.ArchitectureContext,
		PreviousError:          previousError,
	}
	if len(reviewFindings) > 0 {
		scenCtx.ReviewFindings = reviewFindings[0]
	}

	// Resolve model for planning capability.
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(capability)

	// Assemble system prompt via fragment pipeline.
	provider := c.resolveProvider()
	var maxTokens int
	if c.modelRegistry != nil {
		if ep := c.modelRegistry.GetEndpoint(modelName); ep != nil {
			maxTokens = ep.MaxTokens
		}
	}
	asmCtx := &prompt.AssemblyContext{
		Role:                    prompt.RoleScenarioGenerator,
		Provider:                provider,
		Domain:                  "software",
		AvailableTools:          prompt.FilterTools(c.availableToolNames(), prompt.RoleScenarioGenerator),
		SupportsTools:           true,
		MaxTokens:               maxTokens,
		Persona:                 prompt.GlobalPersonas().ForRole(prompt.RoleScenarioGenerator),
		Vocabulary:              prompt.GlobalPersonas().Vocabulary(),
		ScenarioGeneratorPrompt: scenCtx,
	}

	// Wire role-scoped lessons learned.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.RotateLessonsForRole(graphCtx, "scenario-generator", 10); err == nil && len(roleLessons) > 0 {
			tk := &prompt.LessonsLearned{}
			for _, les := range roleLessons {
				tk.Lessons = append(tk.Lessons, prompt.LessonEntry{
					Category:      les.Source,
					Summary:       les.Summary,
					InjectionForm: les.InjectionForm,
					Positive:      les.Positive,
					Role:          les.Role,
				})
			}
			asmCtx.LessonsLearned = tk
		}
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		c.logger.Error("Scenario-generator user-prompt render failed",
			"slug", req.Slug, "requirement_id", req.RequirementID, "error", assembled.RenderError)
		return
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForDeliverable(c.toolRegistry, "scenarios", c.availableToolNames()...),
		ToolChoice:   &agentic.ToolChoice{Mode: "required"},
		WorkflowSlug: workflowSlugPlanning,
		WorkflowStep: stepScenarioGeneration,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        req.Slug,
			"requirement_id":   req.RequirementID,
			"deliverable_type": "scenarios",
			"task_id":          "main", // scenario-gen reads repo root, no isolated worktree
		},
	}

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-scenario-generator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal task message, rejecting plan",
			"slug", req.Slug, "requirement_id", req.RequirementID, "error", err)
		c.sendGenerationFailed(ctx, req.Slug, fmt.Sprintf("scenario dispatch marshal failed for requirement %s: %v", req.RequirementID, err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectScenarioGeneratorTask, data); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch scenario generator, rejecting plan",
			"slug", req.Slug, "requirement_id", req.RequirementID, "error", err)
		c.sendGenerationFailed(ctx, req.Slug, fmt.Sprintf("scenario dispatch failed for requirement %s: %v", req.RequirementID, err))
		return
	}

	c.logger.Info("Dispatched scenario generator agent",
		"slug", req.Slug,
		"requirement_id", req.RequirementID,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// ---------------------------------------------------------------------------
// Loop completion watcher
// ---------------------------------------------------------------------------

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for scenario-generator
// agent completions. When a loop reaches terminal state with WorkflowSlug
// matching the planning workflow and WorkflowStep matching scenario-generation,
// the result is parsed and a per-requirement mutation is sent to plan-manager.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for scenario-generation)")

	replayDone := false
	processedLoops := make(map[string]bool)

	for entry := range watcher.Updates() {
		if entry == nil {
			// End of initial KV replay. Subsequent entries are live updates.
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for scenario-generator")
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
		if loop.WorkflowSlug != workflowSlugPlanning {
			continue
		}
		if loop.WorkflowStep != stepScenarioGeneration {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		requirementID, _ := loop.Metadata["requirement_id"].(string)
		if slug == "" || requirementID == "" {
			continue
		}

		// During replay, skip mutation publishing — these loops already
		// produced mutations before the restart. Re-publishing would spam
		// plan-manager with stale scenario mutations.
		if !replayDone {
			c.logger.Debug("Replay: skipping completed scenario-generation loop",
				"slug", slug, "requirement_id", requirementID, "loop_id", loop.ID)
			continue
		}

		// Dedup: skip loops we have already processed in this session.
		if processedLoops[loop.ID] {
			c.logger.Debug("Skipping already-processed loop",
				"loop_id", loop.ID, "slug", slug, "requirement_id", requirementID)
			continue
		}
		processedLoops[loop.ID] = true

		c.handleLoopCompletion(ctx, &loop, slug, requirementID)
	}
}

// handleLoopCompletion processes a completed scenario-generator agent loop.
// It parses scenarios from the loop result and sends a per-requirement mutation
// to plan-manager via plan.mutation.scenarios.generated.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug, requirementID string) {
	c.updateLastActivity()
	key := slug + "/" + requirementID

	// Stale loop guard: drop completions from older dispatches that race a retry.
	if c.retry.IsStaleLoop(key, loop.TaskID) {
		c.logger.Debug("Dropping stale scenario loop completion (task ID mismatch)",
			"slug", slug, "requirement_id", requirementID,
			"loop_task_id", loop.TaskID, "loop_id", loop.ID)
		return
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		c.generationsFailed.Add(1)
		loopErrorMsg := loop.Error
		if loopErrorMsg == "" {
			loopErrorMsg = fmt.Sprintf("agent loop ended with outcome %q", loop.Outcome)
		}
		c.logger.Error("Scenario generator agent loop failed",
			"slug", slug,
			"requirement_id", requirementID,
			"loop_id", loop.ID,
			"outcome", loop.Outcome,
			"error", loopErrorMsg)
		c.retryOrFail(ctx, slug, requirementID, loopErrorMsg)
		return
	}

	scenarios, err := c.parseScenariosFromResult(loop.Result, slug, requirementID)
	if err != nil {
		c.generationsFailed.Add(1)
		parseErrorMsg := fmt.Sprintf("failed to parse scenarios: %s", err.Error())
		c.logger.Error("Failed to parse scenarios from agent result",
			"slug", slug,
			"requirement_id", requirementID,
			"loop_id", loop.ID,
			"error", err)
		c.retryOrFail(ctx, slug, requirementID, parseErrorMsg)
		return
	}

	// Check if the plan has moved past generating_scenarios while we were working.
	// If so, our result is stale — discard it without rejecting the plan.
	if kvPlan, loadErr := c.loadPlanFromKV(ctx, slug); loadErr == nil {
		status := kvPlan.EffectiveStatus()
		if status != workflow.StatusGeneratingScenarios {
			c.logger.Warn("Plan advanced past generating_scenarios, discarding stale result",
				"slug", slug,
				"requirement_id", requirementID,
				"current_status", status,
				"loop_id", loop.ID)
			c.retry.Clear(key)
			return
		}
	}

	// Build a synthetic trigger for publishResults — it only needs Slug,
	// RequirementID, and TraceID (which we leave empty for agentic-dispatch path).
	trigger := &payloads.ScenarioGeneratorRequest{
		Slug:          slug,
		RequirementID: requirementID,
	}

	if err := c.publishResults(ctx, trigger, scenarios); err != nil {
		// If the plan advanced past generating_scenarios, this is NOT a generation
		// failure — our result is simply stale. Discard without rejecting.
		if strings.Contains(err.Error(), "invalid transition") {
			c.logger.Warn("Scenario mutation rejected (plan advanced), discarding stale result",
				"slug", slug,
				"requirement_id", requirementID,
				"error", err,
				"loop_id", loop.ID)
			c.retry.Clear(key)
			return
		}
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to send scenario mutation, rejecting plan",
			"slug", slug,
			"requirement_id", requirementID,
			"loop_id", loop.ID,
			"error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("scenario mutation publish failed for requirement %s: %v", requirementID, err))
		return
	}

	// Clean up retry state on success.
	c.retry.Clear(key)

	c.scenariosGenerated.Add(int64(len(scenarios)))
	c.logger.Info("Scenarios generated via agentic-dispatch and mutation accepted",
		"slug", slug,
		"requirement_id", requirementID,
		"loop_id", loop.ID,
		"scenario_count", len(scenarios))
}

// retryOrFail attempts to re-dispatch scenario generation with the error
// message appended to the prompt. When the maximum retry count is exceeded it
// calls sendGenerationFailed to mark the plan rejected.
//
// Unlike other migrated components, scenario-generator has no PLAN_STATES
// recovery path — the dispatch payload (request + reviewFindings) cannot be
// reconstructed from the plan alone (we'd lose architecture context, etc.).
// If the entry is missing on retry, fail-closed.
func (c *Component) retryOrFail(ctx context.Context, slug, requirementID, errorMsg string) {
	key := slug + "/" + requirementID

	if _, ok := c.retry.Snapshot(key); !ok {
		c.logger.Warn("retryOrFail: no retry state found, failing immediately",
			"slug", slug, "requirement_id", requirementID)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	entry, retryOK := c.retry.Tick(ctx, key)
	if entry == nil {
		if ctx.Err() != nil {
			c.logger.Debug("retryOrFail aborted during backoff",
				"slug", slug, "requirement_id", requirementID, "error", ctx.Err())
			return
		}
		c.logger.Warn("retryOrFail: lost retry context, failing immediately",
			"slug", slug, "requirement_id", requirementID)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	payload, _ := entry.Payload.(*scenarioRetryPayload)
	if payload == nil {
		c.logger.Warn("retryOrFail: payload type mismatch, failing immediately",
			"slug", slug, "requirement_id", requirementID)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	if !retryOK {
		c.logger.Error("Scenario generation exceeded max retries, failing plan",
			"slug", slug,
			"requirement_id", requirementID,
			"attempts", entry.Count,
			"max_retries", c.config.MaxGenerationRetries,
			"error", errorMsg)
		c.sendGenerationFailed(ctx, slug, errorMsg)
		return
	}

	c.logger.Warn("Retrying scenario generation with feedback",
		"slug", slug,
		"requirement_id", requirementID,
		"attempt", entry.Count,
		"max_retries", c.config.MaxGenerationRetries,
		"error", errorMsg)

	// Preserve review findings across error retries so the agent continues to
	// address completeness gaps flagged by the reviewer (ADR-029 H1).
	c.dispatchScenarioGenerator(ctx, payload.req, errorMsg, payload.reviewFindings)
}

// loadPlanFromKV reads a plan from the PLAN_STATES KV bucket. Used to check
// whether the plan has advanced past our phase before publishing stale results.
func (c *Component) loadPlanFromKV(ctx context.Context, slug string) (*workflow.Plan, error) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("no JetStream: %w", err)
	}
	bucket, err := js.KeyValue(ctx, c.config.PlanStateBucket)
	if err != nil {
		return nil, fmt.Errorf("PLAN_STATES bucket: %w", err)
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("get plan %q: %w", slug, err)
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan %q: %w", slug, err)
	}
	return &plan, nil
}

// sendGenerationFailed publishes a generation.failed mutation so plan-manager
// marks the plan rejected and surfaces the error to the caller.
func (c *Component) sendGenerationFailed(ctx context.Context, slug, feedback string) {
	failReq, _ := json.Marshal(map[string]string{
		"slug":  slug,
		"phase": "scenario-generation",
		"error": feedback,
	})
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed", failReq,
		10*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Warn("Failed to publish generation.failed mutation",
			"slug", slug, "error", err)
	}
}

// parseScenariosFromResult extracts and validates scenario JSON from an agent
// loop result string, then assigns IDs based on the slug and requirement ID.
func (c *Component) parseScenariosFromResult(result, slug, requirementID string) ([]workflow.Scenario, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	// The agent output wraps scenarios in {"scenarios": [...]} per output format fragment.
	// Try array extraction first, then object extraction.
	jsonContent := extractJSONArray(result)
	if jsonContent == "" {
		jsonContent = extractJSONObject(result)
	}
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in result")
	}

	// Try direct array parse.
	var raw []llmScenario
	if err := json.Unmarshal([]byte(jsonContent), &raw); err != nil {
		// Try unwrapping from {"scenarios": [...]} object.
		var wrapper struct {
			Scenarios []llmScenario `json:"scenarios"`
		}
		if wrapErr := json.Unmarshal([]byte(jsonContent), &wrapper); wrapErr == nil && len(wrapper.Scenarios) > 0 {
			raw = wrapper.Scenarios
		} else {
			maxLen := len(jsonContent)
			if maxLen > 200 {
				maxLen = 200
			}
			return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:maxLen])
		}
	}

	if len(raw) < 1 {
		return nil, fmt.Errorf("expected at least 1 scenario, got 0")
	}

	reqSeq := requirementSequence(requirementID)

	now := time.Now()
	scenarios := make([]workflow.Scenario, len(raw))
	for i, s := range raw {
		if s.Given == "" {
			return nil, fmt.Errorf("scenario %d missing 'given' field", i+1)
		}
		if s.When == "" {
			return nil, fmt.Errorf("scenario %d missing 'when' field", i+1)
		}
		if len(s.Then) == 0 {
			return nil, fmt.Errorf("scenario %d missing 'then' field (must be non-empty array)", i+1)
		}

		scenarioID := fmt.Sprintf("scenario.%s.%s.%d", slug, reqSeq, i+1)
		scenarios[i] = workflow.Scenario{
			ID:            scenarioID,
			RequirementID: requirementID,
			Given:         s.Given,
			When:          s.When,
			Then:          s.Then,
			Status:        workflow.ScenarioStatusPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}

	return scenarios, nil
}

// requirementSequence extracts the trailing sequence suffix from a requirement ID.
// Given "requirement.my-plan.3", it returns "3". Falls back to the full ID if
// no suffix can be extracted cleanly.
func requirementSequence(requirementID string) string {
	parts := strings.Split(requirementID, ".")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return requirementID
}

// extractJSONArray returns the first JSON array found in s, or "".
func extractJSONArray(s string) string {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '[' {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// extractJSONObject returns the first JSON object found in s, or "".
func extractJSONObject(s string) string {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Event publication
// ---------------------------------------------------------------------------

// publishResults publishes a ScenariosForRequirementGeneratedEvent carrying
// the full scenario data. Plan-manager (the single writer) handles persistence,
// convergence checking, and status transitions.
func (c *Component) publishResults(ctx context.Context, trigger *payloads.ScenarioGeneratorRequest, scenarios []workflow.Scenario) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	// Send results to plan-manager via request/reply (KV twofer — manager writes, watchers react).
	mutationReq := struct {
		Slug          string              `json:"slug"`
		RequirementID string              `json:"requirement_id"`
		Scenarios     []workflow.Scenario `json:"scenarios"`
		TraceID       string              `json:"trace_id,omitempty"`
	}{
		Slug:          trigger.Slug,
		RequirementID: trigger.RequirementID,
		Scenarios:     scenarios,
		TraceID:       trigger.TraceID,
	}

	data, err := json.Marshal(mutationReq)
	if err != nil {
		return fmt.Errorf("marshal scenarios mutation: %w", err)
	}

	if c.natsClient == nil {
		return fmt.Errorf("nats client not configured")
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.scenarios.generated", data, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("scenarios mutation request: %w", err)
	}

	var mutResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp, &mutResp); err != nil || !mutResp.Success {
		errMsg := "unknown error"
		if err != nil {
			errMsg = err.Error()
		} else if mutResp.Error != "" {
			errMsg = mutResp.Error
		}
		return fmt.Errorf("scenarios mutation failed: %s", errMsg)
	}

	c.logger.Info("Scenarios sent to plan-manager via mutation",
		"slug", trigger.Slug,
		"requirement_id", trigger.RequirementID,
		"scenario_count", len(scenarios))

	return nil
}

// ---------------------------------------------------------------------------
// Component.Discoverable implementation
// ---------------------------------------------------------------------------

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "scenario-generator",
		Type:        "processor",
		Description: "Generates BDD scenarios from requirements via agentic-dispatch",
		Version:     "0.2.0",
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
	return scenarioGeneratorSchema
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

// resolveProvider determines the LLM provider for prompt formatting.
func (c *Component) resolveProvider() prompt.Provider {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}
	modelName := c.modelRegistry.Resolve(capability)
	if endpoint := c.modelRegistry.GetEndpoint(modelName); endpoint != nil {
		return prompt.Provider(endpoint.Provider)
	}
	return prompt.ProviderOllama
}

// availableToolNames returns the full list of tool names for prompt assembly.
// Actual tool availability is controlled by agentic-tools at runtime.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work",
	}
}
