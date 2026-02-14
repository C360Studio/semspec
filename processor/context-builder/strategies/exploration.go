package strategies

import (
	"context"
	"log/slog"
	"strings"
)

// ExplorationStrategy builds context for exploration tasks.
// Priority order:
// 1. Codebase summary
// 2. Entities matching topic
// 3. Related docs
type ExplorationStrategy struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewExplorationStrategy creates a new exploration strategy.
func NewExplorationStrategy(gatherers *Gatherers, logger *slog.Logger) *ExplorationStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExplorationStrategy{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Build implements Strategy.
func (s *ExplorationStrategy) Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error) {
	result := &StrategyResult{
		Documents: make(map[string]string),
	}

	estimator := NewTokenEstimator()

	// Step 1: Codebase summary (always include)
	summary, err := s.gatherers.Graph.GetCodebaseSummary(ctx)
	if err != nil {
		s.logger.Warn("Failed to get codebase summary", "error", err)
	} else if summary != "" {
		tokens := estimator.Estimate(summary)
		if budget.CanFit(tokens) {
			if err := budget.Allocate("codebase_summary", tokens); err == nil {
				result.Documents["__summary__"] = summary
			}
		}
	}

	// Step 2: Entities matching topic
	// Note: This performs client-side filtering which may be slow on large codebases.
	// Consider implementing server-side search if performance becomes an issue.
	if req.Topic != "" && budget.Remaining() > MinTokensForPatterns {
		matchingEntities, err := s.findMatchingEntities(ctx, req.Topic)
		if err != nil {
			s.logger.Warn("Failed to find matching entities", "error", err)
		} else {
			for _, entity := range matchingEntities {
				if budget.Remaining() < MinTokensForPartial {
					break
				}

				content, err := s.gatherers.Graph.HydrateEntity(ctx, entity.ID, 1)
				if err != nil {
					continue
				}

				tokens := estimator.Estimate(content)
				if budget.CanFit(tokens) {
					if err := budget.Allocate("entity:"+entity.ID, tokens); err == nil {
						result.Documents["__entity__"+entity.ID] = content
						result.Entities = append(result.Entities, EntityRef{
							ID:      entity.ID,
							Type:    entity.Type,
							Content: content,
							Tokens:  tokens,
						})
					}
				}
			}
		}
	}

	// Step 3: Related docs
	if budget.Remaining() > MinTokensForConventions {
		docFiles := []string{
			"README.md",
			"docs/README.md",
			"docs/architecture.md",
			"docs/getting-started.md",
			"CONTRIBUTING.md",
		}

		for _, df := range docFiles {
			if budget.Remaining() < MinTokensForPartial {
				break
			}

			if s.gatherers.File.FileExists(df) {
				file, err := s.gatherers.File.ReadFile(ctx, df)
				if err != nil {
					continue
				}

				if budget.CanFit(file.Tokens) {
					if err := budget.Allocate("doc:"+df, file.Tokens); err == nil {
						result.Documents[df] = file.Content
					}
				} else if budget.Remaining() > MinTokensForDocs {
					// Truncate to fit
					truncated, _ := estimator.TruncateToTokens(file.Content, budget.Remaining())
					tokens := estimator.Estimate(truncated)
					if err := budget.Allocate("doc:"+df, tokens); err == nil {
						result.Documents[df] = truncated
						result.Truncated = true
					}
				}
			}
		}
	}

	// Step 4: If specific files were requested, include them
	if len(req.Files) > 0 && budget.Remaining() > MinTokensForConventions {
		docs, tokens, truncated, err := s.gatherers.File.ReadFilesPartial(ctx, req.Files, budget.Remaining())
		if err != nil {
			s.logger.Warn("Failed to read requested files", "error", err)
		} else if len(docs) > 0 {
			if allocated := budget.TryAllocate("requested_files", tokens); allocated > 0 {
				for path, content := range docs {
					result.Documents[path] = content
				}
				result.Truncated = result.Truncated || truncated
			}
		}
	}

	return result, nil
}

// findMatchingEntities searches for entities matching the given topic.
// Note: This implementation fetches entities and filters client-side, which may be
// slow on large codebases. For better performance, consider implementing server-side
// search or using a dedicated search index.
func (s *ExplorationStrategy) findMatchingEntities(ctx context.Context, topic string) ([]EntityRef, error) {
	entities := make([]EntityRef, 0)
	topicLower := strings.ToLower(topic)

	// Search across different entity types
	prefixes := []struct {
		prefix string
		typ    string
	}{
		{"code.function", "function"},
		{"code.type", "type"},
		{"code.interface", "interface"},
		{"code.package", "package"},
		{"semspec.proposal", "proposal"},
	}

	for _, p := range prefixes {
		found, err := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, p.prefix)
		if err != nil {
			continue
		}

		matchesInType := 0
		for _, e := range found {
			idLower := strings.ToLower(e.ID)
			if strings.Contains(idLower, topicLower) {
				entities = append(entities, EntityRef{
					ID:   e.ID,
					Type: p.typ,
				})
				matchesInType++

				// Limit per type
				if matchesInType >= MaxEntitiesPerType {
					break
				}
			}
		}

		// Overall limit
		if len(entities) >= MaxMatchingEntities {
			break
		}
	}

	return entities, nil
}
