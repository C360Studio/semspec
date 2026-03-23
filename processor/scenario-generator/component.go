// Package scenariogenerator provides a processor that generates BDD scenarios
// from plan requirements using LLM. Each requirement receives its own set of
// Given/When/Then scenarios. Multiple instances may fire in parallel (one per
// requirement); the last one to finish detects full coverage and publishes the
// ScenariosGenerated event.
package scenariogenerator

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
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

// maxFormatRetries is the total number of LLM call attempts when the response
// is not valid JSON. On each retry, the parse error is fed back to the LLM so
// it can correct its output format.
const maxFormatRetries = 3

// llmCompleter is the subset of the LLM client used by the scenario-generator.
// Extracted as an interface to enable testing with mock responses.
type llmCompleter interface {
	Complete(ctx context.Context, req llm.Request) (*llm.Response, error)
}

// Component implements the scenario-generator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	llmClient llmCompleter

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

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
// LLM response shape
// ---------------------------------------------------------------------------

// llmScenario is the raw JSON shape returned by the LLM before we assign IDs.
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
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	logger := deps.GetLogger()

	return &Component{
		name:       "scenario-generator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     logger,
		llmClient: llm.NewClient(model.Global(),
			llm.WithLogger(logger),
		),
	}, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("Initialized scenario-generator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject)
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

	// Push-based consumption — messages arrive via callback, no polling delay.
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       180 * time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleMessagePush); err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("consume scenario triggers: %w", err)
	}

	c.logger.Info("scenario-generator started",
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
// Message consumption
// ---------------------------------------------------------------------------

// handleMessagePush is the push-based callback for ConsumeStreamWithConfig.
// Messages arrive immediately when published — no polling delay.
func (c *Component) handleMessagePush(ctx context.Context, msg jetstream.Msg) {
	c.handleMessage(ctx, msg)
}

// handleMessage processes a single scenario generation trigger.
func (c *Component) handleMessage(ctx context.Context, msg jetstream.Msg) {
	if ctx.Err() != nil {
		if err := msg.Nak(); err != nil {
			c.logger.Warn("Failed to NAK message during shutdown", "error", err)
		}
		return
	}

	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	trigger, err := payloads.ParseReactivePayload[payloads.ScenarioGeneratorRequest](msg.Data())
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

	c.logger.Info("Processing scenario generation trigger",
		"slug", trigger.Slug,
		"requirement_id", trigger.RequirementID,
		"trace_id", trigger.TraceID)

	// Signal in-progress to prevent redelivery during LLM operations.
	if err := msg.InProgress(); err != nil {
		c.logger.Debug("Failed to signal in-progress", "error", err)
	}

	// Inject trace context for LLM call attribution.
	llmCtx := ctx
	if trigger.TraceID != "" {
		llmCtx = llm.WithTraceContext(ctx, llm.TraceContext{
			TraceID: trigger.TraceID,
		})
	}

	scenarios, err := c.generateScenarios(llmCtx, trigger)
	if err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to generate scenarios",
			"slug", trigger.Slug,
			"requirement_id", trigger.RequirementID,
			"error", err)
		// NAK to allow retry — scenario generation failures are transient.
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	if err := c.saveAndCheckCompletion(ctx, trigger, scenarios); err != nil {
		c.generationsFailed.Add(1)
		c.logger.Error("Failed to save scenarios or check completion",
			"slug", trigger.Slug,
			"requirement_id", trigger.RequirementID,
			"error", err)
		if nakErr := msg.Nak(); nakErr != nil {
			c.logger.Warn("Failed to NAK message", "error", nakErr)
		}
		return
	}

	c.scenariosGenerated.Add(int64(len(scenarios)))

	if err := msg.Ack(); err != nil {
		c.logger.Warn("Failed to ACK message", "error", err)
	}

	c.logger.Info("Scenarios generated successfully",
		"slug", trigger.Slug,
		"requirement_id", trigger.RequirementID,
		"scenario_count", len(scenarios))
}

// ---------------------------------------------------------------------------
// Scenario generation
// ---------------------------------------------------------------------------

// generateScenarios calls the LLM to produce BDD scenarios for a single requirement.
// It requests planning context from the context-builder (graph-first) before calling
// the LLM, and retries up to maxFormatRetries times if the response is malformed JSON.
func (c *Component) generateScenarios(ctx context.Context, trigger *payloads.ScenarioGeneratorRequest) ([]workflow.Scenario, error) {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}
	manager := workflow.NewManager(repoRoot)

	// Load the plan for goal/context/scope.
	plan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		return nil, fmt.Errorf("load plan: %w", err)
	}

	// Load and find the specific requirement.
	requirements, err := manager.LoadRequirements(ctx, trigger.Slug)
	if err != nil {
		return nil, fmt.Errorf("load requirements: %w", err)
	}

	var req *workflow.Requirement
	for i := range requirements {
		if requirements[i].ID == trigger.RequirementID {
			req = &requirements[i]
			break
		}
	}
	if req == nil {
		return nil, fmt.Errorf("requirement %q not found in plan %q", trigger.RequirementID, trigger.Slug)
	}

	systemPrompt := c.buildSystemPrompt()
	userPrompt := c.buildUserPrompt(plan, req, "")

	return c.callLLMWithRetry(ctx, systemPrompt, userPrompt, trigger.Slug, req.ID)
}

// buildSystemPrompt returns the system prompt that instructs the LLM to produce
// BDD-style scenarios as a JSON array.
func (c *Component) buildSystemPrompt() string {
	return `You are a BDD scenario generator. For the requirement provided, generate a set of Given/When/Then scenarios that fully cover its behavior.

Output ONLY a valid JSON array of scenario objects. Each object must have exactly these fields:
- "given": string — the precondition or initial context
- "when": string — the action or event that occurs
- "then": array of strings — the expected outcomes (one or more)

Example:
` + "```json" + `
[
  {
    "given": "a user has an active session",
    "when": "the user submits valid credentials",
    "then": ["the session is refreshed", "the user remains logged in"]
  },
  {
    "given": "a user's session has expired",
    "when": "the user submits valid credentials",
    "then": ["a new session is created", "the user is logged in"]
  }
]
` + "```" + `

Rules:
- Generate at least 2 scenarios per requirement
- Cover both happy paths and edge/error cases
- "then" must always be a non-empty array of strings
- Return ONLY the JSON array — no explanation, no markdown outside the code block`
}

// buildUserPrompt assembles the user prompt with requirement details and plan context.
func (c *Component) buildUserPrompt(plan *workflow.Plan, req *workflow.Requirement, graphContext string) string {
	var sb strings.Builder

	sb.WriteString("## Requirement\n\n")
	sb.WriteString(fmt.Sprintf("**Title**: %s\n\n", req.Title))
	if req.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description**: %s\n\n", req.Description))
	}

	sb.WriteString("## Plan Context\n\n")
	if plan.Goal != "" {
		sb.WriteString(fmt.Sprintf("**Goal**: %s\n\n", plan.Goal))
	}
	if plan.Context != "" {
		sb.WriteString(fmt.Sprintf("**Context**: %s\n\n", plan.Context))
	}
	if len(plan.Scope.Include) > 0 {
		sb.WriteString(fmt.Sprintf("**Scope**: %s\n\n", strings.Join(plan.Scope.Include, ", ")))
	}

	if graphContext != "" {
		sb.WriteString("## Codebase Context\n\n")
		sb.WriteString("The following context from the knowledge graph provides information about the existing codebase:\n\n")
		sb.WriteString(graphContext)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Generate BDD scenarios for the requirement above.")

	return sb.String()
}

// callLLMWithRetry calls the LLM and retries with format correction if the
// response is not valid JSON. Returns the parsed scenarios with IDs assigned.
func (c *Component) callLLMWithRetry(ctx context.Context, systemPrompt, userPrompt, slug, requirementID string) ([]workflow.Scenario, error) {
	capability := c.config.DefaultCapability
	if capability == "" {
		capability = string(model.CapabilityPlanning)
	}

	temperature := 0.7
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	var lastErr error

	for attempt := range maxFormatRetries {
		llmResp, err := c.llmClient.Complete(ctx, llm.Request{
			Capability:  capability,
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   4096,
		})
		if err != nil {
			return nil, fmt.Errorf("LLM completion: %w", err)
		}

		c.logger.Debug("LLM response received",
			"model", llmResp.Model,
			"tokens_used", llmResp.TokensUsed,
			"attempt", attempt+1)

		scenarios, parseErr := c.parseScenariosFromResponse(llmResp.Content, slug, requirementID)
		if parseErr == nil {
			return scenarios, nil
		}

		lastErr = parseErr

		if attempt+1 >= maxFormatRetries {
			break
		}

		c.logger.Warn("Scenario generator LLM format retry",
			"attempt", attempt+1,
			"error", parseErr)

		// Append assistant response and correction to conversation history.
		messages = append(messages,
			llm.Message{Role: "assistant", Content: llmResp.Content},
			llm.Message{Role: "user", Content: scenarioFormatCorrectionPrompt(parseErr)},
		)
	}

	return nil, fmt.Errorf("parse scenarios from response after %d attempts: %w", maxFormatRetries, lastErr)
}

// parseScenariosFromResponse extracts and validates scenario JSON from the LLM
// response, then assigns IDs based on the slug and requirement ID.
func (c *Component) parseScenariosFromResponse(content, slug, requirementID string) ([]workflow.Scenario, error) {
	// Try array extraction first, then fall back to object extraction.
	jsonContent := llm.ExtractJSONArray(content)
	if jsonContent == "" {
		jsonContent = llm.ExtractJSON(content)
	}
	if jsonContent == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var raw []llmScenario
	if err := json.Unmarshal([]byte(jsonContent), &raw); err != nil {
		// Try unwrapping from an object with a "scenarios" key.
		var wrapper struct {
			Scenarios []llmScenario `json:"scenarios"`
		}
		if wrapErr := json.Unmarshal([]byte(jsonContent), &wrapper); wrapErr == nil && len(wrapper.Scenarios) > 0 {
			raw = wrapper.Scenarios
		} else {
			return nil, fmt.Errorf("parse JSON: %w (content: %s)", err, jsonContent[:min(200, len(jsonContent))])
		}
	}

	if len(raw) < 2 {
		return nil, fmt.Errorf("expected at least 2 scenarios, got %d", len(raw))
	}

	// Extract the numeric suffix from the requirement ID for use in scenario IDs.
	// Requirement IDs have format "requirement.{slug}.{seq}", so we take the last segment.
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

// scenarioFormatCorrectionPrompt builds a correction message when the LLM
// response cannot be parsed as a JSON scenario array.
func scenarioFormatCorrectionPrompt(err error) string {
	return fmt.Sprintf(
		"Your response could not be parsed. Error: %s\n\n"+
			"Please respond with ONLY a valid JSON array of scenario objects:\n"+
			"```json\n"+
			"[\n"+
			"  {\n"+
			"    \"given\": \"<precondition>\",\n"+
			"    \"when\": \"<action>\",\n"+
			"    \"then\": [\"<expected outcome 1>\", \"<expected outcome 2>\"]\n"+
			"  }\n"+
			"]\n"+
			"```\n\n"+
			"Rules:\n"+
			"- At least 2 scenarios required\n"+
			"- 'then' must be a non-empty array of strings\n"+
			"- Return ONLY the JSON array",
		err.Error(),
	)
}

// ---------------------------------------------------------------------------
// Save and coordination
// ---------------------------------------------------------------------------

// saveAndCheckCompletion appends the newly generated scenarios for the given
// requirement to the plan's scenarios.json file, then checks whether every
// requirement now has at least one scenario. If coverage is complete, it
// transitions the plan status to scenarios_generated and publishes a
// ScenariosGenerated JetStream event.
//
// SaveScenarios replaces the full file on each write, so we load the existing
// scenarios first, replace any prior entries for this requirement, and save the
// merged slice. This makes the operation idempotent for the same requirement ID.
func (c *Component) saveAndCheckCompletion(ctx context.Context, trigger *payloads.ScenarioGeneratorRequest, newScenarios []workflow.Scenario) error {
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

	// Load existing scenarios so we can append (or replace for this requirement).
	existing, err := manager.LoadScenarios(ctx, trigger.Slug)
	if err != nil {
		return fmt.Errorf("load existing scenarios: %w", err)
	}

	// Drop any prior scenarios for this requirement so the save is idempotent.
	merged := make([]workflow.Scenario, 0, len(existing)+len(newScenarios))
	for _, s := range existing {
		if s.RequirementID != trigger.RequirementID {
			merged = append(merged, s)
		}
	}
	merged = append(merged, newScenarios...)

	if err := manager.SaveScenarios(ctx, merged, trigger.Slug); err != nil {
		return fmt.Errorf("save scenarios: %w", err)
	}

	c.logger.Info("Saved scenarios",
		"slug", trigger.Slug,
		"requirement_id", trigger.RequirementID,
		"new_scenario_count", len(newScenarios),
		"total_scenario_count", len(merged))

	// Check whether every requirement now has at least one scenario.
	requirements, err := manager.LoadRequirements(ctx, trigger.Slug)
	if err != nil {
		return fmt.Errorf("load requirements for coverage check: %w", err)
	}

	coveredReqs := make(map[string]bool, len(merged))
	for _, s := range merged {
		coveredReqs[s.RequirementID] = true
	}

	allCovered := true
	for _, r := range requirements {
		if r.Status == workflow.RequirementStatusDeprecated {
			continue
		}
		if !coveredReqs[r.ID] {
			allCovered = false
			break
		}
	}

	if !allCovered {
		c.logger.Debug("Not all requirements have scenarios yet — waiting",
			"slug", trigger.Slug,
			"covered", len(coveredReqs),
			"total", len(requirements))
		return nil
	}

	// All requirements are covered — transition status and publish event.
	c.logger.Info("All requirements have scenarios — transitioning plan status",
		"slug", trigger.Slug,
		"scenario_count", len(merged))

	plan, err := manager.LoadPlan(ctx, trigger.Slug)
	if err != nil {
		return fmt.Errorf("load plan for status transition: %w", err)
	}

	if err := manager.SetPlanStatus(ctx, plan, workflow.StatusScenariosGenerated); err != nil {
		// Log and continue — event publication is more important than status update.
		c.logger.Warn("Failed to transition plan status to scenarios_generated",
			"slug", trigger.Slug, "error", err)
	}

	if err := c.publishScenariosGeneratedEvent(ctx, trigger.Slug, len(merged), trigger.TraceID); err != nil {
		return fmt.Errorf("publish scenarios generated event: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Event publication
// ---------------------------------------------------------------------------

// scenariosGeneratedPayloadType is the message.Type for the scenarios-generated event.
var scenariosGeneratedPayloadType = message.Type{
	Domain:   "workflow",
	Category: "scenarios-generated",
	Version:  "v1",
}

// scenariosGeneratedPayload wraps workflow.ScenariosGeneratedEvent so it satisfies
// message.Payload, enabling use with message.NewBaseMessage.
type scenariosGeneratedPayload struct {
	workflow.ScenariosGeneratedEvent
}

// Schema implements message.Payload.
func (p *scenariosGeneratedPayload) Schema() message.Type { return scenariosGeneratedPayloadType }

// Validate implements message.Payload.
func (p *scenariosGeneratedPayload) Validate() error { return nil }

// MarshalJSON implements json.Marshaler.
func (p *scenariosGeneratedPayload) MarshalJSON() ([]byte, error) {
	type Alias workflow.ScenariosGeneratedEvent
	return json.Marshal((*Alias)(&p.ScenariosGeneratedEvent))
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *scenariosGeneratedPayload) UnmarshalJSON(data []byte) error {
	type Alias workflow.ScenariosGeneratedEvent
	return json.Unmarshal(data, (*Alias)(&p.ScenariosGeneratedEvent))
}

// publishScenariosGeneratedEvent publishes a ScenariosGeneratedEvent to the
// WORKFLOW JetStream stream so that plan-api can react and transition the
// plan to ready_for_execution.
func (c *Component) publishScenariosGeneratedEvent(ctx context.Context, slug string, scenarioCount int, traceID string) error {
	payload := &scenariosGeneratedPayload{
		ScenariosGeneratedEvent: workflow.ScenariosGeneratedEvent{
			Slug:          slug,
			ScenarioCount: scenarioCount,
			TraceID:       traceID,
		},
	}

	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "scenario-generator")

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return fmt.Errorf("marshal scenarios generated event: %w", err)
	}

	subject := workflow.ScenariosGenerated.Pattern
	if c.natsClient == nil {
		return fmt.Errorf("publish to stream %s: nats client not configured", subject)
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to stream %s: %w", subject, err)
	}

	c.logger.Info("Published ScenariosGenerated event",
		"slug", slug,
		"scenario_count", scenarioCount,
		"subject", subject)

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
		Description: "Generates BDD scenarios from requirements using LLM",
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
