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

// ProposeCommand implements the /propose command for creating proposals.
type ProposeCommand struct{}

// Config returns the command configuration.
func (c *ProposeCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/propose\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/propose <description> - Create new proposal",
	}
}

// Execute runs the propose command.
func (c *ProposeCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	description := ""
	if len(args) > 0 {
		description = strings.TrimSpace(args[0])
	}

	if description == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Usage: /propose <description>",
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Creating proposal",
		"user_id", msg.UserID,
		"description", description)

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeStatus,
		Content:     fmt.Sprintf("Creating proposal: %s", description),
		Timestamp:   time.Now(),
	}, nil
}
