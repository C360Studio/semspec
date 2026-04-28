// Package requirementgenerator provides a processor that decomposes approved plans
// into structured Requirements by dispatching a requirement-generator agent through
// agentic-dispatch.
//
// The agent explores the codebase via graph and bash tools, reads the plan, and
// produces a JSON array of requirements. This replaces the previous inline
// llmClient.Complete() path which had zero codebase visibility.
package requirementgenerator

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
	// stepRequirementGeneration is the workflow step for requirement generation.
	stepRequirementGeneration = "requirement-generation"

	// subjectRequirementGenerationTask is the NATS subject for requirement-generation agent tasks.
	subjectRequirementGenerationTask = "agent.task.requirement-generation"
)

// RequirementsGeneratedType is the message type for requirements-generated events.
// This matches the type consumed by plan-api's dispatchCascadeEvent handler.
var RequirementsGeneratedType = message.Type{
	Domain:   "workflow",
	Category: "requirements-generated",
	Version:  "v1",
}

// requirementsGeneratedPayload wraps workflow.RequirementsGeneratedEvent to satisfy
// the message.Payload interface required by message.NewBaseMessage.
// The JSON layout is identical to RequirementsGeneratedEvent so plan-manager's
// ParseReactivePayload[workflow.RequirementsGeneratedEvent] can deserialise it.
type requirementsGeneratedPayload struct {
	Slug             string                 `json:"slug"`
	Requirements     []workflow.Requirement `json:"requirements"`
	RequirementCount int                    `json:"requirement_count"`
	TraceID          string                 `json:"trace_id,omitempty"`
}

func (p *requirementsGeneratedPayload) Schema() message.Type {
	return RequirementsGeneratedType
}

func (p *requirementsGeneratedPayload) Validate() error {
	if p.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	return nil
}

func (p *requirementsGeneratedPayload) MarshalJSON() ([]byte, error) {
	type Alias requirementsGeneratedPayload
	return json.Marshal((*Alias)(p))
}

func (p *requirementsGeneratedPayload) UnmarshalJSON(data []byte) error {
	type Alias requirementsGeneratedPayload
	return json.Unmarshal(data, (*Alias)(p))
}

// Result is the result payload for requirement generation.
// Registered in payload_registry.go and implements message.Payload.
type Result struct {
	Slug             string `json:"slug"`
	RequirementCount int    `json:"requirement_count"`
	Status           string `json:"status"`
}

// Schema implements message.Payload.
func (r *Result) Schema() message.Type {
	return message.Type{Domain: "workflow", Category: "requirement-generator-result", Version: "v1"}
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

// requirementItem is the agent-generated JSON shape for a single requirement.
// The agent is instructed to output an array of these objects.
type requirementItem struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on,omitempty"`  // titles of prerequisite requirements (resolved to IDs at build time)
	FilesOwned  []string `json:"files_owned,omitempty"` // workspace-relative paths this requirement may modify
}

// pendingDispatch records metadata for an in-flight requirement-generation dispatch.
// Used to reconstruct the publishResults call when the loop completes.
type pendingDispatch struct {
	trigger        *payloads.RequirementGeneratorRequest
	reviewFindings string // preserved across error retries (ADR-029)
}

// Component implements the requirement-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	modelRegistry ssmodel.RegistryReader
	toolRegistry  component.ToolRegistryReader
	assembler     *prompt.Assembler
	lessonWriter  *lessons.Writer

	// retry tracks per-plan attempts keyed by slug; payload is *pendingDispatch
	// (carrying the trigger + reviewFindings needed to re-publish on success
	// and re-dispatch with feedback on retry).
	retry *dispatchretry.State

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed     atomic.Int64
	requirementsGenerated atomic.Int64
	generationsFailed     atomic.Int64
	lastActivityMu        sync.RWMutex
	lastActivity          time.Time
}

// NewComponent creates a new requirement-generator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults
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
		ComponentName: "requirement-generator",
	}

	return &Component{
		name:          "requirement-generator",
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

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized requirement-generator",
		"plan_state_bucket", c.config.PlanStateBucket)
	return nil
}

// Start begins processing requirement-generator triggers.
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

	// Watch PLAN_STATES for plans reaching "approved" status.
	// This is the KV twofer — the write IS the trigger.
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Warn("Cannot start PLAN_STATES watcher: no JetStream", "error", err)
	} else {
		go c.watchPlanStates(subCtx, js)
	}

	// Loop completion watcher — picks up agentic-dispatch results from AGENT_LOOPS KV.
	go c.watchLoopCompletions(subCtx)

	c.logger.Info("requirement-generator started",
		"plan_state_bucket", c.config.PlanStateBucket)

	return nil
}

// dispatchRequirementGenerator dispatches a requirement-generator agent loop via
// agentic-dispatch. The agent reads the plan, explores the codebase, and outputs
// a JSON array of requirements. previousError, when non-empty, is appended to the
// prompt so the agent knows what went wrong in the prior attempt.
func (c *Component) dispatchRequirementGenerator(ctx context.Context, trigger *payloads.RequirementGeneratorRequest, previousError string, reviewFindings ...string) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	taskID := fmt.Sprintf("reqgen-%s-%s", trigger.Slug, uuid.New().String())

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
		Role:                 prompt.RoleRequirementGenerator,
		Provider:             provider,
		Domain:               "software",
		AvailableTools:       prompt.FilterTools(c.availableToolNames(), prompt.RoleRequirementGenerator),
		SupportsTools:        true,
		MaxTokens:            maxTokens,
		Persona:              prompt.GlobalPersonas().ForRole(prompt.RoleRequirementGenerator),
		Vocabulary:           prompt.GlobalPersonas().Vocabulary(),
		RequirementGenerator: buildRequirementGeneratorPromptContext(trigger, previousError, reviewFindings...),
	}

	// Wire role-scoped lessons learned.
	if c.lessonWriter != nil {
		graphCtx := context.WithoutCancel(ctx)
		if roleLessons, err := c.lessonWriter.ListLessonsForRole(graphCtx, "requirement-generator", 10); err == nil && len(roleLessons) > 0 {
			tk := &prompt.LessonsLearned{}
			for _, les := range roleLessons {
				tk.Lessons = append(tk.Lessons, prompt.LessonEntry{
					Category: les.Source,
					Summary:  les.Summary,
					Role:     les.Role,
				})
			}
			asmCtx.LessonsLearned = tk
		}
	}

	assembled := c.assembler.Assemble(asmCtx)
	if assembled.RenderError != nil {
		// User-prompt render failure is a registration / data-shape bug, not
		// a transient runtime error — fail the dispatch loud rather than
		// shipping an empty user message to the LLM.
		c.logger.Error("Requirement-generator user-prompt render failed",
			"slug", trigger.Slug, "error", assembled.RenderError)
		c.sendGenerationFailed(ctx, trigger.Slug, assembled.RenderError.Error())
		return
	}

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleGeneral,
		Model:        modelName,
		Prompt:       assembled.UserMessage,
		Tools:        terminal.ToolsForDeliverable(c.toolRegistry, "requirements", c.availableToolNames()...),
		ToolChoice:   &agentic.ToolChoice{Mode: "required"},
		WorkflowSlug: workflow.WorkflowSlugPlanning,
		WorkflowStep: stepRequirementGeneration,
		Context: &agentic.ConstructedContext{
			Content: assembled.SystemMessage,
		},
		Metadata: map[string]any{
			"plan_slug":        trigger.Slug,
			"deliverable_type": "requirements",
		},
	}

	// Track the dispatch context for retry and completion lookup. Track is
	// no-op when an entry already exists (retry re-entry preserves the
	// running count and the original trigger).
	var rf string
	if len(reviewFindings) > 0 {
		rf = reviewFindings[0]
	}
	c.retry.Track(trigger.Slug, &pendingDispatch{trigger: trigger, reviewFindings: rf})
	c.retry.SetActiveLoop(trigger.Slug, taskID)

	baseMsg := message.NewBaseMessage(task.Schema(), task, "semspec-requirement-generator")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.retry.Clear(trigger.Slug)
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to marshal task message, rejecting plan", "slug", trigger.Slug, "error", err)
		c.sendGenerationFailed(ctx, trigger.Slug, fmt.Sprintf("requirement dispatch marshal failed: %v", err))
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subjectRequirementGenerationTask, data); err != nil {
		c.retry.Clear(trigger.Slug)
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to dispatch requirement generator, rejecting plan", "slug", trigger.Slug, "error", err)
		c.sendGenerationFailed(ctx, trigger.Slug, fmt.Sprintf("requirement dispatch failed: %v", err))
		return
	}

	c.logger.Info("Dispatched requirement generator agent",
		"slug", trigger.Slug,
		"task_id", taskID,
		"model", modelName,
		"fragments", len(assembled.FragmentsUsed),
		"system_chars", assembled.SystemMessageChars)
}

// buildRequirementGeneratorPromptContext maps the trigger payload into the
// typed prompt-package context the user-prompt fragment renders against.
// All actual prompt content lives in
// prompt/domain/software_render.go::renderRequirementGeneratorPrompt — this
// function does pure data adaptation so the prompt and the dispatch logic
// can evolve independently. Replaces the legacy c.buildUserPrompt method
// the registry consumed before Plan B.
func buildRequirementGeneratorPromptContext(trigger *payloads.RequirementGeneratorRequest, previousError string, reviewFindings ...string) *prompt.RequirementGeneratorContext {
	rg := &prompt.RequirementGeneratorContext{
		Title:                 trigger.Title,
		Goal:                  trigger.Goal,
		Context:               trigger.Context,
		ReplaceRequirementIDs: trigger.ReplaceRequirementIDs,
		RejectionReasons:      trigger.RejectionReasons,
		PreviousError:         previousError,
	}
	if trigger.Scope != nil {
		rg.ScopeInclude = trigger.Scope.Include
		rg.ScopeExclude = trigger.Scope.Exclude
		rg.ScopeDoNotTouch = trigger.Scope.DoNotTouch
	}
	if len(trigger.ExistingRequirements) > 0 {
		rg.ExistingRequirements = make([]prompt.ExistingRequirementSummary, 0, len(trigger.ExistingRequirements))
		for _, r := range trigger.ExistingRequirements {
			rg.ExistingRequirements = append(rg.ExistingRequirements, prompt.ExistingRequirementSummary{
				ID:         r.ID,
				Title:      r.Title,
				Status:     string(r.Status),
				FilesOwned: r.FilesOwned,
				DependsOn:  r.DependsOn,
			})
		}
	}
	if len(reviewFindings) > 0 {
		rg.ReviewFindings = reviewFindings[0]
	}
	return rg
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for requirement-generation
// agent completions. When a loop reaches terminal state with WorkflowStep matching
// our step, the result is parsed and the requirements mutation is sent.
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

	c.logger.Info("Loop completion watcher started (watching AGENT_LOOPS for requirement-generation)")

	replayDone := false
	processedLoops := make(map[string]bool)

	for entry := range watcher.Updates() {
		if entry == nil {
			// Nil sentinel marks end of initial KV replay.
			replayDone = true
			c.logger.Info("AGENT_LOOPS replay complete for requirement-generator")
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
		if loop.WorkflowSlug != workflow.WorkflowSlugPlanning {
			continue
		}
		if loop.WorkflowStep != stepRequirementGeneration {
			continue
		}

		slug, _ := loop.Metadata["plan_slug"].(string)
		if slug == "" {
			continue
		}

		// During replay, skip — these loops already produced mutations
		// before the restart.
		if !replayDone {
			c.logger.Debug("Replay: skipping completed requirement-generation loop",
				"slug", slug, "loop_id", loop.ID)
			continue
		}

		// Dedup: skip loops we have already processed in this session.
		// Prevents double-processing if the KV entry is updated again
		// (e.g., completion metadata) or on watcher reconnection.
		if processedLoops[loop.ID] {
			c.logger.Debug("Skipping already-processed loop",
				"loop_id", loop.ID, "slug", slug)
			continue
		}
		processedLoops[loop.ID] = true

		dp := c.resolveDispatchContext(ctx, loop.TaskID, slug)
		if dp == nil {
			continue
		}

		// Stale loop guard: drop completions from older dispatches that race a retry.
		if c.retry.IsStaleLoop(slug, loop.TaskID) {
			c.logger.Debug("Dropping stale requirement-gen loop completion (task ID mismatch)",
				"slug", slug, "loop_task_id", loop.TaskID, "loop_id", loop.ID)
			continue
		}

		c.handleLoopCompletion(ctx, &loop, slug, dp)
	}
}

// handleLoopCompletion processes a completed requirement-generation agent loop.
// It parses the requirements from the loop result and calls publishResults.
// On failure or parse error, it retries up to MaxGenerationRetries times,
// passing the error text back to the agent as previousError. Once the retry
// limit is reached, plan.mutation.generation.failed is sent and the slug is
// cleaned up.
func (c *Component) handleLoopCompletion(ctx context.Context, loop *agentic.LoopEntity, slug string, dp *pendingDispatch) {
	c.updateLastActivity()
	trigger := dp.trigger

	// retryOrFail Tick's the retry counter and re-dispatches with feedback if
	// under the cap, otherwise sends generation.failed. Review findings are
	// preserved across retries via the dispatch payload (ADR-029 H1).
	retryOrFail := func(errMsg string) {
		entry, retryOK := c.retry.Tick(ctx, slug)
		if entry == nil {
			if ctx.Err() != nil {
				c.logger.Debug("retryOrFail aborted during backoff", "slug", slug, "error", ctx.Err())
				return
			}
			c.generationsFailed.Add(1)
			c.logger.Warn("retryOrFail: lost retry context, failing immediately", "slug", slug)
			c.sendGenerationFailed(ctx, slug, errMsg)
			return
		}
		if !retryOK {
			c.generationsFailed.Add(1)
			c.logger.Error("Requirement generation failed after max retries",
				"slug", slug,
				"loop_id", loop.ID,
				"max_retries", c.config.MaxGenerationRetries,
				"error", errMsg)
			c.sendGenerationFailed(ctx, slug, errMsg)
			return
		}
		c.logger.Warn("Retrying requirement generation",
			"slug", slug,
			"loop_id", loop.ID,
			"attempt", entry.Count,
			"max", c.config.MaxGenerationRetries,
			"reason", errMsg)
		go c.dispatchRequirementGenerator(ctx, trigger, errMsg, dp.reviewFindings)
	}

	if loop.Outcome != agentic.OutcomeSuccess {
		errMsg := loop.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("agent loop ended with outcome %q", loop.Outcome)
		}
		retryOrFail(errMsg)
		return
	}

	items, err := parseRequirementsFromResult(loop.Result)
	if err != nil {
		retryOrFail(err.Error())
		return
	}

	// Successful parse — clear retry state for this slug.
	c.retry.Clear(slug)

	// Check if the plan has moved past generating_requirements while we were working.
	// If so, our result is stale — discard it without rejecting the plan.
	if kvPlan, loadErr := c.loadPlanFromKV(ctx, slug); loadErr == nil {
		status := kvPlan.EffectiveStatus()
		if status != workflow.StatusGeneratingRequirements {
			c.logger.Warn("Plan advanced past generating_requirements, discarding stale result",
				"slug", slug,
				"current_status", status,
				"loop_id", loop.ID)
			return
		}
	}
	// If KV read fails, proceed with the mutation — plan-manager will validate.

	requirements := buildRequirementsFromItems(slug, trigger, items)

	if err := c.publishResults(ctx, trigger, requirements); err != nil {
		// If the plan advanced past generating_requirements, this is NOT a generation
		// failure — our result is simply stale. Discard without rejecting.
		if strings.Contains(err.Error(), "invalid transition") {
			c.logger.Warn("Requirements mutation rejected (plan advanced), discarding stale result",
				"slug", slug,
				"error", err,
				"loop_id", loop.ID)
			c.retry.Clear(slug)
			return
		}
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to publish requirements from loop completion, rejecting plan",
			"slug", slug,
			"loop_id", loop.ID,
			"error", err)
		c.sendGenerationFailed(ctx, slug, fmt.Sprintf("requirements mutation publish failed: %v", err))
		return
	}

	c.requirementsGenerated.Add(1)
	c.logger.Info("Requirements generated via agentic-dispatch and mutation accepted",
		"slug", slug,
		"loop_id", loop.ID,
		"requirement_count", len(requirements))
}

// buildRequirementsFromItems converts agent-emitted items into workflow.Requirement
// structs, assigning sequential IDs and handling the partial-regen sequence
// offset so new IDs don't collide with existing ones on the plan.
//
// DependsOn comes off the wire as a list of titles (the LLM doesn't see IDs
// at generation time); we resolve titles to IDs against the current batch
// AND the surviving existing requirements (partial regen needs to depend on
// kept reqs by title). Unknown titles are silently dropped — downstream
// ValidateRequirementDAG will catch dangling references on the resolved IDs,
// and we don't want a typo'd dependency title to block the rest of the plan.
//
// FilesOwned paths are normalized via normalizeFilePath before persistence so
// "./main.go" and "main.go" don't slip past the partition validator only to
// collide at git-merge time.
func buildRequirementsFromItems(slug string, trigger *payloads.RequirementGeneratorRequest, items []requirementItem) []workflow.Requirement {
	seqOffset := 0
	if len(trigger.ReplaceRequirementIDs) > 0 {
		seqOffset = len(trigger.ExistingRequirements)
	}
	planID := workflow.PlanEntityID(slug)
	now := time.Now()
	out := make([]workflow.Requirement, 0, len(items))

	// Title→ID lookup spans the new batch AND any kept existing requirements
	// from a partial regen, so a new replacement can declare depends_on on a
	// surviving sibling by title.
	titleToID := make(map[string]string, len(items)+len(trigger.ExistingRequirements))
	for _, r := range trigger.ExistingRequirements {
		if r.Status == workflow.RequirementStatusActive {
			titleToID[r.Title] = r.ID
		}
	}
	for i, item := range items {
		id := fmt.Sprintf("requirement.%s.%d", slug, seqOffset+i+1)
		titleToID[item.Title] = id // new wins; downstream validators catch dup-title cases
		out = append(out, workflow.Requirement{
			ID:          id,
			PlanID:      planID,
			Title:       item.Title,
			Description: item.Description,
			FilesOwned:  workflow.NormalizeFilePaths(item.FilesOwned),
			Status:      workflow.RequirementStatusActive,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	for i, item := range items {
		for _, depTitle := range item.DependsOn {
			if depID, ok := titleToID[depTitle]; ok && depID != out[i].ID {
				out[i].DependsOn = append(out[i].DependsOn, depID)
			}
		}
	}
	return out
}

// resolveDispatchContext looks up the dispatch context for a completed loop.
// It first checks the dispatchretry payload (active in-flight dispatch), then
// falls back to PLAN_STATES for restart recovery. Returns nil if context
// cannot be resolved.
//
// Unlike the pre-migration map, the dispatchretry payload is keyed by slug
// and is NOT deleted here — it stays around for the retry path. terminal
// success or fail-closed paths in handleLoopCompletion call retry.Clear(slug)
// to release it.
func (c *Component) resolveDispatchContext(ctx context.Context, taskID, slug string) *pendingDispatch {
	if snap, ok := c.retry.Snapshot(slug); ok {
		if dp, ok2 := snap.Payload.(*pendingDispatch); ok2 {
			return dp
		}
	}
	// Restart recovery: reconstruct dispatch context from PLAN_STATES and
	// re-Track so subsequent retry/dispatch see the same payload.
	recovered, err := c.recoverDispatchFromKV(ctx, slug)
	if err != nil {
		c.logger.Warn("No dispatch context found (in-memory or PLAN_STATES), skipping",
			"task_id", taskID, "slug", slug, "error", err)
		return nil
	}
	c.logger.Info("Recovered dispatch context from PLAN_STATES after restart",
		"task_id", taskID, "slug", slug)
	c.retry.Track(slug, recovered)
	return recovered
}

// recoverDispatchFromKV reads a plan from PLAN_STATES to reconstruct dispatch
// context after a restart when the in-memory pending map was lost.
func (c *Component) recoverDispatchFromKV(ctx context.Context, slug string) (*pendingDispatch, error) {
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
	return &pendingDispatch{
		trigger: &payloads.RequirementGeneratorRequest{
			Slug:                 slug,
			Title:                plan.Title,
			Goal:                 plan.Goal,
			Context:              plan.Context,
			Scope:                &plan.Scope,
			ExistingRequirements: plan.Requirements,
			// ReplaceRequirementIDs left empty — full regen on restart is safe
			// because the mutation replaces all requirements anyway.
		},
	}, nil
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

// sendGenerationFailed publishes a plan.mutation.generation.failed mutation to
// inform plan-manager that requirement generation has permanently failed for slug.
func (c *Component) sendGenerationFailed(ctx context.Context, slug, feedback string) {
	failReq, _ := json.Marshal(map[string]string{
		"slug":  slug,
		"phase": "requirement-generation",
		"error": feedback,
	})
	if _, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.generation.failed", failReq,
		10*time.Second, natsclient.DefaultRetryConfig()); err != nil {
		c.logger.Warn("Failed to publish generation.failed mutation",
			"slug", slug, "error", err)
	}
}

// parseRequirementsFromResult extracts a slice of requirementItem from an agent
// loop result string. The result may be a JSON array, a wrapped object, or
// markdown-fenced JSON.
func parseRequirementsFromResult(result string) ([]requirementItem, error) {
	if result == "" {
		return nil, fmt.Errorf("empty result")
	}

	// Try direct array parse first.
	var items []requirementItem
	if err := json.Unmarshal([]byte(result), &items); err == nil && len(items) > 0 {
		return validateRequirementItems(items)
	}

	// Try object with "requirements" key.
	var wrapper struct {
		Requirements []requirementItem `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(result), &wrapper); err == nil && len(wrapper.Requirements) > 0 {
		return validateRequirementItems(wrapper.Requirements)
	}

	// Try extracting JSON from markdown fences.
	jsonContent := extractJSONArray(result)
	if jsonContent == "" {
		jsonContent = extractJSONObject(result)
	}
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in result")
	}

	if err := json.Unmarshal([]byte(jsonContent), &items); err == nil && len(items) > 0 {
		return validateRequirementItems(items)
	}

	if err := json.Unmarshal([]byte(jsonContent), &wrapper); err == nil && len(wrapper.Requirements) > 0 {
		return validateRequirementItems(wrapper.Requirements)
	}

	return nil, fmt.Errorf("could not parse requirements from result (length %d)", len(result))
}

// validateRequirementItems checks that each item has a non-empty title.
func validateRequirementItems(items []requirementItem) ([]requirementItem, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("requirements array is empty")
	}
	for i, item := range items {
		if item.Title == "" {
			return nil, fmt.Errorf("requirement[%d] missing 'title' field", i)
		}
	}
	return items, nil
}

// extractJSONArray finds and returns the first JSON array in s, tolerating
// markdown code fences.
func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
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

// extractJSONObject finds and returns the first JSON object in s, tolerating
// markdown code fences.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
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

// publishResults merges (for partial regen) and sends requirements to plan-manager
// via request/reply (KV twofer — manager writes, watchers react).
func (c *Component) publishResults(ctx context.Context, trigger *payloads.RequirementGeneratorRequest, requirements []workflow.Requirement) error {
	// Partial regeneration: merge new requirements with existing approved ones
	// carried in the trigger payload (no graph reads needed).
	if len(trigger.ReplaceRequirementIDs) > 0 && len(trigger.ExistingRequirements) > 0 {
		replaceSet := make(map[string]bool, len(trigger.ReplaceRequirementIDs))
		for _, id := range trigger.ReplaceRequirementIDs {
			replaceSet[id] = true
		}
		// Keep existing requirements that aren't being replaced, append new ones.
		var merged []workflow.Requirement
		for _, existing := range trigger.ExistingRequirements {
			if !replaceSet[existing.ID] {
				merged = append(merged, existing)
			}
		}
		requirements = append(merged, requirements...)

		// Strip stale DependsOn references that point to replaced (now-gone) IDs.
		// Without this, ValidateRequirementDAG rejects the mutation because the
		// old IDs no longer exist in the merged set.
		idSet := make(map[string]bool, len(requirements))
		for _, r := range requirements {
			idSet[r.ID] = true
		}
		for i := range requirements {
			if len(requirements[i].DependsOn) == 0 {
				continue
			}
			// Allocate a fresh slice instead of reusing the existing backing
			// array via [:0] — the trigger.ExistingRequirements DependsOn slices
			// can share storage with cached plan state, and an in-place rewrite
			// would mutate that shared cache.
			valid := make([]string, 0, len(requirements[i].DependsOn))
			for _, dep := range requirements[i].DependsOn {
				if idSet[dep] {
					valid = append(valid, dep)
				}
			}
			requirements[i].DependsOn = valid
		}
	}

	// Send results to plan-manager via request/reply.
	mutationReq := struct {
		Slug         string                 `json:"slug"`
		Requirements []workflow.Requirement `json:"requirements"`
		TraceID      string                 `json:"trace_id,omitempty"`
	}{
		Slug:         trigger.Slug,
		Requirements: requirements,
		TraceID:      trigger.TraceID,
	}

	data, err := json.Marshal(mutationReq)
	if err != nil {
		return fmt.Errorf("marshal requirements mutation: %w", err)
	}

	resp, err := c.natsClient.RequestWithRetry(ctx, "plan.mutation.requirements.generated", data, 10*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("requirements mutation request: %w", err)
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
		return fmt.Errorf("requirements mutation failed: %s", errMsg)
	}

	c.logger.Info("Requirements sent to plan-manager via mutation",
		"slug", trigger.Slug,
		"requirement_count", len(requirements))

	return nil
}

// availableToolNames returns the full list of tool names for prompt assembly.
// Actual tool availability is controlled by agentic-tools at runtime.
func (c *Component) availableToolNames() []string {
	return []string{
		"bash", "submit_work", "ask_question",
		"graph_search", "graph_query",
	}
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

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}

	// Copy cancel function and clear state before releasing lock.
	cancel := c.cancel
	c.running = false
	c.cancel = nil
	c.mu.Unlock()

	// Cancel context after releasing lock to avoid potential deadlock.
	if cancel != nil {
		cancel()
	}

	c.logger.Info("requirement-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"requirements_generated", c.requirementsGenerated.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "requirement-generator",
		Type:        "processor",
		Description: "Generates Requirements for approved plans via agentic-dispatch",
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
	return requirementGeneratorSchema
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
