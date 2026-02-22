package strategies

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// PlanReviewStrategy builds context for plan review/approval tasks.
// Priority order:
// 1. Plan content (the actual plan being reviewed)
// 2. Project file tree (for scope hallucination detection)
// 3. Related architecture documents (fill remaining budget)
//
// SOP rules are NOT loaded here — they are injected as a standards preamble
// by Builder.loadStandardsPreamble() from .semspec/standards.json. The source-
// ingester populates standards.json with extracted requirements when SOPs are
// first ingested, so we never need to re-read raw SOP content from the graph.
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

	// NOTE: SOP rules are NOT loaded here.
	// Standards.json (populated by source-ingester from SOP requirements) is
	// injected as a preamble by Builder.loadStandardsPreamble() after all
	// strategies complete. This avoids re-reading raw SOP content from the
	// graph on every context build — the graph stores extracted semantics,
	// not document bodies for repeated retrieval.

	// Step 1: Include plan content
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

	// Step 2: Include project file tree so reviewer can detect hallucinated scope paths
	if budget.Remaining() > MinTokensForDocs {
		files, err := s.gatherers.File.ListFilesRecursive(ctx)
		if err != nil {
			s.logger.Warn("Failed to list project files for review", "error", err)
		} else if len(files) > 0 {
			// Filter out dotfiles and sources/ directory for greenfield detection.
			var sourceFiles []string
			for _, f := range files {
				if strings.HasPrefix(f, ".") || strings.HasPrefix(f, "sources/") {
					continue
				}
				sourceFiles = append(sourceFiles, f)
			}

			var tree string
			if len(sourceFiles) == 0 {
				// Greenfield project — no user source files exist yet.
				tree = "# Project File Tree\n\n" +
					"**GREENFIELD PROJECT** — the workspace is empty, there are no existing source files.\n\n" +
					"IMPORTANT: This is a greenfield project. All scope paths in the plan reference files that the plan INTENDS TO CREATE.\n" +
					"These paths are NOT hallucinations — they are the expected output of the plan.\n" +
					"Do NOT flag scope paths as referencing non-existent files.\n" +
					"Do NOT require migration strategies — there is nothing to migrate from in a greenfield project.\n"
			} else {
				var sb strings.Builder
				sb.WriteString("# Project File Tree\n\n")
				sb.WriteString("These are the existing files in the project.\n\n")
				sb.WriteString("## Scope Path Validation Rules\n\n")
				sb.WriteString("Plan scope.include paths may reference:\n")
				sb.WriteString("1. **Existing files** - paths that match files listed below (will be modified)\n")
				sb.WriteString("2. **New files to create** - paths in existing directories or reasonable new directories\n\n")
				sb.WriteString("Only flag a scope path as an ERROR if it appears to be a TYPO or MISTAKE, such as:\n")
				sb.WriteString("- Misspelled directory names (e.g., 'src/componets/' instead of 'src/components/')\n")
				sb.WriteString("- Wrong file extensions (e.g., 'app.jsx' when project uses '.tsx')\n")
				sb.WriteString("- Paths that contradict the project structure (e.g., 'backend/' in a frontend-only project)\n\n")
				sb.WriteString("Do NOT flag paths like 'tests/test_api.py' as errors - these are files the plan intends to CREATE.\n\n")
				sb.WriteString("## Existing Files\n\n")
				for _, f := range files {
					sb.WriteString(f)
					sb.WriteString("\n")
				}
				tree = sb.String()
			}
			estimator := NewTokenEstimator()
			tokens := estimator.Estimate(tree)
			if tokens > 800 {
				tree, _ = estimator.TruncateToTokens(tree, 800)
				tokens = 800
			}
			if budget.CanFit(tokens) {
				if err := budget.Allocate("file_tree", tokens); err == nil {
					result.Documents["__file_tree__"] = tree
				}
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
