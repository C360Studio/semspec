package strategies

import (
	"context"
	"log/slog"
	"strings"
)

// QuestionStrategy builds context for question-answering tasks.
// Priority order:
// 1. Entities matching the question topic
// 2. Codebase summary (for general understanding)
// 3. Related documentation
// 4. Source documents if topic matches
type QuestionStrategy struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewQuestionStrategy creates a new question strategy.
func NewQuestionStrategy(gatherers *Gatherers, logger *slog.Logger) *QuestionStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &QuestionStrategy{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Build implements Strategy.
func (s *QuestionStrategy) Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error) {
	result := &StrategyResult{
		Documents: make(map[string]string),
		Questions: make([]Question, 0),
	}

	estimator := NewTokenEstimator()

	// Track context sufficiency indicators
	var hasMatchingEntities, hasSourceDocs, hasRelevantDocs bool

	// Step 1: Find entities matching the question topic
	// This is the primary source of context for answering questions
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
						hasMatchingEntities = true
					}
				}
			}

			s.logger.Info("Found matching entities for question",
				"topic", req.Topic,
				"count", len(matchingEntities))
		}
	}

	// Step 2: Include source documents that match the topic
	if req.Topic != "" && budget.Remaining() > MinTokensForPatterns {
		sourceDocs, err := s.findSourceDocuments(ctx, req.Topic)
		if err != nil {
			s.logger.Warn("Failed to find source documents", "error", err)
		} else {
			for _, entity := range sourceDocs {
				if budget.Remaining() < MinTokensForPartial {
					break
				}

				content, err := s.gatherers.Graph.HydrateEntity(ctx, entity.ID, 1)
				if err != nil {
					continue
				}

				tokens := estimator.Estimate(content)
				if budget.CanFit(tokens) {
					if err := budget.Allocate("source:"+entity.ID, tokens); err == nil {
						result.Documents["__source__"+entity.ID] = content
						result.Entities = append(result.Entities, EntityRef{
							ID:      entity.ID,
							Type:    "source",
							Content: content,
							Tokens:  tokens,
						})
						hasSourceDocs = true
					}
				}
			}
		}
	}

	// Step 3: Codebase summary for general context
	if budget.Remaining() > MinTokensForDocs {
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
	}

	// Step 4: Related documentation
	if budget.Remaining() > MinTokensForConventions {
		docFiles := []string{
			"README.md",
			"docs/README.md",
			"docs/architecture.md",
			"docs/how-it-works.md",
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

				// Check if document is relevant to topic
				if req.Topic != "" && !s.isRelevantDocument(file.Content, req.Topic) {
					continue
				}

				if budget.CanFit(file.Tokens) {
					if err := budget.Allocate("doc:"+df, file.Tokens); err == nil {
						result.Documents[df] = file.Content
						hasRelevantDocs = true
					}
				} else if budget.Remaining() > MinTokensForDocs {
					// Truncate to fit
					truncated, _ := estimator.TruncateToTokens(file.Content, budget.Remaining())
					tokens := estimator.Estimate(truncated)
					if err := budget.Allocate("doc:"+df, tokens); err == nil {
						result.Documents[df] = truncated
						result.Truncated = true
						hasRelevantDocs = true
					}
				}
			}
		}
	}

	// Step 5: If specific files were requested, include them
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
	s.detectInsufficientContext(result, req, hasMatchingEntities, hasSourceDocs, hasRelevantDocs)

	return result, nil
}

// detectInsufficientContext checks for critical gaps and generates questions.
func (s *QuestionStrategy) detectInsufficientContext(result *StrategyResult, req *ContextBuildRequest, hasMatchingEntities, hasSourceDocs, hasRelevantDocs bool) {
	// No topic provided - cannot answer without knowing what to answer
	if req.Topic == "" {
		result.Questions = append(result.Questions, Question{
			Topic:    "requirements.clarification",
			Question: "No topic was provided for this question-answering task. What is the question or topic to be answered?",
			Context:  "WorkflowID: " + req.WorkflowID,
			Urgency:  UrgencyBlocking,
		})
		result.InsufficientContext = true
		return
	}

	// No entities, source docs, or relevant docs found for the topic
	if !hasMatchingEntities && !hasSourceDocs && !hasRelevantDocs {
		result.Questions = append(result.Questions, Question{
			Topic:    "knowledge." + extractTopicCategory(req.Topic),
			Question: "No knowledge was found in the codebase related to '" + req.Topic + "'. Can you provide additional context or clarify the topic?",
			Context:  "Original topic: " + req.Topic,
			Urgency:  UrgencyHigh,
		})
		result.InsufficientContext = true
	}

	// Only docs found, no entity matches - might need clarification
	if !hasMatchingEntities && !hasSourceDocs && hasRelevantDocs {
		result.Questions = append(result.Questions, Question{
			Topic:    "knowledge." + extractTopicCategory(req.Topic),
			Question: "Only general documentation was found for '" + req.Topic + "'. Are you asking about a specific implementation detail that may not be documented?",
			Context:  "Original topic: " + req.Topic,
			Urgency:  UrgencyNormal,
		})
	}
}

// extractTopicCategory extracts a category from a topic string for routing.
func extractTopicCategory(topic string) string {
	keywords := extractKeywords(strings.ToLower(topic))
	if len(keywords) > 0 {
		return keywords[0]
	}
	return "general"
}

// findMatchingEntities searches for code entities matching the question topic.
func (s *QuestionStrategy) findMatchingEntities(ctx context.Context, topic string) ([]EntityRef, error) {
	entities := make([]EntityRef, 0)
	topicLower := strings.ToLower(topic)

	// Extract keywords from topic for better matching
	keywords := extractKeywords(topicLower)

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
		{"semspec.spec", "spec"},
	}

	for _, p := range prefixes {
		found, err := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, p.prefix)
		if err != nil {
			continue
		}

		matchesInType := 0
		for _, e := range found {
			idLower := strings.ToLower(e.ID)

			// Check if any keyword matches
			matched := false
			for _, kw := range keywords {
				if strings.Contains(idLower, kw) {
					matched = true
					break
				}
			}

			if matched {
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

		if len(entities) >= MaxMatchingEntities {
			break
		}
	}

	return entities, nil
}

// findSourceDocuments searches for source documents related to the topic.
func (s *QuestionStrategy) findSourceDocuments(ctx context.Context, topic string) ([]EntityRef, error) {
	entities := make([]EntityRef, 0)
	topicLower := strings.ToLower(topic)
	keywords := extractKeywords(topicLower)

	// Search for source documents
	found, err := s.gatherers.Graph.QueryEntitiesByPredicate(ctx, "source.doc")
	if err != nil {
		return entities, err
	}

	for _, e := range found {
		idLower := strings.ToLower(e.ID)

		// Check if any keyword matches
		for _, kw := range keywords {
			if strings.Contains(idLower, kw) {
				entities = append(entities, EntityRef{
					ID:   e.ID,
					Type: "source",
				})
				break
			}
		}

		if len(entities) >= MaxRelatedPatterns {
			break
		}
	}

	return entities, nil
}

// isRelevantDocument checks if a document is relevant to the given topic.
func (s *QuestionStrategy) isRelevantDocument(content, topic string) bool {
	contentLower := strings.ToLower(content)
	keywords := extractKeywords(strings.ToLower(topic))

	matchCount := 0
	for _, kw := range keywords {
		if strings.Contains(contentLower, kw) {
			matchCount++
		}
	}

	// Require at least half the keywords to match
	return matchCount > 0 && matchCount >= len(keywords)/2
}

// extractKeywords extracts meaningful keywords from a topic string.
func extractKeywords(topic string) []string {
	// Split on common delimiters
	words := strings.FieldsFunc(topic, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '.' || r == '/' || r == '?' || r == ','
	})

	// Filter out common stop words and short words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"how": true, "what": true, "where": true, "when": true, "why": true,
		"does": true, "do": true, "can": true, "could": true, "would": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"of": true, "with": true, "and": true, "or": true, "but": true,
		"it": true, "this": true, "that": true, "these": true, "those": true,
	}

	keywords := make([]string, 0)
	for _, word := range words {
		word = strings.TrimSpace(word)
		if len(word) >= 3 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}
