// Package git provides git operation tools for the Semspec agent.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/c360/semstreams/agentic"
)

// conventionalCommitPattern matches conventional commit format
var conventionalCommitPattern = regexp.MustCompile(`^(feat|fix|docs|style|refactor|test|chore|perf|ci|build|revert)(\([a-zA-Z0-9_-]+\))?: .+`)

// Executor implements git operation tools
type Executor struct {
	repoRoot string
}

// NewExecutor creates a new git executor with the given repository root
func NewExecutor(repoRoot string) *Executor {
	return &Executor{repoRoot: repoRoot}
}

// Execute executes a git tool call
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "git_status":
		return e.gitStatus(ctx, call)
	case "git_branch":
		return e.gitBranch(ctx, call)
	case "git_commit":
		return e.gitCommit(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for git operations
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "git_status",
			Description: "Check the status of the git repository",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "git_branch",
			Description: "Create or switch to a git branch",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Branch name to create or switch to",
					},
					"base": map[string]any{
						"type":        "string",
						"description": "Base branch or commit to create from (optional, defaults to HEAD)",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "git_commit",
			Description: "Commit staged changes with a message",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message": map[string]any{
						"type":        "string",
						"description": "Commit message (should follow conventional commit format)",
					},
					"stage_all": map[string]any{
						"type":        "boolean",
						"description": "If true, stage all modified tracked files before committing",
					},
				},
				"required": []string{"message"},
			},
		},
	}
}

// gitStatus returns the status of the repository
func (e *Executor) gitStatus(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	// Check if it's a git repo
	if !e.isGitRepo() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "not a git repository",
		}, nil
	}

	output, err := e.runGit(ctx, "status", "--porcelain")
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("git status failed: %s", err.Error()),
		}, nil
	}

	if strings.TrimSpace(output) == "" {
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: "Working tree clean",
		}, nil
	}

	// Parse status output
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var modified, staged, untracked []string

	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		status := line[:2]
		file := strings.TrimSpace(line[3:])

		switch {
		case status[0] == '?' && status[1] == '?':
			untracked = append(untracked, file)
		case status[0] != ' ':
			staged = append(staged, file)
		case status[1] != ' ':
			modified = append(modified, file)
		}
	}

	var result strings.Builder
	if len(staged) > 0 {
		result.WriteString("Staged:\n")
		for _, f := range staged {
			result.WriteString(fmt.Sprintf("  %s\n", f))
		}
	}
	if len(modified) > 0 {
		result.WriteString("Modified:\n")
		for _, f := range modified {
			result.WriteString(fmt.Sprintf("  %s\n", f))
		}
	}
	if len(untracked) > 0 {
		result.WriteString("Untracked:\n")
		for _, f := range untracked {
			result.WriteString(fmt.Sprintf("  %s\n", f))
		}
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: result.String(),
	}, nil
}

// gitBranch creates or switches to a branch
func (e *Executor) gitBranch(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if !e.isGitRepo() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "not a git repository",
		}, nil
	}

	name, ok := call.Arguments["name"].(string)
	if !ok || name == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "branch name is required",
		}, nil
	}

	base, _ := call.Arguments["base"].(string)

	// Check if branch exists
	branchExists := e.branchExists(ctx, name)

	if branchExists {
		// Switch to existing branch
		output, err := e.runGit(ctx, "checkout", name)
		if err != nil {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("failed to switch branch: %s", err.Error()),
			}, nil
		}
		return agentic.ToolResult{
			CallID:  call.ID,
			Content: fmt.Sprintf("Switched to branch '%s'\n%s", name, output),
		}, nil
	}

	// Create new branch
	args := []string{"checkout", "-b", name}
	if base != "" {
		args = append(args, base)
	}

	output, err := e.runGit(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to create branch: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Created and switched to branch '%s'\n%s", name, output),
	}, nil
}

// gitCommit commits staged changes
func (e *Executor) gitCommit(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if !e.isGitRepo() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "not a git repository",
		}, nil
	}

	message, ok := call.Arguments["message"].(string)
	if !ok || message == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "commit message is required",
		}, nil
	}

	// Validate conventional commit format
	if !ValidateConventionalCommit(message) {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("commit message does not follow conventional commit format: %s", message),
		}, nil
	}

	stageAll, _ := call.Arguments["stage_all"].(bool)

	// Stage all if requested
	if stageAll {
		if _, err := e.runGit(ctx, "add", "-u"); err != nil {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("failed to stage changes: %s", err.Error()),
			}, nil
		}
	}

	// Check for staged changes
	status, _ := e.runGit(ctx, "diff", "--cached", "--name-only")
	if strings.TrimSpace(status) == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "nothing to commit (no staged changes)",
		}, nil
	}

	// Commit
	output, err := e.runGit(ctx, "commit", "-m", message)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("commit failed: %s", err.Error()),
		}, nil
	}

	// Get commit hash
	hash, _ := e.runGit(ctx, "rev-parse", "--short", "HEAD")
	hash = strings.TrimSpace(hash)

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Committed %s: %s\n%s", hash, message, output),
	}, nil
}

// ValidateConventionalCommit checks if a message follows conventional commit format
func ValidateConventionalCommit(message string) bool {
	return conventionalCommitPattern.MatchString(message)
}

// runGit executes a git command in the repo directory
func (e *Executor) runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.repoRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%w: %s", err, string(output))
	}
	return string(output), nil
}

// isGitRepo checks if the repo root is a git repository
func (e *Executor) isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = e.repoRoot
	return cmd.Run() == nil
}

// branchExists checks if a branch exists
func (e *Executor) branchExists(ctx context.Context, name string) bool {
	_, err := e.runGit(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}
