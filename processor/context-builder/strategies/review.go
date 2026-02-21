package strategies

import (
	"context"
	"log/slog"
)

// ReviewStrategy builds context for code review tasks.
// Priority order:
// 1. Git diff (truncate at file boundaries)
// 2. Related tests (include as many as fit)
// 3. Conventions (fill remaining budget)
//
// SOP rules are NOT loaded here — they are injected as a standards preamble
// by Builder.loadStandardsPreamble() from .semspec/standards.json. The source-
// ingester populates standards.json with extracted requirements when SOPs are
// first ingested, so we never need to re-read raw SOP content from the graph.
type ReviewStrategy struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewReviewStrategy creates a new review strategy.
func NewReviewStrategy(gatherers *Gatherers, logger *slog.Logger) *ReviewStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReviewStrategy{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Build implements Strategy.
func (s *ReviewStrategy) Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error) {
	result := &StrategyResult{
		Documents: make(map[string]string),
	}

	// Get changed files - either from request or from git
	files := req.Files
	if len(files) == 0 && req.GitRef != "" {
		var err error
		files, err = s.gatherers.Git.GetChangedFiles(ctx, req.GitRef)
		if err != nil {
			s.logger.Warn("Failed to get changed files from git", "ref", req.GitRef, "error", err)
		}
	}

	// Infer semantic domains from changed files
	var domains []string
	if len(files) > 0 {
		domains = s.gatherers.File.InferDomains(ctx, files)
		result.Domains = domains
		if len(domains) > 0 {
			s.logger.Info("Inferred domains from changed files",
				"domains", domains,
				"file_count", len(files))
		}
	}

	// NOTE: SOP rules are NOT loaded here from the graph.
	// Standards.json (populated by source-ingester from SOP requirements) is
	// injected as a preamble by Builder.loadStandardsPreamble() after all
	// strategies complete. This avoids re-reading raw SOP content from the
	// graph on every context build.

	// Step 1: Git diff (truncate at file boundaries if needed)
	if req.GitRef != "" || len(files) > 0 {
		diff, err := s.gatherers.Git.GetDiff(ctx, req.GitRef, files)
		if err != nil {
			s.logger.Warn("Failed to get git diff", "error", err)
		} else if diff != "" {
			estimator := NewTokenEstimator()
			diffTokens := estimator.Estimate(diff)

			if budget.CanFit(diffTokens) {
				// Diff fits entirely
				if err := budget.Allocate("git_diff", diffTokens); err == nil {
					result.Diffs = diff
				}
			} else {
				// Truncate at file boundaries
				remaining := budget.Remaining()
				maxBytes := remaining * 4 // Approximate chars from tokens
				truncated, wasTruncated := s.gatherers.Git.TruncateDiffByFiles(diff, maxBytes)

				actualTokens := estimator.Estimate(truncated)
				if allocated := budget.TryAllocate("git_diff", actualTokens); allocated > 0 {
					result.Diffs = truncated
					result.Truncated = result.Truncated || wasTruncated
				}
			}
		}
	}

	// Step 2: Related tests (include as many as fit)
	if len(files) > 0 {
		testFiles, err := s.gatherers.File.FindTestFiles(ctx, files)
		if err != nil {
			s.logger.Warn("Failed to find test files", "error", err)
		} else if len(testFiles) > 0 {
			remaining := budget.Remaining()
			if remaining > MinTokensForTests {
				docs, tokens, filesTruncated, readErr := s.gatherers.File.ReadFilesPartial(ctx, testFiles, remaining)
				if readErr != nil {
					s.logger.Warn("Failed to read test files", "error", readErr)
				} else if len(docs) > 0 {
					if allocated := budget.TryAllocate("tests", tokens); allocated > 0 {
						for path, content := range docs {
							result.Documents[path] = content
						}
						result.Truncated = result.Truncated || filesTruncated
					}
				}
			}
		}
	}

	// Step 3: Conventions (fill remaining budget)
	if budget.Remaining() > MinTokensForConventions {
		conventionFiles := []string{
			"CONVENTIONS.md",
			"STYLE.md",
			"CONTRIBUTING.md",
			".github/CONTRIBUTING.md",
			"docs/conventions.md",
			"docs/style-guide.md",
		}

		for _, cf := range conventionFiles {
			if budget.Remaining() < MinTokensForPartial {
				break
			}

			if s.gatherers.File.FileExists(cf) {
				file, err := s.gatherers.File.ReadFile(ctx, cf)
				if err != nil {
					continue
				}

				if budget.CanFit(file.Tokens) {
					if err := budget.Allocate("convention:"+cf, file.Tokens); err == nil {
						result.Documents[cf] = file.Content
					}
				}
			}
		}
	}

	return result, nil
}
