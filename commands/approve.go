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

// ApproveCommand implements the /approve command for approving changes.
type ApproveCommand struct{}

// Config returns the command configuration.
func (c *ApproveCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/approve\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/approve <change> - Approve change for implementation",
	}
}

// Execute runs the approve command.
func (c *ApproveCommand) Execute(
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
			Content:     "Usage: /approve <change>",
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

	// Check required files
	var missing []string
	if !change.Files.HasProposal {
		missing = append(missing, "proposal.md")
	}
	if !change.Files.HasSpec {
		missing = append(missing, "spec.md")
	}

	if len(missing) > 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Cannot approve: missing required files: %s", strings.Join(missing, ", ")),
			Timestamp:   time.Now(),
		}, nil
	}

	// Run constitution check if constitution exists
	constitution, err := manager.LoadConstitution()
	if err == nil {
		// Constitution exists, run check
		var allContent strings.Builder
		if change.Files.HasProposal {
			content, _ := manager.ReadProposal(slug)
			allContent.WriteString(content)
			allContent.WriteString("\n\n")
		}
		if change.Files.HasDesign {
			content, _ := manager.ReadDesign(slug)
			allContent.WriteString(content)
			allContent.WriteString("\n\n")
		}
		if change.Files.HasSpec {
			content, _ := manager.ReadSpec(slug)
			allContent.WriteString(content)
			allContent.WriteString("\n\n")
		}
		if change.Files.HasTasks {
			content, _ := manager.ReadTasks(slug)
			allContent.WriteString(content)
		}

		result := checkAgainstConstitution(constitution, allContent.String(), change)
		if !result.Passed {
			var sb strings.Builder
			sb.WriteString("Cannot approve: constitution check failed\n\n")
			sb.WriteString("### Violations\n\n")
			for _, v := range result.Violations {
				sb.WriteString(fmt.Sprintf("- **Principle %d (%s)**: %s\n",
					v.Principle.Number, v.Principle.Title, v.Message))
			}
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("Run `/check %s` for details.\n", slug))

			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     sb.String(),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	// Transition through states to approved
	// The change might be in any pre-approved state
	transitions := []workflow.Status{
		workflow.StatusDrafted,
		workflow.StatusReviewed,
		workflow.StatusApproved,
	}

	for _, targetStatus := range transitions {
		if change.Status == targetStatus {
			break
		}
		if change.Status.CanTransitionTo(targetStatus) {
			change.Status = targetStatus
		}
	}

	// Save the final status
	change.UpdatedAt = time.Now()
	if err := manager.SaveChangeMetadata(change); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to save change status: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Approved change",
		"user_id", msg.UserID,
		"slug", change.Slug,
		"status", change.Status)

	// Build success response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ **Approved**: %s\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("Status: %s\n", change.Status))
	sb.WriteString(fmt.Sprintf("Approved by: %s\n", msg.UserID))
	sb.WriteString(fmt.Sprintf("Approved at: %s\n\n", change.UpdatedAt.Format("2006-01-02 15:04")))

	sb.WriteString("Files:\n")
	if change.Files.HasProposal {
		sb.WriteString("- ✓ proposal.md\n")
	}
	if change.Files.HasDesign {
		sb.WriteString("- ✓ design.md\n")
	}
	if change.Files.HasSpec {
		sb.WriteString("- ✓ spec.md\n")
	}
	if change.Files.HasTasks {
		sb.WriteString("- ✓ tasks.md\n")
	}

	sb.WriteString("\nNext steps:\n")
	sb.WriteString("1. Begin implementation following tasks.md\n")
	sb.WriteString(fmt.Sprintf("2. Run `/status %s` to check progress\n", slug))
	sb.WriteString(fmt.Sprintf("3. Run `/archive %s` when complete\n", slug))

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
