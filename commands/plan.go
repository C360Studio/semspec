package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// PlannerRole is the role name for planner agent loops.
const PlannerRole = "planner"

// PlanCommand implements the /plan command for creating committed plans.
type PlanCommand struct{}

// Config returns the command configuration.
func (c *PlanCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/plan\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/plan <title> [--llm] - Create a new committed plan with optional LLM assistance",
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
	title, useLLM, showHelp := parsePlanArgs(rawArgs)

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

	// If --llm flag is set, start a planner loop
	if useLLM {
		return c.startPlannerLoop(ctx, cmdCtx, msg, plan)
	}

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

// startPlannerLoop starts an LLM agent loop to create the plan.
func (c *PlanCommand) startPlannerLoop(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	plan *workflow.Plan,
) (agentic.UserResponse, error) {
	plannerLoopID := "loop_" + uuid.New().String()[:8]
	taskID := uuid.New().String()

	// Build the prompt for planning
	systemPrompt := prompts.PlannerSystemPrompt()
	userPrompt := prompts.PlannerPromptWithTitle(plan.Title)
	fullPrompt := systemPrompt + "\n\n" + userPrompt

	// Create task message to start the planner loop
	task := agentic.TaskMessage{
		LoopID:       plannerLoopID,
		TaskID:       taskID,
		Role:         PlannerRole,
		Model:        "qwen", // Default model, will be resolved by agentic-model
		Prompt:       fullPrompt,
		WorkflowSlug: plan.Slug,
		ChannelType:  msg.ChannelType,
		ChannelID:    msg.ChannelID,
		UserID:       msg.UserID,
	}

	// Wrap in base message
	baseMsg := message.NewBaseMessage(
		message.Type{Domain: "agentic", Category: "task", Version: "v1"},
		&task,
		"semspec",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		cmdCtx.Logger.Error("Failed to marshal planner task", "error", err)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Internal error: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Create trace context
	tc := natsclient.NewTraceContext()
	ctx = natsclient.ContextWithTrace(ctx, tc)

	// Publish to agent.task.planner subject
	subject := "agent.task." + PlannerRole
	if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, data); err != nil {
		cmdCtx.Logger.Error("Failed to publish planner task",
			"error", err,
			"subject", subject)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to start planning: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Started planner loop",
		"loop_id", plannerLoopID,
		"task_id", taskID,
		"slug", plan.Slug,
		"subject", subject,
		"trace_id", tc.TraceID)

	return agentic.UserResponse{
		ResponseID:  taskID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   plannerLoopID,
		Type:        agentic.ResponseTypeStatus,
		Content:     formatPlannerStartedResponse(plan, plannerLoopID, tc.TraceID),
		Timestamp:   time.Now(),
	}, nil
}

// parsePlanArgs parses the title and flags from command arguments.
func parsePlanArgs(rawArgs string) (title string, useLLM bool, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var titleParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
		case part == "--llm" || part == "-l":
			useLLM = true
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

**Usage:** ` + "`/plan <title> [--llm]`" + `

Creates a new plan ready for task generation and execution.

**Flags:**
- ` + "`--llm`" + ` or ` + "`-l`" + `: Start an LLM-guided planning session

**Examples:**
` + "```" + `
/plan Add authentication refresh            # Create plan manually
/plan Add authentication refresh --llm      # LLM creates Goal/Context/Scope
/plan Fix database pooling -l               # Same as above
` + "```" + `

**With --llm:**
The LLM will:
1. Read the codebase to understand context
2. Ask clarifying questions if needed
3. Produce a complete Goal/Context/Scope structure

**Manual Planning:**
1. Create plan with ` + "`/plan <title>`" + `
2. Edit plan.json to set Goal, Context, and Scope
3. Run ` + "`/tasks <slug> --generate`" + ` to create tasks
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
- ` + "`/explore <topic>`" + ` - Create an uncommitted exploration
- ` + "`/tasks <slug> --generate`" + ` - Generate tasks from plan
- ` + "`/execute <slug> --run`" + ` - Execute tasks from a plan
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

// formatPlannerStartedResponse formats the response when a planner loop is started.
func formatPlannerStartedResponse(plan *workflow.Plan, loopID, traceID string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Planning Started: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("**Loop ID:** `%s`\n", loopID))
	sb.WriteString("**Status:** Planning with LLM assistance\n\n")

	sb.WriteString("The LLM is analyzing the codebase to create a complete plan.\n")
	sb.WriteString("It will ask questions if any critical information is missing.\n\n")

	sb.WriteString("### What Happens Next\n\n")
	sb.WriteString("1. LLM reads relevant codebase files\n")
	sb.WriteString("2. LLM asks clarifying questions (if needed)\n")
	sb.WriteString("3. LLM produces Goal/Context/Scope structure\n")
	sb.WriteString(fmt.Sprintf("4. Plan saved to `.semspec/changes/%s/plan.json`\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("5. Ready for `/tasks %s --generate`\n\n", plan.Slug))

	sb.WriteString("### Tracking\n\n")
	sb.WriteString(fmt.Sprintf("**Trace ID:** `%s`\n", traceID))
	sb.WriteString(fmt.Sprintf("Debug: `/debug trace %s`\n", traceID))

	return sb.String()
}
