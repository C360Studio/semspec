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

// DesignCommand implements the /design command for creating technical designs.
type DesignCommand struct{}

// Config returns the command configuration.
func (c *DesignCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/design\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/design <slug> [--auto] [--capability <cap>] [--model <model>] - Create technical design using LLM",
	}
}

// Execute runs the design command by triggering an agentic loop for design generation.
func (c *DesignCommand) Execute(
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
			Content:     "Usage: /design <slug> [--auto] [--model <model>]",
			Timestamp:   time.Now(),
		}, nil
	}

	// Parse flags from args
	slug, autoContinue, capabilityStr, modelOverride := parseWorkflowArgsWithCapability(rawArgs)

	if slug == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Change slug is required. Usage: /design <slug> [--auto] [--capability <cap>] [--model <model>]",
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

	// Check if proposal exists (required for design)
	if !change.Files.HasProposal {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("No proposal found for '%s'.\n\nRun `/propose <description>` first to create a proposal.", slug),
			Timestamp:   time.Now(),
		}, nil
	}

	// Resolve model using capability-based selection
	registry := GetModelRegistry()
	role := prompts.DesignWriterRole()

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
	prompt := prompts.DesignWriterPrompt(change.Slug, change.Title)

	payload := &workflow.WorkflowTaskPayload{
		TaskID:        taskID,
		WorkflowID:    workflow.DocumentGenerationWorkflowID,
		Role:          role,
		Model:         primaryModel,
		FallbackChain: fallbackChain,
		Capability:    capability.String(),
		WorkflowSlug:  change.Slug,
		WorkflowStep:  "design",
		Title:         change.Title,
		Description:   change.Description,
		Prompt:        prompt,
		AutoContinue:  autoContinue,
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
		"auto_continue", autoContinue,
		"model", primaryModel,
		"capability", capability.String(),
		"fallback_count", len(fallbackChain),
		"trace_id", tc.TraceID,
		"user_id", msg.UserID)

	// Build response message
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Creating design for: **%s**\n\n", change.Title))
	sb.WriteString(fmt.Sprintf("Workflow: `.semspec/changes/%s/`\n", change.Slug))
	if autoContinue {
		sb.WriteString("\nMode: **Autonomous** (will continue through remaining steps)\n")
	} else {
		sb.WriteString("\nMode: **Interactive** (will pause after design for review)\n")
	}
	sb.WriteString(fmt.Sprintf("Model: %s (capability: %s)\n", primaryModel, capability.String()))
	if len(fallbackChain) > 0 {
		sb.WriteString(fmt.Sprintf("Fallbacks: %s\n", strings.Join(fallbackChain, ", ")))
	}
	sb.WriteString(fmt.Sprintf("\n**Trace ID:** `%s`\n", tc.TraceID))
	sb.WriteString(fmt.Sprintf("Debug: `/debug trace %s`\n", tc.TraceID))
	sb.WriteString("\nGenerating design using LLM...")

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

// parseWorkflowArgs parses the slug and flags from the command arguments.
// Used by design, spec, and tasks commands.
// Deprecated: Use parseWorkflowArgsWithCapability instead.
func parseWorkflowArgs(rawArgs string) (slug string, autoContinue bool, model string) {
	s, a, _, m := parseWorkflowArgsWithCapability(rawArgs)
	return s, a, m
}

// parseWorkflowArgsWithCapability parses the slug, flags, and capability from command arguments.
// Used by design, spec, and tasks commands.
func parseWorkflowArgsWithCapability(rawArgs string) (slug string, autoContinue bool, capability string, modelOverride string) {
	parts := strings.Fields(rawArgs)
	var slugParts []string

	for i := 0; i < len(parts); i++ {
		part := parts[i]

		switch {
		case part == "--auto":
			autoContinue = true
		case part == "--model" && i+1 < len(parts):
			i++
			modelOverride = parts[i]
		case strings.HasPrefix(part, "--model="):
			modelOverride = strings.TrimPrefix(part, "--model=")
		case part == "--capability" && i+1 < len(parts):
			i++
			capability = parts[i]
		case strings.HasPrefix(part, "--capability="):
			capability = strings.TrimPrefix(part, "--capability=")
		case part == "--cap" && i+1 < len(parts):
			// Short alias for --capability
			i++
			capability = parts[i]
		case strings.HasPrefix(part, "--cap="):
			capability = strings.TrimPrefix(part, "--cap=")
		case strings.HasPrefix(part, "--"):
			// Skip unknown flags
			continue
		default:
			slugParts = append(slugParts, part)
		}
	}

	// The slug should be a single hyphenated word (no spaces)
	if len(slugParts) > 0 {
		slug = slugParts[0]
	}
	return
}
