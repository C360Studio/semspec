// Package git provides git operation tools for the Semspec agent.
package git

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"

	"github.com/c360studio/semspec/tools/provenance"
)

// allowedProtocols defines the git URL protocols that are permitted for cloning.
var allowedProtocols = map[string]bool{
	"https": true,
	"git":   true,
	"ssh":   true,
}

// validateGitURL validates that a git URL uses an allowed protocol.
// Returns an error if the URL is invalid or uses a disallowed protocol.
func validateGitURL(rawURL string) error {
	// Handle SSH shorthand (git@github.com:owner/repo.git)
	if strings.HasPrefix(rawURL, "git@") {
		return nil // SSH shorthand is allowed
	}

	// Parse the URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check protocol
	scheme := strings.ToLower(parsed.Scheme)
	if !allowedProtocols[scheme] {
		return fmt.Errorf("protocol %q not allowed; must be https, git, or ssh", scheme)
	}

	// Block file:// protocol explicitly
	if scheme == "file" {
		return fmt.Errorf("file:// protocol is not allowed")
	}

	return nil
}

// validatePath validates that a path is safe and within allowed boundaries.
// baseDir is the expected parent directory; path must be within it after cleaning.
func validatePath(baseDir, path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}

	// Clean and resolve the path
	cleanPath := filepath.Clean(path)

	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		// Even if Clean resolves it, reject paths with .. for safety
		return fmt.Errorf("path traversal not allowed")
	}

	// If baseDir is provided, ensure path is within it
	if baseDir != "" {
		cleanBase := filepath.Clean(baseDir)
		absPath, err := filepath.Abs(cleanPath)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
		absBase, err := filepath.Abs(cleanBase)
		if err != nil {
			return fmt.Errorf("invalid base path: %w", err)
		}

		// Ensure path starts with base directory
		if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
			return fmt.Errorf("path must be within %s", cleanBase)
		}
	}

	return nil
}

// conventionalCommitPattern matches conventional commit format
var conventionalCommitPattern = regexp.MustCompile(`^(feat|fix|docs|style|refactor|test|chore|perf|ci|build|revert)(\([a-zA-Z0-9_-]+\))?: .+`)

// ProvenanceEmitter is called when provenance triples are generated
type ProvenanceEmitter func(triples []message.Triple)

// DecisionEmitter is called when decision entities should be published to the graph.
// Each call receives a fully-formed DecisionEntityPayload ready for graph ingestion.
type DecisionEmitter func(payload *DecisionEntityPayload) error

// Executor implements git operation tools
type Executor struct {
	repoRoot       string
	provenanceEmit ProvenanceEmitter
	provenanceCtx  *provenance.ProvenanceContext
	decisionEmit   DecisionEmitter
}

// NewExecutor creates a new git executor with the given repository root
func NewExecutor(repoRoot string) *Executor {
	return &Executor{repoRoot: repoRoot}
}

// WithProvenance configures the executor to emit provenance triples
func (e *Executor) WithProvenance(ctx *provenance.ProvenanceContext, emit ProvenanceEmitter) *Executor {
	e.provenanceCtx = ctx
	e.provenanceEmit = emit
	return e
}

// WithDecisionEmitter configures the executor to emit decision entities.
// When set, each file changed in a git commit will generate a decision entity
// published to the knowledge graph.
func (e *Executor) WithDecisionEmitter(emit DecisionEmitter) *Executor {
	e.decisionEmit = emit
	return e
}

// emitProvenance emits provenance triples if configured
func (e *Executor) emitProvenance(triples []message.Triple) {
	if e.provenanceEmit != nil && len(triples) > 0 {
		e.provenanceEmit(triples)
	}
}

// emitDecision emits a decision entity if configured
func (e *Executor) emitDecision(payload *DecisionEntityPayload) {
	if e.decisionEmit != nil && payload != nil {
		// Best effort - don't fail the commit if decision emission fails
		_ = e.decisionEmit(payload)
	}
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
	case "git_clone":
		return e.gitClone(ctx, call)
	case "git_pull":
		return e.gitPull(ctx, call)
	case "git_fetch":
		return e.gitFetch(ctx, call)
	case "git_log":
		return e.gitLog(ctx, call)
	case "git_ls_remote":
		return e.gitLsRemote(ctx, call)
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
		{
			Name:        "git_clone",
			Description: "Clone a git repository to a local directory",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Git repository URL to clone",
					},
					"dest": map[string]any{
						"type":        "string",
						"description": "Destination directory path",
					},
					"branch": map[string]any{
						"type":        "string",
						"description": "Branch to checkout after cloning (optional, defaults to default branch)",
					},
					"depth": map[string]any{
						"type":        "integer",
						"description": "Create a shallow clone with history truncated to specified number of commits (optional)",
					},
				},
				"required": []string{"url", "dest"},
			},
		},
		{
			Name:        "git_pull",
			Description: "Pull changes from remote repository",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"remote": map[string]any{
						"type":        "string",
						"description": "Remote name (optional, defaults to 'origin')",
					},
					"branch": map[string]any{
						"type":        "string",
						"description": "Branch name to pull (optional, defaults to current branch)",
					},
				},
			},
		},
		{
			Name:        "git_fetch",
			Description: "Fetch changes from remote repository without merging",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"remote": map[string]any{
						"type":        "string",
						"description": "Remote name (optional, defaults to 'origin')",
					},
					"prune": map[string]any{
						"type":        "boolean",
						"description": "Remove remote-tracking references that no longer exist",
					},
				},
			},
		},
		{
			Name:        "git_log",
			Description: "Get recent commit history",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{
						"type":        "integer",
						"description": "Number of commits to show (optional, defaults to 10)",
					},
					"format": map[string]any{
						"type":        "string",
						"description": "Output format: 'oneline', 'short', 'full', or 'json' (optional, defaults to 'oneline')",
					},
				},
			},
		},
		{
			Name:        "git_ls_remote",
			Description: "List references in a remote repository (validate URL before cloning)",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Git repository URL to check",
					},
					"heads": map[string]any{
						"type":        "boolean",
						"description": "Only show branch heads",
					},
					"tags": map[string]any{
						"type":        "boolean",
						"description": "Only show tags",
					},
				},
				"required": []string{"url"},
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

	// Get list of files in the commit with their status
	filesOutput, _ := e.runGit(ctx, "diff-tree", "--no-commit-id", "--name-status", "-r", "HEAD")
	var files []string
	var fileChanges []FileChangeInfo
	for _, line := range strings.Split(strings.TrimSpace(filesOutput), "\n") {
		if line == "" {
			continue
		}
		// Format: "A\tfile.go" or "M\tfile.go"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			files = append(files, parts[1])
			fileChanges = append(fileChanges, FileChangeInfo{
				Path:      parts[1],
				Operation: ParseFileOperation(parts[0]),
			})
		} else {
			// Fallback for lines without status
			files = append(files, line)
			fileChanges = append(fileChanges, FileChangeInfo{
				Path:      line,
				Operation: "modify",
			})
		}
	}

	// Emit provenance for commit
	if e.provenanceCtx != nil {
		provCtx := provenance.NewProvenanceContext(
			e.provenanceCtx.LoopID,
			e.provenanceCtx.AgentID,
			call.ID,
			"git_commit",
		)
		e.emitProvenance(provCtx.CommitTriples(hash, message, files))
	}

	// Emit decision entities for each file (git-as-memory)
	if e.decisionEmit != nil {
		branch, _ := e.runGit(ctx, "rev-parse", "--abbrev-ref", "HEAD")
		branch = strings.TrimSpace(branch)

		provCtx := &provenance.ProvenanceContext{
			CallID: call.ID,
		}
		if e.provenanceCtx != nil {
			provCtx.LoopID = e.provenanceCtx.LoopID
			provCtx.AgentID = e.provenanceCtx.AgentID
		}

		for _, fc := range fileChanges {
			info := provenance.FileDecisionInfo{
				EntityID:   GenerateDecisionEntityID(hash, fc.Path),
				FilePath:   fc.Path,
				Operation:  fc.Operation,
				CommitHash: hash,
				Message:    message,
				Branch:     branch,
				Repository: e.repoRoot,
			}
			triples := provCtx.DecisionTriples(info)
			payload := NewDecisionEntityPayload(hash, fc.Path, triples)
			e.emitDecision(payload)
		}
	}

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

// gitClone clones a repository to a local directory
func (e *Executor) gitClone(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	repoURL, ok := call.Arguments["url"].(string)
	if !ok || repoURL == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "url is required",
		}, nil
	}

	// Validate URL protocol
	if err := validateGitURL(repoURL); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("invalid URL: %s", err.Error()),
		}, nil
	}

	dest, ok := call.Arguments["dest"].(string)
	if !ok || dest == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "dest is required",
		}, nil
	}

	// Validate destination path (must be within executor's repo root if set)
	if e.repoRoot != "" {
		if err := validatePath(e.repoRoot, dest); err != nil {
			return agentic.ToolResult{
				CallID: call.ID,
				Error:  fmt.Sprintf("invalid destination: %s", err.Error()),
			}, nil
		}
	}

	branch, _ := call.Arguments["branch"].(string)
	depth, _ := call.Arguments["depth"].(float64)

	args := []string{"clone"}

	if branch != "" {
		args = append(args, "--branch", branch)
	}

	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", int(depth)))
	}

	args = append(args, repoURL, dest)

	// Clone runs from the executor's root directory
	output, err := e.runGit(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("clone failed: %s", err.Error()),
		}, nil
	}

	// Get the HEAD commit of the cloned repo
	headCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	headCmd.Dir = dest
	headOutput, _ := headCmd.Output()
	head := strings.TrimSpace(string(headOutput))

	result := fmt.Sprintf("Cloned %s to %s", repoURL, dest)
	if head != "" {
		result += fmt.Sprintf("\nHEAD: %s", head)
	}
	if output != "" {
		result += "\n" + output
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: result,
	}, nil
}

// gitPull pulls changes from remote
func (e *Executor) gitPull(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if !e.isGitRepo() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "not a git repository",
		}, nil
	}

	remote, _ := call.Arguments["remote"].(string)
	branch, _ := call.Arguments["branch"].(string)

	args := []string{"pull"}
	if remote != "" {
		args = append(args, remote)
		if branch != "" {
			args = append(args, branch)
		}
	}

	output, err := e.runGit(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("pull failed: %s", err.Error()),
		}, nil
	}

	// Get new HEAD after pull
	head, _ := e.runGit(ctx, "rev-parse", "--short", "HEAD")
	head = strings.TrimSpace(head)

	result := strings.TrimSpace(output)
	if head != "" {
		result += fmt.Sprintf("\nHEAD: %s", head)
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: result,
	}, nil
}

// gitFetch fetches changes from remote without merging
func (e *Executor) gitFetch(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if !e.isGitRepo() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "not a git repository",
		}, nil
	}

	remote, _ := call.Arguments["remote"].(string)
	prune, _ := call.Arguments["prune"].(bool)

	args := []string{"fetch"}
	if prune {
		args = append(args, "--prune")
	}
	if remote != "" {
		args = append(args, remote)
	}

	output, err := e.runGit(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("fetch failed: %s", err.Error()),
		}, nil
	}

	result := strings.TrimSpace(output)
	if result == "" {
		result = "Fetch complete (no new changes)"
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: result,
	}, nil
}

// gitLog returns recent commit history
func (e *Executor) gitLog(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if !e.isGitRepo() {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "not a git repository",
		}, nil
	}

	count := 10
	if c, ok := call.Arguments["count"].(float64); ok && c > 0 {
		count = int(c)
	}

	format := "oneline"
	if f, ok := call.Arguments["format"].(string); ok && f != "" {
		format = f
	}

	var args []string
	switch format {
	case "json":
		args = []string{"log", fmt.Sprintf("-%d", count),
			"--pretty=format:{\"hash\": \"%H\", \"short_hash\": \"%h\", \"author\": \"%an\", \"date\": \"%ci\", \"message\": \"%s\"}"}
	case "short":
		args = []string{"log", fmt.Sprintf("-%d", count), "--pretty=short"}
	case "full":
		args = []string{"log", fmt.Sprintf("-%d", count), "--pretty=full"}
	default: // oneline
		args = []string{"log", fmt.Sprintf("-%d", count), "--oneline"}
	}

	output, err := e.runGit(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("log failed: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: strings.TrimSpace(output),
	}, nil
}

// gitLsRemote validates a remote repository URL and lists references
func (e *Executor) gitLsRemote(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	repoURL, ok := call.Arguments["url"].(string)
	if !ok || repoURL == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "url is required",
		}, nil
	}

	// Validate URL protocol
	if err := validateGitURL(repoURL); err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("invalid URL: %s", err.Error()),
		}, nil
	}

	heads, _ := call.Arguments["heads"].(bool)
	tags, _ := call.Arguments["tags"].(bool)

	args := []string{"ls-remote"}
	if heads {
		args = append(args, "--heads")
	}
	if tags {
		args = append(args, "--tags")
	}
	args = append(args, repoURL)

	// Run ls-remote from the system (doesn't require a repo)
	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("ls-remote failed: %s: %s", err.Error(), string(output)),
		}, nil
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		result = "Repository is accessible but has no refs"
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: result,
	}, nil
}
