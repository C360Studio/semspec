// Package client provides test clients for e2e scenarios.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SandboxClient provides HTTP operations for the sandbox server.
// It communicates with the sandbox agent container that manages git worktrees
// and scoped command execution for individual tasks.
type SandboxClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewSandboxClient creates a new sandbox HTTP client for e2e testing.
func NewSandboxClient(baseURL string) *SandboxClient {
	return &SandboxClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

// WorktreeCreateRequest is the body for POST /worktree.
type WorktreeCreateRequest struct {
	TaskID     string `json:"task_id"`
	BaseBranch string `json:"base_branch,omitempty"`
}

// WorktreeCreateResponse is returned by POST /worktree (201).
type WorktreeCreateResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// MergeRequest is the optional body for POST /worktree/{taskID}/merge.
type MergeRequest struct {
	TargetBranch  string            `json:"target_branch,omitempty"`
	CommitMessage string            `json:"commit_message,omitempty"`
	Trailers      map[string]string `json:"trailers,omitempty"`
}

// FileChangeInfo describes a file changed in a commit or merge.
type FileChangeInfo struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

// MergeResponse is returned by POST /worktree/{taskID}/merge (200).
type MergeResponse struct {
	Status       string           `json:"status"`
	Commit       string           `json:"commit,omitempty"`
	Note         string           `json:"note,omitempty"`
	FilesChanged []FileChangeInfo `json:"files_changed,omitempty"`
}

// WorktreeFileEntry is a single file in a worktree listing.
type WorktreeFileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// WorktreeFilesResponse is returned by GET /worktree/{taskID}/files (200).
type WorktreeFilesResponse struct {
	Entries []WorktreeFileEntry
}

// BranchCreateRequest is the body for POST /branch.
type BranchCreateRequest struct {
	Name string `json:"name"`
	Base string `json:"base,omitempty"`
}

// FileWriteRequest is the body for PUT /file.
type FileWriteRequest struct {
	TaskID  string `json:"task_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// FileReadResponse is returned by GET /file (200).
type FileReadResponse struct {
	Content string `json:"content"`
	Size    int    `json:"size"`
}

// ListRequest is the body for POST /list.
type ListRequest struct {
	TaskID string `json:"task_id"`
	Path   string `json:"path"`
}

// ListEntry describes a single directory entry.
type ListEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// ListResponse is returned by POST /list (200).
type ListResponse struct {
	Entries []ListEntry `json:"entries"`
}

// SearchRequest is the body for POST /search.
type SearchRequest struct {
	TaskID   string `json:"task_id"`
	Pattern  string `json:"pattern"`
	FileGlob string `json:"file_glob,omitempty"`
}

// SearchMatch is a single line match from POST /search.
type SearchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SearchResponse is returned by POST /search (200).
type SearchResponse struct {
	Matches []SearchMatch `json:"matches"`
}

// GitStatusRequest is the body for POST /git/status.
type GitStatusRequest struct {
	TaskID string `json:"task_id"`
}

// GitStatusResponse is returned by POST /git/status (200).
type GitStatusResponse struct {
	Output string `json:"output"`
}

// GitCommitRequest is the body for POST /git/commit.
type GitCommitRequest struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

// GitCommitResponse is returned by POST /git/commit (200).
type GitCommitResponse struct {
	Status       string           `json:"status"`
	Hash         string           `json:"hash,omitempty"`
	FilesChanged []FileChangeInfo `json:"files_changed,omitempty"`
}

// GitDiffRequest is the body for POST /git/diff.
type GitDiffRequest struct {
	TaskID string `json:"task_id"`
}

// GitDiffResponse is returned by POST /git/diff (200).
type GitDiffResponse struct {
	Output string `json:"output"`
}

// ExecRequest is the body for POST /exec.
type ExecRequest struct {
	TaskID    string `json:"task_id"`
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// ExecResponse is returned by POST /exec (200).
// Classification values: "success", "failure", "command_not_found",
// "permission_denied", "timeout".
type ExecResponse struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exit_code"`
	TimedOut       bool   `json:"timed_out"`
	Classification string `json:"classification,omitempty"`
	MissingCommand string `json:"missing_command,omitempty"`
}

// InstallRequest is the body for POST /install.
type InstallRequest struct {
	TaskID         string   `json:"task_id"`
	PackageManager string   `json:"package_manager"`
	Packages       []string `json:"packages"`
}

// InstallResponse is returned by POST /install (200).
type InstallResponse struct {
	Status   string `json:"status"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// WorkspaceTaskInfo describes a single active worktree.
type WorkspaceTaskInfo struct {
	TaskID    string `json:"task_id"`
	FileCount int    `json:"file_count"`
	Branch    string `json:"branch"`
}

// WorkspaceEntry is a node in the nested file tree from GET /workspace/tree.
type WorkspaceEntry struct {
	Name     string            `json:"name"`
	Path     string            `json:"path"`
	IsDir    bool              `json:"is_dir"`
	Size     int64             `json:"size"`
	Children []*WorkspaceEntry `json:"children,omitempty"`
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (c *SandboxClient) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func decode[T any](body []byte) (T, error) {
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		return v, fmt.Errorf("unmarshal response: %w (body: %s)", err, string(body))
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// HealthCheck checks whether the sandbox server is reachable and healthy.
func (c *SandboxClient) HealthCheck(ctx context.Context) error {
	body, status, err := c.do(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("health check failed: HTTP %d: %s", status, string(body))
	}
	return nil
}

// WaitForHealthy polls /health until the sandbox server responds successfully
// or the context is cancelled.
func (c *SandboxClient) WaitForHealthy(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for sandbox to be healthy: %w", ctx.Err())
		case <-ticker.C:
			if err := c.HealthCheck(ctx); err == nil {
				return nil
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Worktree lifecycle
// ---------------------------------------------------------------------------

// CreateWorktree creates a git worktree for the given task. Returns 201 on success.
func (c *SandboxClient) CreateWorktree(ctx context.Context, req WorktreeCreateRequest) (*WorktreeCreateResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/worktree", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		return nil, fmt.Errorf("create worktree: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[WorktreeCreateResponse](body)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	return &resp, nil
}

// DeleteWorktree removes the worktree and its branch for the given task ID.
func (c *SandboxClient) DeleteWorktree(ctx context.Context, taskID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/worktree/"+taskID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("delete worktree: HTTP %d: %s", status, string(body))
	}
	return nil
}

// MergeWorktree commits pending changes and merges the worktree into the target branch.
func (c *SandboxClient) MergeWorktree(ctx context.Context, taskID string, req MergeRequest) (*MergeResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/worktree/"+taskID+"/merge", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("merge worktree: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[MergeResponse](body)
	if err != nil {
		return nil, fmt.Errorf("merge worktree: %w", err)
	}
	return &resp, nil
}

// ListWorktreeFiles lists all files tracked in the worktree for the given task.
func (c *SandboxClient) ListWorktreeFiles(ctx context.Context, taskID string) (*WorktreeFilesResponse, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/worktree/"+taskID+"/files", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("list worktree files: HTTP %d: %s", status, string(body))
	}
	entries, err := decode[[]WorktreeFileEntry](body)
	if err != nil {
		return nil, fmt.Errorf("list worktree files: %w", err)
	}
	return &WorktreeFilesResponse{Entries: entries}, nil
}

// ---------------------------------------------------------------------------
// Branch management
// ---------------------------------------------------------------------------

// CreateBranch creates a git branch in the main repository.
func (c *SandboxClient) CreateBranch(ctx context.Context, req BranchCreateRequest) error {
	body, status, err := c.do(ctx, http.MethodPost, "/branch", req)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("create branch: HTTP %d: %s", status, string(body))
	}
	return nil
}

// ---------------------------------------------------------------------------
// File operations
// ---------------------------------------------------------------------------

// WriteFile writes content to a path inside the task's worktree.
func (c *SandboxClient) WriteFile(ctx context.Context, req FileWriteRequest) error {
	body, status, err := c.do(ctx, http.MethodPut, "/file", req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("write file: HTTP %d: %s", status, string(body))
	}
	return nil
}

// ReadFile reads a file from the task's worktree.
func (c *SandboxClient) ReadFile(ctx context.Context, taskID, path string) (*FileReadResponse, error) {
	url := fmt.Sprintf("/file?task_id=%s&path=%s", taskID, path)
	body, status, err := c.do(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("read file: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[FileReadResponse](body)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return &resp, nil
}

// ListDirectory lists entries in a directory within the task's worktree.
func (c *SandboxClient) ListDirectory(ctx context.Context, req ListRequest) (*ListResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/list", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("list directory: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[ListResponse](body)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}
	return &resp, nil
}

// Search performs a regex pattern search across files in the task's worktree.
func (c *SandboxClient) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/search", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[SearchResponse](body)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Git operations
// ---------------------------------------------------------------------------

// GitStatus returns the porcelain git status output for the task's worktree.
func (c *SandboxClient) GitStatus(ctx context.Context, taskID string) (*GitStatusResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/git/status", GitStatusRequest{TaskID: taskID})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("git status: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[GitStatusResponse](body)
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	return &resp, nil
}

// GitCommit stages all changes in the worktree and commits them.
func (c *SandboxClient) GitCommit(ctx context.Context, req GitCommitRequest) (*GitCommitResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/git/commit", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("git commit: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[GitCommitResponse](body)
	if err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}
	return &resp, nil
}

// GitDiff returns the combined unstaged and staged diff for the task's worktree.
func (c *SandboxClient) GitDiff(ctx context.Context, taskID string) (*GitDiffResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/git/diff", GitDiffRequest{TaskID: taskID})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("git diff: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[GitDiffResponse](body)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Command execution
// ---------------------------------------------------------------------------

// Exec runs a shell command inside the task's worktree.
// The Classification field in the response uses values from cmd/sandbox/classify.go:
// "success", "failure", "command_not_found", "permission_denied", "timeout".
func (c *SandboxClient) Exec(ctx context.Context, req ExecRequest) (*ExecResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/exec", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("exec: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[ExecResponse](body)
	if err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	return &resp, nil
}

// Install installs packages inside the sandbox container using the specified
// package manager. Supported values: "apt", "npm", "pip", "go".
func (c *SandboxClient) Install(ctx context.Context, req InstallRequest) (*InstallResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/install", req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("install: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[InstallResponse](body)
	if err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}
	return &resp, nil
}

// ---------------------------------------------------------------------------
// Workspace browser
// ---------------------------------------------------------------------------

// WorkspaceTasks lists all active worktrees with their file counts and branches.
func (c *SandboxClient) WorkspaceTasks(ctx context.Context) ([]WorkspaceTaskInfo, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/workspace/tasks", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("workspace tasks: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[[]WorkspaceTaskInfo](body)
	if err != nil {
		return nil, fmt.Errorf("workspace tasks: %w", err)
	}
	return resp, nil
}

// WorkspaceTree returns a nested file tree for the given task's worktree.
func (c *SandboxClient) WorkspaceTree(ctx context.Context, taskID string) ([]*WorkspaceEntry, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/workspace/tree?task_id="+taskID, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("workspace tree: HTTP %d: %s", status, string(body))
	}
	resp, err := decode[[]*WorkspaceEntry](body)
	if err != nil {
		return nil, fmt.Errorf("workspace tree: %w", err)
	}
	return resp, nil
}
