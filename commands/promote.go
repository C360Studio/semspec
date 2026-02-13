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

// PromoteCommand implements the /promote command for promoting explorations to plans.
type PromoteCommand struct{}

// Config returns the command configuration.
func (c *PromoteCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/promote\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/promote <slug> - Promote an exploration to a committed plan",
	}
}

// Execute runs the promote command to promote an exploration to a committed plan.
func (c *PromoteCommand) Execute(
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
	slug, showHelp := parsePromoteArgs(rawArgs)

	// Show help if requested or no slug provided
	if showHelp || slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     promoteHelpText(),
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

	// Load the plan
	plan, err := manager.LoadPlan(ctx, slug)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Exploration not found: `%s`\n\nUse `/explore <topic>` to create one first.", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Check if already committed
	if plan.Committed {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     formatAlreadyCommittedResponse(plan),
			Timestamp:   time.Now(),
		}, nil
	}

	// Promote to committed
	if err := manager.PromotePlan(ctx, plan); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to promote exploration: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Promoted exploration to plan",
		"user_id", msg.UserID,
		"slug", slug,
		"plan_id", plan.ID)

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     formatPromotedResponse(plan),
		Timestamp:   time.Now(),
	}, nil
}

// parsePromoteArgs parses the slug and flags from command arguments.
func parsePromoteArgs(rawArgs string) (slug string, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var slugParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
		case strings.HasPrefix(part, "--"):
			// Skip unknown flags
			continue
		default:
			slugParts = append(slugParts, part)
		}
	}

	if len(slugParts) > 0 {
		slug = slugParts[0] // Only take first argument as slug
	}
	return
}

// promoteHelpText returns the help text for the /promote command.
func promoteHelpText() string {
	return `## /promote - Promote Exploration to Plan

**Usage:** ` + "`/promote <slug>`" + `

Promotes an uncommitted exploration to a committed plan, making it ready for execution.

**Examples:**
` + "```" + `
/promote auth-options
/promote database-redesign
` + "```" + `

**What Promotion Does:**
- Sets the plan status to "committed"
- Records the commitment timestamp
- Makes the plan visible to ` + "`/execute`" + `

**Prerequisites:**
- An exploration must exist (created via ` + "`/explore`" + `)
- The exploration should have SMEAC sections filled in

**Workflow:**
1. ` + "`/explore <topic>`" + ` - Create exploration
2. Edit plan.json to fill in SMEAC sections
3. ` + "`/promote <slug>`" + ` - Commit the plan (you are here)
4. ` + "`/execute <slug>`" + ` - Generate tasks and begin execution

**Related Commands:**
- ` + "`/explore <topic>`" + ` - Create an uncommitted exploration
- ` + "`/plan <title>`" + ` - Create a committed plan directly
- ` + "`/execute <slug>`" + ` - Generate tasks and execute the plan
`
}

// formatPromotedResponse formats the response for a successfully promoted plan.
func formatPromotedResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Plan Committed: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Committed")
	if plan.CommittedAt != nil {
		sb.WriteString(fmt.Sprintf(" (at %s)", plan.CommittedAt.Format(time.RFC3339)))
	}
	sb.WriteString("\n\n")

	// Show readiness summary
	sb.WriteString("### Plan Readiness\n\n")

	readyCount := 0
	totalChecks := 3

	if plan.Situation != "" {
		sb.WriteString("- [x] Situation defined\n")
		readyCount++
	} else {
		sb.WriteString("- [ ] Situation (not yet defined)\n")
	}

	if plan.Mission != "" {
		sb.WriteString("- [x] Mission defined\n")
		readyCount++
	} else {
		sb.WriteString("- [ ] Mission (not yet defined)\n")
	}

	stepCount := countExecutionSteps(plan.Execution)
	if stepCount > 0 {
		sb.WriteString(fmt.Sprintf("- [x] Execution: %d steps defined\n", stepCount))
		readyCount++
	} else {
		sb.WriteString("- [ ] Execution (no numbered steps)\n")
	}

	sb.WriteString("\n")

	if readyCount < totalChecks {
		sb.WriteString("**Note:** Some sections are incomplete. Consider editing the plan before execution.\n\n")
	}

	sb.WriteString("### Next Steps\n\n")
	sb.WriteString(fmt.Sprintf("- `/execute %s` - Generate tasks and begin execution\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("- Edit `.semspec/changes/%s/plan.json` to refine the plan\n", plan.Slug))

	return sb.String()
}

// formatAlreadyCommittedResponse formats the response when a plan is already committed.
func formatAlreadyCommittedResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Already Committed: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Committed")
	if plan.CommittedAt != nil {
		sb.WriteString(fmt.Sprintf(" (at %s)", plan.CommittedAt.Format(time.RFC3339)))
	}
	sb.WriteString("\n\n")

	sb.WriteString("This plan has already been committed and is ready for execution.\n\n")

	sb.WriteString("### Available Actions\n\n")
	sb.WriteString(fmt.Sprintf("- `/execute %s` - Generate tasks and begin execution\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("- Edit `.semspec/changes/%s/plan.json` to modify the plan\n", plan.Slug))

	return sb.String()
}
