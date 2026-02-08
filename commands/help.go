package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	agenticdispatch "github.com/c360studio/semstreams/processor/agentic-dispatch"
	"github.com/google/uuid"
)

// HelpCommand implements the /help command for listing available commands.
type HelpCommand struct{}

// Config returns the command configuration.
func (c *HelpCommand) Config() agenticdispatch.CommandConfig {
	return agenticdispatch.CommandConfig{
		Pattern:     `^/help(?:\s+(.*))?$`,
		Permission:  "view",
		RequireLoop: false,
		Help:        "/help [command] - Show available commands or command details",
	}
}

// Execute runs the help command.
func (c *HelpCommand) Execute(
	ctx context.Context,
	cmdCtx *agenticdispatch.CommandContext,
	msg agentic.UserMessage,
	args []string,
	loopID string,
) (agentic.UserResponse, error) {
	specificCmd := ""
	if len(args) > 0 {
		specificCmd = strings.TrimSpace(args[0])
		// Strip leading slash if provided
		specificCmd = strings.TrimPrefix(specificCmd, "/")
	}

	// Get all registered commands and extract their configs
	executors := agenticdispatch.ListRegisteredCommands()
	commands := make(map[string]agenticdispatch.CommandConfig, len(executors))
	for name, executor := range executors {
		commands[name] = executor.Config()
	}

	// If a specific command is requested, show detailed help
	if specificCmd != "" {
		return c.showCommandHelp(commands, specificCmd, msg)
	}

	// Otherwise, list all commands
	return c.listAllCommands(commands, msg)
}

// showCommandHelp shows detailed help for a specific command.
func (c *HelpCommand) showCommandHelp(commands map[string]agenticdispatch.CommandConfig, name string, msg agentic.UserMessage) (agentic.UserResponse, error) {
	cfg, exists := commands[name]
	if !exists {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Unknown command: /%s\n\nRun `/help` to see available commands.", name),
			Timestamp:   time.Now(),
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## /%s\n\n", name))
	sb.WriteString(fmt.Sprintf("%s\n\n", cfg.Help))
	sb.WriteString(fmt.Sprintf("**Permission**: %s\n", cfg.Permission))
	if cfg.RequireLoop {
		sb.WriteString("**Requires active loop**: yes\n")
	}

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// listAllCommands lists all available commands grouped by category.
func (c *HelpCommand) listAllCommands(commands map[string]agenticdispatch.CommandConfig, msg agentic.UserMessage) (agentic.UserResponse, error) {
	// Group commands by category
	workflow := []string{"propose", "design", "spec", "tasks"}
	validation := []string{"check", "approve"}
	lifecycle := []string{"archive", "changes"}
	integration := []string{"github"}
	observability := []string{"debug"}
	utility := []string{"help", "context"}

	var sb strings.Builder
	sb.WriteString("# Semspec Commands\n\n")

	// Workflow commands
	sb.WriteString("## Workflow\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")
	for _, name := range workflow {
		if cfg, ok := commands[name]; ok {
			sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
		}
	}

	// Validation commands
	sb.WriteString("\n## Validation\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")
	for _, name := range validation {
		if cfg, ok := commands[name]; ok {
			sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
		}
	}

	// Lifecycle commands
	sb.WriteString("\n## Lifecycle\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")
	for _, name := range lifecycle {
		if cfg, ok := commands[name]; ok {
			sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
		}
	}

	// Integration commands
	sb.WriteString("\n## Integration\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")
	for _, name := range integration {
		if cfg, ok := commands[name]; ok {
			sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
		}
	}

	// Observability commands
	sb.WriteString("\n## Observability\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")
	for _, name := range observability {
		if cfg, ok := commands[name]; ok {
			sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
		}
	}

	// Utility commands
	sb.WriteString("\n## Utility\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")
	for _, name := range utility {
		if cfg, ok := commands[name]; ok {
			sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
		}
	}

	// Any other commands not in the categories above
	var other []string
	knownCommands := make(map[string]bool)
	for _, list := range [][]string{workflow, validation, lifecycle, integration, observability, utility} {
		for _, name := range list {
			knownCommands[name] = true
		}
	}
	for name := range commands {
		if !knownCommands[name] {
			other = append(other, name)
		}
	}
	if len(other) > 0 {
		sort.Strings(other)
		sb.WriteString("\n## Other\n\n")
		sb.WriteString("| Command | Description |\n")
		sb.WriteString("|---------|-------------|\n")
		for _, name := range other {
			if cfg, ok := commands[name]; ok {
				sb.WriteString(fmt.Sprintf("| `/%s` | %s |\n", name, extractDescription(cfg.Help)))
			}
		}
	}

	sb.WriteString("\n---\n")
	sb.WriteString("Run `/help <command>` for detailed help on a specific command.\n")

	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeResult,
		Content:     sb.String(),
		Timestamp:   time.Now(),
	}, nil
}

// extractDescription extracts the description portion after the dash in help text.
func extractDescription(help string) string {
	// Help format is "/command <args> - Description"
	parts := strings.SplitN(help, " - ", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return help
}
