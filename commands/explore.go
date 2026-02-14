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

// ExplorationRole is the role name for explorer agent loops.
const ExplorationRole = "explorer"

// ExploreCommand implements the /explore command for creating uncommitted explorations.
type ExploreCommand struct{}

// Config returns the command configuration.
func (c *ExploreCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/explore\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/explore <topic> [-m|--manual] - Create an exploration with LLM assistance (use -m to skip LLM)",
	}
}

// Execute runs the explore command to create a new uncommitted exploration.
func (c *ExploreCommand) Execute(
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

	// Parse arguments (manual flag skips LLM, default is LLM-assisted)
	topic, skipLLM, showHelp := parseExploreArgs(rawArgs)

	// Show help if requested or no topic provided
	if showHelp || topic == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     exploreHelpText(),
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

	// Generate slug from topic
	slug := workflow.Slugify(topic)

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
				Content:     fmt.Sprintf("Failed to load existing exploration: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}

		cmdCtx.Logger.Info("Loaded existing exploration",
			"user_id", msg.UserID,
			"slug", slug,
			"committed", plan.Committed)

		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     formatExistingExplorationResponse(plan),
			Timestamp:   time.Now(),
		}, nil
	}

	// Create new exploration (uncommitted plan)
	plan, err := manager.CreatePlan(ctx, slug, topic)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to create exploration: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	// Note: We do NOT call PromotePlan - explorations stay uncommitted

	cmdCtx.Logger.Info("Created exploration",
		"user_id", msg.UserID,
		"slug", slug,
		"plan_id", plan.ID)

	// Default is LLM-assisted; skip if -m/--manual flag is set
	if skipLLM {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeResult,
			Content:     formatNewExplorationResponse(plan),
			Timestamp:   time.Now(),
		}, nil
	}

	// Trigger the explorer loop (LLM is default)
	return c.startExplorerLoop(ctx, cmdCtx, msg, plan)
}

// startExplorerLoop triggers the explorer processor to generate exploration content.
func (c *ExploreCommand) startExplorerLoop(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	plan *workflow.Plan,
) (agentic.UserResponse, error) {
	requestID := uuid.New().String()

	// Build the prompt for exploration
	systemPrompt := prompts.ExplorerSystemPrompt()
	userPrompt := prompts.ExplorerPromptWithTopic(plan.Title)
	fullPrompt := systemPrompt + "\n\n" + userPrompt

	// Create workflow trigger payload for the explorer processor
	triggerPayload := &workflow.WorkflowTriggerPayload{
		WorkflowID:  "explorer",
		Role:        ExplorationRole,
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

	// Wrap in base message
	baseMsg := message.NewBaseMessage(
		workflow.WorkflowTriggerType,
		triggerPayload,
		"semspec",
	)

	data, err := json.Marshal(baseMsg)
	if err != nil {
		cmdCtx.Logger.Error("Failed to marshal explorer trigger", "error", err)
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

	// Publish to workflow.trigger.explorer subject
	subject := "workflow.trigger.explorer"
	if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, data); err != nil {
		cmdCtx.Logger.Error("Failed to publish explorer trigger",
			"error", err,
			"subject", subject)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to start exploration: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Triggered explorer processor",
		"request_id", requestID,
		"slug", plan.Slug,
		"subject", subject,
		"trace_id", tc.TraceID)

	return agentic.UserResponse{
		ResponseID:  requestID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeStatus,
		Content:     formatExplorerStartedResponse(plan, requestID, tc.TraceID),
		Timestamp:   time.Now(),
	}, nil
}

// parseExploreArgs parses the topic and flags from command arguments.
// Returns skipLLM=true when -m/--manual flag is present.
func parseExploreArgs(rawArgs string) (topic string, skipLLM bool, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var topicParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
		case part == "--manual" || part == "-m":
			skipLLM = true
		case strings.HasPrefix(part, "--"):
			// Skip unknown flags
			continue
		default:
			topicParts = append(topicParts, part)
		}
	}

	topic = strings.Join(topicParts, " ")
	return
}

// exploreHelpText returns the help text for the /explore command.
func exploreHelpText() string {
	return `## /explore - Create an Exploration

**Usage:** ` + "`/explore <topic> [-m|--manual]`" + `

Creates an uncommitted exploration (scratchpad) for brainstorming and research.
Explorations are not visible to execution until promoted to a committed plan.
By default, the LLM guides exploration with clarifying questions.

**Flags:**
- ` + "`--manual`" + ` or ` + "`-m`" + `: Skip LLM, create exploration for manual editing

**Examples:**
` + "```" + `
/explore authentication options            # LLM asks questions, fills Goal/Context/Scope (default)
/explore authentication options -m         # Create exploration manually
/explore database schema redesign --manual # Same as above
` + "```" + `

**Default (LLM-assisted):**
The LLM will:
1. Read relevant codebase files
2. Ask 2-4 clarifying questions about requirements
3. Produce a Goal/Context/Scope structure when enough info is gathered

**Manual Exploration (-m):**
1. Create exploration with ` + "`/explore <topic> -m`" + `
2. Edit plan.json to add Goal, Context, Scope
3. When ready, run ` + "`/promote <slug>`" + ` to commit the plan
4. Run ` + "`/tasks <slug> --generate`" + ` to create tasks

**Related Commands:**
- ` + "`/plan <title>`" + ` - Create a committed plan directly
- ` + "`/promote <slug>`" + ` - Promote exploration to committed plan
- ` + "`/tasks <slug> --generate`" + ` - Generate tasks from plan
`
}

// formatNewExplorationResponse formats the response for a newly created exploration.
func formatNewExplorationResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Exploration Created: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString("**Status:** Exploration (uncommitted)\n\n")

	sb.WriteString("**Location:** `.semspec/changes/" + plan.Slug + "/plan.json`\n\n")

	sb.WriteString("### Next Steps\n\n")
	sb.WriteString("1. Edit the plan file to capture your research:\n")
	sb.WriteString("   - **Situation**: Current state and context\n")
	sb.WriteString("   - **Mission**: What you're exploring and why\n")
	sb.WriteString("   - **Execution**: Potential approaches (numbered items)\n")
	sb.WriteString("   - **Constraints**: Known limitations\n")
	sb.WriteString("   - **Coordination**: Dependencies to consider\n\n")
	sb.WriteString(fmt.Sprintf("2. When ready, run `/promote %s` to commit the plan\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("3. Then run `/execute %s` to generate tasks\n", plan.Slug))

	return sb.String()
}

// formatExistingExplorationResponse formats the response for an existing exploration.
func formatExistingExplorationResponse(plan *workflow.Plan) string {
	var sb strings.Builder

	if plan.Committed {
		// It's already a committed plan, not an exploration
		sb.WriteString(fmt.Sprintf("## Existing Plan: %s\n\n", plan.Title))
		sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
		sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
		sb.WriteString("**Status:** Committed")
		if plan.CommittedAt != nil {
			sb.WriteString(fmt.Sprintf(" (at %s)", plan.CommittedAt.Format(time.RFC3339)))
		}
		sb.WriteString("\n\n")
		sb.WriteString("This is already a committed plan, not an exploration.\n\n")
		sb.WriteString("**Available Actions:**\n")
		sb.WriteString(fmt.Sprintf("- `/execute %s` - Generate tasks and begin execution\n", plan.Slug))
	} else {
		sb.WriteString(fmt.Sprintf("## Existing Exploration: %s\n\n", plan.Title))
		sb.WriteString(fmt.Sprintf("**ID:** `%s`\n", plan.ID))
		sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
		sb.WriteString("**Status:** Exploration (uncommitted)\n\n")

		sb.WriteString("**Location:** `.semspec/changes/" + plan.Slug + "/plan.json`\n\n")

		// Show plan summary if populated
		if plan.Goal != "" || plan.Context != "" {
			sb.WriteString("### Exploration Summary\n\n")
			if plan.Goal != "" {
				sb.WriteString(fmt.Sprintf("**Goal:** %s\n\n", truncateText(plan.Goal, 200)))
			}
			if plan.Context != "" {
				sb.WriteString(fmt.Sprintf("**Context:** %s\n\n", truncateText(plan.Context, 200)))
			}
		}

		sb.WriteString("### Available Actions\n\n")
		sb.WriteString(fmt.Sprintf("- `/promote %s` - Promote to committed plan\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("- Edit `.semspec/changes/%s/plan.json` to continue exploring\n", plan.Slug))
	}

	return sb.String()
}

// formatExplorerStartedResponse formats the response when an explorer loop is started.
func formatExplorerStartedResponse(plan *workflow.Plan, loopID, traceID string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Exploration Started: %s\n\n", plan.Title))
	sb.WriteString(fmt.Sprintf("**Slug:** `%s`\n", plan.Slug))
	sb.WriteString(fmt.Sprintf("**Loop ID:** `%s`\n", loopID))
	sb.WriteString("**Status:** Exploring with LLM assistance\n\n")

	sb.WriteString("The LLM is analyzing the codebase and will ask clarifying questions.\n")
	sb.WriteString("Answer questions to help refine the Goal, Context, and Scope.\n\n")

	sb.WriteString("### What Happens Next\n\n")
	sb.WriteString("1. LLM reads relevant codebase files\n")
	sb.WriteString("2. LLM asks 2-4 clarifying questions\n")
	sb.WriteString("3. You answer questions (via `/answer` or inline)\n")
	sb.WriteString("4. LLM produces Goal/Context/Scope structure\n")
	sb.WriteString(fmt.Sprintf("5. Plan saved to `.semspec/changes/%s/plan.json`\n\n", plan.Slug))

	sb.WriteString("### Tracking\n\n")
	sb.WriteString(fmt.Sprintf("**Trace ID:** `%s`\n", traceID))
	sb.WriteString(fmt.Sprintf("Debug: `/debug trace %s`\n", traceID))

	return sb.String()
}
