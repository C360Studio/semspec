package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// SpecCommand implements the /spec command for creating specifications.
type SpecCommand struct{}

// Config returns the command configuration.
func (c *SpecCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/spec\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/spec <change> - Create specification for a change",
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
			Content:     "Usage: /spec <change>",
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

	// Check if spec already exists
	if change.Files.HasSpec {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Spec already exists for %s. Edit `.semspec/changes/%s/spec.md` directly.", slug, slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Check if proposal exists (recommended before spec)
	if !change.Files.HasProposal {
		cmdCtx.Logger.Warn("Creating spec without proposal",
			"slug", slug)
	}

	// Generate and write the spec template
	specContent := workflow.SpecTemplate(change.Title)
	if err := manager.WriteSpec(change.Slug, specContent); err != nil {
		cmdCtx.Logger.Error("Failed to write spec",
			"error", err,
			"slug", change.Slug)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to write spec: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Created spec",
		"user_id", msg.UserID,
		"slug", change.Slug)

	// Build success response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ Created specification for: **%s**\n\n", change.Title))
	sb.WriteString("File created:\n")
	sb.WriteString(fmt.Sprintf("- `.semspec/changes/%s/spec.md`\n\n", change.Slug))
	sb.WriteString("The specification template uses GIVEN/WHEN/THEN format:\n")
	sb.WriteString("```\n")
	sb.WriteString("### Requirement: (Name)\n")
	sb.WriteString("The system SHALL (describe requirement).\n\n")
	sb.WriteString("#### Scenario: (Name)\n")
	sb.WriteString("- GIVEN (initial context)\n")
	sb.WriteString("- WHEN (action occurs)\n")
	sb.WriteString("- THEN (expected outcome)\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Next steps:\n")
	sb.WriteString("1. Edit `spec.md` to define requirements and scenarios\n")
	sb.WriteString(fmt.Sprintf("2. Run `/tasks %s` to generate task list\n", change.Slug))
	sb.WriteString(fmt.Sprintf("3. Run `/check %s` to validate against constitution\n", change.Slug))

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
