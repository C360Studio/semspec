package gap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
)

// Handler processes detected gaps and creates questions.
type Handler struct {
	nc     *natsclient.Client
	store  *workflow.QuestionStore
	parser *Parser
	router *answerer.Router
	logger *slog.Logger
}

// NewHandler creates a new gap handler.
func NewHandler(nc *natsclient.Client) (*Handler, error) {
	store, err := workflow.NewQuestionStore(nc)
	if err != nil {
		return nil, fmt.Errorf("create question store: %w", err)
	}

	return &Handler{
		nc:     nc,
		store:  store,
		parser: NewParser(),
		logger: slog.Default(),
	}, nil
}

// SetRouter sets the router for question routing.
func (h *Handler) SetRouter(router *answerer.Router) {
	h.router = router
}

// SetLogger sets the logger for the handler.
func (h *Handler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// ProcessResult contains the result of processing content for gaps.
type ProcessResult struct {
	// CleanedContent is the content with gap blocks removed
	CleanedContent string

	// CreatedQuestions are the question IDs created from gaps
	CreatedQuestions []string

	// BlockingQuestions are questions with urgency=blocking
	BlockingQuestions []string

	// ShouldBlock indicates whether the workflow should pause
	ShouldBlock bool
}

// ProcessContent parses content for gaps and creates questions.
func (h *Handler) ProcessContent(ctx context.Context, content string, loopID string, fromAgent string) (*ProcessResult, error) {
	parseResult := h.parser.Parse(content)

	result := &ProcessResult{
		CleanedContent:    parseResult.CleanedOutput,
		CreatedQuestions:  []string{},
		BlockingQuestions: []string{},
		ShouldBlock:       false,
	}

	if !parseResult.HasGaps {
		return result, nil
	}

	// Create questions from gaps
	for _, gap := range parseResult.Gaps {
		questionID := fmt.Sprintf("q-%s", uuid.New().String()[:8])

		q := &workflow.Question{
			ID:            questionID,
			FromAgent:     fromAgent,
			Topic:         gap.Topic,
			Question:      gap.Question,
			Context:       gap.Context,
			Status:        workflow.QuestionStatusPending,
			Urgency:       workflow.QuestionUrgency(gap.Urgency),
			CreatedAt:     time.Now(),
			BlockedLoopID: loopID,
		}

		if err := h.store.Store(ctx, q); err != nil {
			return nil, fmt.Errorf("store question %s: %w", questionID, err)
		}

		// Route the question to the appropriate answerer
		if h.router != nil {
			routeResult, err := h.router.RouteQuestion(ctx, q)
			if err != nil {
				h.logger.Warn("Failed to route question",
					"question_id", questionID,
					"topic", gap.Topic,
					"error", err)
				// Don't fail - question is stored, routing is optional
			} else {
				h.logger.Info("Question routed",
					"question_id", questionID,
					"topic", gap.Topic,
					"answerer", routeResult.Route.Answerer,
					"message", routeResult.Message)

				// Update question with assignment info
				if q.AssignedTo != "" {
					if err := h.store.Store(ctx, q); err != nil {
						h.logger.Warn("Failed to update question assignment",
							"question_id", questionID,
							"error", err)
					}
				}
			}
		}

		result.CreatedQuestions = append(result.CreatedQuestions, questionID)

		// Track blocking questions
		if gap.Urgency == "blocking" || gap.Urgency == "high" {
			result.BlockingQuestions = append(result.BlockingQuestions, questionID)
		}
	}

	// Should block if there are any high or blocking urgency gaps
	result.ShouldBlock = len(result.BlockingQuestions) > 0

	return result, nil
}

// DetectOnly parses content for gaps without creating questions.
// Useful for validation or preview.
func (h *Handler) DetectOnly(content string) *ParseResult {
	return h.parser.Parse(content)
}

// HasBlockingGaps checks if content contains any blocking urgency gaps.
func (h *Handler) HasBlockingGaps(content string) bool {
	result := h.parser.Parse(content)
	if !result.HasGaps {
		return false
	}

	for _, gap := range result.Gaps {
		if gap.Urgency == "blocking" || gap.Urgency == "high" {
			return true
		}
	}
	return false
}

// GapSummary provides a summary of detected gaps.
type GapSummary struct {
	TotalGaps    int      `json:"total_gaps"`
	BlockingGaps int      `json:"blocking_gaps"`
	Topics       []string `json:"topics"`
}

// Summarize returns a summary of gaps in the content.
func (h *Handler) Summarize(content string) *GapSummary {
	result := h.parser.Parse(content)

	summary := &GapSummary{
		TotalGaps:    len(result.Gaps),
		BlockingGaps: 0,
		Topics:       []string{},
	}

	seen := make(map[string]bool)
	for _, gap := range result.Gaps {
		if gap.Urgency == "blocking" || gap.Urgency == "high" {
			summary.BlockingGaps++
		}
		if gap.Topic != "" && !seen[gap.Topic] {
			summary.Topics = append(summary.Topics, gap.Topic)
			seen[gap.Topic] = true
		}
	}

	return summary
}
