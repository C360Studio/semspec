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

Creates a new plan using the SMEAC format (Situation, Mission, Execution, Administration/Logistics, Command/Signal).

**Examples:**
` + "```" + `
/plan Add authentication refresh
/plan Fix database connection pooling
/plan Implement user notifications
` + "```" + `

**Plan Structure:**
The plan will be created at ` + "`.semspec/changes/<slug>/plan.json`" + ` with:
- **Situation**: Current state and context
- **Mission**: Objective and success criteria
- **Execution**: Steps to complete (numbered items become tasks)
- **Constraints**: In-scope, out-of-scope, protected files
- **Coordination**: Dependencies and sync points

**Next Steps:**
1. Edit the plan file to fill in SMEAC sections
2. Add numbered steps to the Execution section
3. Run ` + "`/execute <slug>`" + ` to generate tasks and begin execution

**Related Commands:**
- ` + "`/explore <topic>`" + ` - Create an uncommitted exploration (scratchpad)
- ` + "`/promote <slug>`" + ` - Promote an exploration to a committed plan
- ` + "`/execute <slug>`" + ` - Generate tasks and execute the plan
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
	sb.WriteString("1. Edit the plan file to fill in the SMEAC sections:\n")
	sb.WriteString("   - **Situation**: What exists now, current context\n")
	sb.WriteString("   - **Mission**: What we're doing and why\n")
	sb.WriteString("   - **Execution**: Numbered steps (will become tasks)\n")
	sb.WriteString("   - **Constraints**: In/Out/DoNotTouch scopes\n")
	sb.WriteString("   - **Coordination**: Dependencies and sync points\n\n")
	sb.WriteString(fmt.Sprintf("2. Run `/execute %s` to generate tasks and begin execution\n", plan.Slug))

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

	// Show SMEAC summary if populated
	hasSMEAC := plan.Situation != "" || plan.Mission != "" || plan.Execution != ""
	if hasSMEAC {
		sb.WriteString("### Plan Summary\n\n")
		if plan.Situation != "" {
			sb.WriteString(fmt.Sprintf("**Situation:** %s\n\n", truncateText(plan.Situation, 200)))
		}
		if plan.Mission != "" {
			sb.WriteString(fmt.Sprintf("**Mission:** %s\n\n", truncateText(plan.Mission, 200)))
		}
		if plan.Execution != "" {
			sb.WriteString(fmt.Sprintf("**Execution:** %d steps defined\n\n", countExecutionSteps(plan.Execution)))
		}
	}

	sb.WriteString("### Available Actions\n\n")
	if plan.Committed {
		sb.WriteString(fmt.Sprintf("- `/execute %s` - Generate tasks and begin execution\n", plan.Slug))
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
