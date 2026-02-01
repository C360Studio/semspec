package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360/semspec/tools/github"
	"github.com/c360/semspec/workflow"
	"github.com/c360/semstreams/agentic"
	agenticdispatch "github.com/c360/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// GitHubCommand implements the /github command for GitHub integration.
type GitHubCommand struct{}

// Config returns the command configuration.
func (c *GitHubCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/github\s+(\w+)(?:\s+(.+))?$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/github <sync|status|unlink> <change> - Manage GitHub issues for changes",
	}
}

// Execute runs the github command.
func (c *GitHubCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	if len(args) < 1 {
		return c.errorResponse(msg, "Usage: /github <sync|status|unlink> <change>")
	}

	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	slug := ""
	if len(args) > 1 {
		slug = strings.TrimSpace(args[1])
	}

	// Get repo root
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return c.errorResponse(msg, fmt.Sprintf("Failed to get working directory: %v", err))
		}
	}

	// Check if gh CLI is available
	if !github.IsGHAvailable() {
		return c.errorResponse(msg, "GitHub CLI (gh) is not installed or not authenticated.\nRun `gh auth login` to authenticate.")
	}

	switch subcommand {
	case "sync":
		return c.executeSync(ctx, cmdCtx, msg, repoRoot, slug)
	case "status":
		return c.executeStatus(ctx, msg, repoRoot, slug)
	case "unlink":
		return c.executeUnlink(ctx, msg, repoRoot, slug)
	default:
		return c.errorResponse(msg, fmt.Sprintf("Unknown subcommand: %s\nUsage: /github <sync|status|unlink> <change>", subcommand))
	}
}

// executeSync syncs a change to GitHub issues.
func (c *GitHubCommand) executeSync(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	repoRoot, slug string,
) (agentic.UserResponse, error) {
	if slug == "" {
		return c.errorResponse(msg, "Usage: /github sync <change>")
	}

	manager := workflow.NewManager(repoRoot)

	// Load change
	change, err := manager.LoadChange(slug)
	if err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Change not found: %s", slug))
	}

	// Check status - must be approved or later
	validStatuses := []workflow.Status{
		workflow.StatusApproved,
		workflow.StatusImplementing,
		workflow.StatusComplete,
	}
	statusValid := false
	for _, s := range validStatuses {
		if change.Status == s {
			statusValid = true
			break
		}
	}
	if !statusValid {
		return c.errorResponse(msg, fmt.Sprintf("Change must be approved before syncing to GitHub (current: %s)", change.Status))
	}

	// Get repository name
	repoName, err := github.GetRepoName(repoRoot)
	if err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Failed to get repository info: %v", err))
	}

	// Initialize GitHub metadata if needed
	if change.GitHub == nil {
		change.GitHub = &workflow.GitHubMetadata{
			Repository: repoName,
			TaskIssues: make(map[string]int),
		}
	}

	// Read proposal for epic body
	proposalContent, _ := manager.ReadProposal(slug)
	whySection, whatSection := extractProposalSections(proposalContent)

	// Parse tasks
	var tasks []workflow.ParsedTask
	if change.Files.HasTasks {
		tasksContent, _ := manager.ReadTasks(slug)
		tasks, _ = workflow.ParseTasks(tasksContent)
	}

	var sb strings.Builder

	// Create or update epic issue
	if change.GitHub.EpicNumber == 0 {
		// Create new epic
		epicBody := buildEpicBody(change, whySection, whatSection, nil)
		epicLabels := []string{"semspec-epic", fmt.Sprintf("status:%s", change.Status)}

		epicNum, epicURL, err := github.CreateIssue(ctx, repoRoot, change.Title, epicBody, epicLabels)
		if err != nil {
			return c.errorResponse(msg, fmt.Sprintf("Failed to create epic issue: %v", err))
		}

		change.GitHub.EpicNumber = epicNum
		change.GitHub.EpicURL = epicURL

		sb.WriteString(fmt.Sprintf("Created epic issue #%d\n", epicNum))
	} else {
		sb.WriteString(fmt.Sprintf("Epic issue #%d exists\n", change.GitHub.EpicNumber))
	}

	// Create task issues
	var createdTasks []string
	for _, task := range tasks {
		if _, exists := change.GitHub.TaskIssues[task.ID]; exists {
			continue // Task already synced
		}

		taskTitle := fmt.Sprintf("[%s] %s", slug, task.Description)
		taskBody := buildTaskBody(change, task)
		taskLabels := []string{"semspec-task", fmt.Sprintf("epic:%s", slug)}

		taskNum, _, err := github.CreateIssue(ctx, repoRoot, taskTitle, taskBody, taskLabels)
		if err != nil {
			sb.WriteString(fmt.Sprintf("Failed to create task %s: %v\n", task.ID, err))
			continue
		}

		change.GitHub.TaskIssues[task.ID] = taskNum
		createdTasks = append(createdTasks, fmt.Sprintf("%s -> #%d", task.ID, taskNum))
	}

	if len(createdTasks) > 0 {
		sb.WriteString(fmt.Sprintf("Created %d task issues: %s\n", len(createdTasks), strings.Join(createdTasks, ", ")))
	}

	// Update epic body with task list
	if change.GitHub.EpicNumber > 0 && len(change.GitHub.TaskIssues) > 0 {
		epicBody := buildEpicBody(change, whySection, whatSection, tasks)
		if err := github.EditIssueBody(ctx, repoRoot, change.GitHub.EpicNumber, epicBody); err != nil {
			sb.WriteString(fmt.Sprintf("Warning: Failed to update epic with task list: %v\n", err))
		} else {
			sb.WriteString("Updated epic with task list\n")
		}
	}

	// Save metadata
	change.GitHub.LastSynced = time.Now()
	change.UpdatedAt = time.Now()
	if err := manager.SaveChangeMetadata(change); err != nil {
		sb.WriteString(fmt.Sprintf("Warning: Failed to save metadata: %v\n", err))
	}

	cmdCtx.Logger.Info("Synced change to GitHub",
		"user_id", msg.UserID,
		"slug", slug,
		"epic", change.GitHub.EpicNumber,
		"tasks", len(change.GitHub.TaskIssues))

	sb.WriteString(fmt.Sprintf("\nEpic: %s\n", change.GitHub.EpicURL))
	sb.WriteString(fmt.Sprintf("Repository: %s\n", change.GitHub.Repository))

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

// executeStatus shows GitHub sync status for a change.
func (c *GitHubCommand) executeStatus(
	ctx context.Context,
	msg agentic.UserMessage,
	repoRoot, slug string,
) (agentic.UserResponse, error) {
	if slug == "" {
		return c.errorResponse(msg, "Usage: /github status <change>")
	}

	manager := workflow.NewManager(repoRoot)

	change, err := manager.LoadChange(slug)
	if err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Change not found: %s", slug))
	}

	if change.GitHub == nil || change.GitHub.EpicNumber == 0 {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     fmt.Sprintf("Change '%s' is not synced to GitHub.\nRun `/github sync %s` to create issues.", slug, slug),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## GitHub Status: %s\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("**Repository**: %s\n", change.GitHub.Repository))
	sb.WriteString(fmt.Sprintf("**Epic**: #%d (%s)\n", change.GitHub.EpicNumber, change.GitHub.EpicURL))
	sb.WriteString(fmt.Sprintf("**Last Synced**: %s\n\n", change.GitHub.LastSynced.Format("2006-01-02 15:04")))

	if len(change.GitHub.TaskIssues) > 0 {
		sb.WriteString("### Task Issues\n\n")
		sb.WriteString("| Task | Issue |\n")
		sb.WriteString("|------|-------|\n")
		for taskID, issueNum := range change.GitHub.TaskIssues {
			sb.WriteString(fmt.Sprintf("| %s | #%d |\n", taskID, issueNum))
		}
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

// executeUnlink clears GitHub metadata from a change.
func (c *GitHubCommand) executeUnlink(
	ctx context.Context,
	msg agentic.UserMessage,
	repoRoot, slug string,
) (agentic.UserResponse, error) {
	if slug == "" {
		return c.errorResponse(msg, "Usage: /github unlink <change>")
	}

	manager := workflow.NewManager(repoRoot)

	change, err := manager.LoadChange(slug)
	if err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Change not found: %s", slug))
	}

	if change.GitHub == nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     fmt.Sprintf("Change '%s' is not linked to GitHub.", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	epicNum := change.GitHub.EpicNumber
	taskCount := len(change.GitHub.TaskIssues)

	// Clear GitHub metadata
	change.GitHub = nil
	change.UpdatedAt = time.Now()

	if err := manager.SaveChangeMetadata(change); err != nil {
		return c.errorResponse(msg, fmt.Sprintf("Failed to save metadata: %v", err))
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     fmt.Sprintf("Unlinked GitHub issues from '%s'.\nNote: Issues #%d and %d task issues still exist on GitHub.", slug, epicNum, taskCount),
		Timestamp:   time.Now(),
	}, nil
}

// errorResponse creates an error response.
func (c *GitHubCommand) errorResponse(msg agentic.UserMessage, content string) (agentic.UserResponse, error) {
	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeError,
		Content:     content,
		Timestamp:   time.Now(),
	}, nil
}

// extractProposalSections extracts Why and What sections from proposal content.
func extractProposalSections(content string) (why, what string) {
	lines := strings.Split(content, "\n")
	var currentSection string
	var whyLines, whatLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if strings.HasPrefix(lower, "## why") || strings.HasPrefix(lower, "### why") {
			currentSection = "why"
			continue
		}
		if strings.HasPrefix(lower, "## what") || strings.HasPrefix(lower, "### what") {
			currentSection = "what"
			continue
		}
		if strings.HasPrefix(trimmed, "##") {
			currentSection = ""
			continue
		}

		switch currentSection {
		case "why":
			whyLines = append(whyLines, line)
		case "what":
			whatLines = append(whatLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(whyLines, "\n")),
		strings.TrimSpace(strings.Join(whatLines, "\n"))
}

// buildEpicBody builds the epic issue body.
func buildEpicBody(change *workflow.Change, why, what string, tasks []workflow.ParsedTask) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", change.Title))

	if why != "" {
		sb.WriteString("## Why\n\n")
		sb.WriteString(why)
		sb.WriteString("\n\n")
	}

	if what != "" {
		sb.WriteString("## What Changes\n\n")
		sb.WriteString(what)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Tasks\n\n")
	if len(tasks) > 0 && change.GitHub != nil && len(change.GitHub.TaskIssues) > 0 {
		for _, task := range tasks {
			if issueNum, ok := change.GitHub.TaskIssues[task.ID]; ok {
				checkbox := " "
				if task.Completed {
					checkbox = "x"
				}
				sb.WriteString(fmt.Sprintf("- [%s] #%d %s\n", checkbox, issueNum, task.Description))
			}
		}
	} else {
		sb.WriteString("*Tasks will be linked here after sync*\n")
	}

	sb.WriteString("\n---\n")
	sb.WriteString(fmt.Sprintf("*Managed by Semspec | `%s` | Status: %s*\n", change.Slug, change.Status))

	return sb.String()
}

// buildTaskBody builds the task issue body.
func buildTaskBody(change *workflow.Change, task workflow.ParsedTask) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("**Parent Epic**: #%d\n", change.GitHub.EpicNumber))
	sb.WriteString(fmt.Sprintf("**Section**: %s\n", task.Section))
	sb.WriteString(fmt.Sprintf("**Task ID**: %s\n\n", task.ID))

	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("*Part of: [%s](#%d)*\n", change.Title, change.GitHub.EpicNumber))

	return sb.String()
}
