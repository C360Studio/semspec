// Package github provides GitHub CLI wrapper tools for the Semspec agent.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

// Executor implements GitHub operation tools via the gh CLI.
type Executor struct {
	repoRoot string
}

// NewExecutor creates a new GitHub executor with the given repository root.
func NewExecutor(repoRoot string) *Executor {
	return &Executor{repoRoot: repoRoot}
}

// Execute executes a GitHub tool call.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	switch call.Name {
	case "github_issue_create":
		return e.issueCreate(ctx, call)
	case "github_issue_edit":
		return e.issueEdit(ctx, call)
	case "github_issue_view":
		return e.issueView(ctx, call)
	case "github_repo_info":
		return e.repoInfo(ctx, call)
	default:
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("unknown tool: %s", call.Name),
		}, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// ListTools returns the tool definitions for GitHub operations.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{
		{
			Name:        "github_issue_create",
			Description: "Create a new GitHub issue",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "Issue title",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "Issue body (markdown)",
					},
					"labels": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Labels to apply to the issue",
					},
				},
				"required": []string{"title", "body"},
			},
		},
		{
			Name:        "github_issue_edit",
			Description: "Edit an existing GitHub issue",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"number": map[string]any{
						"type":        "integer",
						"description": "Issue number to edit",
					},
					"body": map[string]any{
						"type":        "string",
						"description": "New issue body (markdown)",
					},
					"add_labels": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Labels to add to the issue",
					},
				},
				"required": []string{"number"},
			},
		},
		{
			Name:        "github_issue_view",
			Description: "View a GitHub issue's details",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"number": map[string]any{
						"type":        "integer",
						"description": "Issue number to view",
					},
				},
				"required": []string{"number"},
			},
		},
		{
			Name:        "github_repo_info",
			Description: "Get repository information (owner/repo)",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// IssueCreateResult contains the result of creating an issue.
type IssueCreateResult struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// IssueViewResult contains the result of viewing an issue.
type IssueViewResult struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// RepoInfoResult contains repository information.
type RepoInfoResult struct {
	NameWithOwner string `json:"nameWithOwner"`
	DefaultBranch string `json:"defaultBranchRef"`
}

// issueCreate creates a new GitHub issue.
func (e *Executor) issueCreate(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	title, ok := call.Arguments["title"].(string)
	if !ok || title == "" {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "title argument is required",
		}, nil
	}

	body, ok := call.Arguments["body"].(string)
	if !ok {
		body = ""
	}

	args := []string{"issue", "create", "--title", title, "--body", body}

	// Add labels if provided
	if labelsRaw, ok := call.Arguments["labels"]; ok {
		if labels, ok := labelsRaw.([]interface{}); ok {
			for _, label := range labels {
				if labelStr, ok := label.(string); ok {
					args = append(args, "--label", labelStr)
				}
			}
		}
	}

	output, err := e.runGH(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to create issue: %s", err.Error()),
		}, nil
	}

	// Parse the URL from output to extract issue number
	url := strings.TrimSpace(output)
	number := extractIssueNumber(url)

	result := IssueCreateResult{
		Number: number,
		URL:    url,
	}

	resultJSON, _ := json.Marshal(result)
	return agentic.ToolResult{
		CallID:  call.ID,
		Content: string(resultJSON),
	}, nil
}

// issueEdit edits an existing GitHub issue.
func (e *Executor) issueEdit(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	numberRaw, ok := call.Arguments["number"]
	if !ok {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "number argument is required",
		}, nil
	}

	number := toInt(numberRaw)
	if number == 0 {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "invalid issue number",
		}, nil
	}

	args := []string{"issue", "edit", fmt.Sprintf("%d", number)}

	// Add body if provided
	if body, ok := call.Arguments["body"].(string); ok && body != "" {
		args = append(args, "--body", body)
	}

	// Add labels if provided
	if labelsRaw, ok := call.Arguments["add_labels"]; ok {
		if labels, ok := labelsRaw.([]interface{}); ok {
			for _, label := range labels {
				if labelStr, ok := label.(string); ok {
					args = append(args, "--add-label", labelStr)
				}
			}
		}
	}

	_, err := e.runGH(ctx, args...)
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to edit issue: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: fmt.Sprintf("Successfully updated issue #%d", number),
	}, nil
}

// issueView views a GitHub issue.
func (e *Executor) issueView(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	numberRaw, ok := call.Arguments["number"]
	if !ok {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "number argument is required",
		}, nil
	}

	number := toInt(numberRaw)
	if number == 0 {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  "invalid issue number",
		}, nil
	}

	output, err := e.runGH(ctx, "issue", "view", fmt.Sprintf("%d", number), "--json", "number,title,state,body,url")
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to view issue: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: output,
	}, nil
}

// repoInfo gets repository information.
func (e *Executor) repoInfo(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	output, err := e.runGH(ctx, "repo", "view", "--json", "nameWithOwner,defaultBranchRef")
	if err != nil {
		return agentic.ToolResult{
			CallID: call.ID,
			Error:  fmt.Sprintf("failed to get repo info: %s", err.Error()),
		}, nil
	}

	return agentic.ToolResult{
		CallID:  call.ID,
		Content: output,
	}, nil
}

// runGH executes a gh command in the repo directory.
func (e *Executor) runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = e.repoRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%w: %s", err, string(output))
	}
	return string(output), nil
}

// extractIssueNumber extracts the issue number from a GitHub issue URL.
func extractIssueNumber(url string) int {
	// URL format: https://github.com/owner/repo/issues/123
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return 0
	}
	lastPart := strings.TrimSpace(parts[len(parts)-1])
	return toInt(lastPart)
}

// toInt converts various numeric types to int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		var result int
		for _, c := range n {
			if c >= '0' && c <= '9' {
				result = result*10 + int(c-'0')
			}
		}
		return result
	default:
		return 0
	}
}

// IsGHAvailable checks if the gh CLI is available and authenticated.
func IsGHAvailable() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

// GetRepoName returns the current repository name (owner/repo format).
func GetRepoName(repoRoot string) (string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// CreateIssue creates a GitHub issue and returns its number and URL.
func CreateIssue(ctx context.Context, repoRoot, title, body string, labels []string) (int, string, error) {
	args := []string{"issue", "create", "--title", title, "--body", body}
	for _, label := range labels {
		args = append(args, "--label", label)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("failed to create issue: %w: %s", err, string(output))
	}

	url := strings.TrimSpace(string(output))
	number := extractIssueNumber(url)
	return number, url, nil
}

// EditIssueBody updates the body of an existing issue.
func EditIssueBody(ctx context.Context, repoRoot string, number int, body string) error {
	cmd := exec.CommandContext(ctx, "gh", "issue", "edit", fmt.Sprintf("%d", number), "--body", body)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to edit issue: %w: %s", err, string(output))
	}
	return nil
}
