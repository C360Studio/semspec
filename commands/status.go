package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// StatusCommand implements the /status command for listing changes and their state.
type StatusCommand struct{}

// Config returns the command configuration.
func (c *StatusCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/changes(?:\s+(.*))?$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/changes [slug] - Show status of changes",
	}
}

// Execute runs the status command.
func (c *StatusCommand) Execute(
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

	// If a specific change is requested, show detailed status
	if slug != "" {
		return c.showChangeStatus(manager, slug, msg)
	}

	// Otherwise, list all changes
	return c.listAllChanges(manager, msg)
}

// showChangeStatus shows detailed status for a specific change.
func (c *StatusCommand) showChangeStatus(manager *workflow.Manager, slug string, msg agentic.UserMessage) (agentic.UserResponse, error) {
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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("**Slug**: `%s`\n", change.Slug))
	sb.WriteString(fmt.Sprintf("**Status**: %s\n", formatStatus(change.Status)))
	sb.WriteString(fmt.Sprintf("**Author**: %s\n", change.Author))
	sb.WriteString(fmt.Sprintf("**Created**: %s\n", change.CreatedAt.Format("2006-01-02 15:04")))
	sb.WriteString(fmt.Sprintf("**Updated**: %s\n\n", change.UpdatedAt.Format("2006-01-02 15:04")))

	// Files section
	sb.WriteString("### Files\n\n")
	sb.WriteString(fmt.Sprintf("| File | Status |\n"))
	sb.WriteString(fmt.Sprintf("|------|--------|\n"))
	sb.WriteString(fmt.Sprintf("| proposal.md | %s |\n", fileStatus(change.Files.HasProposal)))
	sb.WriteString(fmt.Sprintf("| design.md | %s |\n", fileStatus(change.Files.HasDesign)))
	sb.WriteString(fmt.Sprintf("| spec.md | %s |\n", fileStatus(change.Files.HasSpec)))
	sb.WriteString(fmt.Sprintf("| tasks.md | %s |\n\n", fileStatus(change.Files.HasTasks)))

	// Workflow progress
	sb.WriteString("### Workflow Progress\n\n")
	sb.WriteString(workflowProgress(change.Status))

	// GitHub integration status
	if change.GitHub != nil && change.GitHub.EpicNumber > 0 {
		sb.WriteString("\n### GitHub Integration\n\n")
		sb.WriteString(fmt.Sprintf("**Epic**: [#%d](%s)\n", change.GitHub.EpicNumber, change.GitHub.EpicURL))
		sb.WriteString(fmt.Sprintf("**Repository**: %s\n", change.GitHub.Repository))
		sb.WriteString(fmt.Sprintf("**Tasks Synced**: %d\n", len(change.GitHub.TaskIssues)))
		sb.WriteString(fmt.Sprintf("**Last Synced**: %s\n", change.GitHub.LastSynced.Format("2006-01-02 15:04")))
	}

	// Next steps based on current status
	sb.WriteString("\n### Next Steps\n\n")
	sb.WriteString(nextSteps(change))

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

// listAllChanges lists all active changes.
func (c *StatusCommand) listAllChanges(manager *workflow.Manager, msg agentic.UserMessage) (agentic.UserResponse, error) {
	changes, err := manager.ListChanges()
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to list changes: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	if len(changes) == 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     "No active changes.\n\nRun `/propose <description>` to create a new change.",
			Timestamp:   time.Now(),
		}, nil
	}

	// Sort by updated time (most recent first)
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].UpdatedAt.After(changes[j].UpdatedAt)
	})

	var sb strings.Builder
	sb.WriteString("## Active Changes\n\n")
	sb.WriteString("| Change | Status | Author | Updated |\n")
	sb.WriteString("|--------|--------|--------|----------|\n")

	for _, change := range changes {
		sb.WriteString(fmt.Sprintf("| [%s](%s) | %s | %s | %s |\n",
			change.Title,
			change.Slug,
			formatStatus(change.Status),
			change.Author,
			change.UpdatedAt.Format("2006-01-02")))
	}

	sb.WriteString(fmt.Sprintf("\n*%d active change(s)*\n\n", len(changes)))
	sb.WriteString("Run `/status <change>` for detailed status.")

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

// formatStatus formats a status with an emoji indicator.
func formatStatus(status workflow.Status) string {
	switch status {
	case workflow.StatusCreated:
		return "📝 created"
	case workflow.StatusDrafted:
		return "✏️ drafted"
	case workflow.StatusReviewed:
		return "👀 reviewed"
	case workflow.StatusApproved:
		return "✅ approved"
	case workflow.StatusImplementing:
		return "🔨 implementing"
	case workflow.StatusComplete:
		return "🎉 complete"
	case workflow.StatusArchived:
		return "📦 archived"
	case workflow.StatusRejected:
		return "❌ rejected"
	default:
		return string(status)
	}
}

// fileStatus formats a file existence status.
func fileStatus(exists bool) string {
	if exists {
		return "✓ exists"
	}
	return "○ missing"
}

// workflowProgress shows the workflow progress as a visual indicator.
func workflowProgress(current workflow.Status) string {
	stages := []workflow.Status{
		workflow.StatusCreated,
		workflow.StatusDrafted,
		workflow.StatusReviewed,
		workflow.StatusApproved,
		workflow.StatusImplementing,
		workflow.StatusComplete,
	}

	var sb strings.Builder
	sb.WriteString("```\n")

	currentIdx := -1
	for i, s := range stages {
		if s == current {
			currentIdx = i
			break
		}
	}

	for i, s := range stages {
		if i <= currentIdx {
			sb.WriteString(fmt.Sprintf("[✓] %s", s))
		} else {
			sb.WriteString(fmt.Sprintf("[ ] %s", s))
		}
		if i < len(stages)-1 {
			sb.WriteString(" → ")
		}
	}
	sb.WriteString("\n```")

	return sb.String()
}

// nextSteps suggests next steps based on current status and files.
func nextSteps(change *workflow.Change) string {
	var steps []string

	switch change.Status {
	case workflow.StatusCreated:
		if !change.Files.HasProposal {
			steps = append(steps, fmt.Sprintf("Edit `.semspec/changes/%s/proposal.md`", change.Slug))
		}
		if !change.Files.HasDesign {
			steps = append(steps, fmt.Sprintf("Run `/design %s`", change.Slug))
		}
		if !change.Files.HasSpec {
			steps = append(steps, fmt.Sprintf("Run `/spec %s`", change.Slug))
		}
	case workflow.StatusDrafted:
		steps = append(steps, "Review proposal and spec")
		steps = append(steps, fmt.Sprintf("Run `/check %s` to validate", change.Slug))
	case workflow.StatusReviewed:
		steps = append(steps, fmt.Sprintf("Run `/approve %s`", change.Slug))
	case workflow.StatusApproved:
		if !change.Files.HasTasks {
			steps = append(steps, fmt.Sprintf("Run `/tasks %s`", change.Slug))
		}
		if change.GitHub == nil || change.GitHub.EpicNumber == 0 {
			steps = append(steps, fmt.Sprintf("Run `/github sync %s` to create GitHub issues", change.Slug))
		}
		steps = append(steps, "Begin implementation")
	case workflow.StatusImplementing:
		steps = append(steps, "Complete implementation tasks")
		steps = append(steps, fmt.Sprintf("Run `/archive %s` when done", change.Slug))
	case workflow.StatusComplete:
		steps = append(steps, fmt.Sprintf("Run `/archive %s`", change.Slug))
	}

	if len(steps) == 0 {
		return "No pending actions."
	}

	var sb strings.Builder
	for i, step := range steps {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
	}
	return sb.String()
}
