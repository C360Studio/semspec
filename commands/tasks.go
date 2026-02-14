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

// TaskGenerationWorkflowID is the workflow ID for task generation.
const TaskGenerationWorkflowID = "task-generator"

// TasksCommand implements the /tasks command for viewing and generating tasks.
type TasksCommand struct{}

// Config returns the command configuration.
func (c *TasksCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/tasks\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/tasks <slug> [--list|--generate] - View or generate tasks from plan",
	}
}

// Execute runs the tasks command to list or generate tasks.
func (c *TasksCommand) Execute(
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
	slug, listMode, generateMode, showHelp := parseTasksArgs(rawArgs)

	// Show help if requested or no slug provided
	if showHelp || slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     tasksHelpText(),
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
			Content:     fmt.Sprintf("Plan not found: `%s`\n\nUse `/plan <title>` or `/explore <topic>` to create one first.", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Handle generate mode
	if generateMode {
		return c.triggerTaskGeneration(ctx, cmdCtx, msg, plan)
	}

	// Default to list mode (listMode is true by default if neither flag is set)
	_ = listMode // Used implicitly as default
	return c.listTasks(ctx, cmdCtx, msg, manager, plan)
}

// listTasks loads and displays existing tasks for a plan.
func (c *TasksCommand) listTasks(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	manager *workflow.Manager,
	plan *workflow.Plan,
) (agentic.UserResponse, error) {
	tasks, err := manager.LoadTasks(ctx, plan.Slug)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to load tasks: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Listed tasks for plan",
		"user_id", msg.UserID,
		"slug", plan.Slug,
		"task_count", len(tasks))

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     formatTaskListResponse(plan, tasks),
		Timestamp:   time.Now(),
	}, nil
}

// triggerTaskGeneration triggers the LLM to generate tasks from the plan.
func (c *TasksCommand) triggerTaskGeneration(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	plan *workflow.Plan,
) (agentic.UserResponse, error) {
	// Verify plan has Goal/Context set
	if plan.Goal == "" && plan.Context == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     formatNoGoalContextError(plan),
			Timestamp:   time.Now(),
		}, nil
	}

	// Build the prompt parameters
	params := prompts.TaskGeneratorParams{
		Title:          plan.Title,
		Goal:           plan.Goal,
		Context:        plan.Context,
		ScopeInclude:   plan.Scope.Include,
		ScopeExclude:   plan.Scope.Exclude,
		ScopeProtected: plan.Scope.DoNotTouch,
	}

	// Generate the prompt
	prompt := prompts.TaskGeneratorPrompt(params)

	requestID := uuid.New().String()

	// Build trigger payload for task generation workflow
	triggerPayload := &workflow.WorkflowTriggerPayload{
		WorkflowID: TaskGenerationWorkflowID,
		Role:       "task-generator",
		Prompt:     prompt,

		// Well-known routing fields
		UserID:      msg.UserID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,

		// Request tracking
		RequestID: requestID,

		// Semspec-specific fields
		Data: &workflow.WorkflowTriggerData{
			Slug:        plan.Slug,
			Title:       plan.Title,
			Description: plan.Goal,
			Auto:        true,
		},
	}

	baseMsg := message.NewBaseMessage(workflow.WorkflowTriggerType, triggerPayload, "semspec")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		cmdCtx.Logger.Error("Failed to marshal workflow trigger", "error", err)
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

	subject := "workflow.trigger." + TaskGenerationWorkflowID

	// Create trace context before publishing
	tc := natsclient.NewTraceContext()
	ctx = natsclient.ContextWithTrace(ctx, tc)

	// Publish to workflow trigger subject
	if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, data); err != nil {
		cmdCtx.Logger.Error("Failed to publish task generation workflow",
			"error", err,
			"subject", subject)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to trigger task generation: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Triggered task generation workflow",
		"request_id", requestID,
		"slug", plan.Slug,
		"subject", subject,
		"trace_id", tc.TraceID)

	return agentic.UserResponse{
		ResponseID:  requestID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   requestID,
		Type:        agentic.ResponseTypeStatus,
		Content:     formatTaskGenerationTriggeredResponse(plan, tc.TraceID),
		Timestamp:   time.Now(),
	}, nil
}

// parseTasksArgs parses the slug and flags from command arguments.
func parseTasksArgs(rawArgs string) (slug string, listMode bool, generateMode bool, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var slugParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
		case part == "--list" || part == "-l":
			listMode = true
		case part == "--generate" || part == "-g":
			generateMode = true
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

	// Default to list mode if neither flag is set
	if !listMode && !generateMode {
		listMode = true
	}

	return
}

// tasksHelpText returns the help text for the /tasks command.
func tasksHelpText() string {
	return `## /tasks - View or Generate Tasks

**Usage:** ` + "`/tasks <slug> [--list|--generate]`" + `

View existing tasks or generate new tasks from a plan using LLM.

**Examples:**
` + "```" + `
/tasks auth-refresh           # List existing tasks (default)
/tasks auth-refresh --list    # Same as above
/tasks auth-refresh --generate # Generate tasks from plan using LLM
` + "```" + `

**Flags:**
- ` + "`--list`" + ` or ` + "`-l`" + `: Show existing tasks (default)
- ` + "`--generate`" + ` or ` + "`-g`" + `: Generate tasks from plan's Goal/Context/Scope

**Task Generation:**
When using ` + "`--generate`" + `, the LLM creates 3-8 tasks with:
- Clear descriptions of what to implement
- BDD acceptance criteria (Given/When/Then)
- File references from the plan's scope

**Plan Fields for Generation:**
Tasks are generated from the plan's:
- **Goal**: What we're building (or Mission as fallback)
- **Context**: Current state (or Situation as fallback)
- **Scope**: Files to include/exclude/protect

**Output:**
Tasks are saved to ` + "`.semspec/changes/<slug>/tasks.json`" + `

**Related Commands:**
- ` + "`/plan <title>`" + ` - Create a plan
- ` + "`/execute <slug>`" + ` - Execute tasks from a plan
`
}

// formatTaskListResponse formats the response showing existing tasks.
func formatTaskListResponse(plan *workflow.Plan, tasks []workflow.Task) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Tasks: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**Plan:** `%s`\n", plan.ID))

	if len(tasks) == 0 {
		sb.WriteString("**Tasks:** None\n\n")
		sb.WriteString("No tasks have been generated for this plan yet.\n\n")
		sb.WriteString("**To generate tasks:**\n")
		sb.WriteString(fmt.Sprintf("1. Ensure the plan has Goal and Context set\n"))
		sb.WriteString(fmt.Sprintf("2. Run `/tasks %s --generate`\n", plan.Slug))
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("**Tasks:** %d\n\n", len(tasks)))

	// Count by status
	pending, inProgress, completed, failed := 0, 0, 0, 0
	for _, task := range tasks {
		switch task.Status {
		case workflow.TaskStatusPending:
			pending++
		case workflow.TaskStatusInProgress:
			inProgress++
		case workflow.TaskStatusCompleted:
			completed++
		case workflow.TaskStatusFailed:
			failed++
		}
	}

	sb.WriteString(fmt.Sprintf("**Status:** %d pending, %d in progress, %d completed, %d failed\n\n",
		pending, inProgress, completed, failed))

	sb.WriteString("### Task List\n\n")
	for _, task := range tasks {
		statusIcon := taskStatusIcon(task.Status)
		typeLabel := ""
		if task.Type != "" {
			typeLabel = fmt.Sprintf(" [%s]", task.Type)
		}
		sb.WriteString(fmt.Sprintf("%s **%s**%s: %s\n", statusIcon, task.ID, typeLabel, task.Description))

		// Show acceptance criteria if present
		if len(task.AcceptanceCriteria) > 0 {
			for _, ac := range task.AcceptanceCriteria {
				sb.WriteString(fmt.Sprintf("   - Given %s, when %s, then %s\n", ac.Given, ac.When, ac.Then))
			}
		}
	}

	sb.WriteString("\n### File Location\n\n")
	sb.WriteString(fmt.Sprintf("`.semspec/changes/%s/tasks.json`\n", plan.Slug))

	return sb.String()
}

// formatNoGoalContextError formats the error when plan lacks Goal/Context.
func formatNoGoalContextError(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Cannot Generate Tasks: %s\n\n", plan.Title))
	sb.WriteString("This plan needs **Goal** and **Context** fields to generate tasks.\n\n")
	sb.WriteString("**To fix:**\n\n")
	sb.WriteString(fmt.Sprintf("1. Edit `.semspec/changes/%s/plan.json`\n", plan.Slug))
	sb.WriteString("2. Add the following fields:\n")
	sb.WriteString("   ```json\n")
	sb.WriteString("   \"goal\": \"What we're building or fixing\",\n")
	sb.WriteString("   \"context\": \"Current state and why this matters\",\n")
	sb.WriteString("   \"scope\": {\n")
	sb.WriteString("     \"include\": [\"path/to/files\"],\n")
	sb.WriteString("     \"exclude\": [],\n")
	sb.WriteString("     \"do_not_touch\": []\n")
	sb.WriteString("   }\n")
	sb.WriteString("   ```\n")
	sb.WriteString(fmt.Sprintf("3. Run `/tasks %s --generate` again\n", plan.Slug))

	return sb.String()
}

// formatTaskGenerationTriggeredResponse formats the response when task generation is triggered.
func formatTaskGenerationTriggeredResponse(plan *workflow.Plan, traceID string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Task Generation Started: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**Plan:** `%s`\n", plan.ID))
	sb.WriteString("**Status:** Generating tasks...\n\n")

	sb.WriteString("The LLM is analyzing your plan and generating tasks with BDD acceptance criteria.\n\n")

	sb.WriteString("### Tracking\n\n")
	sb.WriteString(fmt.Sprintf("**Trace ID:** `%s`\n", traceID))
	sb.WriteString(fmt.Sprintf("Debug: `/debug trace %s`\n\n", traceID))

	sb.WriteString("### Next Steps\n\n")
	sb.WriteString(fmt.Sprintf("Once complete, run `/tasks %s` to see the generated tasks.\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("Then run `/execute %s --run` to start execution.\n", plan.Slug))

	return sb.String()
}

// taskStatusIcon returns an icon for the task status.
func taskStatusIcon(status workflow.TaskStatus) string {
	switch status {
	case workflow.TaskStatusPending:
		return "[ ]"
	case workflow.TaskStatusInProgress:
		return "[~]"
	case workflow.TaskStatusCompleted:
		return "[x]"
	case workflow.TaskStatusFailed:
		return "[!]"
	default:
		return "[ ]"
	}
}
