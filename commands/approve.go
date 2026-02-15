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

// ApproveCommand implements the /approve command for approving draft plans.
type ApproveCommand struct{}

// Config returns the command configuration.
func (c *ApproveCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/approve\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/approve <slug> - Approve a draft plan for execution",
	}
}

// Execute runs the approve command to approve a draft plan.
func (c *ApproveCommand) Execute(
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
	slug, showHelp := parseApproveArgs(rawArgs)

	// Show help if requested or no slug provided
	if showHelp || slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     approveHelpText(),
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
			Content:     fmt.Sprintf("Plan not found: `%s`\n\nUse `/plan <title>` to create one first.", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Check if already approved
	if plan.Approved {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     formatAlreadyApprovedResponse(plan),
			Timestamp:   time.Now(),
		}, nil
	}

	// Approve the plan
	if err := manager.ApprovePlan(ctx, plan); err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to approve plan: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Approved plan",
		"user_id", msg.UserID,
		"slug", slug,
		"plan_id", plan.ID)

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     formatApprovedResponse(plan),
		Timestamp:   time.Now(),
	}, nil
}

// parseApproveArgs parses the slug and flags from command arguments.
func parseApproveArgs(rawArgs string) (slug string, showHelp bool) {
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

// approveHelpText returns the help text for the /approve command.
func approveHelpText() string {
	return `## /approve - Approve a Plan for Execution

**Usage:** ` + "`/approve <slug>`" + `

Approves a draft plan, making it ready for task generation and execution.

**Examples:**
` + "```" + `
/approve auth-options
/approve database-redesign
` + "```" + `

**What Approval Does:**
- Sets the plan status to "approved"
- Records the approval timestamp
- Enables task generation via ` + "`/tasks --generate`" + `

**Prerequisites:**
- A plan must exist (created via ` + "`/plan`" + `)
- The plan should have Goal/Context filled in

**Workflow:**
1. ` + "`/plan <title>`" + ` - Create draft plan (LLM generates Goal/Context)
2. Review and refine plan.json
3. ` + "`/approve <slug>`" + ` - Approve the plan (you are here)
4. ` + "`/tasks <slug> --generate`" + ` - Generate tasks
5. ` + "`/execute <slug> --run`" + ` - Execute the plan

**Related Commands:**
- ` + "`/plan <title>`" + ` - Create a new draft plan
- ` + "`/tasks <slug> --generate`" + ` - Generate tasks from plan
- ` + "`/execute <slug> --run`" + ` - Execute tasks from the plan
`
}

// formatApprovedResponse formats the response for a successfully approved plan.
func formatApprovedResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Plan Approved: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Approved")
	if plan.ApprovedAt != nil {
		sb.WriteString(fmt.Sprintf(" (at %s)", plan.ApprovedAt.Format(time.RFC3339)))
	}
	sb.WriteString("\n\n")

	// Show readiness summary
	sb.WriteString("### Plan Readiness\n\n")

	readyCount := 0
	totalChecks := 2

	if plan.Goal != "" {
		sb.WriteString("- [x] Goal defined\n")
		readyCount++
	} else {
		sb.WriteString("- [ ] Goal (not yet defined)\n")
	}

	if plan.Context != "" {
		sb.WriteString("- [x] Context defined\n")
		readyCount++
	} else {
		sb.WriteString("- [ ] Context (not yet defined)\n")
	}

	// Show scope status
	scopeCount := len(plan.Scope.Include) + len(plan.Scope.Exclude)
	if scopeCount > 0 {
		sb.WriteString(fmt.Sprintf("- [x] Scope: %d boundaries defined\n", scopeCount))
	} else {
		sb.WriteString("- [ ] Scope (optional, not defined)\n")
	}

	sb.WriteString("\n")

	if readyCount < totalChecks {
		sb.WriteString("**Note:** Goal and Context should be defined before execution.\n\n")
	}

	sb.WriteString("### Next Steps\n\n")
	sb.WriteString(fmt.Sprintf("- `/tasks %s --generate` - Generate tasks from plan\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("- `/execute %s` - Execute tasks\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("- Edit `.semspec/projects/default/plans/%s/plan.json` to refine the plan\n", plan.Slug))

	return sb.String()
}

// formatAlreadyApprovedResponse formats the response when a plan is already approved.
func formatAlreadyApprovedResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Already Approved: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Approved")
	if plan.ApprovedAt != nil {
		sb.WriteString(fmt.Sprintf(" (at %s)", plan.ApprovedAt.Format(time.RFC3339)))
	}
	sb.WriteString("\n\n")

	sb.WriteString("This plan has already been approved and is ready for execution.\n\n")

	sb.WriteString("### Available Actions\n\n")
	sb.WriteString(fmt.Sprintf("- `/tasks %s --generate` - Generate tasks from plan\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("- `/execute %s` - Execute tasks\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("- Edit `.semspec/projects/default/plans/%s/plan.json` to modify the plan\n", plan.Slug))

	return sb.String()
}
