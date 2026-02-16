package strategies

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// ImplementationStrategy builds context for implementation tasks.
// Priority order:
// 1. Spec document (required - fail if not found)
// 2. Source files in scope
// 3. Related patterns/examples
// 4. Architecture docs
type ImplementationStrategy struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewImplementationStrategy creates a new implementation strategy.
func NewImplementationStrategy(gatherers *Gatherers, logger *slog.Logger) *ImplementationStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &ImplementationStrategy{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Build implements Strategy.
func (s *ImplementationStrategy) Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error) {
	result := &StrategyResult{
		Documents: make(map[string]string),
	}

	estimator := NewTokenEstimator()

	// Step 1: Spec document (required)
	if req.SpecEntityID != "" {
		content, err := s.gatherers.Graph.HydrateEntity(ctx, req.SpecEntityID, 2)
		if err != nil {
			return &StrategyResult{
				Error: fmt.Sprintf("failed to get spec entity %s: %v", req.SpecEntityID, err),
			}, nil
		}

		tokens := estimator.Estimate(content)
		if !budget.CanFit(tokens) {
			return &StrategyResult{
				Error: fmt.Sprintf("spec requires %d tokens but only %d available", tokens, budget.Remaining()),
			}, nil
		}

		if err := budget.Allocate("spec", tokens); err != nil {
			return &StrategyResult{
				Error: fmt.Sprintf("failed to allocate spec tokens: %v", err),
			}, nil
		}

		result.Documents["__spec__"] = content
		result.Entities = append(result.Entities, EntityRef{
			ID:      req.SpecEntityID,
			Type:    "spec",
			Content: content,
			Tokens:  tokens,
		})
	}

	// Step 2: Source files in scope (from request files)
	if len(req.Files) > 0 {
		remaining := budget.Remaining()
		docs, tokens, truncated, err := s.gatherers.File.ReadFilesPartial(ctx, req.Files, remaining)
		if err != nil {
			s.logger.Warn("Failed to read source files", "error", err)
		} else if len(docs) > 0 {
			if allocated := budget.TryAllocate("source_files", tokens); allocated > 0 {
				for path, content := range docs {
					result.Documents[path] = content
				}
				result.Truncated = result.Truncated || truncated
			}
		}
	}

	// Step 3: Related patterns/examples from graph
	// Note: This performs client-side filtering which may be slow on large codebases.
	// Consider implementing server-side search if performance becomes an issue.
	if budget.Remaining() > MinTokensForPatterns {
		if req.Topic != "" {
			relatedEntities, err := s.findRelatedPatterns(ctx, req.Topic)
			if err != nil {
				s.logger.Warn("Failed to find related patterns", "error", err)
			} else {
				for _, entity := range relatedEntities {
					if budget.Remaining() < MinTokensForPartial {
						break
					}

					content, err := s.gatherers.Graph.HydrateEntity(ctx, entity.ID, 1)
					if err != nil {
						continue
					}

					tokens := estimator.Estimate(content)
					if budget.CanFit(tokens) {
						if err := budget.Allocate("pattern:"+entity.ID, tokens); err == nil {
							result.Documents["__pattern__"+entity.ID] = content
							result.Entities = append(result.Entities, EntityRef{
								ID:      entity.ID,
								Type:    "pattern",
								Content: content,
								Tokens:  tokens,
							})
						}
					}
				}
			}
		}
	}

	// Step 4: Architecture docs
	if budget.Remaining() > MinTokensForConventions {
		archFiles := []string{
			"docs/03-architecture.md",
			"ARCHITECTURE.md",
			"docs/design.md",
			"docs/README.md",
			"README.md",
		}

		for _, af := range archFiles {
			if budget.Remaining() < MinTokensForPartial {
				break
			}

			if s.gatherers.File.FileExists(af) {
				file, err := s.gatherers.File.ReadFile(ctx, af)
				if err != nil {
					continue
				}

				if budget.CanFit(file.Tokens) {
					if err := budget.Allocate("arch:"+af, file.Tokens); err == nil {
						result.Documents[af] = file.Content
					}
				} else if budget.Remaining() > MinTokensForConventions {
					// Truncate to fit
					truncated, _ := estimator.TruncateToTokens(file.Content, budget.Remaining())
					tokens := estimator.Estimate(truncated)
					if err := budget.Allocate("arch:"+af, tokens); err == nil {
						result.Documents[af] = truncated
						result.Truncated = true
					}
				}
			}
		}
	}

	return result, nil
}

// findRelatedPatterns finds code patterns related to a topic.
// Note: This implementation fetches entities and filters client-side, which may be
// slow on large codebases. For better performance, consider implementing server-side
// search or using a dedicated search index.
func (s *ImplementationStrategy) findRelatedPatterns(ctx context.Context, topic string) ([]EntityRef, error) {
	patterns := make([]EntityRef, 0)
	topicLower := strings.ToLower(topic)
	functionsFound := 0

	// Search functions
	functions, err := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, "code.function")
	if err != nil {
		return nil, err
	}

	for _, f := range functions {
		idLower := strings.ToLower(f.ID)
		if strings.Contains(idLower, topicLower) {
			patterns = append(patterns, EntityRef{
				ID:   f.ID,
				Type: "function",
			})
			functionsFound++
			if functionsFound >= 5 {
				break
			}
		}
	}

	// Search types
	types, err := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, "code.type")
	if err != nil {
		return patterns, nil // Return what we have
	}

	typesFound := 0
	for _, t := range types {
		idLower := strings.ToLower(t.ID)
		if strings.Contains(idLower, topicLower) {
			patterns = append(patterns, EntityRef{
				ID:   t.ID,
				Type: "type",
			})
			typesFound++
			if typesFound >= 5 {
				break
			}
		}
		if len(patterns) >= MaxRelatedPatterns {
			break
		}
	}

	return patterns, nil
}
