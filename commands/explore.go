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

// ExploreCommand implements the /explore command for creating uncommitted explorations.
type ExploreCommand struct{}

// Config returns the command configuration.
func (c *ExploreCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/explore\s*(.*)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/explore <topic> - Create an uncommitted exploration (scratchpad)",
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

	// Parse arguments
	topic, showHelp := parseExploreArgs(rawArgs)

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

// parseExploreArgs parses the topic and flags from command arguments.
func parseExploreArgs(rawArgs string) (topic string, showHelp bool) {
	parts := strings.Fields(rawArgs)
	var topicParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--help" || part == "-h":
			showHelp = true
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

**Usage:** ` + "`/explore <topic>`" + `

Creates an uncommitted exploration (scratchpad) for brainstorming and research.
Explorations are not visible to execution until promoted to a committed plan.

**Examples:**
` + "```" + `
/explore authentication options
/explore database schema redesign
/explore performance optimization strategies
` + "```" + `

**Exploration vs Plan:**
- **Exploration** (uncommitted): Scratchpad for ideas, can be modified freely
- **Plan** (committed): Frozen intent document, drives task generation

**Workflow:**
1. Create exploration with ` + "`/explore <topic>`" + `
2. Fill in SMEAC sections as you research
3. When ready, run ` + "`/promote <slug>`" + ` to commit the plan
4. Run ` + "`/execute <slug>`" + ` to generate tasks and begin work

**Related Commands:**
- ` + "`/plan <title>`" + ` - Create a committed plan directly (skip exploration)
- ` + "`/promote <slug>`" + ` - Promote exploration to committed plan
- ` + "`/execute <slug>`" + ` - Generate tasks and execute (coming soon)
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

		// Show SMEAC summary if populated
		hasSMEAC := plan.Situation != "" || plan.Mission != "" || plan.Execution != ""
		if hasSMEAC {
			sb.WriteString("### Exploration Summary\n\n")
			if plan.Situation != "" {
				sb.WriteString(fmt.Sprintf("**Situation:** %s\n\n", truncateText(plan.Situation, 200)))
			}
			if plan.Mission != "" {
				sb.WriteString(fmt.Sprintf("**Mission:** %s\n\n", truncateText(plan.Mission, 200)))
			}
			if plan.Execution != "" {
				sb.WriteString(fmt.Sprintf("**Execution:** %d steps drafted\n\n", countExecutionSteps(plan.Execution)))
			}
		}

		sb.WriteString("### Available Actions\n\n")
		sb.WriteString(fmt.Sprintf("- `/promote %s` - Promote to committed plan\n", plan.Slug))
		sb.WriteString(fmt.Sprintf("- Edit `.semspec/changes/%s/plan.json` to continue exploring\n", plan.Slug))
	}

	return sb.String()
}
