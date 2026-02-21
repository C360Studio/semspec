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

	var hasArchDocs, hasExistingSpecs, hasCodePatterns bool

	// Step 1: Project file tree (critical — prevents scope hallucination)
	s.addFileTree(ctx, budget, result, estimator)

	// Step 2: Codebase summary from graph (best-effort, with timeout guard)
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
	hasArchDocs = s.addArchDocs(ctx, budget, result, estimator)

	// Step 4: Existing specs and plans (graph queries, timeout-guarded)
	if req.GraphReady && budget.Remaining() > MinTokensForPatterns {
		hasExistingSpecs = s.addExistingSpecs(ctx, req, budget, result, estimator)
	} else if !req.GraphReady {
		s.logger.Info("Skipping graph existing specs (graph not ready)")
	}

	// Step 5: Relevant code patterns (graph queries, timeout-guarded)
	if req.GraphReady && req.Topic != "" && budget.Remaining() > MinTokensForPatterns {
		hasCodePatterns = s.addCodePatterns(ctx, req, budget, result, estimator)
	}

	// Step 6: Requested files (filesystem — fast)
	if len(req.Files) > 0 && budget.Remaining() > MinTokensForConventions {
		docs, tokens, truncated, err := s.gatherers.File.ReadFilesPartial(ctx, req.Files, budget.Remaining())
		if err != nil {
			s.logger.Warn("Failed to read requested files", "error", err)
		} else if len(docs) > 0 {
			if allocated := budget.TryAllocate("requested_files", tokens); allocated > 0 {
				for path, docContent := range docs {
					result.Documents[path] = docContent
				}
				result.Truncated = result.Truncated || truncated
			}
		}
	}

	// NOTE: SOP rules are NOT loaded here from the graph.
	// Standards.json (populated by source-ingester from SOP requirements) is
	// injected as a preamble by Builder.loadStandardsPreamble() after all
	// strategies complete. This avoids re-reading raw SOP content from the
	// graph on every context build.

	// Step 7: Detect context insufficiency and generate questions
	s.detectInsufficientContext(result, req, hasArchDocs, hasExistingSpecs, hasCodePatterns)

	return result, nil
}

// addFileTree adds the project file tree to the result (step 1).
func (s *PlanningStrategy) addFileTree(ctx context.Context, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) {
	files, err := s.gatherers.File.ListFilesRecursive(ctx)
	if err != nil {
		s.logger.Warn("Failed to list project files", "error", err)
		return
	}
	if len(files) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("# Project File Tree\n\n")
	sb.WriteString("These are the actual files in the project. Use ONLY these paths in scope.\n\n")
	for _, f := range files {
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	tree := sb.String()
	tokens := estimator.Estimate(tree)
	if tokens > 500 {
		tree, _ = estimator.TruncateToTokens(tree, 500)
		tokens = 500
	}
	if budget.CanFit(tokens) {
		if err := budget.Allocate("file_tree", tokens); err == nil {
			result.Documents["__file_tree__"] = tree
			s.logger.Info("Included project file tree", "files", len(files), "tokens", tokens)
		}
	}
}

// addArchDocs adds architecture documentation files to the result (step 3).
// Returns true if any docs were added.
func (s *PlanningStrategy) addArchDocs(ctx context.Context, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) bool {
	archDocs := []string{
		"docs/03-architecture.md", "docs/how-it-works.md",
		"CLAUDE.md", "docs/components.md", "docs/getting-started.md",
	}
	added := false
	for _, df := range archDocs {
		if budget.Remaining() < MinTokensForDocs {
			break
		}
		if !s.gatherers.File.FileExists(df) {
			continue
		}
		file, err := s.gatherers.File.ReadFile(ctx, df)
		if err != nil {
			continue
		}
		if budget.CanFit(file.Tokens) {
			if err := budget.Allocate("arch:"+df, file.Tokens); err == nil {
				result.Documents[df] = file.Content
				added = true
			}
		} else if budget.Remaining() > MinTokensForPartial {
			truncated, _ := estimator.TruncateToTokens(file.Content, budget.Remaining())
			tokens := estimator.Estimate(truncated)
			if err := budget.Allocate("arch:"+df, tokens); err == nil {
				result.Documents[df] = truncated
				result.Truncated = true
				added = true
			}
		}
	}
	return added
}

// addExistingSpecs adds hydrated spec/plan entities to the result (step 4).
// Returns true if any specs were added.
func (s *PlanningStrategy) addExistingSpecs(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) bool {
	specCtx, specCancel := context.WithTimeout(ctx, 10*time.Second)
	existingSpecs, err := s.findExistingSpecs(specCtx, req.Topic)
	specCancel()
	if err != nil {
		s.logger.Warn("Failed to find existing specs", "error", err)
		return false
	}
	added := false
	for _, entity := range existingSpecs {
		if budget.Remaining() < MinTokensForPartial {
			break
		}
		hydrateCtx, hydrateCancel := context.WithTimeout(ctx, 5*time.Second)
		ec, err := s.gatherers.Graph.HydrateEntity(hydrateCtx, entity.ID, 1)
		hydrateCancel()
		if err != nil {
			continue
		}
		tokens := estimator.Estimate(ec)
		if budget.CanFit(tokens) {
			if err := budget.Allocate("spec:"+entity.ID, tokens); err == nil {
				result.Documents["__spec__"+entity.ID] = ec
				result.Entities = append(result.Entities, EntityRef{ID: entity.ID, Type: entity.Type, Content: ec, Tokens: tokens})
				added = true
			}
		}
	}
	return added
}

// addCodePatterns adds hydrated code pattern entities to the result (step 5).
// Returns true if any patterns were added.
func (s *PlanningStrategy) addCodePatterns(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) bool {
	patternCtx, patternCancel := context.WithTimeout(ctx, 10*time.Second)
	patterns, err := s.findRelevantPatterns(patternCtx, req.Topic)
	patternCancel()
	if err != nil {
		s.logger.Warn("Failed to find relevant patterns", "error", err)
		return false
	}
	added := false
	for _, entity := range patterns {
		if budget.Remaining() < MinTokensForPartial {
			break
		}
		hydrateCtx, hydrateCancel := context.WithTimeout(ctx, 5*time.Second)
		ec, err := s.gatherers.Graph.HydrateEntity(hydrateCtx, entity.ID, 1)
		hydrateCancel()
		if err != nil {
			continue
		}
		tokens := estimator.Estimate(ec)
		if budget.CanFit(tokens) {
			if err := budget.Allocate("pattern:"+entity.ID, tokens); err == nil {
				result.Documents["__pattern__"+entity.ID] = ec
				result.Entities = append(result.Entities, EntityRef{ID: entity.ID, Type: entity.Type, Content: ec, Tokens: tokens})
				added = true
			}
		}
	}
	return added
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
