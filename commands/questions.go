package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// QuestionsCommand implements the /questions command for listing questions.
type QuestionsCommand struct{}

// Config returns the command configuration.
func (c *QuestionsCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/questions(?:\s+(.*))?$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/questions [status|id] - List questions or show specific question",
	}
}

// Execute runs the questions command.
func (c *QuestionsCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
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

	// Parse argument
	arg := ""
	if len(args) > 0 {
		arg = strings.TrimSpace(args[0])
	}

	// Check if it's a specific question ID
	if strings.HasPrefix(arg, "q-") {
		return c.showQuestion(ctx, store, arg, msg)
	}

	// Otherwise treat as status filter
	var status workflow.QuestionStatus
	switch arg {
	case "pending", "":
		status = workflow.QuestionStatusPending
	case "answered":
		status = workflow.QuestionStatusAnswered
	case "timeout":
		status = workflow.QuestionStatusTimeout
	case "all":
		status = "" // No filter
	default:
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Usage: /questions [pending|answered|timeout|all|<question-id>]",
			Timestamp:   time.Now(),
		}, nil
	}

	return c.listQuestions(ctx, store, status, msg)
}

// showQuestion displays details for a specific question.
func (c *QuestionsCommand) showQuestion(
	ctx context.Context,
	store *workflow.QuestionStore,
	id string,
	msg agentic.UserMessage,
) (agentic.UserResponse, error) {
	q, err := store.Get(ctx, id)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Question not found: %s", id),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Question %s\n\n", q.ID))
	sb.WriteString(fmt.Sprintf("**Status**: %s\n", q.Status))
	sb.WriteString(fmt.Sprintf("**Topic**: %s\n", q.Topic))
	sb.WriteString(fmt.Sprintf("**From**: %s\n", q.FromAgent))
	sb.WriteString(fmt.Sprintf("**Created**: %s\n", q.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Urgency**: %s\n\n", q.Urgency))

	sb.WriteString("## Question\n\n")
	sb.WriteString(q.Question)
	sb.WriteString("\n\n")

	if q.Context != "" {
		sb.WriteString("## Context\n\n")
		sb.WriteString(q.Context)
		sb.WriteString("\n\n")
	}

	if q.Status == workflow.QuestionStatusAnswered {
		sb.WriteString("## Answer\n\n")
		sb.WriteString(q.Answer)
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("**Answered by**: %s (%s)\n", q.AnsweredBy, q.AnswererType))
		if q.AnsweredAt != nil {
			sb.WriteString(fmt.Sprintf("**Answered at**: %s\n", q.AnsweredAt.Format(time.RFC3339)))
		}
		if q.Confidence != "" {
			sb.WriteString(fmt.Sprintf("**Confidence**: %s\n", q.Confidence))
		}
		if q.Sources != "" {
			sb.WriteString(fmt.Sprintf("**Sources**: %s\n", q.Sources))
		}
	} else {
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("Use `/answer %s <response>` to answer this question\n", q.ID))
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

// listQuestions displays a table of questions.
func (c *QuestionsCommand) listQuestions(
	ctx context.Context,
	store *workflow.QuestionStore,
	status workflow.QuestionStatus,
	msg agentic.UserMessage,
) (agentic.UserResponse, error) {
	questions, err := store.List(ctx, status)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to list questions: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	if len(questions) == 0 {
		statusText := "pending"
		if status != "" {
			statusText = string(status)
		}
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     fmt.Sprintf("No %s questions found.\n\nUse `/ask <topic> <question>` to create a question.", statusText),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	statusText := "Pending"
	if status == workflow.QuestionStatusAnswered {
		statusText = "Answered"
	} else if status == workflow.QuestionStatusTimeout {
		statusText = "Timed Out"
	} else if status == "" {
		statusText = "All"
	}
	sb.WriteString(fmt.Sprintf("# %s Questions (%d)\n\n", statusText, len(questions)))

	sb.WriteString("| ID | Topic | Status | Question |\n")
	sb.WriteString("|-----|-------|--------|----------|\n")

	for _, q := range questions {
		// Truncate question for table display
		questionText := q.Question
		if len(questionText) > 40 {
			questionText = questionText[:37] + "..."
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			q.ID, q.Topic, q.Status, questionText))
	}

	sb.WriteString("\n---\n")
	sb.WriteString("Use `/questions <id>` to view details\n")
	if status == workflow.QuestionStatusPending || status == "" {
		sb.WriteString("Use `/answer <id> <response>` to answer\n")
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
