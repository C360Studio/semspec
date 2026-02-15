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

// PlanOptions contains parsed options for the plan command.
type PlanOptions struct {
	Title       string
	SkipLLM     bool
	ShowHelp    bool
	AutoApprove bool     // If true, auto-approve the plan (skip approval gate)
	Parallel    int      // 0=auto (LLM decides), 1=force single, -1=disabled
	Focuses     []string // Optional explicit focus areas
}

// Config returns the command configuration.
func (c *PlanCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/plan\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/plan <title> [-m|--manual] [-a|--auto] [-p N] [--focus areas] - Create a draft plan with LLM assistance",
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
	opts := parsePlanArgs(rawArgs)
	title := opts.Title
	skipLLM := opts.SkipLLM
	showHelp := opts.ShowHelp

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
			"approved", plan.Approved)

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

	// Auto-approve the plan only if --auto flag is set
	if opts.AutoApprove {
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
	}

	cmdCtx.Logger.Info("Created plan",
		"user_id", msg.UserID,
		"slug", slug,
		"plan_id", plan.ID,
		"auto_approved", opts.AutoApprove)

	// Default is LLM-assisted; skip if -m/--manual flag is set
	if skipLLM {
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

	// Trigger planning (coordinator or single planner based on options)
	return c.startPlannerLoop(ctx, cmdCtx, msg, plan, opts)
}

// startPlannerLoop triggers the planner or coordinator to generate Goal/Context/Scope.
func (c *PlanCommand) startPlannerLoop(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	plan *workflow.Plan,
	opts PlanOptions,
) (agentic.UserResponse, error) {
	requestID := uuid.New().String()

	// Determine which processor to use
	// -p 1 forces single planner, otherwise use coordinator
	useCoordinator := opts.Parallel != 1

	var subject string
	var data []byte
	var err error

	if useCoordinator {
		// Use plan coordinator for concurrent planning
		triggerPayload := &workflow.PlanCoordinatorTrigger{
			WorkflowTriggerPayload: &workflow.WorkflowTriggerPayload{
				WorkflowID:  "plan-coordinator",
				Role:        PlannerRole,
				UserID:      msg.UserID,
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				RequestID:   requestID,
				Data: &workflow.WorkflowTriggerData{
					Slug:        plan.Slug,
					Title:       plan.Title,
					Description: plan.Title,
					Auto:        true,
				},
			},
			Focuses:     opts.Focuses,
			MaxPlanners: opts.Parallel,
		}
		subject = "workflow.trigger.plan-coordinator"

		baseMsg := message.NewBaseMessage(
			workflow.PlanCoordinatorTriggerType,
			triggerPayload,
			"semspec",
		)
		data, err = json.Marshal(baseMsg)
	} else {
		// Use single planner directly
		systemPrompt := prompts.PlannerSystemPrompt()
		userPrompt := prompts.PlannerPromptWithTitle(plan.Title)
		fullPrompt := systemPrompt + "\n\n" + userPrompt

		triggerPayload := &workflow.WorkflowTriggerPayload{
			WorkflowID:  "planner",
			Role:        PlannerRole,
			Prompt:      fullPrompt,
			UserID:      msg.UserID,
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			RequestID:   requestID,
			Data: &workflow.WorkflowTriggerData{
				Slug:        plan.Slug,
				Title:       plan.Title,
				Description: plan.Title,
				Auto:        true,
			},
		}
		subject = "workflow.trigger.planner"

		baseMsg := message.NewBaseMessage(
			workflow.WorkflowTriggerType,
			triggerPayload,
			"semspec",
		)
		data, err = json.Marshal(baseMsg)
	}
	if err != nil {
		cmdCtx.Logger.Error("Failed to marshal planner trigger", "error", err)
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

	// Publish trigger
	if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, data); err != nil {
		cmdCtx.Logger.Error("Failed to publish planner trigger",
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

	cmdCtx.Logger.Info("Triggered planning",
		"request_id", requestID,
		"slug", plan.Slug,
		"subject", subject,
		"trace_id", tc.TraceID,
		"use_coordinator", useCoordinator)

	return agentic.UserResponse{
		ResponseID:  requestID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   requestID,
		Type:        agentic.ResponseTypeStatus,
		Content:     formatPlannerStartedResponse(plan, requestID, tc.TraceID),
		Timestamp:   time.Now(),
	}, nil
}

// parsePlanArgs parses the title and flags from command arguments.
func parsePlanArgs(rawArgs string) PlanOptions {
	parts := strings.Fields(rawArgs)
	var titleParts []string
	opts := PlanOptions{}

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			opts.ShowHelp = true
		case part == "--manual" || part == "-m":
			opts.SkipLLM = true
		case part == "--auto" || part == "-a":
			opts.AutoApprove = true
		case part == "-p" || part == "--parallel":
			// Next argument should be the count
			if i+1 < len(parts) {
				i++
				var n int
				if _, err := fmt.Sscanf(parts[i], "%d", &n); err == nil {
					opts.Parallel = n
				}
			}
		case part == "--focus":
			// Next argument should be comma-separated focus areas
			if i+1 < len(parts) {
				i++
				opts.Focuses = strings.Split(parts[i], ",")
			}
		case strings.HasPrefix(part, "-p"):
			// Handle -p1, -p2, -p3 (no space)
			var n int
			if _, err := fmt.Sscanf(part[2:], "%d", &n); err == nil {
				opts.Parallel = n
			}
		case strings.HasPrefix(part, "--focus="):
			// Handle --focus=api,security
			opts.Focuses = strings.Split(strings.TrimPrefix(part, "--focus="), ",")
		case strings.HasPrefix(part, "--"):
			// Skip unknown flags
			continue
		default:
			titleParts = append(titleParts, part)
		}
	}

	opts.Title = strings.Join(titleParts, " ")
	return opts
}

// planHelpText returns the help text for the /plan command.
func planHelpText() string {
	return `## /plan - Create a Draft Plan

**Usage:** ` + "`/plan <title> [-m|--manual] [-a|--auto] [-p N] [--focus areas]`" + `

Creates a new draft plan that requires approval before task generation.
By default, the LLM coordinator analyzes the codebase and spawns 1-3 focused
planners to generate a comprehensive Goal/Context/Scope.

**Flags:**
- ` + "`--manual`" + ` or ` + "`-m`" + `: Skip LLM, create plan stub for manual editing
- ` + "`--auto`" + ` or ` + "`-a`" + `: Auto-approve the plan (skip approval gate)
- ` + "`-p N`" + ` or ` + "`--parallel N`" + `: Control planner count (1=single, 0=auto)
- ` + "`--focus areas`" + `: Explicit comma-separated focus areas (e.g., ` + "`--focus api,security`" + `)

**Examples:**
` + "```" + `
/plan Add authentication refresh            # Create draft, requires /approve
/plan Add authentication refresh --auto     # Auto-approve, skip approval gate
/plan Add authentication refresh -p 1       # Force single planner (skip coordinator)
/plan Add auth refresh --focus api,security # Explicit focus areas
/plan Fix database pooling -m               # Create plan manually (no LLM)
` + "```" + `

**Default Workflow (approval required):**
1. ` + "`/plan <title>`" + ` - Create draft plan
2. Review and refine plan.json
3. ` + "`/approve <slug>`" + ` - Approve the plan
4. ` + "`/tasks <slug> --generate`" + ` - Generate tasks
5. ` + "`/execute <slug> --run`" + ` - Execute the plan

**Auto Workflow (--auto flag):**
1. ` + "`/plan <title> --auto`" + ` - Create and auto-approve plan
2. ` + "`/tasks <slug> --generate`" + ` - Generate tasks immediately
3. ` + "`/execute <slug> --run`" + ` - Execute the plan

**Planning Modes:**

1. **Coordinator Mode (default):** The LLM coordinator:
   - Queries the knowledge graph to understand the codebase
   - Decides optimal focus areas (api, security, data, etc.)
   - Spawns 1-3 focused planners concurrently
   - Synthesizes results into a unified plan

2. **Single Planner Mode (-p 1):** Single planner analyzes everything directly

3. **Manual Mode (-m):** No LLM, create stub for manual editing

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
- ` + "`/approve <slug>`" + ` - Approve a draft plan
- ` + "`/tasks <slug> --generate`" + ` - Generate tasks from approved plan
- ` + "`/execute <slug> --run`" + ` - Execute tasks from a plan
`
}

// formatNewPlanResponse formats the response for a newly created plan.
func formatNewPlanResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Plan Created: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	if plan.Approved {
		sb.WriteString("**Status:** Approved\n\n")
	} else {
		sb.WriteString("**Status:** Draft (pending approval)\n\n")
	}

	sb.WriteString("**Location:** `.semspec/changes/" + plan.Slug + "/plan.json`\n\n")

	sb.WriteString("### Next Steps\n\n")
	sb.WriteString("1. Edit the plan file to set:\n")
	sb.WriteString("   - **goal**: What we're building or fixing\n")
	sb.WriteString("   - **context**: Current state and why this matters\n")
	sb.WriteString("   - **scope**: Files to include, exclude, protect\n\n")
	if plan.Approved {
		sb.WriteString(fmt.Sprintf("2. Run `/tasks %s --generate` to create tasks with acceptance criteria\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("3. Run `/execute %s --run` to begin execution\n", plan.Slug))
	} else {
		sb.WriteString(fmt.Sprintf("2. Run `/approve %s` to approve the plan\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("3. Run `/tasks %s --generate` to create tasks with acceptance criteria\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("4. Run `/execute %s --run` to begin execution\n", plan.Slug))
	}

	return sb.String()
}

// formatExistingPlanResponse formats the response for an existing plan.
func formatExistingPlanResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Existing Plan: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))

	if plan.Approved {
		sb.WriteString("**Status:** Approved")
		if plan.ApprovedAt != nil {
			sb.WriteString(fmt.Sprintf(" (at %s)", plan.ApprovedAt.Format(time.RFC3339)))
		}
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("**Status:** Draft (pending approval)\n\n")
	}

	sb.WriteString("**Location:** `.semspec/changes/" + plan.Slug + "/plan.json`\n\n")

	// Show plan summary if populated
	if plan.Goal != "" || plan.Context != "" {
		sb.WriteString("### Plan Summary\n\n")
		if plan.Goal != "" {
			sb.WriteString(fmt.Sprintf("**Goal:** %s\n\n", truncateText(plan.Goal, 200)))
		}
		if plan.Context != "" {
			sb.WriteString(fmt.Sprintf("**Context:** %s\n\n", truncateText(plan.Context, 200)))
		}
		// Show scope if defined
		scopeCount := len(plan.Scope.Include) + len(plan.Scope.Exclude) + len(plan.Scope.DoNotTouch)
		if scopeCount > 0 {
			sb.WriteString(fmt.Sprintf("**Scope:** %d include, %d exclude, %d protected\n\n",
				len(plan.Scope.Include), len(plan.Scope.Exclude), len(plan.Scope.DoNotTouch)))
		}
	}

	sb.WriteString("### Available Actions\n\n")
	if plan.Approved {
		sb.WriteString(fmt.Sprintf("- `/tasks %s` - View tasks\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("- `/tasks %s --generate` - Generate tasks from plan\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("- `/execute %s --run` - Execute tasks\n", plan.Slug))
	} else {
		sb.WriteString(fmt.Sprintf("- `/approve %s` - Approve the plan for execution\n", plan.Slug))
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

// formatPlannerStartedResponse formats the response when the planner processor is triggered.
func formatPlannerStartedResponse(plan *workflow.Plan, requestID, traceID string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Planning Started: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("**Request ID:** `%s`\n", requestID))
	sb.WriteString("**Status:** Generating plan with LLM\n\n")

	sb.WriteString("The LLM is analyzing the codebase to generate Goal/Context/Scope.\n\n")

	sb.WriteString("### What Happens Next\n\n")
	sb.WriteString("1. LLM analyzes codebase and generates plan structure\n")
	sb.WriteString(fmt.Sprintf("2. Plan saved to `.semspec/changes/%s/plan.json`\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("3. Ready for `/tasks %s --generate`\n\n", plan.Slug))

	sb.WriteString("### Tracking\n\n")
	sb.WriteString(fmt.Sprintf("**Trace ID:** `%s`\n", traceID))
	sb.WriteString(fmt.Sprintf("Debug: `/debug trace %s`\n", traceID))

	return sb.String()
}
