package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	planreviewer "github.com/c360studio/semspec/processor/plan-reviewer"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
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
	slug, skipReview, showHelp := parseApproveArgs(rawArgs)

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

	// Run plan review against SOPs unless skipped
	if !skipReview && cmdCtx.NATSClient != nil {
		reviewResult, err := runPlanReview(ctx, cmdCtx, plan)
		if err != nil {
			cmdCtx.Logger.Warn("Plan review failed, proceeding with approval",
				"slug", slug,
				"error", err)
			// Log warning but don't block - review is best-effort
		} else if reviewResult != nil && !reviewResult.IsApproved() {
			// Review found issues - block approval
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeResult,
				Content:     formatReviewBlockedResponse(plan, reviewResult),
				Timestamp:   time.Now(),
			}, nil
		}
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
func parseApproveArgs(rawArgs string) (slug string, skipReview bool, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var slugParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
		case part == "--skip-review":
			skipReview = true
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

**Usage:** ` + "`/approve <slug> [--skip-review]`" + `

Approves a draft plan, making it ready for task generation and execution.

**Options:**
- ` + "`--skip-review`" + ` - Skip SOP compliance review (use with caution)

**Examples:**
` + "```" + `
/approve auth-options
/approve database-redesign
/approve quick-fix --skip-review
` + "```" + `

**What Approval Does:**
1. Reviews plan against plan-scope SOPs (unless --skip-review)
2. If review passes, sets the plan status to "approved"
3. Records the approval timestamp
4. Enables task generation via ` + "`/tasks --generate`" + `

**SOP Review:**
Plans are validated against Standard Operating Procedures with scope="plan".
If violations are found, approval is blocked until issues are addressed.

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

// runPlanReview triggers plan review against SOPs and waits for the result.
func runPlanReview(ctx context.Context, cmdCtx *agenticdispatch.CommandContext, plan *workflow.Plan) (*prompts.PlanReviewResult, error) {
	if cmdCtx.NATSClient == nil {
		return nil, fmt.Errorf("NATS client not available")
	}

	// Serialize plan to JSON
	planContent, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serialize plan: %w", err)
	}

	// Build trigger payload
	requestID := uuid.New().String()
	trigger := &planreviewer.PlanReviewTrigger{
		RequestID:     requestID,
		Slug:          plan.Slug,
		ProjectID:     plan.ProjectID,
		PlanContent:   string(planContent),
		ScopePatterns: plan.Scope.Include,
		// SOPContext will be populated by the plan-reviewer component via context builder
	}

	// Wrap in BaseMessage
	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "workflow", Category: "trigger", Version: "v1"},
		trigger,
		"approve-command",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal trigger: %w", err)
	}

	// Get JetStream context
	js, err := cmdCtx.NATSClient.JetStream()
	if err != nil {
		return nil, fmt.Errorf("get jetstream: %w", err)
	}

	// Create unique consumer name for this request
	consumerName := fmt.Sprintf("approve-result-%s", requestID)
	resultSubject := fmt.Sprintf("workflow.result.plan-reviewer.%s", plan.Slug)

	// Subscribe to result before publishing trigger
	// Use DeliverLastPerSubjectPolicy to catch messages even if consumer setup is slow
	sub, err := js.CreateConsumer(ctx, "WORKFLOWS", jetstream.ConsumerConfig{
		Name:          consumerName,
		FilterSubject: resultSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		InactiveThreshold: 5 * time.Minute, // Auto-cleanup if not used
	})
	if err != nil {
		return nil, fmt.Errorf("create result consumer: %w", err)
	}

	// Ensure consumer cleanup on exit
	defer func() {
		if err := js.DeleteConsumer(ctx, "WORKFLOWS", consumerName); err != nil {
			cmdCtx.Logger.Debug("Failed to cleanup consumer", "name", consumerName, "error", err)
		}
	}()

	// Publish trigger to plan-reviewer
	_, err = js.Publish(ctx, "workflow.trigger.plan-reviewer", data)
	if err != nil {
		return nil, fmt.Errorf("publish trigger: %w", err)
	}

	cmdCtx.Logger.Info("Triggered plan review",
		"slug", plan.Slug,
		"request_id", requestID)

	// Wait for result with timeout
	msgs, err := sub.Fetch(1, jetstream.FetchMaxWait(2*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("fetch result: %w", err)
	}

	for msg := range msgs.Messages() {
		// Parse result
		var resultMsg message.BaseMessage
		if err := json.Unmarshal(msg.Data(), &resultMsg); err != nil {
			if err := msg.Nak(); err != nil {
				cmdCtx.Logger.Warn("Failed to NAK message", "error", err)
			}
			return nil, fmt.Errorf("parse result message: %w", err)
		}

		// Extract payload
		payloadBytes, err := json.Marshal(resultMsg.Payload())
		if err != nil {
			if err := msg.Nak(); err != nil {
				cmdCtx.Logger.Warn("Failed to NAK message", "error", err)
			}
			return nil, fmt.Errorf("marshal payload: %w", err)
		}

		var result planreviewer.PlanReviewResult
		if err := json.Unmarshal(payloadBytes, &result); err != nil {
			if err := msg.Nak(); err != nil {
				cmdCtx.Logger.Warn("Failed to NAK message", "error", err)
			}
			return nil, fmt.Errorf("parse result payload: %w", err)
		}

		if err := msg.Ack(); err != nil {
			cmdCtx.Logger.Warn("Failed to ACK message", "error", err)
		}

		// Convert to prompts.PlanReviewResult
		return &prompts.PlanReviewResult{
			Verdict:  result.Verdict,
			Summary:  result.Summary,
			Findings: result.Findings,
		}, nil
	}

	return nil, fmt.Errorf("no result received")
}

// formatReviewBlockedResponse formats the response when plan review blocks approval.
func formatReviewBlockedResponse(plan *workflow.Plan, result *prompts.PlanReviewResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Plan Review Failed: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Needs Changes\n\n")

	sb.WriteString("### Summary\n\n")
	sb.WriteString(result.Summary)
	sb.WriteString("\n\n")

	// Show findings
	sb.WriteString(result.FormatFindings())

	sb.WriteString("### Resolution\n\n")
	sb.WriteString("Address the violations above, then run `/approve` again.\n\n")
	sb.WriteString("To bypass SOP review (not recommended):\n")
	sb.WriteString(fmt.Sprintf("```\n/approve %s --skip-review\n```\n", plan.Slug))

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
