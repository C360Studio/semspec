package contextbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/processor/context-builder/strategies"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/natsclient"
)

// Builder orchestrates context building for different task types.
type Builder struct {
	gatherers       *strategies.Gatherers
	strategyFactory *strategies.StrategyFactory
	calculator      *BudgetCalculator
	modelRegistry   *model.Registry
	logger          *slog.Logger
	qaIntegration   *QAIntegration
	qaEnabled       bool
	config          Config

	// Graph readiness probe state (cached after first successful probe)
	graphReady  atomic.Bool
	graphMu     sync.Mutex
	graphBudget time.Duration
}

// NewBuilder creates a new context builder.
func NewBuilder(config Config, modelRegistry *model.Registry, logger *slog.Logger) *Builder {
	gatherers := strategies.NewGatherers(
		config.GraphGatewayURL,
		config.RepoPath,
		config.SOPEntityPrefix,
	)

	// Parse graph readiness budget (default 15s)
	graphBudget := 15 * time.Second
	if config.GraphReadinessBudget != "" {
		if parsed, err := time.ParseDuration(config.GraphReadinessBudget); err == nil {
			graphBudget = parsed
		} else {
			logger.Warn("Invalid graph_readiness_budget, using default",
				"value", config.GraphReadinessBudget, "default", graphBudget)
		}
	}

	calculator := NewBudgetCalculator(config.DefaultTokenBudget, config.HeadroomTokens)
	// Wire the model registry as the capability resolver so budget calculation
	// can look up the model for a capability and get its max_tokens.
	if modelRegistry != nil {
		calculator.SetCapabilityResolver(modelRegistry)
	}

	return &Builder{
		gatherers:       gatherers,
		strategyFactory: strategies.NewStrategyFactory(gatherers, logger),
		calculator:      calculator,
		modelRegistry:   modelRegistry,
		logger:          logger,
		qaEnabled:       false, // Will be enabled when SetQAIntegration is called
		graphBudget:     graphBudget,
		config:          config,
	}
}

// SetQAIntegration configures the Q&A integration for handling insufficient context.
// This must be called after creating the Builder to enable Q&A functionality.
func (b *Builder) SetQAIntegration(
	natsClient *natsclient.Client,
	config Config,
) error {
	// Create question store
	questionStore, err := workflow.NewQuestionStore(natsClient)
	if err != nil {
		b.logger.Warn("Failed to create question store, Q&A disabled",
			"error", err)
		return nil // Graceful degradation
	}

	// Load answerer registry
	registry, err := answerer.LoadRegistryFromDir(config.RepoPath)
	if err != nil {
		b.logger.Warn("Failed to load answerers config, using defaults",
			"error", err)
		registry = answerer.NewRegistry()
	}

	// Create router
	router := answerer.NewRouter(registry, natsClient, b.logger)

	// Create Q&A integration
	qaConfig := QAIntegrationConfig{
		BlockingTimeout: time.Duration(config.BlockingTimeoutSeconds) * time.Second,
		AllowBlocking:   config.AllowBlocking,
		SourceName:      "context-builder",
	}

	b.qaIntegration = NewQAIntegration(natsClient, questionStore, router, qaConfig, b.logger)
	b.qaEnabled = true

	b.logger.Info("Q&A integration enabled",
		"blocking_timeout", qaConfig.BlockingTimeout,
		"allow_blocking", qaConfig.AllowBlocking)

	return nil
}

// ensureGraphReady probes the graph pipeline and caches the result on success.
// Once the graph is confirmed ready, subsequent calls return immediately.
// Failed probes are retried on the next call to handle delayed graph startup.
func (b *Builder) ensureGraphReady(ctx context.Context) bool {
	if b.graphReady.Load() {
		return true
	}

	b.graphMu.Lock()
	defer b.graphMu.Unlock()

	// Double-check after acquiring lock
	if b.graphReady.Load() {
		return true
	}

	b.logger.Info("Probing graph pipeline readiness",
		"budget", b.graphBudget)

	err := b.gatherers.Graph.WaitForReady(ctx, b.graphBudget)
	if err != nil {
		b.logger.Warn("Graph pipeline not ready, graph steps will be skipped",
			"budget", b.graphBudget, "error", err)
		return false
	}

	b.graphReady.Store(true)
	b.logger.Info("Graph pipeline ready")
	return true
}

// Build constructs context for the given request.
func (b *Builder) Build(ctx context.Context, req *ContextBuildRequest) (*ContextBuildResponse, error) {
	// Probe graph readiness (cached after first success)
	graphReady := b.ensureGraphReady(ctx)

	// Calculate token budget (uses capability -> model -> max_tokens if available)
	budget := b.calculateBudget(req)

	b.logger.Debug("Building context",
		"request_id", req.RequestID,
		"task_type", req.TaskType,
		"capability", req.Capability,
		"budget", budget,
		"graph_ready", graphReady)

	// Create budget allocation (using strategies package type)
	allocation := strategies.NewBudgetAllocation(budget)

	// Convert request to strategy request
	stratReq := &strategies.ContextBuildRequest{
		RequestID:     req.RequestID,
		TaskType:      strategies.TaskType(req.TaskType),
		WorkflowID:    req.WorkflowID,
		Files:         req.Files,
		GitRef:        req.GitRef,
		Topic:         req.Topic,
		SpecEntityID:  req.SpecEntityID,
		PlanSlug:      req.PlanSlug,
		PlanContent:   req.PlanContent,
		ScopePatterns: req.ScopePatterns,
		Capability:    req.Capability,
		Model:         req.Model,
		TokenBudget:   req.TokenBudget,
		GraphReady:    graphReady,
	}

	// Get strategy for task type
	strategy := b.strategyFactory.Create(stratReq.TaskType)

	// Execute strategy
	result, err := strategy.Build(ctx, stratReq, allocation)
	if err != nil {
		return &ContextBuildResponse{
			RequestID:    req.RequestID,
			TaskType:     req.TaskType,
			TokensBudget: budget,
			Error:        fmt.Sprintf("strategy execution failed: %v", err),
		}, nil
	}

	// Check for strategy error
	if result.Error != "" {
		return &ContextBuildResponse{
			RequestID:    req.RequestID,
			TaskType:     req.TaskType,
			TokensBudget: budget,
			Error:        result.Error,
		}, nil
	}

	// Inject standards as a post-strategy preamble. Standards are always active
	// regardless of which strategy ran, and they do not consume from the
	// strategy's token budget.
	if preamble, tokens := b.loadStandardsPreamble(); preamble != "" {
		if result.Documents == nil {
			result.Documents = make(map[string]string)
		}
		result.Documents["__standards__"] = preamble
		b.logger.Debug("Standards injected", "tokens", tokens)
	}

	// Handle insufficient context via Q&A integration
	if result.InsufficientContext && len(result.Questions) > 0 && b.qaEnabled {
		result = b.handleInsufficientContext(ctx, result, req.WorkflowID, req.PlanSlug)
	}

	// Convert entities
	entities := make([]EntityRef, len(result.Entities))
	for i, e := range result.Entities {
		entities[i] = EntityRef{
			ID:      e.ID,
			Type:    e.Type,
			Content: e.Content,
			Tokens:  e.Tokens,
		}
	}

	// Build response
	response := &ContextBuildResponse{
		RequestID:       req.RequestID,
		TaskType:        req.TaskType,
		TokenCount:      allocation.Allocated,
		Entities:        entities,
		Documents:       result.Documents,
		Diffs:           result.Diffs,
		Provenance:      b.buildProvenance(allocation),
		SOPIDs:          result.SOPIDs,
		SOPRequirements: result.SOPRequirements,
		TokensUsed:      allocation.Allocated,
		TokensBudget:    budget,
		Truncated:       result.Truncated,
	}

	b.logger.Info("Context built successfully",
		"request_id", req.RequestID,
		"task_type", req.TaskType,
		"tokens_used", allocation.Allocated,
		"tokens_budget", budget,
		"documents", len(result.Documents),
		"entities", len(result.Entities),
		"truncated", result.Truncated,
		"questions_remaining", len(result.Questions),
		"insufficient_context", result.InsufficientContext)

	return response, nil
}

// handleInsufficientContext uses Q&A integration to resolve knowledge gaps.
func (b *Builder) handleInsufficientContext(ctx context.Context, result *strategies.StrategyResult, workflowID string, planSlug string) *strategies.StrategyResult {
	if b.qaIntegration == nil {
		return result
	}

	b.logger.Info("Handling insufficient context",
		"questions", len(result.Questions),
		"workflow_id", workflowID)

	// Attempt to get answers via Q&A integration
	answers, err := b.qaIntegration.HandleInsufficientContext(ctx, result.Questions, workflowID, planSlug)
	if err != nil {
		b.logger.Warn("Q&A integration failed, returning partial context",
			"error", err)
		return result
	}

	// Enrich result with answers
	enrichedResult := b.qaIntegration.EnrichWithAnswers(result, answers)

	answeredCount := 0
	for _, a := range answers {
		if a.Answered {
			answeredCount++
		}
	}

	b.logger.Info("Q&A integration completed",
		"total_questions", len(result.Questions),
		"answered", answeredCount,
		"remaining", len(enrichedResult.Questions))

	return enrichedResult
}

// loadStandardsPreamble reads standards.json and formats its rules as a
// markdown preamble ready for injection into agent context.
//
// Rules are sorted by severity (error > warning > info) so the most critical
// constraints appear first. When the formatted preamble would exceed
// config.StandardsMaxTokens, rules are truncated and a note is appended.
//
// Graceful degradation: if the file is missing or malformed, ("", 0) is
// returned so callers can continue without standards.
func (b *Builder) loadStandardsPreamble() (string, int) {
	standardsPath := b.config.StandardsPath
	if !filepath.IsAbs(standardsPath) {
		standardsPath = filepath.Join(b.config.RepoPath, standardsPath)
	}

	data, err := os.ReadFile(standardsPath)
	if err != nil {
		if os.IsNotExist(err) {
			b.logger.Debug("Standards file not found, skipping injection",
				"path", standardsPath)
		} else {
			b.logger.Warn("Failed to read standards file, skipping injection",
				"path", standardsPath, "error", err)
		}
		return "", 0
	}

	var standards workflow.Standards
	if err := json.Unmarshal(data, &standards); err != nil {
		b.logger.Warn("Failed to parse standards file, skipping injection",
			"path", standardsPath, "error", err)
		return "", 0
	}

	if len(standards.Rules) == 0 {
		return "", 0
	}

	// Sort rules by severity: error first, then warning, then info.
	sorted := make([]workflow.Rule, len(standards.Rules))
	copy(sorted, standards.Rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityOrder(sorted[i].Severity) < severityOrder(sorted[j].Severity)
	})

	// Estimate tokens using the same heuristic as the rest of the builder
	// (roughly 4 characters per token).
	const charsPerToken = 4

	maxTokens := b.config.StandardsMaxTokens
	if maxTokens <= 0 {
		maxTokens = 1000 // safe fallback
	}

	// Build the header first; it counts against the budget.
	const header = "## Project Standards (Implementation Requirements)\n\n" +
		"These rules define what the IMPLEMENTATION must achieve, not what the plan\n" +
		"must explicitly enumerate. For example, if a standard requires 'all endpoints\n" +
		"have tests', a plan adding an endpoint is compliant if it can reasonably be\n" +
		"expected to include tests during implementation — the plan does NOT need to\n" +
		"explicitly list test files in its scope.\n\n" +
		"Only flag an error-severity violation if the plan CONTRADICTS a standard\n" +
		"(e.g., explicitly states 'no tests needed').\n\n"

	headerTokens := len(header) / charsPerToken

	var sb strings.Builder
	sb.WriteString(header)

	usedTokens := headerTokens
	truncated := false
	rulesIncluded := 0

	for _, rule := range sorted {
		line := fmt.Sprintf("- [%s] %s\n", strings.ToUpper(string(rule.Severity)), rule.Text)
		lineTokens := len(line) / charsPerToken

		if usedTokens+lineTokens > maxTokens {
			truncated = true
			break
		}

		sb.WriteString(line)
		usedTokens += lineTokens
		rulesIncluded++
	}

	if rulesIncluded == 0 {
		// Even the header alone did not fit — return nothing rather than
		// emitting a preamble with no rules.
		return "", 0
	}

	if truncated {
		omitted := len(sorted) - rulesIncluded
		note := fmt.Sprintf("- [...%d additional rules truncated — increase standards_max_tokens to see all]\n", omitted)
		sb.WriteString(note)
	}

	preamble := sb.String()
	// Cap the returned token count to maxTokens. The truncation note added
	// at the end may push the raw character count slightly above the budget,
	// but callers rely on this value to measure budget consumption and should
	// never see more than what was configured.
	tokenCount := len(preamble) / charsPerToken
	if tokenCount > maxTokens {
		tokenCount = maxTokens
	}
	return preamble, tokenCount
}

// severityOrder maps a RuleSeverity to a numeric sort key.
// Lower numbers sort first so error rules appear before warning and info.
func severityOrder(s workflow.RuleSeverity) int {
	switch s {
	case workflow.RuleSeverityError:
		return 0
	case workflow.RuleSeverityWarning:
		return 1
	case workflow.RuleSeverityInfo:
		return 2
	default:
		return 3
	}
}

// calculateBudget determines the token budget for a request.
func (b *Builder) calculateBudget(req *ContextBuildRequest) int {
	return b.calculator.Calculate(req, func(modelName string) int {
		if b.modelRegistry == nil {
			return 0
		}
		endpoint := b.modelRegistry.GetEndpoint(modelName)
		if endpoint == nil {
			return 0
		}
		return endpoint.MaxTokens
	})
}

// buildProvenance converts budget allocations to provenance entries.
func (b *Builder) buildProvenance(allocation *strategies.BudgetAllocation) []ProvenanceEntry {
	entries := make([]ProvenanceEntry, 0, len(allocation.Order))

	for i, name := range allocation.Order {
		tokens := allocation.Items[name]
		ptype := b.inferProvenanceType(name)

		entries = append(entries, ProvenanceEntry{
			Source:   name,
			Type:     ptype,
			Tokens:   tokens,
			Priority: i,
		})
	}

	return entries
}

// inferProvenanceType determines the provenance type from the allocation name.
func (b *Builder) inferProvenanceType(name string) ProvenanceType {
	switch {
	case name == "sops":
		return ProvenanceTypeSOP
	case name == "git_diff":
		return ProvenanceTypeGitDiff
	case name == "tests":
		return ProvenanceTypeTest
	case name == "spec":
		return ProvenanceTypeSpec
	case name == "codebase_summary":
		return ProvenanceTypeSummary
	case name == "source_files" || name == "requested_files":
		return ProvenanceTypeFile
	case strings.HasPrefix(name, "convention"):
		return ProvenanceTypeConvention
	case strings.HasPrefix(name, "arch:"):
		return ProvenanceTypeFile
	case strings.HasPrefix(name, "doc:"):
		return ProvenanceTypeFile
	case strings.HasPrefix(name, "entity:"):
		return ProvenanceTypeEntity
	case strings.HasPrefix(name, "pattern:"):
		return ProvenanceTypeEntity
	default:
		return ProvenanceTypeFile
	}
}

// ValidateRequest validates a context build request.
func ValidateRequest(req *ContextBuildRequest) error {
	if req.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}

	if !req.TaskType.IsValid() {
		return fmt.Errorf("invalid task_type: %s", req.TaskType)
	}

	// Task-specific validation
	switch req.TaskType {
	case TaskTypeReview:
		// Review needs either files or git ref
		if len(req.Files) == 0 && req.GitRef == "" {
			return fmt.Errorf("review task requires files or git_ref")
		}

	case TaskTypeImplementation:
		// Implementation benefits from spec entity but doesn't require it
		// (can work with just files or topic)

	case TaskTypeExploration:
		// Exploration can work with just a topic or codebase summary
	}

	// Validate token budget if specified
	if req.TokenBudget < 0 {
		return fmt.Errorf("token_budget cannot be negative")
	}

	return nil
}
