package strategies

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
	sourceVocab "github.com/c360studio/semspec/vocabulary/source"
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

	// Step 0: Include current plan content (revision context).
	// When the planner is revising after reviewer rejection, the current plan
	// is passed via PlanContent so the LLM can make targeted fixes instead of
	// regenerating from scratch. Uses budget-aware truncation (same pattern as
	// PlanReviewStrategy).
	if req.PlanContent != "" {
		s.addPlanContent(req, budget, result, estimator)
	}

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

	// Step 3: Architecture documentation (graph-first, filesystem fallback)
	hasArchDocs = s.addArchDocsFromGraph(ctx, req, budget, result, estimator)

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

// addPlanContent adds the current plan to context for revision requests (step 0).
func (s *PlanningStrategy) addPlanContent(req *ContextBuildRequest, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) {
	planTokens := estimator.Estimate(req.PlanContent)

	if budget.CanFit(planTokens) {
		if err := budget.Allocate("plan_content", planTokens); err == nil {
			result.Documents["__plan__"] = formatPlanContent(req.PlanSlug, req.PlanContent)
			s.logger.Info("Included current plan in revision context",
				"slug", req.PlanSlug,
				"tokens", planTokens)
		}
	} else {
		// Plan content is essential for revision — truncate if needed
		remaining := budget.Remaining()
		truncated, wasTruncated := estimator.TruncateToTokens(req.PlanContent, remaining)
		actualTokens := estimator.Estimate(truncated)

		if allocated := budget.TryAllocate("plan_content", actualTokens); allocated > 0 {
			result.Documents["__plan__"] = formatPlanContent(req.PlanSlug, truncated)
			result.Truncated = result.Truncated || wasTruncated
			s.logger.Info("Included truncated plan in revision context",
				"slug", req.PlanSlug,
				"tokens", actualTokens,
				"truncated", wasTruncated)
		}
	}
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

// addArchDocsFromGraph queries the knowledge graph for architecture documents
// and adds them to the result, budget-permitting. Falls back to filesystem reads
// when the graph is not ready (cold-start, unavailable).
//
// This is the graph-first approach: documents ingested via source-ingester are
// stored as graph entities with source.doc.* predicates. The strategy discovers
// them by querying for source.doc entities and filtering by scope (plan/all).
func (s *PlanningStrategy) addArchDocsFromGraph(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) bool {
	if !req.GraphReady {
		s.logger.Info("Graph not ready — falling back to filesystem architecture docs")
		return s.addArchDocsFromFilesystem(ctx, budget, result, estimator)
	}

	docCtx, docCancel := context.WithTimeout(ctx, 10*time.Second)
	defer docCancel()

	found, err := s.gatherers.Graph.QueryEntitiesByPredicate(docCtx, sourceVocab.DocCategory)
	if err != nil {
		s.logger.Warn("Failed to query architecture docs from graph, falling back to filesystem", "error", err)
		return s.addArchDocsFromFilesystem(ctx, budget, result, estimator)
	}

	if len(found) == 0 {
		s.logger.Info("No source docs in graph — falling back to filesystem architecture docs")
		return s.addArchDocsFromFilesystem(ctx, budget, result, estimator)
	}

	added := false
	for _, e := range found {
		if budget.Remaining() < MinTokensForDocs {
			break
		}

		if !isDocRelevantForPlanning(e) {
			continue
		}

		hydrateCtx, hydrateCancel := context.WithTimeout(ctx, 5*time.Second)
		content, err := s.gatherers.Graph.HydrateEntity(hydrateCtx, e.ID, 1)
		hydrateCancel()
		if err != nil {
			continue
		}

		tokens := estimator.Estimate(content)
		if budget.CanFit(tokens) {
			if err := budget.Allocate("arch:"+e.ID, tokens); err == nil {
				result.Documents["__arch__"+e.ID] = content
				result.Entities = append(result.Entities, EntityRef{
					ID: e.ID, Type: "architecture", Content: content, Tokens: tokens,
				})
				added = true
			}
		} else if budget.Remaining() > MinTokensForPartial {
			truncated, _ := estimator.TruncateToTokens(content, budget.Remaining())
			truncTokens := estimator.Estimate(truncated)
			if err := budget.Allocate("arch:"+e.ID, truncTokens); err == nil {
				result.Documents["__arch__"+e.ID] = truncated
				result.Entities = append(result.Entities, EntityRef{
					ID: e.ID, Type: "architecture", Content: truncated, Tokens: truncTokens,
				})
				result.Truncated = true
				added = true
			}
		}
	}

	if !added {
		s.logger.Info("No planning-relevant docs found in graph — falling back to filesystem")
		return s.addArchDocsFromFilesystem(ctx, budget, result, estimator)
	}

	return added
}

// isDocRelevantForPlanning checks if a graph entity is relevant for planning context.
// Documents with scope "plan" or "all" are included. Documents without a scope
// predicate are included by default (legacy documents not yet classified).
func isDocRelevantForPlanning(e gatherers.Entity) bool {
	for _, t := range e.Triples {
		if t.Predicate == sourceVocab.DocScope {
			scope, _ := t.Object.(string)
			return scope == string(sourceVocab.DocScopePlan) || scope == string(sourceVocab.DocScopeAll)
		}
	}
	// No scope predicate → include by default (unclassified documents)
	return true
}

// addArchDocsFromFilesystem reads architecture documentation from hardcoded
// filesystem paths. This is a fallback for when the knowledge graph is not ready
// (cold-start, pipeline not yet running). Prefer graph-based discovery via
// addArchDocsFromGraph when possible.
func (s *PlanningStrategy) addArchDocsFromFilesystem(ctx context.Context, budget *BudgetAllocation, result *StrategyResult, estimator *TokenEstimator) bool {
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
