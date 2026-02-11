package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/c360studio/semspec/model"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/prompts"
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// TasksCommand implements the /tasks command for generating task lists.
type TasksCommand struct{}

// Config returns the command configuration.
func (c *TasksCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/tasks\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/tasks <slug> [--capability <cap>] [--model <model>] - Generate task list using LLM",
	}
}

// Execute runs the tasks command by triggering an agentic loop for task generation.
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

	if rawArgs == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Usage: /tasks <slug> [--model <model>]",
			Timestamp:   time.Now(),
		}, nil
	}

	// Parse flags from args (tasks doesn't support --auto since it's the final step)
	slug, _, capabilityStr, modelOverride := parseWorkflowArgsWithCapability(rawArgs)

	if slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Change slug is required. Usage: /tasks <slug> [--capability <cap>] [--model <model>]",
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

	// Load the change
	change, err := manager.LoadChange(slug)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Change not found: %s\n\nRun `/propose <description>` first to create a proposal.", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Check for prerequisite documents
	var warnings []string
	if !change.Files.HasProposal {
		warnings = append(warnings, "No proposal found. Task generation may lack context.")
	}
	if !change.Files.HasDesign {
		warnings = append(warnings, "No design found. Consider running `/design "+slug+"` first.")
	}
	if !change.Files.HasSpec {
		warnings = append(warnings, "No spec found. Consider running `/spec "+slug+"` first for better task breakdown.")
	}

	// Resolve model using capability-based selection
	registry := GetModelRegistry()
	role := prompts.TasksWriterRole()

	var primaryModel string
	var fallbackChain []string
	var capability model.Capability

	if modelOverride != "" {
		// Direct model override bypasses registry
		primaryModel = modelOverride
		capability = model.CapabilityForRole(role)
	} else if capabilityStr != "" {
		// Explicit capability specified
		capability = model.ParseCapability(capabilityStr)
		if capability == "" {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Invalid capability: %s. Valid options: planning, writing, coding, reviewing, fast", capabilityStr),
				Timestamp:   time.Now(),
			}, nil
		}
		primaryModel = registry.Resolve(capability)
		chain := registry.GetFallbackChain(capability)
		if len(chain) > 1 {
			fallbackChain = chain[1:] // Exclude primary
		}
	} else {
		// Use role's default capability
		capability = model.CapabilityForRole(role)
		primaryModel = registry.ForRole(role)
		fallbackChain = registry.GetFallbackChainForRole(role)
		if len(fallbackChain) > 0 {
			fallbackChain = fallbackChain[1:] // Exclude primary
		}
	}

	// Build the workflow task payload
	taskID := uuid.New().String()
	prompt := prompts.TasksWriterPrompt(change.Slug, change.Title)

	payload := &workflow.WorkflowTaskPayload{
		TaskID:        taskID,
		WorkflowID:    workflow.DocumentGenerationWorkflowID,
		Role:          role,
		Model:         primaryModel,
		FallbackChain: fallbackChain,
		Capability:    capability.String(),
		WorkflowSlug:  change.Slug,
		WorkflowStep:  "tasks",
		Title:         change.Title,
		Description:   change.Description,
		Prompt:        prompt,
		AutoContinue:  false, // Tasks is the final step
		UserID:        msg.UserID,
		ChannelType:   msg.ChannelType,
		ChannelID:     msg.ChannelID,
	}

	// Wrap in BaseMessage and publish to agent.task.workflow
	baseMsg := message.NewBaseMessage(workflow.WorkflowTaskType, payload, "semspec")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		cmdCtx.Logger.Error("Failed to marshal workflow task", "error", err)
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

	// Create trace context before publishing so we can return it to user
	tc := natsclient.NewTraceContext()
	ctx = natsclient.ContextWithTrace(ctx, tc)

	// Publish to the workflow task subject
	subject := "agent.task.workflow"
	if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, data); err != nil {
		cmdCtx.Logger.Error("Failed to publish workflow task",
			"error", err,
			"subject", subject)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to submit workflow task: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Published workflow task",
		"task_id", taskID,
		"slug", change.Slug,
		"role", role,
		"model", primaryModel,
		"capability", capability.String(),
		"fallback_count", len(fallbackChain),
		"trace_id", tc.TraceID,
		"user_id", msg.UserID)

	// Build response message
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Creating task list for: **%s**\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("Workflow: `.semspec/changes/%s/`\n", change.Slug))

	if len(warnings) > 0 {
		sb.WriteString("\n**Warnings:**\n")
		for _, w := range warnings {
			sb.WriteString(fmt.Sprintf("- %s\n", w))
		}
	}

	sb.WriteString(fmt.Sprintf("\nModel: %s (capability: %s)\n", primaryModel, capability.String()))
	if len(fallbackChain) > 0 {
		sb.WriteString(fmt.Sprintf("Fallbacks: %s\n", strings.Join(fallbackChain, ", ")))
	}
	sb.WriteString(fmt.Sprintf("\n**Trace ID:** `%s`\n", tc.TraceID))
	sb.WriteString(fmt.Sprintf("Debug: `/debug trace %s`\n", tc.TraceID))
	sb.WriteString("\nGenerating task list using LLM...")
	sb.WriteString("\n\n*This is the final workflow step. After generation, run `/check " + slug + "` to validate.*")

	return agentic.UserResponse{
		ResponseID:  taskID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   taskID, // Track the workflow task for async responses
		Type:        agentic.ResponseTypeStatus,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}
