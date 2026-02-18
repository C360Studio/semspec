package strategies

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
)

// ReviewStrategy builds context for code review tasks.
// Priority order:
// 1. SOPs (all-or-nothing - fail if exceeds budget)
// 2. Git diff (truncate at file boundaries)
// 3. Related tests (include as many as fit)
// 4. Conventions (fill remaining budget)
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

	// Step 1: SOPs (all-or-nothing per ADR-005, graph query — skip if graph not ready)
	if len(files) > 0 && !req.GraphReady {
		s.logger.Info("Skipping SOPs for review (graph not ready)")
	} else if len(files) > 0 {
		allSOPs := s.gatherAllSOPs(ctx, files, domains)
		if len(allSOPs) > 0 {
			sopTokens := s.gatherers.SOP.TotalTokens(allSOPs)

			// All-or-nothing: if SOPs don't fit, fail the build
			if !budget.CanFit(sopTokens) {
				return &StrategyResult{
					Error: fmt.Sprintf("SOPs require %d tokens but only %d available (all-or-nothing policy)", sopTokens, budget.Remaining()),
				}, nil
			}

			content, tokens, ids := s.gatherers.SOP.GetSOPContent(allSOPs)
			if err := budget.Allocate("sops", tokens); err != nil {
				return &StrategyResult{
					Error: fmt.Sprintf("failed to allocate SOP tokens: %v", err),
				}, nil
			}

			result.Documents["__sops__"] = content
			result.SOPIDs = ids

			for _, sop := range allSOPs {
				result.Entities = append(result.Entities, EntityRef{
					ID:     sop.ID,
					Type:   "sop",
					Tokens: sop.Tokens,
				})
			}

			s.logger.Info("Gathered SOPs for review",
				"total_sops", len(allSOPs),
				"total_tokens", sopTokens)
		}
	}

	// Step 2: Git diff (truncate at file boundaries if needed)
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

	// Step 3: Related tests (include as many as fit)
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

	// Step 4: Conventions (fill remaining budget)
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

// gatherAllSOPs gathers SOPs from multiple sources and merges them:
// 1. Pattern-matched SOPs (based on file path patterns)
// 2. Domain-matched SOPs (based on inferred semantic domains)
// 3. Cross-domain SOPs (based on related domains)
//
// This method uses "best effort" gathering: individual source failures are logged
// but don't prevent other sources from being gathered. This ensures the review
// can proceed even if one gathering method fails.
func (s *ReviewStrategy) gatherAllSOPs(ctx context.Context, files []string, domains []string) []*gatherers.SOPDocument {
	seen := make(map[string]bool)
	var allSOPs []*gatherers.SOPDocument

	// Step 1: Pattern-matched SOPs (existing behavior)
	patternSOPs, err := s.gatherers.SOP.GetSOPsForFiles(ctx, files)
	if err != nil {
		s.logger.Warn("Failed to get pattern-matched SOPs", "error", err)
	} else {
		for _, sop := range patternSOPs {
			if !seen[sop.ID] {
				seen[sop.ID] = true
				allSOPs = append(allSOPs, sop)
			}
		}
		if len(patternSOPs) > 0 {
			s.logger.Debug("Found pattern-matched SOPs", "count", len(patternSOPs))
		}
	}

	// Step 2: Domain-matched SOPs
	if len(domains) > 0 {
		domainSOPs, err := s.gatherers.SOP.GetSOPsByDomain(ctx, domains)
		if err != nil {
			s.logger.Warn("Failed to get domain-matched SOPs", "error", err)
		} else {
			for _, sop := range domainSOPs {
				if !seen[sop.ID] {
					seen[sop.ID] = true
					allSOPs = append(allSOPs, sop)
				}
			}
			if len(domainSOPs) > 0 {
				s.logger.Debug("Found domain-matched SOPs",
					"count", len(domainSOPs),
					"domains", domains)
			}
		}

		// Step 3: Cross-domain SOPs (expand to related domains)
		relatedDomains := s.gatherers.File.ExpandRelatedDomains(domains)
		// Filter out domains we already searched
		var newDomains []string
		for _, d := range relatedDomains {
			isOriginal := false
			for _, orig := range domains {
				if d == orig {
					isOriginal = true
					break
				}
			}
			if !isOriginal {
				newDomains = append(newDomains, d)
			}
		}

		if len(newDomains) > 0 {
			crossDomainSOPs, err := s.gatherers.SOP.GetSOPsByDomain(ctx, newDomains)
			if err != nil {
				s.logger.Warn("Failed to get cross-domain SOPs", "error", err)
			} else {
				for _, sop := range crossDomainSOPs {
					if !seen[sop.ID] {
						seen[sop.ID] = true
						allSOPs = append(allSOPs, sop)
					}
				}
				if len(crossDomainSOPs) > 0 {
					s.logger.Debug("Found cross-domain SOPs",
						"count", len(crossDomainSOPs),
						"related_domains", newDomains)
				}
			}
		}
	}

	return allSOPs
}
