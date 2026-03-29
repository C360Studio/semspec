package question

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
)

const answerToolName = "answer_question"

// AnswerExecutor implements agentic.ToolExecutor for the answer_question tool.
// This is a terminal tool (StopLoop=true) used by answerer agents to write
// their answer directly to QUESTIONS KV and signal loop completion.
type AnswerExecutor struct {
	questionStore *workflow.QuestionStore
	logger        *slog.Logger
}

// NewAnswerExecutor constructs an answer_question executor.
func NewAnswerExecutor(store *workflow.QuestionStore, logger *slog.Logger) *AnswerExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	return &AnswerExecutor{
		questionStore: store,
		logger:        logger,
	}
}

// Execute writes the answer to QUESTIONS KV and returns StopLoop=true.
func (e *AnswerExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	questionID := stringArg(call.Arguments, "question_id")
	if questionID == "" {
		return errorResult(call, `missing required argument "question_id"`), nil
	}
	answer := stringArg(call.Arguments, "answer")
	if answer == "" {
		return errorResult(call, `missing required argument "answer"`), nil
	}

	if e.questionStore == nil {
		return agentic.ToolResult{
			CallID:   call.ID,
			Content:  answer,
			StopLoop: true,
		}, nil
	}

	// Load the question, update with answer, and persist.
	q, err := e.questionStore.Get(ctx, questionID)
	if err != nil {
		e.logger.Warn("Question not found in KV, storing answer anyway",
			"question_id", questionID, "error", err)
		// Create a minimal question entry so the answer is persisted.
		q = &workflow.Question{
			ID:        questionID,
			Status:    workflow.QuestionStatusPending,
			CreatedAt: time.Now(),
		}
	}

	now := time.Now()
	q.Status = workflow.QuestionStatusAnswered
	q.Answer = answer
	q.AnsweredBy = fmt.Sprintf("agent/%s", call.LoopID)
	q.AnswererType = "agent"
	q.AnsweredAt = &now

	if err := e.questionStore.Store(ctx, q); err != nil {
		e.logger.Error("Failed to store answer in QUESTIONS KV",
			"question_id", questionID, "error", err)
		// Return the answer anyway — the agent produced it, don't lose it.
	}

	e.logger.Info("Question answered via answer_question tool",
		"question_id", questionID,
		"answered_by", q.AnsweredBy)

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  answer,
		StopLoop: true, // Terminal — ends the answerer agent loop
	}, nil
}

// ListTools returns the tool definition for answer_question.
func (e *AnswerExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        answerToolName,
		Description: "Submit your answer to a question. This ends your agent loop and delivers the answer to the waiting agent. You MUST call this tool when you have the answer.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"question_id", "answer"},
			"properties": map[string]any{
				"question_id": map[string]any{
					"type":        "string",
					"description": "The question ID from your task prompt",
				},
				"answer": map[string]any{
					"type":        "string",
					"description": "Your answer to the question",
				},
			},
		},
	}}
}
