// Package sandbox provides an HTTP client for the sandbox server.
// The sandbox server runs file, git, and command operations inside an isolated
// container so that agent-generated code never touches the host repository.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// maxResponseBytes caps sandbox response bodies at 2 MB to prevent
	// runaway output from filling agent memory.
	maxResponseBytes = 2 * 1024 * 1024

	// errCodeNeedsReconciliation matches the server-side constant. Returned
	// in the response body's error_code field when the sandbox main repo is
	// wedged in an unrecoverable state.
	errCodeNeedsReconciliation = "needs_reconciliation"
)

// ErrNeedsReconciliation is returned when the sandbox server responds with
// HTTP 503 + error_code=needs_reconciliation. Callers should use
// errors.Is(err, sandbox.ErrNeedsReconciliation) to detect this condition
// and MUST NOT retry — subsequent merge/branch requests will fail identically
// until an operator clears the flag via POST /admin/reconcile.
var ErrNeedsReconciliation = errors.New("sandbox needs reconciliation")

// ErrBranchExistsAtDifferentBase is returned by CreateBranch when invariant
// A3 refuses the request: the branch already exists but points at a different
// commit than the requested base. Callers who want to reset the branch to
// the new base must DeleteBranch first (explicit intent), then retry
// CreateBranch. Requirement-executor's init path does this automatically
// when a prior failed attempt left a stale branch behind.
var ErrBranchExistsAtDifferentBase = errors.New("branch exists at different base")

// Client communicates with the sandbox server via HTTP.
// All operations are scoped to a task ID that maps to a git worktree on the
// server side.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a sandbox client pointing at baseURL.
// Returns nil if baseURL is empty, which callers treat as "sandbox disabled".
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: trimRight(baseURL, "/"),
		httpClient: &http.Client{
			// Slightly above the maximum command timeout (5 min) so the HTTP
			// layer doesn't race with the server-side exec deadline.
			Timeout: 6 * time.Minute,
		},
	}
}

// trimRight removes trailing occurrences of cutset from s.
func trimRight(s, cutset string) string {
	for len(s) > 0 && s[len(s)-1:] == cutset {
		s = s[:len(s)-1]
	}
	return s
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// WorktreeInfo is returned by CreateWorktree and describes the newly created
// isolated workspace.
type WorktreeInfo struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// FileEntry represents a single filesystem entry returned by ListDir and
// ListWorktreeFiles.
type FileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// CommitResult holds the outcome of a git commit inside the sandbox.
type CommitResult struct {
	Status       string           `json:"status"`
	Hash         string           `json:"hash,omitempty"`
	FilesChanged []FileChangeInfo `json:"files_changed,omitempty"`
}

// FileChangeInfo describes a file changed in a commit.
// Mirrors the server's FileChangeInfo in cmd/sandbox/server.go.
type FileChangeInfo struct {
	Path      string `json:"path"`      // relative to worktree root
	Operation string `json:"operation"` // add, modify, delete, rename, copy, type_change
}

// ExecClassification constants match the server-side classification values.
// Use these instead of string literals when checking ExecResult.Classification.
const (
	ClassSuccess          = "success"
	ClassFailure          = "failure"
	ClassCommandNotFound  = "command_not_found"
	ClassPermissionDenied = "permission_denied"
	ClassTimeout          = "timeout"
)

// ExecResult holds the outcome of a command executed inside the sandbox.
type ExecResult struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exit_code"`
	TimedOut       bool   `json:"timed_out"`
	Classification string `json:"classification,omitempty"`
	MissingCommand string `json:"missing_command,omitempty"`
}

// ---------------------------------------------------------------------------
// Worktree lifecycle
// ---------------------------------------------------------------------------

// WorktreeOption configures optional parameters for CreateWorktree.
type WorktreeOption func(*worktreeOptions)

type worktreeOptions struct {
	baseBranch string
}

// WithBaseBranch sets the base branch for the worktree (default: HEAD).
func WithBaseBranch(branch string) WorktreeOption {
	return func(o *worktreeOptions) { o.baseBranch = branch }
}

// CreateWorktree asks the sandbox server to create an isolated git worktree
// for the given task. The server checks out a detached HEAD from the repo and
// returns the worktree path and branch name.
// Server route: POST /worktree  body: {"task_id": taskID, "base_branch": "..."}
func (c *Client) CreateWorktree(ctx context.Context, taskID string, opts ...WorktreeOption) (*WorktreeInfo, error) {
	var o worktreeOptions
	for _, opt := range opts {
		opt(&o)
	}

	body := struct {
		TaskID     string `json:"task_id"`
		BaseBranch string `json:"base_branch,omitempty"`
	}{TaskID: taskID, BaseBranch: o.baseBranch}
	var info WorktreeInfo
	if err := c.doJSON(ctx, http.MethodPost, "/worktree", body, &info); err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	return &info, nil
}

// DeleteWorktree removes the worktree associated with taskID, discarding any
// uncommitted changes.
// Server route: DELETE /worktree/{taskID}
func (c *Client) DeleteWorktree(ctx context.Context, taskID string) error {
	if err := c.doJSON(ctx, http.MethodDelete, "/worktree/"+taskID, nil, nil); err != nil {
		return fmt.Errorf("delete worktree: %w", err)
	}
	return nil
}

// MergeResult holds the outcome of a worktree merge.
//
// NothingToCommit is the typed signal that the worktree was empty AND its
// HEAD was already reachable from the merge target — i.e. there was nothing
// to do. Callers MUST check this flag rather than inferring no-op from
// `Commit == ""`. An empty Commit without NothingToCommit==true means the
// sandbox response was malformed or the contract was violated.
type MergeResult struct {
	Status          string           `json:"status"`
	Commit          string           `json:"commit,omitempty"`
	Note            string           `json:"note,omitempty"`
	NothingToCommit bool             `json:"nothing_to_commit,omitempty"`
	FilesChanged    []FileChangeInfo `json:"files_changed,omitempty"`
}

// MergeOption configures optional parameters for MergeWorktree.
type MergeOption func(*mergeOptions)

type mergeOptions struct {
	targetBranch  string
	commitMessage string
	trailers      map[string]string
	keepWorktree  bool
}

// WithTargetBranch sets the branch to merge into (default: current HEAD branch).
func WithTargetBranch(branch string) MergeOption {
	return func(o *mergeOptions) { o.targetBranch = branch }
}

// WithCommitMessage sets the commit message for the worktree commit.
func WithCommitMessage(msg string) MergeOption {
	return func(o *mergeOptions) { o.commitMessage = msg }
}

// WithKeepWorktree prevents the server from deleting the worktree after merge.
// The caller is responsible for calling DeleteWorktree when done.
func WithKeepWorktree() MergeOption {
	return func(o *mergeOptions) { o.keepWorktree = true }
}

// WithTrailer appends a git trailer to the commit message.
func WithTrailer(key, value string) MergeOption {
	return func(o *mergeOptions) {
		if o.trailers == nil {
			o.trailers = make(map[string]string)
		}
		o.trailers[key] = value
	}
}

// TrailersFromOptions runs the given MergeOption list against the internal
// options struct and returns the resulting trailer map. Intended for tests
// in other packages that want to assert on provenance trailers applied to a
// MergeWorktree call without exposing the unexported options type.
func TrailersFromOptions(opts []MergeOption) map[string]string {
	var o mergeOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.trailers == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(o.trailers))
	maps.Copy(out, o.trailers)
	return out
}

// MergeWorktree commits all staged and unstaged changes inside the worktree,
// then merges them back into the target branch (or the main repository's current branch).
// Server route: POST /worktree/{taskID}/merge  body: {"target_branch": "...", "commit_message": "...", "trailers": {...}}
func (c *Client) MergeWorktree(ctx context.Context, taskID string, opts ...MergeOption) (*MergeResult, error) {
	var o mergeOptions
	for _, opt := range opts {
		opt(&o)
	}

	body := struct {
		TargetBranch  string            `json:"target_branch,omitempty"`
		CommitMessage string            `json:"commit_message,omitempty"`
		Trailers      map[string]string `json:"trailers,omitempty"`
		KeepWorktree  bool              `json:"keep_worktree,omitempty"`
	}{
		TargetBranch:  o.targetBranch,
		CommitMessage: o.commitMessage,
		Trailers:      o.trailers,
		KeepWorktree:  o.keepWorktree,
	}

	var result MergeResult
	if err := c.doJSON(ctx, http.MethodPost, "/worktree/"+taskID+"/merge", body, &result); err != nil {
		return nil, fmt.Errorf("merge worktree: %w", err)
	}
	return &result, nil
}

// CreateBranch creates a git branch in the main repository.
//
// Returns nil when the branch is created OR when it already exists AT THE
// REQUESTED BASE (idempotent — the caller's precondition is satisfied
// either way).
//
// Returns a wrapped ErrBranchExistsAtDifferentBase when the branch exists
// but points at a different commit than the requested base. Invariant A3
// from the task-11 worktree audit: silent reuse of a drifted branch was
// how stale content could leak into a new requirement execution. Callers
// that genuinely want to reset the branch must DeleteBranch first and
// then retry CreateBranch — explicit intent.
//
// Server route: POST /branch  body: {"name": branchName, "base": baseRef}
func (c *Client) CreateBranch(ctx context.Context, name, base string) error {
	body := struct {
		Name string `json:"name"`
		Base string `json:"base,omitempty"`
	}{Name: name, Base: base}
	if err := c.doJSON(ctx, http.MethodPost, "/branch", body, nil); err != nil {
		// server error 409 is A3's "exists at different base" refusal.
		// Surface it as a typed sentinel so callers can errors.Is-match
		// without string parsing.
		if strings.Contains(err.Error(), "server error 409") {
			return fmt.Errorf("create branch %q: %w", name, ErrBranchExistsAtDifferentBase)
		}
		return fmt.Errorf("create branch: %w", err)
	}
	return nil
}

// MergeBranchesCommit identifies a merge commit produced by MergeBranches.
type MergeBranchesCommit struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

// MergeBranchesRequest describes a plan-level branch assembly: create the
// target branch from base (force-reset if it exists) and sequentially merge
// each source branch into it with --no-ff.
type MergeBranchesRequest struct {
	Target   string
	Base     string // empty = HEAD
	Branches []string
	Trailers map[string]string
}

// MergeBranchesResult reports the outcome. Status is "merged" on full
// success or "conflict" when a source branch could not merge cleanly.
// MergeCommits carries any branches that landed before the conflict, so the
// caller can surface partial progress to humans without re-deriving it
// from git log.
type MergeBranchesResult struct {
	Status            string                `json:"status"`
	Target            string                `json:"target"`
	MergeCommits      []MergeBranchesCommit `json:"merge_commits,omitempty"`
	ConflictingBranch string                `json:"conflicting_branch,omitempty"`
	Error             string                `json:"error,omitempty"`
}

// ErrMergeBranchesConflict is returned when a source branch cannot merge
// cleanly into the target. Callers inspect the embedded MergeBranchesResult
// for the conflicting branch name and the list of merges that did land.
// This is a normal caller-actionable error — it does NOT imply the sandbox
// is wedged (use errors.Is(err, ErrNeedsReconciliation) for that).
var ErrMergeBranchesConflict = errors.New("merge-branches conflict")

// MergeBranches assembles a set of source branches onto a target branch via
// sequential --no-ff merges. Used by plan-manager at plan-complete time to
// land every completed requirement branch on a plan branch (invariant B1).
// Returns a wrapped ErrMergeBranchesConflict on 409 so callers can surface
// the structured conflict payload without retrying the request. Other
// non-2xx responses (including the catastrophic 503 needs_reconciliation
// path) return the normal wrapped client error.
// Server route: POST /git/merge-branches
func (c *Client) MergeBranches(ctx context.Context, req MergeBranchesRequest) (*MergeBranchesResult, error) {
	body := struct {
		Target   string            `json:"target"`
		Base     string            `json:"base,omitempty"`
		Branches []string          `json:"branches"`
		Trailers map[string]string `json:"trailers,omitempty"`
	}{
		Target:   req.Target,
		Base:     req.Base,
		Branches: req.Branches,
		Trailers: req.Trailers,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("merge branches: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/git/merge-branches", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("merge branches: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("merge branches: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("merge branches: read response: %w", err)
	}

	var result MergeBranchesResult
	// 200 and 409 both carry structured JSON payloads the caller needs.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("merge branches: decode %d response: %w", resp.StatusCode, err)
		}
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return &result, nil
	case http.StatusConflict:
		return &result, fmt.Errorf("%w: target=%s conflicting_branch=%s",
			ErrMergeBranchesConflict, result.Target, result.ConflictingBranch)
	}

	// Extract error + error_code from the generic error body shape.
	var errBody struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	_ = json.Unmarshal(raw, &errBody)
	if errBody.ErrorCode == errCodeNeedsReconciliation {
		return nil, fmt.Errorf("merge branches: %w: server %d: %s",
			ErrNeedsReconciliation, resp.StatusCode, errBody.Error)
	}
	msg := errBody.Error
	if msg == "" {
		msg = fmt.Sprintf("server error %d", resp.StatusCode)
	}
	return nil, fmt.Errorf("merge branches: server error %d: %s", resp.StatusCode, msg)
}

// AncestryResult reports the three-way answer from GET /git/ancestry:
// whether each ref resolves, and whether the ancestor/descendant relation
// holds. All three fields are primitive — callers can render structured
// audit reports without catching HTTP errors for missing refs.
type AncestryResult struct {
	AncestorExists   bool `json:"ancestor_exists"`
	DescendantExists bool `json:"descendant_exists"`
	IsAncestor       bool `json:"is_ancestor"`
}

// Ancestry answers "is ancestor an ancestor of descendant, and do both
// exist?" Read-only — no branch creation or index mutation. Used by the
// C3 divergence-detector endpoint to verify every completed requirement
// branch actually landed on the plan's AssembledBranch (or on HEAD when
// the plan didn't go through B1 assembly).
// Server route: POST /git/ancestry
func (c *Client) Ancestry(ctx context.Context, ancestor, descendant string) (*AncestryResult, error) {
	body := struct {
		Ancestor   string `json:"ancestor"`
		Descendant string `json:"descendant"`
	}{Ancestor: ancestor, Descendant: descendant}
	var result AncestryResult
	if err := c.doJSON(ctx, http.MethodPost, "/git/ancestry", body, &result); err != nil {
		return nil, fmt.Errorf("ancestry: %w", err)
	}
	return &result, nil
}

// DeleteBranch force-deletes a branch. Used during restructure retries to
// discard polluted state and start fresh.
// Server route: DELETE /branch/{name}
func (c *Client) DeleteBranch(ctx context.Context, name string) error {
	if err := c.doJSON(ctx, http.MethodDelete, "/branch/"+name, nil, nil); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}
	return nil
}

// ListWorktreeFiles returns all tracked and untracked files in the worktree.
// Server route: GET /worktree/{taskID}/files
func (c *Client) ListWorktreeFiles(ctx context.Context, taskID string) ([]FileEntry, error) {
	var entries []FileEntry
	if err := c.doGet(ctx, "/worktree/"+taskID+"/files", nil, &entries); err != nil {
		return nil, fmt.Errorf("list worktree files: %w", err)
	}
	return entries, nil
}

// ---------------------------------------------------------------------------
// File operations
// ---------------------------------------------------------------------------

// ReadFile returns the contents of path inside the taskID worktree.
// Server route: GET /file?task_id=X&path=Y
func (c *Client) ReadFile(ctx context.Context, taskID, path string) (string, error) {
	params := url.Values{"task_id": {taskID}, "path": {path}}
	var result struct {
		Content string `json:"content"`
	}
	if err := c.doGet(ctx, "/file", params, &result); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return result.Content, nil
}

// WriteFile writes content to path inside the taskID worktree, creating
// intermediate directories as needed.
// Server route: PUT /file  body: {"task_id", "path", "content"}
func (c *Client) WriteFile(ctx context.Context, taskID, path, content string) error {
	body := struct {
		TaskID  string `json:"task_id"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}{TaskID: taskID, Path: path, Content: content}
	if err := c.doJSON(ctx, http.MethodPut, "/file", body, nil); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// ListDir lists the entries in directory path inside the taskID worktree.
// Server route: POST /list  body: {"task_id", "path"}
func (c *Client) ListDir(ctx context.Context, taskID, path string) ([]FileEntry, error) {
	body := struct {
		TaskID string `json:"task_id"`
		Path   string `json:"path"`
	}{TaskID: taskID, Path: path}
	var result struct {
		Entries []FileEntry `json:"entries"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/list", body, &result); err != nil {
		return nil, fmt.Errorf("list dir: %w", err)
	}
	return result.Entries, nil
}

// Search runs a regex search for pattern inside the taskID worktree.
// fileGlob filters by filename pattern.
// Server route: POST /search  body: {"task_id", "pattern", "file_glob"}
func (c *Client) Search(ctx context.Context, taskID, pattern, fileGlob string) ([]SearchMatch, error) {
	body := struct {
		TaskID   string `json:"task_id"`
		Pattern  string `json:"pattern"`
		FileGlob string `json:"file_glob,omitempty"`
	}{TaskID: taskID, Pattern: pattern, FileGlob: fileGlob}
	var result struct {
		Matches []SearchMatch `json:"matches"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/search", body, &result); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return result.Matches, nil
}

// SearchMatch represents a single search result.
type SearchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Git operations
// ---------------------------------------------------------------------------

// GitStatus returns the output of `git status --porcelain` inside the taskID
// worktree.
// Server route: POST /git/status  body: {"task_id"}
func (c *Client) GitStatus(ctx context.Context, taskID string) (string, error) {
	body := struct {
		TaskID string `json:"task_id"`
	}{TaskID: taskID}
	var result struct {
		Output string `json:"output"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/git/status", body, &result); err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	return result.Output, nil
}

// GitCommit stages all changes and creates a commit with the given message
// inside the taskID worktree. Returns the commit result including hash and
// changed files, or status "nothing_to_commit" when there are no changes.
// Server route: POST /git/commit  body: {"task_id", "message"}
func (c *Client) GitCommit(ctx context.Context, taskID, message string) (*CommitResult, error) {
	body := struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}{TaskID: taskID, Message: message}
	var result CommitResult
	if err := c.doJSON(ctx, http.MethodPost, "/git/commit", body, &result); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}
	return &result, nil
}

// GitDiff returns the output of `git diff` (staged + unstaged) inside the
// taskID worktree.
// Server route: POST /git/diff  body: {"task_id"}
func (c *Client) GitDiff(ctx context.Context, taskID string) (string, error) {
	body := struct {
		TaskID string `json:"task_id"`
	}{TaskID: taskID}
	var result struct {
		Output string `json:"output"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/git/diff", body, &result); err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return result.Output, nil
}

// ---------------------------------------------------------------------------
// Command execution
// ---------------------------------------------------------------------------

// Exec runs command inside the taskID worktree with a server-side timeout of
// timeoutMs milliseconds. The server kills the process and sets TimedOut=true
// when the deadline is exceeded.
// Server route: POST /exec  body: {"task_id", "command", "timeout_ms"}
func (c *Client) Exec(ctx context.Context, taskID, command string, timeoutMs int) (*ExecResult, error) {
	body := struct {
		TaskID    string `json:"task_id"`
		Command   string `json:"command"`
		TimeoutMs int    `json:"timeout_ms"`
	}{TaskID: taskID, Command: command, TimeoutMs: timeoutMs}
	var result ExecResult
	if err := c.doJSON(ctx, http.MethodPost, "/exec", body, &result); err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Package installation
// ---------------------------------------------------------------------------

// InstallResult holds the outcome of a package install inside the sandbox.
type InstallResult struct {
	Status   string `json:"status"` // installed, failed
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
}

// Install installs packages inside the sandbox container using the specified
// package manager (apt, npm, pip, go).
// Server route: POST /install  body: {"task_id", "package_manager", "packages"}
func (c *Client) Install(ctx context.Context, taskID, packageManager string, packages []string) (*InstallResult, error) {
	body := struct {
		TaskID         string   `json:"task_id"`
		PackageManager string   `json:"package_manager"`
		Packages       []string `json:"packages"`
	}{TaskID: taskID, PackageManager: packageManager, Packages: packages}
	var result InstallResult
	if err := c.doJSON(ctx, http.MethodPost, "/install", body, &result); err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// Health pings the sandbox server. Returns nil when the server is reachable
// and healthy, otherwise an error describing the failure.
func (c *Client) Health(ctx context.Context) error {
	if err := c.doGet(ctx, "/health", nil, nil); err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal HTTP helpers
// ---------------------------------------------------------------------------

// doJSON sends a request with an optional JSON body and decodes the JSON
// response into result (if non-nil). method should be an HTTP verb constant
// (e.g. http.MethodPost). path must start with "/".
func (c *Client) doJSON(ctx context.Context, method, path string, body, result any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.do(req, result)
}

// doGet sends a GET request with optional query parameters and decodes the
// JSON response into result (if non-nil).
func (c *Client) doGet(ctx context.Context, path string, params url.Values, result any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	return c.do(req, result)
}

// do executes req, reads at most maxResponseBytes of the response body, and
// decodes into result. HTTP status >= 400 is turned into an error; if the
// server included a JSON error body {"error": "..."} the message is extracted.
func (c *Client) do(req *http.Request, result any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		// Attempt to extract a structured error message + error_code from the
		// response body. error_code=needs_reconciliation signals the sandbox
		// main repo is wedged and callers must not retry.
		var errBody struct {
			Error     string `json:"error"`
			ErrorCode string `json:"error_code"`
		}
		_ = json.Unmarshal(data, &errBody)
		msg := errBody.Error
		if msg == "" {
			msg = fmt.Sprintf("server error %d", resp.StatusCode)
		}
		if errBody.ErrorCode == errCodeNeedsReconciliation {
			return fmt.Errorf("%w: server %d: %s", ErrNeedsReconciliation, resp.StatusCode, msg)
		}
		if errBody.Error != "" {
			return fmt.Errorf("server error %d: %s", resp.StatusCode, errBody.Error)
		}
		return fmt.Errorf("server error %d", resp.StatusCode)
	}

	if result != nil && len(data) > 0 {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}
