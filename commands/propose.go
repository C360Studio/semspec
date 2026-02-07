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
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// ProposeCommand implements the /propose command for creating proposals.
type ProposeCommand struct{}

// Config returns the command configuration.
func (c *ProposeCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/propose\s+(.+)$`,
		Permission:  "submit_task",
		RequireLoop: false,
		Help:        "/propose <description> [--auto] [--capability <cap>] [--model <model>] - Create new proposal using LLM",
	}
}

// Execute runs the propose command by triggering an agentic loop for proposal generation.
func (c *ProposeCommand) Execute(
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
			Content:     "Usage: /propose <description> [--auto] [--model <model>]",
			Timestamp:   time.Now(),
		}, nil
	}

	// Parse flags from args
	description, autoContinue, capabilityStr, modelOverride := parseProposalArgs(rawArgs)

	if description == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Description is required. Usage: /propose <description> [--auto] [--capability <cap>] [--model <model>]",
			Timestamp:   time.Now(),
		}, nil
	}

	// Resolve model using capability-based selection
	registry := GetModelRegistry()
	role := prompts.ProposalWriterRole()

	var primaryModel string
	// fallbackChain is computed for user feedback only; the workflow processor
	// handles model fallback internally via on_fail steps
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

	// Create the change directory structure
	manager := workflow.NewManager(repoRoot)
	change, err := manager.CreateChange(description, msg.UserID)
	if err != nil {
		// If change already exists, load it instead
		if strings.Contains(err.Error(), "already exists") {
			slug := workflow.Slugify(description)
			change, err = manager.LoadChange(slug)
			if err != nil {
				return agentic.UserResponse{
					ResponseID:  uuid.New().String(),
					ChannelType: msg.ChannelType,
					ChannelID:   msg.ChannelID,
					UserID:      msg.UserID,
					Type:        agentic.ResponseTypeError,
					Content:     fmt.Sprintf("Failed to load existing change: %v", err),
					Timestamp:   time.Now(),
				}, nil
			}
		} else {
			return agentic.UserResponse{
				ResponseID:  uuid.New().String(),
				ChannelType: msg.ChannelType,
				ChannelID:   msg.ChannelID,
				UserID:      msg.UserID,
				Type:        agentic.ResponseTypeError,
				Content:     fmt.Sprintf("Failed to create change: %v", err),
				Timestamp:   time.Now(),
			}, nil
		}
	}

	taskID := uuid.New().String()
	prompt := prompts.ProposalWriterPrompt(change.Slug, description)

	// Always use workflow processor - has built-in failure handling via on_fail steps
	triggerPayload := &workflow.WorkflowTriggerPayload{
		WorkflowID:  workflow.DocumentGenerationWorkflowID,
		Slug:        change.Slug,
		Title:       change.Title,
		Description: description,
		Prompt:      prompt,
		Model:       primaryModel,
		Auto:        autoContinue, // Controls whether to chain steps
		UserID:      msg.UserID,
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
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
	subject := "workflow.trigger.document-generation"

	// Publish to the appropriate subject
	if err := cmdCtx.NATSClient.PublishToStream(ctx, subject, data); err != nil {
		cmdCtx.Logger.Error("Failed to publish workflow message",
			"error", err,
			"subject", subject)
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Failed to submit workflow: %v", err),
			Timestamp:   time.Now(),
		}, nil
	}

	cmdCtx.Logger.Info("Published workflow message",
		"task_id", taskID,
		"slug", change.Slug,
		"role", role,
		"auto_continue", autoContinue,
		"subject", subject,
		"model", primaryModel,
		"capability", capability.String(),
		"fallback_count", len(fallbackChain),
		"user_id", msg.UserID)

	// Build response message
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Creating proposal for: **%s**\n\n", description))
	sb.WriteString(fmt.Sprintf("Workflow: `.semspec/changes/%s/`\n", change.Slug))
	if autoContinue {
		sb.WriteString("\nMode: **Autonomous** (will continue through all workflow steps)\n")
	} else {
		sb.WriteString("\nMode: **Interactive** (will pause after proposal for review)\n")
	}
	sb.WriteString(fmt.Sprintf("Model: %s (capability: %s)\n", primaryModel, capability.String()))
	if len(fallbackChain) > 0 {
		sb.WriteString(fmt.Sprintf("Fallbacks: %s\n", strings.Join(fallbackChain, ", ")))
	}
	sb.WriteString("\nGenerating proposal using LLM...")

	return agentic.UserResponse{
		ResponseID:  taskID, // Use task ID for correlation
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeStatus,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// parseProposalArgs parses the description and flags from the command arguments.
func parseProposalArgs(rawArgs string) (description string, autoContinue bool, capability string, modelOverride string) {
	parts := strings.Fields(rawArgs)
	var descParts []string

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
			descParts = append(descParts, part)
		}
	}

	description = strings.Join(descParts, " ")
	return
}
