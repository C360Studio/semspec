package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/graph"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
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

	// Get repo root from environment or current directory
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to get working directory: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	manager := workflow.NewManager(repoRoot)

	// Create the change
	change, err := manager.CreateChange(description, msg.UserID)
	if err != nil {
		cmdCtx.Logger.Error("Failed to create change",
			"error", err,
			"description", description)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to create change: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Generate and write the proposal template
	proposalContent := workflow.ProposalTemplate(change.Title, description)
	if err := manager.WriteProposal(change.Slug, proposalContent); err != nil {
		cmdCtx.Logger.Error("Failed to write proposal",
			"error", err,
			"slug", change.Slug)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to write proposal: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Created proposal",
		"user_id", msg.UserID,
		"slug", change.Slug,
		"description", description)

	// Publish to knowledge graph (best effort - don't fail if graph unavailable)
	if err := graph.PublishProposal(ctx, cmdCtx.NATSClient, change); err != nil {
		cmdCtx.Logger.Warn("Failed to publish proposal to graph",
			"error", err,
			"slug", change.Slug)
	}

	// Build success response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ Created proposal: **%s**\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("Change slug: `%s`\n", change.Slug))
	sb.WriteString(fmt.Sprintf("Status: %s\n\n", change.Status))
	sb.WriteString("Files created:\n")
	sb.WriteString(fmt.Sprintf("- `.semspec/changes/%s/proposal.md`\n", change.Slug))
	sb.WriteString(fmt.Sprintf("- `.semspec/changes/%s/metadata.json`\n\n", change.Slug))
	sb.WriteString("Next steps:\n")
	sb.WriteString("1. Edit `proposal.md` to describe Why, What Changes, and Impact\n")
	sb.WriteString(fmt.Sprintf("2. Run `/design %s` to create technical design\n", change.Slug))
	sb.WriteString(fmt.Sprintf("3. Run `/spec %s` to create specification\n", change.Slug))
	sb.WriteString(fmt.Sprintf("4. Run `/check %s` to validate against constitution\n", change.Slug))

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
