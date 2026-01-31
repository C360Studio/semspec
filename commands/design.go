package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360/semspec/workflow"
	"github.com/c360/semstreams/agentic"
	agenticdispatch "github.com/c360/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// DesignCommand implements the /design command for creating technical designs.
type DesignCommand struct{}

// Config returns the command configuration.
func (c *DesignCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/design\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/design <change> - Create technical design for a change",
	}
}

// Execute runs the design command.
func (c *DesignCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	slug := ""
	if len(args) > 0 {
		slug = strings.TrimSpace(args[0])
	}

	if slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Usage: /design <change>",
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

	// Load the change
	change, err := manager.LoadChange(slug)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Change not found: %s", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Check if design already exists
	if change.Files.HasDesign {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Design already exists for %s. Edit `.semspec/changes/%s/design.md` directly.", slug, slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Generate and write the design template
	designContent := workflow.DesignTemplate(change.Title)
	if err := manager.WriteDesign(change.Slug, designContent); err != nil {
		cmdCtx.Logger.Error("Failed to write design",
			"error", err,
			"slug", change.Slug)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to write design: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Created design",
		"user_id", msg.UserID,
		"slug", change.Slug)

	// Build success response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ Created design for: **%s**\n\n", change.Title))
	sb.WriteString("File created:\n")
	sb.WriteString(fmt.Sprintf("- `.semspec/changes/%s/design.md`\n\n", change.Slug))
	sb.WriteString("The design template includes:\n")
	sb.WriteString("- Technical Approach\n")
	sb.WriteString("- Components Affected\n")
	sb.WriteString("- Data Flow\n")
	sb.WriteString("- Dependencies\n")
	sb.WriteString("- Alternatives Considered\n")
	sb.WriteString("- Security Considerations\n")
	sb.WriteString("- Performance Considerations\n\n")
	sb.WriteString("Next steps:\n")
	sb.WriteString("1. Edit `design.md` to describe the technical approach\n")
	sb.WriteString(fmt.Sprintf("2. Run `/spec %s` to create specification\n", change.Slug))

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeText,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}
