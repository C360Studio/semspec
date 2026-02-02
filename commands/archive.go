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

// ArchiveCommand implements the /archive command for archiving completed changes.
type ArchiveCommand struct{}

// Config returns the command configuration.
func (c *ArchiveCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/archive\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/archive <change> - Archive completed change",
	}
}

// Execute runs the archive command.
func (c *ArchiveCommand) Execute(
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
			Content:     "Usage: /archive <change>",
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

	// Load the change to get its details before archiving
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

	// Check if change is in a state that can be archived
	// We need to transition through implementing -> complete -> archived
	if change.Status != workflow.StatusComplete {
		if change.Status == workflow.StatusApproved {
			// Transition to implementing first
			change.Status = workflow.StatusImplementing
			change.UpdatedAt = time.Now()
			if err := manager.SaveChangeMetadata(change); err != nil {
				return agentic.UserResponse{
					ResponseID:  uuid.New().String(),
					ChannelType: msg.ChannelType,
					ChannelID:   msg.ChannelID,
					UserID:      msg.UserID,
					Type:        agentic.ResponseTypeError,
					Content:     fmt.Sprintf("Failed to update status: %v", err),
					Timestamp:   time.Now(),
				}, nil
			}
		}

		if change.Status == workflow.StatusImplementing {
			// Transition to complete
			change.Status = workflow.StatusComplete
			change.UpdatedAt = time.Now()
			if err := manager.SaveChangeMetadata(change); err != nil {
				return agentic.UserResponse{
					ResponseID:  uuid.New().String(),
					ChannelType: msg.ChannelType,
					ChannelID:   msg.ChannelID,
					UserID:      msg.UserID,
					Type:        agentic.ResponseTypeError,
					Content:     fmt.Sprintf("Failed to update status: %v", err),
					Timestamp:   time.Now(),
				}, nil
			}
		}

		// Reload change to get updated status
		change, err = manager.LoadChange(slug)
		if err != nil {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to reload change: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}

		if change.Status != workflow.StatusComplete {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Cannot archive change with status '%s'. Change must be approved first.", change.Status),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	// Perform the archive
	if err := manager.ArchiveChange(slug); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to archive: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Archived change",
		"user_id", msg.UserID,
		"slug", slug)

	// Build success response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ **Archived**: %s\n\n", change.Title))
	sb.WriteString("Actions performed:\n")
	sb.WriteString(fmt.Sprintf("- Moved change to `.semspec/archive/%s/`\n", slug))
	sb.WriteString("- Moved specs to `.semspec/specs/` (source of truth)\n")
	sb.WriteString(fmt.Sprintf("- Status updated to: %s\n\n", workflow.StatusArchived))
	sb.WriteString("The change is now complete and archived.\n")
	sb.WriteString("Specs are available in the source of truth for future reference.\n")

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
