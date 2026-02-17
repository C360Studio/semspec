package strategies

import (
	"context"
	"log/slog"
	"strings"
)

// PlanningStrategy builds context for plan generation tasks.
// Priority order:
// 1. Codebase summary (essential for understanding scope)
// 2. Architecture docs (for design decisions)
// 3. Existing specs/plans (for continuity)
// 4. Relevant code patterns (for implementation awareness)
type PlanningStrategy struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewPlanningStrategy creates a new planning strategy.
func NewPlanningStrategy(gatherers *Gatherers, logger *slog.Logger) *PlanningStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlanningStrategy{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Build implements Strategy.
func (s *PlanningStrategy) Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error) {
	result := &StrategyResult{
		Documents: make(map[string]string),
		Questions: make([]Question, 0),
	}

	estimator := NewTokenEstimator()

	// Track context sufficiency indicators
	var hasArchDocs, hasExistingSpecs, hasCodePatterns bool

	// Step 1: Codebase summary (essential for planning)
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

	// Step 2: Architecture documentation
	archDocs := []string{
		"docs/03-architecture.md",
		"docs/how-it-works.md",
		"CLAUDE.md",
		"docs/components.md",
		"docs/getting-started.md",
	}

	for _, df := range archDocs {
		if budget.Remaining() < MinTokensForDocs {
			break
		}

		if s.gatherers.File.FileExists(df) {
			file, err := s.gatherers.File.ReadFile(ctx, df)
			if err != nil {
				continue
			}

			if budget.CanFit(file.Tokens) {
				if err := budget.Allocate("arch:"+df, file.Tokens); err == nil {
					result.Documents[df] = file.Content
					hasArchDocs = true
				}
			} else if budget.Remaining() > MinTokensForPartial {
				// Truncate to fit
				truncated, _ := estimator.TruncateToTokens(file.Content, budget.Remaining())
				tokens := estimator.Estimate(truncated)
				if err := budget.Allocate("arch:"+df, tokens); err == nil {
					result.Documents[df] = truncated
					result.Truncated = true
					hasArchDocs = true
				}
			}
		}
	}

	// Step 3: Existing specs and plans (for continuity)
	if budget.Remaining() > MinTokensForPatterns {
		existingSpecs, err := s.findExistingSpecs(ctx, req.Topic)
		if err != nil {
			s.logger.Warn("Failed to find existing specs", "error", err)
		} else {
			for _, entity := range existingSpecs {
				if budget.Remaining() < MinTokensForPartial {
					break
				}

				content, err := s.gatherers.Graph.HydrateEntity(ctx, entity.ID, 1)
				if err != nil {
					continue
				}

				tokens := estimator.Estimate(content)
				if budget.CanFit(tokens) {
					if err := budget.Allocate("spec:"+entity.ID, tokens); err == nil {
						result.Documents["__spec__"+entity.ID] = content
						result.Entities = append(result.Entities, EntityRef{
							ID:      entity.ID,
							Type:    entity.Type,
							Content: content,
							Tokens:  tokens,
						})
						hasExistingSpecs = true
					}
				}
			}
		}
	}

	// Step 4: Relevant code patterns (for implementation awareness)
	if req.Topic != "" && budget.Remaining() > MinTokensForPatterns {
		patterns, err := s.findRelevantPatterns(ctx, req.Topic)
		if err != nil {
			s.logger.Warn("Failed to find relevant patterns", "error", err)
		} else {
			for _, entity := range patterns {
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
							Type:    entity.Type,
							Content: content,
							Tokens:  tokens,
						})
						hasCodePatterns = true
					}
				}
			}
		}
	}

	// Step 5: If specific files were mentioned, include them
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

	// Step 6: Detect context insufficiency and generate questions
	s.detectInsufficientContext(result, req, hasArchDocs, hasExistingSpecs, hasCodePatterns)

	return result, nil
}

// detectInsufficientContext checks for critical gaps and generates questions.
func (s *PlanningStrategy) detectInsufficientContext(result *StrategyResult, req *ContextBuildRequest, hasArchDocs, hasExistingSpecs, hasCodePatterns bool) {
	// No architecture docs - ask about architecture context
	if !hasArchDocs && !hasExistingSpecs {
		result.Questions = append(result.Questions, Question{
			Topic:    "architecture.context",
			Question: "No architecture documentation or existing specifications were found. What is the architectural context for this planning task?",
			Context:  "Topic: " + req.Topic,
			Urgency:  UrgencyHigh,
		})
	}

	// No code patterns found for topic - ask about implementation approach
	if req.Topic != "" && !hasCodePatterns && !hasExistingSpecs {
		result.Questions = append(result.Questions, Question{
			Topic:    "architecture.patterns",
			Question: "No existing code patterns were found related to '" + req.Topic + "'. What implementation patterns should be followed?",
			Context:  "Topic: " + req.Topic,
			Urgency:  UrgencyNormal,
		})
	}

	// Ambiguous scope detection - no topic and no files
	if req.Topic == "" && len(req.Files) == 0 && req.SpecEntityID == "" {
		result.Questions = append(result.Questions, Question{
			Topic:    "requirements.scope",
			Question: "The planning request has no specified topic, files, or specification. What is the scope of this planning task?",
			Context:  "WorkflowID: " + req.WorkflowID,
			Urgency:  UrgencyBlocking,
		})
		result.InsufficientContext = true
	}

	// If we have multiple questions and any are high urgency, mark as insufficient
	if len(result.Questions) > 1 {
		for _, q := range result.Questions {
			if q.Urgency == UrgencyBlocking || q.Urgency == UrgencyHigh {
				result.InsufficientContext = true
				break
			}
		}
	}
}

// findExistingSpecs searches for existing specs and proposals related to the topic.
func (s *PlanningStrategy) findExistingSpecs(ctx context.Context, topic string) ([]EntityRef, error) {
	entities := make([]EntityRef, 0)
	topicLower := strings.ToLower(topic)

	// Search for existing plans and specs
	prefixes := []struct {
		prefix string
		typ    string
	}{
		{"semspec.plan", "plan"},
		{"semspec.spec", "spec"},
	}

	for _, p := range prefixes {
		found, err := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, p.prefix)
		if err != nil {
			continue
		}

		for _, e := range found {
			idLower := strings.ToLower(e.ID)
			if topic == "" || strings.Contains(idLower, topicLower) {
				entities = append(entities, EntityRef{
					ID:   e.ID,
					Type: p.typ,
				})

				// Limit to avoid overwhelming context
				if len(entities) >= MaxRelatedPatterns {
					return entities, nil
				}
			}
		}
	}

	return entities, nil
}

// findRelevantPatterns searches for code patterns related to the planning topic.
func (s *PlanningStrategy) findRelevantPatterns(ctx context.Context, topic string) ([]EntityRef, error) {
	entities := make([]EntityRef, 0)
	topicLower := strings.ToLower(topic)

	// Search across different entity types for relevant patterns
	prefixes := []struct {
		prefix string
		typ    string
	}{
		{"code.function", "function"},
		{"code.type", "type"},
		{"code.interface", "interface"},
		{"code.package", "package"},
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

				if matchesInType >= MaxEntitiesPerType {
					break
				}
			}
		}

		if len(entities) >= MaxRelatedPatterns {
			break
		}
	}

	return entities, nil
}
