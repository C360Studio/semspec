// Package taskgenerator provides a processor that generates tasks from plans
// using LLM agents based on the plan's Goal, Context, and Scope.
package taskgenerator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	semstreamsWorkflow "github.com/c360studio/semstreams/pkg/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// maxFormatRetries is the total number of LLM call attempts when the response
// isn't valid JSON. On each retry, the parse error is fed back to the LLM as a
// correction prompt so it can fix the output format.
const maxFormatRetries = 5

// llmCompleter is the subset of the LLM client used by task-generator.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the task-generator processor.
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
	tasksGenerated    atomic.Int64
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
	return "task-review-loop"
}

// Phase returns the phase name this component represents.
func (c *Component) Phase() string {
	return phases.TasksGenerated
}

// StateManager returns nil - this component updates state directly via KV bucket.
// The reactive engine manages state; we just update it on completion.
func (c *Component) StateManager() *semstreamsWorkflow.StateManager {
	return nil
}

// NewComponent creates a new task-generator processor.
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

	// Initialize context helper for centralized context building
	ctxHelper := contexthelper.New(deps.NATSClient, contexthelper.Config{
		SubjectPrefix:  config.ContextSubjectPrefix,
		ResponseBucket: config.ContextResponseBucket,
		Timeout:        config.GetContextTimeout(),
		SourceName:     "task-generator",
	}, logger)

	return &Component{
		name:       "task-generator",
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
	c.logger.Debug("Initialized task-generator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
	return nil
}

// Start begins processing task generation triggers.
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

	// Get or create workflow state bucket
	stateBucket, err := js.KeyValue(subCtx, c.config.StateBucket)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("get state bucket %s: %w", c.config.StateBucket, err)
	}
	c.stateBucket = stateBucket

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

	c.logger.Info("task-generator started",
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

// handleMessage processes a single task generation trigger.
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

	// Parse the reactive engine's BaseMessage-wrapped payload.
	trigger, err := reactive.ParseReactivePayload[reactive.TaskGeneratorRequest](msg.Data())
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

	c.logger.Info("Processing task generation trigger",
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

	// Generate tasks using LLM
	tasks, llmRequestIDs, err := c.generateTasks(llmCtx, trigger)
	if err != nil {
		c.handleTriggerFailure(ctx, msg, trigger, "Failed to generate tasks", err)
		return
	}

	// Save tasks to file
	if err := c.saveTasks(ctx, trigger, tasks); err != nil {
		c.handleTriggerFailure(ctx, msg, trigger, "Failed to save tasks", err)
		return
	}

	// Publish success notification
	if err := c.publishResult(ctx, trigger, tasks, llmRequestIDs); err != nil {
		c.logger.Warn("Failed to publish result notification",
			"request_id", trigger.RequestID,
			"slug", trigger.Slug,
			"error", err)
		// Don't fail - tasks were saved successfully
	}

	c.tasksGenerated.Add(int64(len(tasks)))

	// ACK the message
	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Tasks generated successfully",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug,
		"task_count", len(tasks))
}

// handleTriggerFailure handles a failed task generation or save operation.
func (c *Component) handleTriggerFailure(ctx context.Context, msg jetstream.Msg, trigger *reactive.TaskGeneratorRequest, operation string, err error) {
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
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Warn("Failed to ACK message", "error", ackErr)
		}
		return
	}

	// Legacy path: NAK for retry
	c.logger.Debug("No ExecutionID - NAKing for retry",
		"request_id", trigger.RequestID,
		"slug", trigger.Slug)
	if nakErr := msg.Nak(); nakErr != nil {
		c.logger.Warn("Failed to NAK message", "error", nakErr)
	}
}

// transitionToFailure transitions the workflow to the generator-failed phase.
func (c *Component) transitionToFailure(ctx context.Context, executionID string, cause string) error {
	entry, err := c.stateBucket.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get workflow state: %w", err)
	}

	var state reactive.TaskReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	state.Phase = phases.TaskGeneratorFailed
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
		"phase", phases.TaskGeneratorFailed,
		"cause", cause)
	return nil
}

// generateTasks calls the LLM to generate tasks from the plan.
// It follows the graph-first pattern by requesting context from the
// centralized context-builder before making the LLM call.
func (c *Component) generateTasks(ctx context.Context, trigger *reactive.TaskGeneratorRequest) ([]workflow.Task, []string, error) {
	// The prompt should already be in trigger.Prompt from the command
	prompt := trigger.Prompt
	if prompt == "" {
		return nil, nil, fmt.Errorf("no prompt provided in trigger")
	}

	// Step 0: Load phases for phase-aware task generation
	phases := c.loadPhases(ctx, trigger.Slug)

	// Step 1: Request task generation context from centralized context-builder (graph-first)
	// Pass the capability so context-builder can calculate the correct token budget
	// based on the model that will actually be used for LLM calls.
	var graphContext string
	var sopRequirements []string
	resp := c.contextHelper.BuildContextGraceful(ctx, &contextbuilder.ContextBuildRequest{
		TaskType:   contextbuilder.TaskTypePlanning, // Task generation is part of planning
		Topic:      trigger.Title,
		Capability: c.config.DefaultCapability,
	})
	if resp != nil {
		graphContext = contexthelper.FormatContextResponse(resp)
		sopRequirements = resp.SOPRequirements
		c.logger.Info("Built task generation context via context-builder",
			"title", trigger.Title,
			"entities", len(resp.Entities),
			"documents", len(resp.Documents),
			"sop_requirements", len(sopRequirements),
			"tokens_used", resp.TokensUsed)
	} else {
		c.logger.Warn("Context build returned nil, proceeding without graph context",
			"title", trigger.Title)
	}

	// Step 2: Enrich prompt with graph context and SOP requirements
	if graphContext != "" {
		prompt = fmt.Sprintf("%s\n\n## Codebase Context\n\nThe following context from the knowledge graph provides information about the existing codebase structure. Use this to generate tasks that reference actual files and patterns:\n\n%s", prompt, graphContext)
	}
	if len(sopRequirements) > 0 {
		prompt = prompt + "\n\n" + prompts.FormatSOPRequirements(sopRequirements)
	}

	// Step 3: Call LLM via client with format retry loop
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	temperature := 0.7
	messages := []llm.Message{{Role: "user", Content: prompt}}
	var lastErr error
	var tasks []workflow.Task
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

		// Parse JSON from response
		parsedTasks, parseErr := c.parseTasksFromResponse(llmResp.Content, trigger.Slug, phases)
		if parseErr == nil {
			tasks = parsedTasks
			break
		}

		lastErr = parseErr

		if attempt+1 >= maxFormatRetries {
			break
		}

		c.logger.Warn("Task generator LLM format retry",
			"attempt", attempt+1,
			"error", parseErr)

		// Add correction prompt for retry
		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: formatCorrectionPrompt(parseErr)},
		)
	}

	if tasks == nil {
		return nil, llmRequestIDs, fmt.Errorf("parse tasks from response after %d attempts: %w", maxFormatRetries, lastErr)
	}

	// Step 4: Validate and auto-correct hallucinated file paths
	if resp != nil {
		knownFiles := extractKnownFiles(resp)
		if len(knownFiles) > 0 {
			tasks = c.validateTaskFiles(tasks, knownFiles)
		}
	}

	return tasks, llmRequestIDs, nil
}

// loadPhases loads phases from the plan directory. Returns nil if no phases exist.
func (c *Component) loadPhases(ctx context.Context, slug string) []workflow.Phase {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			c.logger.Warn("Failed to get working directory for phase loading", "error", err)
			return nil
		}
	}

	manager := workflow.NewManager(repoRoot)
	phases, err := manager.LoadPhases(ctx, slug)
	if err != nil {
		c.logger.Warn("Failed to load phases for task generation", "slug", slug, "error", err)
		return nil
	}

	if len(phases) > 0 {
		c.logger.Info("Loaded phases for phase-aware task generation",
			"slug", slug,
			"phase_count", len(phases))
	}

	return phases
}

// parseTasksFromResponse extracts tasks from the LLM response content.
// When phases are provided, it validates and assigns phase_id to each task.
func (c *Component) parseTasksFromResponse(content, slug string, phases []workflow.Phase) ([]workflow.Task, error) {
	// Extract JSON from the response (may be wrapped in markdown code blocks)
	jsonContent := llm.ExtractJSON(content)
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var resp prompts.TaskGeneratorResponse
	if err := json.Unmarshal([]byte(jsonContent), &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
	}

	// Build phase ID lookup for validation
	phaseIDs := make(map[string]bool, len(phases))
	for _, p := range phases {
		phaseIDs[p.ID] = true
	}

	// Convert to workflow.Task
	tasks := make([]workflow.Task, len(resp.Tasks))
	now := time.Now()
	planID := workflow.PlanEntityID(slug)

	for i, genTask := range resp.Tasks {
		seq := i + 1
		tasks[i] = workflow.Task{
			ID:          workflow.TaskEntityID(slug, seq),
			PlanID:      planID,
			Sequence:    seq,
			Description: genTask.Description,
			Type:        workflow.TaskType(genTask.Type),
			Status:      workflow.TaskStatusPendingApproval,
			Files:       genTask.Files,
			DependsOn:   normalizeDependsOn(genTask.DependsOn, slug),
			CreatedAt:   now,
		}

		// Assign phase_id when phases exist
		if len(phases) > 0 {
			phaseID := genTask.PhaseID
			if phaseID != "" && phaseIDs[phaseID] {
				tasks[i].PhaseID = phaseID
			} else {
				// Fallback: assign round-robin by sequence
				phaseIdx := i % len(phases)
				tasks[i].PhaseID = phases[phaseIdx].ID
				if phaseID != "" {
					c.logger.Warn("Task has invalid phase_id, assigned round-robin fallback",
						"task_seq", seq,
						"invalid_phase_id", phaseID,
						"assigned_phase_id", tasks[i].PhaseID)
				}
			}
		}

		// Convert acceptance criteria
		for _, ac := range genTask.AcceptanceCriteria {
			tasks[i].AcceptanceCriteria = append(tasks[i].AcceptanceCriteria, workflow.AcceptanceCriterion{
				Given: ac.Given,
				When:  ac.When,
				Then:  ac.Then,
			})
		}

		// Default type if not specified
		if tasks[i].Type == "" {
			tasks[i].Type = workflow.TaskTypeImplement
		}
	}

	return tasks, nil
}

// normalizeDependsOn converts depends_on references to full entity IDs.
// LLM may output various formats:
//   - "{slug}" placeholder: "task.{slug}.1" or "task.{slug}-1"
//   - Short reference: "task.slug-1" → "c360.semspec.workflow.task.task.slug-1"
//   - Already full ID: "c360.semspec.workflow.task.task.slug-1" → unchanged
//
// The canonical task ID format uses dash before sequence: {slug}-{seq}
// but LLMs often use dot: {slug}.{seq}. We normalize both.
func normalizeDependsOn(deps []string, slug string) []string {
	if len(deps) == 0 {
		return nil
	}

	const entityPrefix = "c360.semspec.workflow.task.task."
	const shortPrefix = "task."

	normalized := make([]string, 0, len(deps))
	for _, dep := range deps {
		// Replace {slug} placeholder with actual slug
		dep = strings.ReplaceAll(dep, "{slug}", slug)

		// If already a full entity ID, use as-is
		if strings.HasPrefix(dep, entityPrefix) {
			normalized = append(normalized, fixSequenceSeparator(dep))
			continue
		}

		// Convert short "task." prefix to full entity ID
		if strings.HasPrefix(dep, shortPrefix) {
			suffix := strings.TrimPrefix(dep, shortPrefix)
			normalized = append(normalized, fixSequenceSeparator(entityPrefix+suffix))
			continue
		}

		// For any other format, assume it's a suffix and prepend full prefix
		// This handles cases like "slug-1" or just "1"
		normalized = append(normalized, fixSequenceSeparator(entityPrefix+dep))
	}
	return normalized
}

// fixSequenceSeparator normalizes the separator before the trailing sequence number.
// LLMs often produce "slug.1" but canonical IDs use "slug-1".
// Converts trailing ".N" to "-N" where N is one or more digits.
func fixSequenceSeparator(id string) string {
	// Find the last dot
	lastDot := strings.LastIndex(id, ".")
	if lastDot < 0 || lastDot == len(id)-1 {
		return id
	}
	suffix := id[lastDot+1:]
	// Check if everything after the last dot is digits
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return id
		}
	}
	// Replace the dot with a dash
	return id[:lastDot] + "-" + suffix
}

// formatCorrectionPrompt builds a correction message for the LLM when
// the task generation response isn't valid JSON.
func formatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed as JSON. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON object matching this structure:\n"+
			"```json\n"+
			"{\n"+
			"  \"tasks\": [\n"+
			"    {\n"+
			"      \"description\": \"Clear description of what to implement\",\n"+
			"      \"type\": \"implement\" or \"test\" or \"document\" or \"review\" or \"refactor\",\n"+
			"      \"depends_on\": [],\n"+
			"      \"acceptance_criteria\": [\n"+
			"        {\n"+
			"          \"given\": \"a specific precondition or state\",\n"+
			"          \"when\": \"an action is performed\",\n"+
			"          \"then\": \"the expected outcome or behavior\"\n"+
			"        }\n"+
			"      ],\n"+
			"      \"files\": [\"path/to/relevant/file.go\"]\n"+
			"    }\n"+
			"  ]\n"+
			"}\n"+
			"```",
		err.Error(),
	)
}

// extractKnownFiles parses the file tree document from context-builder response
// and returns a list of known file paths. The __file_tree__ document contains
// lines like "- path/to/file.go" under a "# Project File Tree" heading.
func extractKnownFiles(resp *contextbuilder.ContextBuildResponse) []string {
	tree, ok := resp.Documents["__file_tree__"]
	if !ok || tree == "" {
		return nil
	}

	var files []string
	for _, line := range strings.Split(tree, "\n") {
		line = strings.TrimSpace(line)
		// File tree lines are formatted as "- path/to/file"
		if strings.HasPrefix(line, "- ") {
			path := strings.TrimPrefix(line, "- ")
			path = strings.TrimSpace(path)
			if path != "" && !strings.HasPrefix(path, "#") {
				files = append(files, path)
			}
		}
	}
	return files
}

// validateTaskFiles checks each task's file paths against the known project files
// and attempts to auto-correct hallucinated paths. When a task file doesn't match
// any known file, it tries fuzzy matching by basename. Uncorrectable hallucinations
// are logged as warnings so the system can surface them.
func (c *Component) validateTaskFiles(tasks []workflow.Task, knownFiles []string) []workflow.Task {
	knownSet, basenameMap := buildFileLookups(knownFiles)
	for i := range tasks {
		if len(tasks[i].Files) > 0 {
			tasks[i].Files = c.correctTaskFilePaths(tasks[i], knownSet, basenameMap)
		}
	}
	return tasks
}

// buildFileLookups constructs lowercase lookup maps for exact and basename matching.
func buildFileLookups(knownFiles []string) (map[string]bool, map[string][]string) {
	knownSet := make(map[string]bool, len(knownFiles))
	basenameMap := make(map[string][]string)
	for _, f := range knownFiles {
		lower := strings.ToLower(f)
		knownSet[lower] = true
		if idx := strings.LastIndex(lower, "/"); idx >= 0 {
			basenameMap[lower[idx+1:]] = append(basenameMap[lower[idx+1:]], f)
		} else {
			basenameMap[lower] = append(basenameMap[lower], f)
		}
	}
	return knownSet, basenameMap
}

// correctTaskFilePaths filters and corrects a single task's file list.
func (c *Component) correctTaskFilePaths(task workflow.Task, knownSet map[string]bool, basenameMap map[string][]string) []string {
	corrected := make([]string, 0, len(task.Files))
	for _, taskFile := range task.Files {
		lower := strings.ToLower(taskFile)
		if strings.ContainsAny(taskFile, "*?") {
			continue // skip glob patterns
		}
		if knownSet[lower] {
			corrected = append(corrected, taskFile)
			continue
		}
		if best := findBestMatch(lower, basenameMap); best != "" {
			c.logger.Info("Auto-corrected hallucinated task file path",
				"original", taskFile, "corrected", best, "task", task.Description)
			corrected = append(corrected, best)
		} else {
			c.logger.Warn("Task references non-existent file (hallucinated path)",
				"file", taskFile, "task", task.Description, "task_id", task.ID)
		}
	}
	return corrected
}

// findBestMatch attempts to find the closest known file path for a hallucinated path.
// It tries basename match, then stem overlap, then directory-segment overlap.
func findBestMatch(lower string, basenameMap map[string][]string) string {
	basename := lower
	if idx := strings.LastIndex(lower, "/"); idx >= 0 {
		basename = lower[idx+1:]
	}
	stem := basename
	if dotIdx := strings.LastIndex(stem, "."); dotIdx > 0 {
		stem = stem[:dotIdx]
	}

	// Exact basename match
	if matches, ok := basenameMap[basename]; ok && len(matches) == 1 {
		return matches[0]
	}

	// Stem overlap match
	if len(stem) >= 3 {
		for knownBasename, paths := range basenameMap {
			ks := knownBasename
			if dotIdx := strings.LastIndex(ks, "."); dotIdx > 0 {
				ks = ks[:dotIdx]
			}
			if (strings.Contains(ks, stem) || strings.Contains(stem, ks)) && len(paths) == 1 {
				return paths[0]
			}
		}
	}

	// Directory-segment overlap match
	for _, part := range strings.Split(lower, "/") {
		if len(part) < 3 {
			continue
		}
		for knownBasename, paths := range basenameMap {
			ks := knownBasename
			if dotIdx := strings.LastIndex(ks, "."); dotIdx > 0 {
				ks = ks[:dotIdx]
			}
			if (ks == part || strings.Contains(ks, part) || strings.Contains(part, ks)) && len(paths) == 1 {
				return paths[0]
			}
		}
	}
	return ""
}

// saveTasks saves the generated tasks to the plan's tasks.json file.
func (c *Component) saveTasks(ctx context.Context, trigger *reactive.TaskGeneratorRequest, tasks []workflow.Task) error {
	// Check context cancellation before filesystem operations
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
	if err := manager.SaveTasks(ctx, tasks, trigger.Slug); err != nil {
		return err
	}

	// Update plan status to tasks_generated
	plan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		c.logger.Warn("Failed to load plan after saving tasks — status not updated",
			"slug", trigger.Slug, "error", err)
		return nil // Tasks saved successfully, non-fatal if status update fails
	}
	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusTasksGenerated); err != nil {
		c.logger.Warn("Failed to update plan status to tasks_generated",
			"slug", trigger.Slug, "error", err)
	}
	return nil
}

// TaskGeneratorResultType is the message type for task generator results.
var TaskGeneratorResultType = message.Type{Domain: "workflow", Category: "task-generator-result", Version: "v1"}

// Result is the result payload for task generation.
type Result struct {
	RequestID     string          `json:"request_id"`
	Slug          string          `json:"slug"`
	TaskCount     int             `json:"task_count"`
	Tasks         []workflow.Task `json:"tasks"`
	Status        string          `json:"status"`
	LLMRequestIDs []string        `json:"llm_request_ids,omitempty"`
}

// Schema implements message.Payload.
func (r *Result) Schema() message.Type {
	return TaskGeneratorResultType
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

// publishResult publishes a success notification for the task generation.
// Uses the workflow-processor's async callback pattern (ADR-005 Phase 6).
// publishResult updates the workflow state with the generated tasks.
// This transitions the workflow to the tasks-generated phase, which triggers
// the reactive engine to advance to the next step.
func (c *Component) publishResult(ctx context.Context, trigger *reactive.TaskGeneratorRequest, generatedTasks []workflow.Task, llmRequestIDs []string) error {
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
	var state reactive.TaskReviewState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		return fmt.Errorf("unmarshal workflow state: %w", err)
	}

	// Marshal tasks to JSON for storage
	tasksJSON, err := json.Marshal(generatedTasks)
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}

	// Update state with results
	state.TasksContent = tasksJSON
	state.TaskCount = len(generatedTasks)
	state.LLMRequestIDs = llmRequestIDs
	state.Phase = phases.TasksGenerated
	state.UpdatedAt = time.Now()

	// Write back to KV
	stateData, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal updated state: %w", err)
	}

	if _, err := c.stateBucket.Update(ctx, trigger.ExecutionID, stateData, entry.Revision()); err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}

	c.logger.Info("Updated workflow state with generated tasks",
		"slug", trigger.Slug,
		"execution_id", trigger.ExecutionID,
		"phase", phases.TasksGenerated,
		"task_count", len(generatedTasks))
	return nil
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

	c.logger.Info("task-generator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"tasks_generated", c.tasksGenerated.Load(),
		"generations_failed", c.generationsFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "task-generator",
		Type:        "processor",
		Description: "Generates tasks from plans using LLM with BDD acceptance criteria",
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
	return taskGeneratorSchema
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
