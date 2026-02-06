// Package workfloworchestrator provides a component that watches for agentic-loop
// completions and triggers the next workflow step based on configured rules.
// This enables autonomous mode where /propose --auto chains through all steps.
package workfloworchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semspec/workflow/validation"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// orchestratorSchema defines the configuration schema.
var orchestratorSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the workflow orchestrator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	// Rules configuration
	rules        *RulesFile
	registry     *model.Registry
	retryManager *validation.RetryManager
	validator    *validation.Validator
	repoPath     string
	mu           sync.RWMutex

	// Lifecycle management
	running    bool
	startTime  time.Time
	cancelFunc context.CancelFunc

	// Metrics
	rulesMatched int64
	tasksCreated int64
	lastActivity time.Time
}

// NewComponent creates a new workflow orchestrator component.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.LoopsBucket == "" {
		config.LoopsBucket = defaults.LoopsBucket
	}
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.RulesPath == "" {
		config.RulesPath = defaults.RulesPath
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}
	if config.Validation == nil {
		config.Validation = defaults.Validation
	}
	if config.RepoPath == "" {
		// Try environment variable
		config.RepoPath = os.Getenv("SEMSPEC_REPO_PATH")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize retry manager with config
	retryConfig := validation.RetryConfig{
		MaxAttempts:       config.Validation.MaxRetries,
		BackoffBase:       time.Duration(config.Validation.BackoffBaseSeconds) * time.Second,
		BackoffMultiplier: config.Validation.BackoffMultiplier,
	}

	return &Component{
		name:         "workflow-orchestrator",
		config:       config,
		natsClient:   deps.NATSClient,
		logger:       deps.GetLogger(),
		platform:     deps.Platform,
		registry:     model.NewDefaultRegistry(),
		retryManager: validation.NewRetryManager(retryConfig),
		validator:    validation.NewValidator(),
		repoPath:     config.RepoPath,
	}, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	// Load rules from file
	rulesPath := c.config.RulesPath

	// Try relative to current directory first
	if _, err := os.Stat(rulesPath); os.IsNotExist(err) {
		// Try relative to repo root
		if repoRoot := os.Getenv("SEMSPEC_REPO_PATH"); repoRoot != "" {
			rulesPath = filepath.Join(repoRoot, c.config.RulesPath)
		}
	}

	rules, err := LoadRules(rulesPath)
	if err != nil {
		c.logger.Warn("Failed to load workflow rules, using defaults",
			"path", rulesPath,
			"error", err)
		// Create empty rules - component will still work but won't trigger auto-continue
		rules = &RulesFile{Rules: []Rule{}}
	}

	c.rules = rules
	c.logger.Info("Loaded workflow rules",
		"path", rulesPath,
		"rule_count", len(rules.Rules))

	return nil
}

// Start begins watching for loop completions.
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

	watchCtx, cancel := context.WithCancel(ctx)
	c.cancelFunc = cancel
	c.running = true
	c.startTime = time.Now()
	c.mu.Unlock()

	c.logger.Info("Workflow orchestrator started",
		"loops_bucket", c.config.LoopsBucket,
		"rules", len(c.rules.Rules))

	// Watch KV bucket for loop completions
	go c.watchLoopCompletions(watchCtx)

	return nil
}

// watchLoopCompletions watches the AGENT_LOOPS KV bucket for completion entries.
func (c *Component) watchLoopCompletions(ctx context.Context) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		c.logger.Error("Failed to get JetStream context", "error", err)
		return
	}

	kv, err := js.KeyValue(ctx, c.config.LoopsBucket)
	if err != nil {
		c.logger.Error("Failed to get KV bucket", "bucket", c.config.LoopsBucket, "error", err)
		return
	}

	// Watch for keys matching COMPLETE_* pattern
	watcher, err := kv.Watch(ctx, "COMPLETE_*")
	if err != nil {
		c.logger.Error("Failed to create KV watcher", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Debug("Watching for loop completions", "pattern", "COMPLETE_*")

	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-watcher.Updates():
			if entry == nil {
				continue
			}

			// Skip delete operations
			if entry.Operation() == jetstream.KeyValueDelete {
				continue
			}

			c.handleCompletion(ctx, entry)
		}
	}
}

// handleCompletion processes a loop completion entry.
func (c *Component) handleCompletion(ctx context.Context, entry jetstream.KeyValueEntry) {
	key := entry.Key()
	c.logger.Debug("Processing loop completion", "key", key)

	// Parse the loop state
	var state LoopState
	if err := json.Unmarshal(entry.Value(), &state); err != nil {
		c.logger.Warn("Failed to parse loop state", "key", key, "error", err)
		return
	}

	// Ensure metadata exists
	if state.Metadata == nil {
		state.Metadata = make(map[string]string)
	}

	// Copy top-level fields to metadata for rule matching
	if state.WorkflowSlug != "" {
		state.Metadata["workflow_slug"] = state.WorkflowSlug
	}
	if state.WorkflowStep != "" {
		state.Metadata["workflow_step"] = state.WorkflowStep
	}

	c.logger.Debug("Loop state parsed",
		"loop_id", state.LoopID,
		"role", state.Role,
		"status", state.Status,
		"auto_continue", state.Metadata["auto_continue"])

	// Validate the generated document before proceeding
	if c.config.Validation != nil && c.config.Validation.Enabled {
		if !c.validateAndHandleRetry(ctx, &state) {
			// Validation failed and retry was triggered (or max retries exceeded)
			// Don't proceed with rule matching
			return
		}
	}

	// Check rules
	c.mu.RLock()
	rules := c.rules.Rules
	c.mu.RUnlock()

	for _, rule := range rules {
		if rule.Matches(&state) {
			c.logger.Info("Rule matched",
				"rule", rule.Name,
				"loop_id", state.LoopID,
				"role", state.Role)

			c.mu.Lock()
			c.rulesMatched++
			c.lastActivity = time.Now()
			c.mu.Unlock()

			if err := c.executeAction(ctx, &rule.Action, &state); err != nil {
				c.logger.Error("Failed to execute rule action",
					"rule", rule.Name,
					"error", err)
			}

			// Only execute first matching rule
			break
		}
	}
}

// validateAndHandleRetry validates the generated document and handles retry if needed.
// Returns true if validation passed (or no document to validate), false if retry was triggered.
func (c *Component) validateAndHandleRetry(ctx context.Context, state *LoopState) bool {
	// Only validate on successful completions
	if state.Status != "complete" {
		return true
	}

	// Only validate if we have workflow context
	workflowSlug, workflowStep := state.GetWorkflowContext()
	if workflowSlug == "" || workflowStep == "" {
		// No workflow context, skip validation
		return true
	}

	// Determine document type from workflow step
	docType := c.stepToDocumentType(workflowStep)
	if docType == "" {
		// Unknown step type, skip validation
		return true
	}

	// Read the generated document
	docPath := c.getDocumentPath(workflowSlug, workflowStep)
	content, err := os.ReadFile(docPath)
	if err != nil {
		c.logger.Warn("Failed to read document for validation",
			"path", docPath,
			"error", err)
		// Can't validate, allow proceeding
		return true
	}

	// Validate the document
	result := c.validator.Validate(string(content), docType)

	if result.Valid {
		// Clear retry state on success
		c.retryManager.ClearState(workflowSlug, workflowStep)
		c.logger.Debug("Document validation passed",
			"workflow_slug", workflowSlug,
			"workflow_step", workflowStep)
		return true
	}

	// Validation failed - check if we should retry
	c.retryManager.RecordAttempt(workflowSlug, workflowStep)
	decision := c.retryManager.ShouldRetry(workflowSlug, workflowStep, result)

	c.logger.Warn("Document validation failed",
		"workflow_slug", workflowSlug,
		"workflow_step", workflowStep,
		"attempt", decision.AttemptNumber,
		"max_attempts", decision.MaxAttempts,
		"missing_sections", result.MissingSections)

	if decision.ShouldRetry {
		// Apply backoff before retry
		if decision.BackoffSeconds > 0 {
			backoff := time.Duration(decision.BackoffSeconds * float64(time.Second))
			c.logger.Info("Applying backoff before retry",
				"workflow_slug", workflowSlug,
				"workflow_step", workflowStep,
				"backoff", backoff)

			select {
			case <-ctx.Done():
				c.logger.Warn("Context cancelled during backoff",
					"workflow_slug", workflowSlug,
					"workflow_step", workflowStep)
				return false
			case <-time.After(backoff):
				// Backoff complete, proceed with retry
			}
		}

		// Trigger retry with feedback
		if err := c.triggerRetry(ctx, state, result, decision); err != nil {
			c.logger.Error("Failed to trigger retry",
				"workflow_slug", workflowSlug,
				"workflow_step", workflowStep,
				"error", err)
		}
		return false
	}

	// Max retries exceeded - notify user
	if decision.IsFinalFailure {
		c.notifyValidationFailure(ctx, state, result, decision)
	}

	return false
}

// stepToDocumentType converts a workflow step to a document type.
func (c *Component) stepToDocumentType(step string) validation.DocumentType {
	switch step {
	case "propose", "proposal":
		return validation.DocumentTypeProposal
	case "design":
		return validation.DocumentTypeDesign
	case "spec":
		return validation.DocumentTypeSpec
	case "tasks":
		return validation.DocumentTypeTasks
	default:
		return ""
	}
}

// getDocumentPath returns the path to the generated document.
func (c *Component) getDocumentPath(slug, step string) string {
	repoPath := c.repoPath
	if repoPath == "" {
		repoPath = "."
	}

	// Map step to filename
	var filename string
	switch step {
	case "propose":
		filename = "proposal.md"
	case "design":
		filename = "design.md"
	case "spec":
		filename = "spec.md"
	case "tasks":
		filename = "tasks.md"
	default:
		filename = step + ".md"
	}

	return filepath.Join(repoPath, ".semspec", "changes", slug, filename)
}

// triggerRetry publishes a retry task with validation feedback.
func (c *Component) triggerRetry(ctx context.Context, state *LoopState, result *validation.ValidationResult, decision *validation.RetryDecision) error {
	workflowSlug, workflowStep := state.GetWorkflowContext()

	// Build retry prompt with feedback
	feedback := result.FormatFeedback()
	retryPrompt := fmt.Sprintf(`Previous attempt failed validation. Please regenerate addressing these issues:

%s

Attempt %d of %d. Please ensure all required sections are present and meet minimum content requirements.`,
		feedback,
		decision.AttemptNumber+1,
		decision.MaxAttempts)

	// Determine role and capability
	role := state.Role
	capability := model.CapabilityForRole(role)
	primaryModel := c.registry.Resolve(capability)
	chain := c.registry.GetFallbackChain(capability)
	var fallbackChain []string
	if len(chain) > 1 {
		fallbackChain = chain[1:]
	}

	// Build the base prompt based on role
	var basePrompt string
	title := state.Metadata["title"]
	description := state.Metadata["description"]

	switch role {
	case "proposal-writer":
		basePrompt = prompts.ProposalWriterPrompt(workflowSlug, description)
	case "design-writer":
		basePrompt = prompts.DesignWriterPrompt(workflowSlug, title)
	case "spec-writer":
		basePrompt = prompts.SpecWriterPrompt(workflowSlug, title)
	case "tasks-writer":
		basePrompt = prompts.TasksWriterPrompt(workflowSlug, title)
	default:
		basePrompt = prompts.ProposalWriterPrompt(workflowSlug, description)
	}

	// Combine base prompt with retry feedback
	fullPrompt := fmt.Sprintf("%s\n\n---\n\n%s", basePrompt, retryPrompt)

	taskID := uuid.New().String()
	task := &workflow.WorkflowTaskPayload{
		TaskID:        taskID,
		Role:          role,
		Model:         primaryModel,
		FallbackChain: fallbackChain,
		Capability:    capability.String(),
		WorkflowSlug:  workflowSlug,
		WorkflowStep:  workflowStep,
		Title:         title,
		Description:   description,
		Prompt:        fullPrompt,
		AutoContinue:  state.Metadata["auto_continue"] == "true",
		UserID:        state.Metadata["user_id"],
		ChannelType:   state.Metadata["channel_type"],
		ChannelID:     state.Metadata["channel_id"],
		RetryAttempt:  decision.AttemptNumber + 1,
	}

	// Wrap in BaseMessage and publish
	baseMsg := message.NewBaseMessage(workflow.WorkflowTaskType, task, "semspec")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal retry task: %w", err)
	}

	subject := "agent.task.workflow"
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish retry task: %w", err)
	}

	c.logger.Info("Published retry task",
		"task_id", taskID,
		"role", role,
		"workflow_slug", workflowSlug,
		"workflow_step", workflowStep,
		"attempt", decision.AttemptNumber+1)

	return nil
}

// notifyValidationFailure sends a user notification about validation failure.
func (c *Component) notifyValidationFailure(ctx context.Context, state *LoopState, result *validation.ValidationResult, decision *validation.RetryDecision) {
	workflowSlug, workflowStep := state.GetWorkflowContext()

	feedback := result.FormatFeedback()
	content := fmt.Sprintf(`**Workflow Validation Failed**

The %s document for **%s** failed validation after %d attempts.

%s

Please review and fix manually, then run the workflow step again.`,
		workflowStep, workflowSlug, decision.MaxAttempts, feedback)

	subject := fmt.Sprintf("user.response.%s.%s",
		state.Metadata["channel_type"],
		state.Metadata["channel_id"])

	response := agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: state.Metadata["channel_type"],
		ChannelID:   state.Metadata["channel_id"],
		UserID:      state.Metadata["user_id"],
		Type:        agentic.ResponseTypeError,
		Content:     content,
		Timestamp:   time.Now(),
	}

	data, err := json.Marshal(response)
	if err != nil {
		c.logger.Error("Failed to marshal failure notification", "error", err)
		return
	}

	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to publish failure notification", "error", err)
		return
	}

	c.logger.Info("Published validation failure notification",
		"workflow_slug", workflowSlug,
		"workflow_step", workflowStep,
		"attempts", decision.MaxAttempts)
}

// executeAction executes a rule action.
func (c *Component) executeAction(ctx context.Context, action *Action, state *LoopState) error {
	switch action.Type {
	case "publish_task":
		return c.executePublishTask(ctx, action, state)
	case "publish_response":
		return c.executePublishResponse(ctx, action, state)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// executePublishTask publishes a workflow task to trigger the next step.
func (c *Component) executePublishTask(ctx context.Context, action *Action, state *LoopState) error {
	payload := action.BuildPayload(state)

	// Extract fields from payload
	role, _ := payload["role"].(string)
	workflowStep, _ := payload["workflow_step"].(string)
	workflowSlug, _ := payload["workflow_slug"].(string)
	title, _ := payload["title"].(string)
	description, _ := payload["description"].(string)
	autoContinue, _ := payload["auto_continue"].(bool)
	userID, _ := payload["user_id"].(string)
	channelType, _ := payload["channel_type"].(string)
	channelID, _ := payload["channel_id"].(string)
	capabilityStr, _ := payload["capability"].(string)

	// Resolve capability to model using registry
	var primaryModel string
	var fallbackChain []string
	var capability model.Capability

	if capabilityStr != "" {
		capability = model.ParseCapability(capabilityStr)
	}
	if capability == "" {
		capability = model.CapabilityForRole(role)
	}

	primaryModel = c.registry.Resolve(capability)
	chain := c.registry.GetFallbackChain(capability)
	if len(chain) > 1 {
		fallbackChain = chain[1:]
	}

	// Build the prompt based on role
	var prompt string
	switch role {
	case "design-writer":
		prompt = prompts.DesignWriterPrompt(workflowSlug, title)
	case "spec-writer":
		prompt = prompts.SpecWriterPrompt(workflowSlug, title)
	case "tasks-writer":
		prompt = prompts.TasksWriterPrompt(workflowSlug, title)
	default:
		prompt = prompts.ProposalWriterPrompt(workflowSlug, description)
	}

	// Handle previous entities
	var previousEntities []string
	if prev, ok := payload["previous_entities"].([]interface{}); ok {
		for _, p := range prev {
			if s, ok := p.(string); ok && s != "" {
				previousEntities = append(previousEntities, s)
			}
		}
	}

	// Create the workflow task
	taskID := uuid.New().String()
	task := &workflow.WorkflowTaskPayload{
		TaskID:           taskID,
		Role:             role,
		Model:            primaryModel,
		FallbackChain:    fallbackChain,
		Capability:       capability.String(),
		WorkflowSlug:     workflowSlug,
		WorkflowStep:     workflowStep,
		Title:            title,
		Description:      description,
		Prompt:           prompt,
		AutoContinue:     autoContinue,
		UserID:           userID,
		ChannelType:      channelType,
		ChannelID:        channelID,
		PreviousEntities: previousEntities,
	}

	// Wrap in BaseMessage and publish
	baseMsg := message.NewBaseMessage(workflow.WorkflowTaskType, task, "semspec")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	subject := action.BuildSubject(state)
	if subject == "" {
		subject = "agent.task.workflow"
	}

	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish task: %w", err)
	}

	c.mu.Lock()
	c.tasksCreated++
	c.mu.Unlock()

	c.logger.Info("Published workflow task",
		"task_id", taskID,
		"role", role,
		"workflow_slug", workflowSlug,
		"workflow_step", workflowStep,
		"model", primaryModel,
		"capability", capability.String())

	return nil
}

// executePublishResponse publishes a user response notification.
func (c *Component) executePublishResponse(ctx context.Context, action *Action, state *LoopState) error {
	content := action.BuildContent(state)
	if content == "" {
		content = action.BuildPayload(state)["content"].(string)
	}

	subject := action.BuildSubject(state)
	if subject == "" {
		subject = fmt.Sprintf("user.response.%s.%s",
			state.Metadata["channel_type"],
			state.Metadata["channel_id"])
	}

	responseType := agentic.ResponseTypeResult
	if typeStr, ok := action.Payload["type"].(string); ok {
		switch typeStr {
		case "error":
			responseType = agentic.ResponseTypeError
		case "status":
			responseType = agentic.ResponseTypeStatus
		}
	}

	response := agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: state.Metadata["channel_type"],
		ChannelID:   state.Metadata["channel_id"],
		UserID:      state.Metadata["user_id"],
		Type:        responseType,
		Content:     content,
		Timestamp:   time.Now(),
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish response: %w", err)
	}

	c.logger.Info("Published user notification",
		"subject", subject,
		"type", responseType)

	return nil
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	c.running = false
	c.logger.Info("Workflow orchestrator stopped",
		"rules_matched", c.rulesMatched,
		"tasks_created", c.tasksCreated)

	return nil
}

// SetRegistry sets the model registry (for testing or custom configuration).
// Passing nil is a no-op to prevent nil pointer dereferences.
func (c *Component) SetRegistry(r *model.Registry) {
	if r == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registry = r
}

// Discoverable interface implementation

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "workflow-orchestrator",
		Type:        "processor",
		Description: "Watches loop completions and triggers next workflow steps",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "loop-completions",
			Direction:   component.DirectionInput,
			Description: "Watch for loop completion events",
			Config: component.KVWatchPort{
				Bucket: c.config.LoopsBucket,
				Keys:   []string{"COMPLETE_*"},
			},
		},
	}
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "workflow-tasks",
			Direction:   component.DirectionOutput,
			Description: "Publish workflow tasks",
			Config: component.JetStreamPort{
				StreamName: c.config.StreamName,
				Subjects:   []string{"agent.task.workflow"},
			},
		},
		{
			Name:        "user-responses",
			Direction:   component.DirectionOutput,
			Description: "Publish user notifications",
			Config: component.JetStreamPort{
				StreamName: "USER",
				Subjects:   []string{"user.response.>"},
			},
		},
	}
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return orchestratorSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := "stopped"
	if c.running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    c.running,
		LastCheck:  time.Now(),
		ErrorCount: 0,
		Uptime:     time.Since(c.startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.lastActivity,
	}
}
