package strategies

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// PlanningStrategy builds context for plan generation tasks.
// Priority order:
// 1. File tree (fast, filesystem only — critical to prevent scope hallucination)
// 2. Codebase summary from graph (best-effort, timeout-guarded)
// 3. Architecture docs (filesystem reads)
// 4. Existing specs/plans (graph queries, timeout-guarded)
// 5. Relevant code patterns (graph queries, timeout-guarded)
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

	// Step 1: Project file tree (critical for scope generation — prevents hallucinated paths)
	// This runs FIRST because it's fast (filesystem only, no graph/network) and essential
	// for preventing LLM scope hallucination. Graph queries may timeout if index isn't ready.
	{
		files, err := s.gatherers.File.ListFilesRecursive(ctx)
		if err != nil {
			s.logger.Warn("Failed to list project files", "error", err)
		} else if len(files) > 0 {
			var sb strings.Builder
			sb.WriteString("# Project File Tree\n\n")
			sb.WriteString("These are the actual files in the project. Use ONLY these paths in scope.\n\n")
			for _, f := range files {
				sb.WriteString(f)
				sb.WriteString("\n")
			}
			tree := sb.String()
			tokens := estimator.Estimate(tree)
			// Cap at 500 tokens — truncate if project is very large
			if tokens > 500 {
				tree, _ = estimator.TruncateToTokens(tree, 500)
				tokens = 500
			}
			if budget.CanFit(tokens) {
				if err := budget.Allocate("file_tree", tokens); err == nil {
					result.Documents["__file_tree__"] = tree
					s.logger.Info("Included project file tree in planning context",
						"files", len(files), "tokens", tokens)
				}
			}
		}
	}

	// Step 2: Codebase summary from graph (best-effort, with timeout guard)
	// Skipped when graph pipeline isn't ready to avoid wasting time on doomed queries.
	if req.GraphReady {
		graphCtx, graphCancel := context.WithTimeout(ctx, 10*time.Second)
		summary, err := s.gatherers.Graph.GetCodebaseSummary(graphCtx)
		graphCancel()
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
	} else {
		s.logger.Info("Skipping graph codebase summary (graph not ready)")
	}

	// Step 3: Architecture documentation (filesystem reads — fast)
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

	// Step 4: Existing specs and plans (for continuity — graph queries, timeout-guarded)
	if req.GraphReady && budget.Remaining() > MinTokensForPatterns {
		specCtx, specCancel := context.WithTimeout(ctx, 10*time.Second)
		existingSpecs, err := s.findExistingSpecs(specCtx, req.Topic)
		specCancel()
		if err != nil {
			s.logger.Warn("Failed to find existing specs", "error", err)
		} else {
			for _, entity := range existingSpecs {
				if budget.Remaining() < MinTokensForPartial {
					break
				}

				hydrateCtx, hydrateCancel := context.WithTimeout(ctx, 5*time.Second)
				content, err := s.gatherers.Graph.HydrateEntity(hydrateCtx, entity.ID, 1)
				hydrateCancel()
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
	} else if !req.GraphReady {
		s.logger.Info("Skipping graph existing specs (graph not ready)")
	}

	// Step 5: Relevant code patterns (for implementation awareness — graph queries, timeout-guarded)
	if req.GraphReady && req.Topic != "" && budget.Remaining() > MinTokensForPatterns {
		patternCtx, patternCancel := context.WithTimeout(ctx, 10*time.Second)
		patterns, err := s.findRelevantPatterns(patternCtx, req.Topic)
		patternCancel()
		if err != nil {
			s.logger.Warn("Failed to find relevant patterns", "error", err)
		} else {
			for _, entity := range patterns {
				if budget.Remaining() < MinTokensForPartial {
					break
				}

				hydrateCtx, hydrateCancel := context.WithTimeout(ctx, 5*time.Second)
				content, err := s.gatherers.Graph.HydrateEntity(hydrateCtx, entity.ID, 1)
				hydrateCancel()
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

	// Step 6: If specific files were mentioned, include them (filesystem — fast)
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

	// Step 7: SOPs applicable to planning scope (best-effort)
	if s.gatherers.SOP != nil && budget.Remaining() > MinTokensForConventions {
		sops, err := s.gatherers.SOP.GetSOPsByScope(ctx, "plan", req.ScopePatterns)
		if err != nil {
			s.logger.Warn("Failed to get plan-scope SOPs", "error", err)
		} else if len(sops) > 0 {
			content, tokens, ids := s.gatherers.SOP.GetSOPContent(sops)
			if budget.CanFit(tokens) {
				if err := budget.Allocate("plan_sops", tokens); err == nil {
					result.Documents["__sops__"] = content
					result.SOPIDs = ids
					result.SOPRequirements = s.gatherers.SOP.CollectRequirements(sops)
					s.logger.Info("Included plan-scope SOPs in planning context",
						"count", len(sops),
						"tokens", tokens,
						"requirements", len(result.SOPRequirements))
				}
			} else {
				s.logger.Warn("Plan-scope SOPs exceed remaining budget, skipping",
					"sop_tokens", tokens,
					"remaining", budget.Remaining())
			}
		}
	}

	// Step 8: Detect context insufficiency and generate questions
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
