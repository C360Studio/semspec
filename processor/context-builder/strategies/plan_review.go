package strategies

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// PlanReviewStrategy builds context for plan review/approval tasks.
// Priority order:
// 1. Plan-scope SOPs (all-or-nothing - fail if exceeds budget)
// 2. Plan content (the actual plan being reviewed)
// 3. Related architecture documents (fill remaining budget)
type PlanReviewStrategy struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewPlanReviewStrategy creates a new plan review strategy.
func NewPlanReviewStrategy(gatherers *Gatherers, logger *slog.Logger) *PlanReviewStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlanReviewStrategy{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Build implements Strategy.
func (s *PlanReviewStrategy) Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error) {
	result := &StrategyResult{
		Documents: make(map[string]string),
	}

	// Step 1: Get plan-scope SOPs (all-or-nothing)
	sops, err := s.gatherers.SOP.GetSOPsByScope(ctx, "plan", req.ScopePatterns)
	if err != nil {
		s.logger.Warn("Failed to get plan-scope SOPs", "error", err)
	} else if len(sops) > 0 {
		sopTokens := s.gatherers.SOP.TotalTokens(sops)

		// All-or-nothing: if SOPs don't fit, fail the build
		if !budget.CanFit(sopTokens) {
			return &StrategyResult{
				Error: fmt.Sprintf("plan-scope SOPs require %d tokens but only %d available (all-or-nothing policy)", sopTokens, budget.Remaining()),
			}, nil
		}

		content, tokens, ids := s.gatherers.SOP.GetSOPContent(sops)
		if err := budget.Allocate("plan_sops", tokens); err != nil {
			return &StrategyResult{
				Error: fmt.Sprintf("failed to allocate SOP tokens: %v", err),
			}, nil
		}

		// Update header for plan review context
		content = strings.Replace(content,
			"The following SOPs apply to the files being reviewed:",
			"The following SOPs apply to this plan review:",
			1)
		result.Documents["__sops__"] = content
		result.SOPIDs = ids

		for _, sop := range sops {
			result.Entities = append(result.Entities, EntityRef{
				ID:     sop.ID,
				Type:   "sop",
				Tokens: sop.Tokens,
			})
		}

		s.logger.Info("Included plan-scope SOPs",
			"count", len(sops),
			"tokens", sopTokens)
	}

	// Step 2: Include plan content
	if req.PlanContent != "" {
		estimator := NewTokenEstimator()
		planTokens := estimator.Estimate(req.PlanContent)

		if budget.CanFit(planTokens) {
			if err := budget.Allocate("plan_content", planTokens); err == nil {
				result.Documents["__plan__"] = formatPlanContent(req.PlanSlug, req.PlanContent)
			}
		} else {
			// Plan content is essential - truncate if needed
			remaining := budget.Remaining()
			truncated, wasTruncated := estimator.TruncateToTokens(req.PlanContent, remaining)
			actualTokens := estimator.Estimate(truncated)

			if allocated := budget.TryAllocate("plan_content", actualTokens); allocated > 0 {
				result.Documents["__plan__"] = formatPlanContent(req.PlanSlug, truncated)
				result.Truncated = result.Truncated || wasTruncated
			}
		}
	}

	// Step 3: Include architecture documents if budget allows
	if budget.Remaining() > MinTokensForDocs {
		archDocs := []string{
			"docs/03-architecture.md",
			"docs/ARCHITECTURE.md",
			"ARCHITECTURE.md",
			"docs/design.md",
			"docs/api-design.md",
			".semspec/architecture.md",
		}

		for _, docPath := range archDocs {
			if budget.Remaining() < MinTokensForPartial {
				break
			}

			if s.gatherers.File.FileExists(docPath) {
				file, err := s.gatherers.File.ReadFile(ctx, docPath)
				if err != nil {
					continue
				}

				if budget.CanFit(file.Tokens) {
					if err := budget.Allocate("arch:"+docPath, file.Tokens); err == nil {
						result.Documents[docPath] = file.Content
					}
				}
			}
		}
	}

	return result, nil
}

// formatPlanContent formats plan content for the context.
func formatPlanContent(slug, content string) string {
	var sb strings.Builder
	sb.WriteString("# Plan Under Review\n\n")
	if slug != "" {
		sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n\n", slug))
	}
	sb.WriteString("```json\n")
	sb.WriteString(content)
	sb.WriteString("\n```\n")
	return sb.String()
}
