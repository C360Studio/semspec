package contextbuilder

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/processor/context-builder/strategies"
)

// Builder orchestrates context building for different task types.
type Builder struct {
	gatherers       *strategies.Gatherers
	strategyFactory *strategies.StrategyFactory
	calculator      *BudgetCalculator
	modelRegistry   *model.Registry
	logger          *slog.Logger
}

// NewBuilder creates a new context builder.
func NewBuilder(config Config, modelRegistry *model.Registry, logger *slog.Logger) *Builder {
	gatherers := strategies.NewGatherers(
		config.GraphGatewayURL,
		config.RepoPath,
		config.SOPEntityPrefix,
	)

	return &Builder{
		gatherers:       gatherers,
		strategyFactory: strategies.NewStrategyFactory(gatherers, logger),
		calculator:      NewBudgetCalculator(config.DefaultTokenBudget, config.HeadroomTokens),
		modelRegistry:   modelRegistry,
		logger:          logger,
	}
}

// Build constructs context for the given request.
func (b *Builder) Build(ctx context.Context, req *ContextBuildRequest) (*ContextBuildResponse, error) {
	// Calculate token budget
	budget := b.calculateBudget(req)

	b.logger.Debug("Building context",
		"request_id", req.RequestID,
		"task_type", req.TaskType,
		"budget", budget)

	// Create budget allocation (using strategies package type)
	allocation := strategies.NewBudgetAllocation(budget)

	// Convert request to strategy request
	stratReq := &strategies.ContextBuildRequest{
		RequestID:    req.RequestID,
		TaskType:     strategies.TaskType(req.TaskType),
		WorkflowID:   req.WorkflowID,
		Files:        req.Files,
		GitRef:       req.GitRef,
		Topic:        req.Topic,
		SpecEntityID: req.SpecEntityID,
		Capability:   req.Capability,
		Model:        req.Model,
		TokenBudget:  req.TokenBudget,
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
		RequestID:    req.RequestID,
		TaskType:     req.TaskType,
		TokenCount:   allocation.Allocated,
		Entities:     entities,
		Documents:    result.Documents,
		Diffs:        result.Diffs,
		Provenance:   b.buildProvenance(allocation),
		SOPIDs:       result.SOPIDs,
		TokensUsed:   allocation.Allocated,
		TokensBudget: budget,
		Truncated:    result.Truncated,
	}

	b.logger.Info("Context built successfully",
		"request_id", req.RequestID,
		"task_type", req.TaskType,
		"tokens_used", allocation.Allocated,
		"tokens_budget", budget,
		"documents", len(result.Documents),
		"entities", len(result.Entities),
		"truncated", result.Truncated)

	return response, nil
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
