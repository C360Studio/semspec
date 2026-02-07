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

// AskCommand implements the /ask command for asking questions.
type AskCommand struct{}

// Config returns the command configuration.
func (c *AskCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/ask\s+(\S+)\s+(.+)$`,
		Permission:  "write",
		RequireLoop: false,
		Help:        "/ask <topic> <question> - Ask a question routed by topic",
	}
}

// Execute runs the ask command.
func (c *AskCommand) Execute(
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
			Content:     "Usage: /ask <topic> <question>\n\nExample: /ask api.semstreams \"Does LoopInfo include workflow_slug?\"",
			Timestamp:   time.Now(),
		}, nil
	}

	topic := strings.TrimSpace(args[0])
	question := strings.Trim(strings.TrimSpace(args[1]), "\"'")

	// Create the question
	q := workflow.NewQuestion(
		"user", // FromAgent - could be enhanced to detect calling agent
		topic,
		question,
		"", // Context - could be enhanced with conversation context
	)

	// Store the question
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

	if err := store.Store(ctx, q); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to store question: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Publish to question subject for routing
	subject := fmt.Sprintf("question.ask.%s", topic)
	questionData, err := json.Marshal(q)
	if err != nil {
		cmdCtx.Logger.Warn("Failed to marshal question event",
			"question_id", q.ID,
			"error", err,
		)
	} else if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, questionData); err != nil {
		cmdCtx.Logger.Warn("Failed to publish question event",
			"question_id", q.ID,
			"topic", topic,
			"error", err,
		)
		// Don't fail - the question is stored, routing is optional
	}

	cmdCtx.Logger.Info("Question created",
		"question_id", q.ID,
		"topic", topic,
		"from", msg.UserID,
	)

	// Build response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Created question **%s**\n\n", q.ID))
	sb.WriteString(fmt.Sprintf("**Topic**: %s\n", topic))
	sb.WriteString(fmt.Sprintf("**Question**: %s\n", question))
	sb.WriteString(fmt.Sprintf("**Status**: pending\n\n"))
	sb.WriteString("Use `/questions` to view pending questions\n")
	sb.WriteString(fmt.Sprintf("Use `/answer %s <response>` to answer", q.ID))

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
