package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// AnswerCommand implements the /answer command for answering questions.
type AnswerCommand struct{}

// Config returns the command configuration.
func (c *AnswerCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/answer\s+(\S+)\s+(.+)$`,
		Permission:  "write",
		RequireLoop: false,
		Help:        "/answer <question-id> <response> - Answer a pending question",
	}
}

// Execute runs the answer command.
func (c *AnswerCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	if len(args) < 2 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Usage: /answer <question-id> <response>\n\nExample: /answer q-abc123 \"Yes, the field was added\"",
			Timestamp:   time.Now(),
		}, nil
	}

	questionID := strings.TrimSpace(args[0])
	answer := strings.Trim(strings.TrimSpace(args[1]), "\"'")

	// Validate question ID format
	if !strings.HasPrefix(questionID, "q-") {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Invalid question ID: %s (should start with 'q-')", questionID),
			Timestamp:   time.Now(),
		}, nil
	}

	store, err := workflow.NewQuestionStore(cmdCtx.NATSClient)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to initialize question store: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Get the question to verify it exists and is pending
	q, err := store.Get(ctx, questionID)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Question not found: %s", questionID),
			Timestamp:   time.Now(),
		}, nil
	}

	if q.Status != workflow.QuestionStatusPending {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Question %s is already %s", questionID, q.Status),
			Timestamp:   time.Now(),
		}, nil
	}

	// Answer the question
	answeredBy := msg.UserID
	if answeredBy == "" {
		answeredBy = "unknown"
	}

	if err := store.Answer(ctx, questionID, answer, answeredBy, "human", "", ""); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to store answer: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Publish answer event for any waiting workflows
	subject := fmt.Sprintf("question.answer.%s", questionID)
	answerPayload := map[string]interface{}{
		"question_id":   questionID,
		"answer":        answer,
		"answered_by":   answeredBy,
		"answerer_type": "human",
	}
	answerData, err := json.Marshal(answerPayload)
	if err != nil {
		cmdCtx.Logger.Warn("Failed to marshal answer event",
			"question_id", questionID,
			"error", err,
		)
	} else if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, answerData); err != nil {
		cmdCtx.Logger.Warn("Failed to publish answer event",
			"question_id", questionID,
			"error", err,
		)
		// Don't fail - the answer is stored, routing is optional
	}

	cmdCtx.Logger.Info("Question answered",
		"question_id", questionID,
		"answered_by", answeredBy,
	)

	// Build response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Answered question **%s**\n\n", questionID))
	sb.WriteString(fmt.Sprintf("**Original question**: %s\n\n", q.Question))
	sb.WriteString(fmt.Sprintf("**Your answer**: %s\n\n", answer))

	// Note if there was a blocked loop
	if q.BlockedLoopID != "" {
		sb.WriteString(fmt.Sprintf("Loop %s may resume with this answer.\n", q.BlockedLoopID))
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}
