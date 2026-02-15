// Package strategies provides context building strategies for different task types.
package strategies

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/c360studio/semspec/processor/context-builder/gatherers"
)

// Token budget thresholds for context allocation decisions.
const (
	// MinTokensForTests is the minimum remaining budget to include test files.
	MinTokensForTests = 1000

	// MinTokensForConventions is the minimum remaining budget to include convention docs.
	MinTokensForConventions = 500

	// MinTokensForDocs is the minimum remaining budget to include documentation.
	MinTokensForDocs = 300

	// MinTokensForPartial is the minimum remaining budget to include partial file content.
	MinTokensForPartial = 200

	// MinTokensForPatterns is the minimum remaining budget to include code patterns.
	MinTokensForPatterns = 1000

	// MaxRelatedPatterns limits the number of related patterns to include.
	MaxRelatedPatterns = 10

	// MaxMatchingEntities limits the number of entities matching a topic.
	MaxMatchingEntities = 15

	// MaxEntitiesPerType limits entities per type when searching.
	MaxEntitiesPerType = 3
)

// TaskType represents the type of context building task.
// Note: This is intentionally duplicated from the main package's types.go
// to avoid import cycles. The main package converts between these types.
type TaskType string

const (
	// TaskTypeReview builds context for code review tasks.
	TaskTypeReview TaskType = "review"

	// TaskTypeImplementation builds context for implementation tasks.
	TaskTypeImplementation TaskType = "implementation"

	// TaskTypeExploration builds context for exploration tasks.
	TaskTypeExploration TaskType = "exploration"

	// TaskTypePlanReview builds context for plan review/approval tasks.
	TaskTypePlanReview TaskType = "plan-review"

	// TaskTypePlanning builds context for plan generation tasks.
	TaskTypePlanning TaskType = "planning"

	// TaskTypeQuestion builds context for question-answering tasks.
	TaskTypeQuestion TaskType = "question"
)

// IsValid returns true if the task type is recognized.
func (t TaskType) IsValid() bool {
	switch t {
	case TaskTypeReview, TaskTypeImplementation, TaskTypeExploration, TaskTypePlanReview, TaskTypePlanning, TaskTypeQuestion:
		return true
	}
	return false
}

// ContextBuildRequest is the input for context building.
// Note: This is a simplified internal type. The main package has a
// corresponding type with message.Payload interface implementation.
type ContextBuildRequest struct {
	RequestID     string   `json:"request_id"`
	TaskType      TaskType `json:"task_type"`
	WorkflowID    string   `json:"workflow_id,omitempty"`
	Files         []string `json:"files,omitempty"`
	GitRef        string   `json:"git_ref,omitempty"`
	Topic         string   `json:"topic,omitempty"`
	SpecEntityID  string   `json:"spec_entity_id,omitempty"`
	PlanSlug      string   `json:"plan_slug,omitempty"`
	PlanContent   string   `json:"plan_content,omitempty"`
	ScopePatterns []string `json:"scope_patterns,omitempty"`
	Capability    string   `json:"capability,omitempty"`
	Model         string   `json:"model,omitempty"`
	TokenBudget   int      `json:"token_budget,omitempty"`
}

// EntityRef is a reference to a graph entity in the context.
type EntityRef struct {
	ID      string `json:"id"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content,omitempty"`
	Tokens  int    `json:"tokens,omitempty"`
}

// BudgetAllocation manages token budget allocation across context items.
type BudgetAllocation struct {
	Total     int
	Allocated int
	Items     map[string]int
	Order     []string
}

// NewBudgetAllocation creates a new budget allocation with the given total budget.
func NewBudgetAllocation(total int) *BudgetAllocation {
	return &BudgetAllocation{
		Total: total,
		Items: make(map[string]int),
		Order: make([]string, 0),
	}
}

// Allocate reserves tokens for a named item.
func (b *BudgetAllocation) Allocate(name string, tokens int) error {
	if tokens <= 0 {
		return nil
	}

	if b.Allocated+tokens > b.Total {
		return fmt.Errorf("allocation of %d tokens for %q exceeds budget (used: %d, total: %d)",
			tokens, name, b.Allocated, b.Total)
	}

	if _, exists := b.Items[name]; exists {
		b.Allocated -= b.Items[name]
	} else {
		b.Order = append(b.Order, name)
	}

	b.Items[name] = tokens
	b.Allocated += tokens
	return nil
}

// TryAllocate attempts to allocate tokens, returning the actual amount allocated.
func (b *BudgetAllocation) TryAllocate(name string, tokens int) int {
	if tokens <= 0 {
		return 0
	}

	available := b.Remaining()
	if available <= 0 {
		return 0
	}

	actual := tokens
	if actual > available {
		actual = available
	}

	if _, exists := b.Items[name]; !exists {
		b.Order = append(b.Order, name)
	} else {
		b.Allocated -= b.Items[name]
	}

	b.Items[name] = actual
	b.Allocated += actual
	return actual
}

// Remaining returns the number of tokens still available.
func (b *BudgetAllocation) Remaining() int {
	return b.Total - b.Allocated
}

// CanFit returns true if the given number of tokens can fit.
func (b *BudgetAllocation) CanFit(tokens int) bool {
	return b.Remaining() >= tokens
}

// Summary returns a human-readable summary of allocations.
func (b *BudgetAllocation) Summary() string {
	var sb strings.Builder
	usage := float64(b.Allocated) / float64(b.Total) * 100
	sb.WriteString(fmt.Sprintf("Budget: %d/%d tokens (%.1f%% used)\n", b.Allocated, b.Total, usage))

	type item struct {
		name   string
		tokens int
	}
	items := make([]item, 0, len(b.Items))
	for name, tokens := range b.Items {
		items = append(items, item{name, tokens})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].tokens > items[j].tokens
	})

	for _, it := range items {
		pct := float64(it.tokens) / float64(b.Total) * 100
		sb.WriteString(fmt.Sprintf("  %s: %d tokens (%.1f%%)\n", it.name, it.tokens, pct))
	}

	return sb.String()
}

// TokenEstimator estimates token counts for content.
// Note: This uses a simple character-based estimation (roughly 4 chars per token
// for English text). For production use with specific models, consider using
// tiktoken or model-specific tokenizers for more accurate estimates.
type TokenEstimator struct {
	charsPerToken float64
}

// NewTokenEstimator creates a new token estimator.
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{
		charsPerToken: 4.0,
	}
}

// Estimate returns an estimated token count for the given content.
func (e *TokenEstimator) Estimate(content string) int {
	if content == "" {
		return 0
	}
	return int(float64(len(content)) / e.charsPerToken)
}

// TruncateToTokens truncates content to approximately fit within a token budget.
func (e *TokenEstimator) TruncateToTokens(content string, maxTokens int) (string, bool) {
	if maxTokens <= 0 {
		return "", true
	}

	estimated := e.Estimate(content)
	if estimated <= maxTokens {
		return content, false
	}

	maxChars := int(float64(maxTokens) * e.charsPerToken)
	if maxChars >= len(content) {
		return content, false
	}

	truncated := content[:maxChars-20]

	if idx := strings.LastIndex(truncated, "\n"); idx > maxChars/2 {
		truncated = truncated[:idx]
	} else if idx := strings.LastIndex(truncated, " "); idx > maxChars/2 {
		truncated = truncated[:idx]
	}

	return truncated + "\n...[truncated]", true
}

// Strategy is the interface for context building strategies.
type Strategy interface {
	Build(ctx context.Context, req *ContextBuildRequest, budget *BudgetAllocation) (*StrategyResult, error)
}

// StrategyResult contains the result of a strategy execution.
type StrategyResult struct {
	Entities  []EntityRef
	Documents map[string]string
	Diffs     string
	SOPIDs    []string
	Truncated bool
	Error     string

	// Domains contains inferred semantic domains for the task.
	// Used by reviewers to understand what areas of the codebase are affected.
	Domains []string

	// Questions contains detected knowledge gaps that need resolution.
	// When InsufficientContext is true, these questions should be asked
	// before proceeding with context building.
	Questions []Question

	// InsufficientContext indicates the strategy detected critical gaps
	// that prevent effective context building without additional input.
	InsufficientContext bool
}

// QuestionUrgency indicates how urgent a question is.
type QuestionUrgency string

const (
	// UrgencyLow indicates a non-critical question that can wait.
	UrgencyLow QuestionUrgency = "low"

	// UrgencyNormal indicates a standard priority question.
	UrgencyNormal QuestionUrgency = "normal"

	// UrgencyHigh indicates an important question that should be prioritized.
	UrgencyHigh QuestionUrgency = "high"

	// UrgencyBlocking indicates the question must be answered before proceeding.
	UrgencyBlocking QuestionUrgency = "blocking"
)

// Question represents a knowledge gap detected during context building.
// Questions are routed to answerers (agents, humans, teams, tools) based on topic.
type Question struct {
	// Topic is hierarchical (e.g., "architecture.scope", "requirements.clarification")
	// Used for routing to the appropriate answerer via configs/answerers.yaml
	Topic string

	// Question is the actual question text to ask
	Question string

	// Context provides background information to help answer the question
	Context string

	// Urgency indicates how urgent the question is
	Urgency QuestionUrgency
}

// Gatherers holds all gatherer instances for strategies.
type Gatherers struct {
	Graph *gatherers.GraphGatherer
	Git   *gatherers.GitGatherer
	File  *gatherers.FileGatherer
	SOP   *gatherers.SOPGatherer
}

// NewGatherers creates a new Gatherers instance.
func NewGatherers(graphGatewayURL, repoPath, sopEntityPrefix string) *Gatherers {
	graph := gatherers.NewGraphGatherer(graphGatewayURL)
	return &Gatherers{
		Graph: graph,
		Git:   gatherers.NewGitGatherer(repoPath),
		File:  gatherers.NewFileGatherer(repoPath),
		SOP:   gatherers.NewSOPGatherer(graph, sopEntityPrefix),
	}
}

// StrategyFactory creates strategies for different task types.
type StrategyFactory struct {
	gatherers *Gatherers
	logger    *slog.Logger
}

// NewStrategyFactory creates a new strategy factory.
func NewStrategyFactory(gatherers *Gatherers, logger *slog.Logger) *StrategyFactory {
	if logger == nil {
		logger = slog.Default()
	}
	return &StrategyFactory{
		gatherers: gatherers,
		logger:    logger,
	}
}

// Create returns the appropriate strategy for a task type.
func (f *StrategyFactory) Create(taskType TaskType) Strategy {
	switch taskType {
	case TaskTypeReview:
		return NewReviewStrategy(f.gatherers, f.logger)
	case TaskTypeImplementation:
		return NewImplementationStrategy(f.gatherers, f.logger)
	case TaskTypeExploration:
		return NewExplorationStrategy(f.gatherers, f.logger)
	case TaskTypePlanReview:
		return NewPlanReviewStrategy(f.gatherers, f.logger)
	case TaskTypePlanning:
		return NewPlanningStrategy(f.gatherers, f.logger)
	case TaskTypeQuestion:
		return NewQuestionStrategy(f.gatherers, f.logger)
	default:
		return NewExplorationStrategy(f.gatherers, f.logger)
	}
}
