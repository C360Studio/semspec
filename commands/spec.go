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

// SpecCommand implements the /spec command for spec-driven development.
type SpecCommand struct{}

// Config returns the command configuration.
func (c *SpecCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/spec\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/spec [name] - Run spec-driven development workflow",
	}
}

// Execute runs the spec command.
func (c *SpecCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	specName := ""
	if len(args) > 0 {
		specName = strings.TrimSpace(args[0])
	}

	cmdCtx.Logger.Info("Starting spec workflow",
		"user_id", msg.UserID,
		"spec_name", specName)

	content := "Starting spec-driven development workflow"
	if specName != "" {
		content = fmt.Sprintf("Starting spec workflow: %s", specName)
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeStatus,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}
