package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c360/semstreams/agentic"
	agenticdispatch "github.com/c360/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// TasksCommand implements the /tasks command for listing tasks.
type TasksCommand struct{}

// Config returns the command configuration.
func (c *TasksCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/tasks(?:\s+(\w+))?$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/tasks [status] - List tasks (optional status filter)",
	}
}

// Execute runs the tasks command.
func (c *TasksCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	statusFilter := ""
	if len(args) > 0 {
		statusFilter = strings.TrimSpace(args[0])
	}

	cmdCtx.Logger.Info("Listing tasks",
		"user_id", msg.UserID,
		"status_filter", statusFilter)

	content := "Tasks:"
	if statusFilter != "" {
		content = fmt.Sprintf("Tasks (status: %s):", statusFilter)
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeText,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}
