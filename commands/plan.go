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

// PlanCommand implements the /plan command for creating committed plans.
type PlanCommand struct{}

// Config returns the command configuration.
func (c *PlanCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/plan\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/plan <title> - Create a new committed plan (SMEAC format)",
	}
}

// Execute runs the plan command to create a new committed plan.
func (c *PlanCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	rawArgs := ""
	if len(args) > 0 {
		rawArgs = strings.TrimSpace(args[0])
	}

	// Parse arguments
	title, showHelp := parsePlanArgs(rawArgs)

	// Show help if requested or no title provided
	if showHelp || title == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     planHelpText(),
			Timestamp:   time.Now(),
		}, nil
	}

	// Get repo root
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

	// Generate slug from title
	slug := workflow.Slugify(title)

	// Check if plan already exists
	if manager.PlanExists(slug) {
		// Load existing plan
		plan, err := manager.LoadPlan(ctx, slug)
		if err != nil {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to load existing plan: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}

		cmdCtx.Logger.Info("Loaded existing plan",
			"user_id", msg.UserID,
			"slug", slug,
			"committed", plan.Committed)

		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     formatExistingPlanResponse(plan),
			Timestamp:   time.Now(),
		}, nil
	}

	// Create new plan
	plan, err := manager.CreatePlan(ctx, slug, title)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to create plan: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Promote to committed immediately (this is /plan, not /explore)
	if err := manager.PromotePlan(ctx, plan); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to commit plan: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Created committed plan",
		"user_id", msg.UserID,
		"slug", slug,
		"plan_id", plan.ID)

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     formatNewPlanResponse(plan),
		Timestamp:   time.Now(),
	}, nil
}

// parsePlanArgs parses the title and flags from command arguments.
func parsePlanArgs(rawArgs string) (title string, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var titleParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
		case strings.HasPrefix(part, "--"):
			// Skip unknown flags
			continue
		default:
			titleParts = append(titleParts, part)
		}
	}

	title = strings.Join(titleParts, " ")
	return
}

// planHelpText returns the help text for the /plan command.
func planHelpText() string {
	return `## /plan - Create a Committed Plan

**Usage:** ` + "`/plan <title>`" + `

Creates a new plan ready for task generation and execution.

**Examples:**
` + "```" + `
/plan Add authentication refresh
/plan Fix database connection pooling
/plan Implement user notifications
` + "```" + `

**Plan Structure:**
The plan will be created at ` + "`.semspec/changes/<slug>/plan.json`" + ` with:
- **Goal**: What we're building or fixing
- **Context**: Current state and why this matters
- **Scope**: Files to include, exclude, and protect

**Workflow:**
1. Create plan with ` + "`/plan <title>`" + `
2. Edit plan.json to set Goal, Context, and Scope
3. Run ` + "`/tasks <slug> --generate`" + ` to create tasks with acceptance criteria
4. Run ` + "`/execute <slug> --run`" + ` to begin execution

**Example plan.json:**
` + "```json" + `
{
  "goal": "Add token refresh to prevent session expiry",
  "context": "Users are logged out after 1 hour. Need silent refresh.",
  "scope": {
    "include": ["internal/auth/", "api/v1/auth.go"],
    "exclude": ["internal/auth/legacy/"],
    "do_not_touch": ["internal/auth/oauth.go"]
  }
}
` + "```" + `

**Related Commands:**
- ` + "`/explore <topic>`" + ` - Create an uncommitted exploration (scratchpad)
- ` + "`/promote <slug>`" + ` - Promote an exploration to a committed plan
- ` + "`/tasks <slug>`" + ` - View or generate tasks
- ` + "`/execute <slug>`" + ` - Execute tasks from a plan
`
}

// formatNewPlanResponse formats the response for a newly created plan.
func formatNewPlanResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Plan Created: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Committed\n\n")

	sb.WriteString("**Location:** `.semspec/changes/" + plan.Slug + "/plan.json`\n\n")

	sb.WriteString("### Next Steps\n\n")
	sb.WriteString("1. Edit the plan file to set:\n")
	sb.WriteString("   - **goal**: What we're building or fixing\n")
	sb.WriteString("   - **context**: Current state and why this matters\n")
	sb.WriteString("   - **scope**: Files to include, exclude, protect\n\n")
	sb.WriteString(fmt.Sprintf("2. Run `/tasks %s --generate` to create tasks with acceptance criteria\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("3. Run `/execute %s --run` to begin execution\n", plan.Slug))

	return sb.String()
}

// formatExistingPlanResponse formats the response for an existing plan.
func formatExistingPlanResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Existing Plan: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))

	if plan.Committed {
		sb.WriteString("**Status:** Committed")
		if plan.CommittedAt != nil {
			sb.WriteString(fmt.Sprintf(" (at %s)", plan.CommittedAt.Format(time.RFC3339)))
		}
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("**Status:** Exploration (uncommitted)\n\n")
	}

	sb.WriteString("**Location:** `.semspec/changes/" + plan.Slug + "/plan.json`\n\n")

	// Show plan summary - prefer new Goal/Context, fallback to SMEAC
	hasContent := plan.Goal != "" || plan.Context != "" ||
		plan.Situation != "" || plan.Mission != "" || plan.Execution != ""
	if hasContent {
		sb.WriteString("### Plan Summary\n\n")
		// New structure
		if plan.Goal != "" {
			sb.WriteString(fmt.Sprintf("**Goal:** %s\n\n", truncateText(plan.Goal, 200)))
		} else if plan.Mission != "" {
			sb.WriteString(fmt.Sprintf("**Mission:** %s\n\n", truncateText(plan.Mission, 200)))
		}
		if plan.Context != "" {
			sb.WriteString(fmt.Sprintf("**Context:** %s\n\n", truncateText(plan.Context, 200)))
		} else if plan.Situation != "" {
			sb.WriteString(fmt.Sprintf("**Situation:** %s\n\n", truncateText(plan.Situation, 200)))
		}
		// Show scope if defined
		scopeCount := len(plan.Scope.Include) + len(plan.Scope.Exclude) + len(plan.Scope.DoNotTouch)
		if scopeCount > 0 {
			sb.WriteString(fmt.Sprintf("**Scope:** %d include, %d exclude, %d protected\n\n",
				len(plan.Scope.Include), len(plan.Scope.Exclude), len(plan.Scope.DoNotTouch)))
		}
		// Legacy execution steps
		if plan.Execution != "" {
			sb.WriteString(fmt.Sprintf("**Execution:** %d steps defined\n\n", countExecutionSteps(plan.Execution)))
		}
	}

	sb.WriteString("### Available Actions\n\n")
	if plan.Committed {
		sb.WriteString(fmt.Sprintf("- `/tasks %s` - View tasks\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("- `/tasks %s --generate` - Generate tasks from plan\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("- `/execute %s --run` - Execute tasks\n", plan.Slug))
	} else {
		sb.WriteString(fmt.Sprintf("- `/promote %s` - Promote to committed plan\n", plan.Slug))
	}
	sb.WriteString(fmt.Sprintf("- Edit `.semspec/changes/%s/plan.json` to modify the plan\n", plan.Slug))

	return sb.String()
}

// truncateText truncates text to maxLen runes, adding "..." if truncated.
// Handles multi-byte UTF-8 characters safely.
func truncateText(text string, maxLen int) string {
	// Replace newlines with spaces for single-line display
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ") // Normalize whitespace

	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen-3]) + "..."
}

// countExecutionSteps counts numbered items in the execution text.
func countExecutionSteps(execution string) int {
	count := 0
	lines := strings.Split(execution, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Match lines starting with digit followed by . or )
		if len(trimmed) > 2 {
			if trimmed[0] >= '0' && trimmed[0] <= '9' {
				if trimmed[1] == '.' || trimmed[1] == ')' ||
					(trimmed[1] >= '0' && trimmed[1] <= '9' && len(trimmed) > 2 && (trimmed[2] == '.' || trimmed[2] == ')')) {
					count++
				}
			}
		}
	}
	return count
}
