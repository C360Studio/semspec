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

// TasksCommand implements the /tasks command for generating task lists.
type TasksCommand struct{}

// Config returns the command configuration.
func (c *TasksCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/tasks\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/tasks <change> - Generate task list for a change",
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
			Content:     "Usage: /tasks <change>",
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

	// Check if tasks already exist
	if change.Files.HasTasks {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Tasks already exist for %s. Edit `.semspec/changes/%s/tasks.md` directly.", slug, slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Try to extract sections from spec if it exists
	sections := []string{"Setup", "Implementation", "Testing", "Documentation"}
	if change.Files.HasSpec {
		specContent, err := manager.ReadSpec(slug)
		if err == nil {
			extracted := extractSectionsFromSpec(specContent)
			if len(extracted) > 0 {
				sections = extracted
			}
		}
	}

	// Generate and write the tasks template
	tasksContent := workflow.TasksTemplate(change.Title, sections)
	if err := manager.WriteTasks(change.Slug, tasksContent); err != nil {
		cmdCtx.Logger.Error("Failed to write tasks",
			"error", err,
			"slug", change.Slug)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to write tasks: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Created tasks",
		"user_id", msg.UserID,
		"slug", change.Slug,
		"sections", len(sections))

	// Build success response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✓ Created task list for: **%s**\n\n", change.Title))
	sb.WriteString("File created:\n")
	sb.WriteString(fmt.Sprintf("- `.semspec/changes/%s/tasks.md`\n\n", change.Slug))
	sb.WriteString("Task sections:\n")
	for i, section := range sections {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, section))
	}
	sb.WriteString("\n")
	sb.WriteString("The task format uses numbered checklists:\n")
	sb.WriteString("```\n")
	sb.WriteString("## 1. Setup\n")
	sb.WriteString("- [ ] 1.1 Task description\n")
	sb.WriteString("- [ ] 1.2 Task description\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Next steps:\n")
	sb.WriteString("1. Edit `tasks.md` to fill in specific tasks\n")
	sb.WriteString(fmt.Sprintf("2. Run `/check %s` to validate against constitution\n", change.Slug))
	sb.WriteString(fmt.Sprintf("3. Run `/approve %s` to mark ready for implementation\n", change.Slug))

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

// extractSectionsFromSpec extracts requirement names from a spec file.
func extractSectionsFromSpec(content string) []string {
	var sections []string
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for requirement headers
		if strings.HasPrefix(line, "### Requirement") {
			// Extract the requirement name
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				if name != "" && name != "(Name)" {
					sections = append(sections, name)
				}
			}
		}
	}

	return sections
}
