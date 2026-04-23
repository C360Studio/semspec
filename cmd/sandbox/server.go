package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// errCodeNeedsReconciliation is the JSON error_code returned when the main
// repository is wedged in an inconsistent state (a merge restore failed and
// self-heal could not recover). Callers match on this to distinguish a true
// infrastructure catastrophe from a normal merge conflict or transient race.
const errCodeNeedsReconciliation = "needs_reconciliation"

// errSandboxNeedsReconciliation is a sentinel wrapped into errors returned by
// internal handlers when the main repo is unrecoverable. handleMergeWorktree
// and similar mutation entry points check errors.Is against it and translate
// to 503 + errCodeNeedsReconciliation. Keeping it as a sentinel (vs a special
// return bool) lets error-wrapping chains preserve the signal.
var errSandboxNeedsReconciliation = errors.New("sandbox needs reconciliation")

// Server handles sandbox HTTP API requests.
// All file and command operations are scoped to a worktree identified by task_id.
type Server struct {
	repoPath       string // absolute path to mounted repository
	worktreeRoot   string // {repoPath}/.semspec/worktrees
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	maxOutputBytes int
	maxFileSize    int64
	logger         *slog.Logger

	// repoMu serializes operations that mutate the main repo's HEAD or branch
	// state (checkout, merge, branch create). Without this, concurrent merges
	// targeting different branches would race on the working directory.
	repoMu sync.Mutex

	// needsReconciliation is set when the main repo is left in an unrecoverable
	// state — a merge's restore step failed AND self-heal (merge --abort /
	// reset --hard) also failed. While set, merge and branch endpoints refuse
	// requests with HTTP 503 + errCodeNeedsReconciliation so that plan
	// execution halts rather than silently operating on a drifted HEAD.
	// Cleared via POST /admin/reconcile.
	needsReconciliation atomic.Bool
}

// taskIDMain is a reserved task_id that maps to the main workspace (repoPath)
// instead of a worktree. Non-execution agents (planner, plan-reviewer) use this
// to run read-only commands against the repo without a dedicated worktree.
const taskIDMain = "main"

// worktreeFor returns the working directory for a task_id.
// "main" maps to the repo root; all other IDs map to their worktree.
// Returns empty string if the worktree doesn't exist.
func (s *Server) worktreeFor(taskID string) string {
	if taskID == taskIDMain {
		return s.repoPath
	}
	wt := filepath.Join(s.worktreeRoot, taskID)
	if _, err := os.Stat(wt); err != nil {
		return ""
	}
	return wt
}

// RegisterRoutes binds all HTTP handlers to the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)

	// Worktree lifecycle.
	mux.HandleFunc("POST /worktree", s.handleCreateWorktree)
	mux.HandleFunc("GET /worktree/{taskID}", s.handleWorktreeExists)
	mux.HandleFunc("DELETE /worktree/{taskID}", s.handleDeleteWorktree)
	mux.HandleFunc("POST /worktree/{taskID}/merge", s.handleMergeWorktree)
	mux.HandleFunc("GET /worktree/{taskID}/files", s.handleListWorktreeFiles)

	// Branch management.
	mux.HandleFunc("POST /branch", s.handleCreateBranch)

	// File operations (scoped to worktree).
	mux.HandleFunc("PUT /file", s.handleWriteFile)
	mux.HandleFunc("GET /file", s.handleReadFile)
	mux.HandleFunc("POST /list", s.handleList)
	mux.HandleFunc("POST /search", s.handleSearch)

	// Git operations (scoped to worktree).
	mux.HandleFunc("POST /git/status", s.handleGitStatus)
	mux.HandleFunc("POST /git/commit", s.handleGitCommit)
	mux.HandleFunc("POST /git/diff", s.handleGitDiff)
	mux.HandleFunc("POST /git/branch-diff", s.handleGitBranchDiff)
	mux.HandleFunc("POST /git/branch-file-diff", s.handleGitBranchFileDiff)

	// Command execution (scoped to worktree).
	mux.HandleFunc("POST /exec", s.handleExec)

	// Package installation.
	mux.HandleFunc("POST /install", s.handleInstall)

	// Workspace browser (read-only).
	mux.HandleFunc("GET /workspace/tasks", s.handleWorkspaceTasks)
	mux.HandleFunc("GET /workspace/tree", s.handleWorkspaceTree)
	mux.HandleFunc("GET /workspace/download", s.handleWorkspaceDownload)

	// Admin: clear the needs-reconciliation flag after an operator has fixed
	// the main repo manually. No auth — the sandbox is a local-network service
	// bound to a single workspace.
	mux.HandleFunc("POST /admin/reconcile", s.handleReconcile)
}

// refuseIfNeedsReconciliation returns true (and writes a 503 response) when the
// repo is in needs-reconciliation state. Mutation endpoints that affect the
// main repo must call this at entry so callers get a distinctive error_code
// they can match on, not a generic 500 or a successful-looking result.
func (s *Server) refuseIfNeedsReconciliation(w http.ResponseWriter) bool {
	if !s.needsReconciliation.Load() {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":      "sandbox main repo requires operator reconciliation before further merges can proceed",
		"error_code": errCodeNeedsReconciliation,
	})
	return true
}

// handleReconcile clears the needs-reconciliation flag after an operator has
// manually fixed the main repo. Restricted to loopback (defense-in-depth —
// matches the local-workspace posture of other sandbox endpoints but makes
// the administrative surface explicit: even if the sandbox port ever gets
// exposed, an external request can't flip the flag). The log record captures
// the current HEAD + working-tree status so the operator's "I attest the
// repo is healthy" signal is auditable after the fact.
func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		writeError(w, http.StatusForbidden, "reconcile requires loopback")
		return
	}
	head := s.currentHEAD(r.Context())
	status, _ := gitOutput(r.Context(), s.repoPath, "status", "--porcelain")
	was := s.needsReconciliation.Swap(false)
	s.logger.Warn("needs-reconciliation flag cleared by operator",
		"was_set", was,
		"head", head,
		"status_at_clear", strings.TrimSpace(status),
	)
	writeJSON(w, http.StatusOK, map[string]any{"status": "cleared", "was_set": was})
}

// isLoopback reports whether the remote address refers to the loopback
// interface. Accepts both IPv4 (127.0.0.0/8) and IPv6 ([::1]) forms, with
// or without a port suffix. An empty remote addr (e.g. from httptest or a
// unix socket) is treated as loopback.
func isLoopback(remoteAddr string) bool {
	if remoteAddr == "" {
		return true
	}
	host := remoteAddr
	if i := strings.LastIndex(remoteAddr, ":"); i >= 0 {
		host = remoteAddr[:i]
	}
	host = strings.Trim(host, "[]")
	if host == "::1" || host == "localhost" {
		return true
	}
	return strings.HasPrefix(host, "127.")
}

// ---------------------------------------------------------------------------
// Request / Response types — unexported because this is package main.
// Tests in the same package can still reference them directly.
// ---------------------------------------------------------------------------

type worktreeCreateRequest struct {
	TaskID     string `json:"task_id"`
	BaseBranch string `json:"base_branch,omitempty"` // default: HEAD
}

type worktreeCreateResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

type fileWriteRequest struct {
	TaskID  string `json:"task_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type fileResponse struct {
	Content string `json:"content"`
	Size    int    `json:"size"`
}

type execRequest struct {
	TaskID    string `json:"task_id"`
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type execResponse struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exit_code"`
	TimedOut       bool   `json:"timed_out"`
	Classification string `json:"classification,omitempty"`
	MissingCommand string `json:"missing_command,omitempty"`
}

type installRequest struct {
	TaskID         string   `json:"task_id"`
	PackageManager string   `json:"package_manager"` // apt, npm, pip, go
	Packages       []string `json:"packages"`
}

type installResponse struct {
	Status   string `json:"status"` // installed, failed
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

type listRequest struct {
	TaskID string `json:"task_id"`
	Path   string `json:"path"`
}

type listEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type listResponse struct {
	Entries []listEntry `json:"entries"`
}

type searchRequest struct {
	TaskID   string `json:"task_id"`
	Pattern  string `json:"pattern"`
	FileGlob string `json:"file_glob,omitempty"`
}

type searchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type searchResponse struct {
	Matches []searchMatch `json:"matches"`
}

type gitCommitRequest struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

type gitCommitResponse struct {
	Status       string           `json:"status"`
	Hash         string           `json:"hash,omitempty"`
	FilesChanged []fileChangeInfo `json:"files_changed,omitempty"`
}

// fileChangeInfo describes a file changed in a commit.
type fileChangeInfo struct {
	Path      string `json:"path"`      // relative to worktree root
	Operation string `json:"operation"` // add, modify, delete, rename
}

type gitStatusResponse struct {
	Output string `json:"output"`
}

type gitDiffResponse struct {
	Output string `json:"output"`
}

// branchDiffFile describes one file changed between base..branch.
type branchDiffFile struct {
	Path       string `json:"path"`
	OldPath    string `json:"old_path,omitempty"` // set for renames
	Status     string `json:"status"`             // added, modified, deleted, renamed, copied, binary
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
	Binary     bool   `json:"binary,omitempty"`
}

type branchDiffRequest struct {
	Branch string `json:"branch"`
	Base   string `json:"base"` // defaults to "main"
}

type branchDiffResponse struct {
	Base            string           `json:"base"`
	Branch          string           `json:"branch"`
	Files           []branchDiffFile `json:"files"`
	TotalInsertions int              `json:"total_insertions"`
	TotalDeletions  int              `json:"total_deletions"`
}

type branchFileDiffRequest struct {
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Path   string `json:"path"`
}

type branchFileDiffResponse struct {
	Patch string `json:"patch"`
}

// ---------------------------------------------------------------------------
// Route handlers
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCreateWorktree creates a new git worktree for a task.
// POST /worktree  {"task_id": "abc123"}
func (s *Server) handleCreateWorktree(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfNeedsReconciliation(w) {
		return
	}
	var req worktreeCreateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)

	if _, err := os.Stat(worktreePath); err == nil {
		writeError(w, http.StatusConflict, "worktree already exists for task_id: "+req.TaskID)
		return
	}

	branch := "agent/" + req.TaskID
	ctx := r.Context()

	base := "HEAD"
	if req.BaseBranch != "" {
		if !isValidBranchName(req.BaseBranch) {
			writeError(w, http.StatusBadRequest, "invalid base_branch")
			return
		}
		base = req.BaseBranch
	}

	// Validate the base reference exists before attempting worktree creation.
	// This turns a cryptic git error into an actionable 400 response.
	if _, err := gitOutput(ctx, s.repoPath, "rev-parse", "--verify", base); err != nil {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("base reference %q does not exist (does the repository have at least one commit?)", base))
		return
	}

	if err := runGit(ctx, s.repoPath, "worktree", "add", "-b", branch, worktreePath, base); err != nil {
		s.logger.Error("git worktree add failed", "task_id", req.TaskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create worktree: "+err.Error())
		return
	}

	// Copy git user config into worktree for proper commit attribution.
	s.copyGitConfig(ctx, worktreePath)

	writeJSON(w, http.StatusCreated, worktreeCreateResponse{
		Status: "created",
		Path:   worktreePath,
		Branch: branch,
	})
}

// handleWorktreeExists reports whether a worktree directory exists.
// GET /worktree/{taskID} → 200 if present, 404 if not.
func (s *Server) handleWorktreeExists(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if !isValidID(taskID) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	if _, err := os.Stat(worktreePath); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleDeleteWorktree removes a worktree and its branch.
// DELETE /worktree/{taskID}
func (s *Server) handleDeleteWorktree(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	ctx := r.Context()

	if err := s.removeWorktree(ctx, worktreePath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove worktree: "+err.Error())
		return
	}

	// Delete the branch — best-effort, ignore errors.
	_ = runGit(ctx, s.repoPath, "branch", "-D", "agent/"+taskID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// mergeRequest is the optional JSON body for POST /worktree/{taskID}/merge.
type mergeRequest struct {
	TargetBranch  string            `json:"target_branch,omitempty"`  // default: current HEAD branch
	CommitMessage string            `json:"commit_message,omitempty"` // default: "agent: {taskID} task completion"
	Trailers      map[string]string `json:"trailers,omitempty"`       // appended as git trailers
	KeepWorktree  bool              `json:"keep_worktree,omitempty"`  // skip worktree deletion after merge
}

// mergeResponse is the JSON response from POST /worktree/{taskID}/merge.
type mergeResponse struct {
	Status       string           `json:"status"`
	Commit       string           `json:"commit,omitempty"`
	Note         string           `json:"note,omitempty"`
	FilesChanged []fileChangeInfo `json:"files_changed,omitempty"`
}

// handleMergeWorktree commits any pending changes in the worktree and merges
// them into the target branch (or the main repository's current branch) via --no-ff.
// POST /worktree/{taskID}/merge  body (optional): {"target_branch": "...", "commit_message": "...", "trailers": {...}}
func (s *Server) handleMergeWorktree(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfNeedsReconciliation(w) {
		return
	}
	taskID := r.PathValue("taskID")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	// Parse optional request body.
	var req mergeRequest
	if r.ContentLength > 0 {
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	if req.TargetBranch != "" && !isValidBranchName(req.TargetBranch) {
		writeError(w, http.StatusBadRequest, "invalid target_branch")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	ctx := r.Context()

	nothingToCommit, stageErr := stageAndCommitWorktree(ctx, worktreePath, taskID, req)
	if stageErr != nil {
		writeError(w, stageErr.status, stageErr.msg)
		return
	}

	if nothingToCommit {
		// Nothing to merge — clean up and return success.
		if err := s.removeWorktree(ctx, worktreePath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove worktree: "+err.Error())
			return
		}
		_ = runGit(ctx, s.repoPath, "branch", "-D", "agent/"+taskID)
		writeJSON(w, http.StatusOK, mergeResponse{Status: "merged", Note: "nothing_to_commit"})
		return
	}

	// Get the commit hash from the worktree.
	hash, err := gitOutput(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get commit hash: "+err.Error())
		return
	}
	hash = strings.TrimSpace(hash)

	// Serialize main-repo mutations (checkout + merge) to prevent concurrent
	// merges from racing on the working directory.
	s.repoMu.Lock()
	mergeResp, mergeErr := s.mergeIntoMainRepo(ctx, taskID, hash, req)
	s.repoMu.Unlock()

	if mergeErr != nil {
		writeMergeError(w, mergeErr)
		return
	}

	// Clean up worktree and branch on success — unless caller requested keep.
	if !req.KeepWorktree {
		if err := s.removeWorktree(ctx, worktreePath); err != nil {
			s.logger.Warn("failed to remove worktree after successful merge", "task_id", taskID, "error", err)
		}
		_ = runGit(ctx, s.repoPath, "branch", "-D", "agent/"+taskID)
	}

	writeJSON(w, http.StatusOK, mergeResp)
}

// httpErrorInfo carries a status code + message for helpers that need to
// report an HTTP error back through the handler.
type httpErrorInfo struct {
	status int
	msg    string
}

// stageAndCommitWorktree stages all changes in the worktree and creates a
// commit with the configured message and trailers. Returns nothingToCommit=true
// when the index is clean (no-op case). The caller handles the nothing-to-
// commit cleanup and the subsequent merge flow.
func stageAndCommitWorktree(ctx context.Context, worktreePath, taskID string, req mergeRequest) (nothingToCommit bool, errInfo *httpErrorInfo) {
	if err := runGit(ctx, worktreePath, "add", "-A"); err != nil {
		return false, &httpErrorInfo{http.StatusInternalServerError, "failed to stage changes: " + err.Error()}
	}
	commitMsg := req.CommitMessage
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("agent: %s task completion", taskID)
	}
	commitMsg = appendTrailers(commitMsg, req.Trailers)
	commitErr := runGit(ctx, worktreePath, "commit", "-m", commitMsg)
	if commitErr == nil {
		return false, nil
	}
	if strings.Contains(commitErr.Error(), "nothing to commit") {
		return true, nil
	}
	return false, &httpErrorInfo{http.StatusInternalServerError, "failed to commit: " + commitErr.Error()}
}

// writeMergeError writes the appropriate response for a mergeIntoMainRepo
// failure. Catastrophic (needs-reconciliation) becomes 503 with a
// distinguishing error_code; normal merge conflicts become 409.
func writeMergeError(w http.ResponseWriter, mergeErr error) {
	if errors.Is(mergeErr, errSandboxNeedsReconciliation) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":      mergeErr.Error(),
			"error_code": errCodeNeedsReconciliation,
		})
		return
	}
	writeError(w, http.StatusConflict, "merge conflict: "+mergeErr.Error())
}

// selfHealAfterFailedRestore attempts to return the main repo to origBranch
// after a merge failed AND the initial `checkout origBranch` also failed
// (typically: the failed merge left the working tree dirty with conflict
// markers, and checkout refuses to overwrite). Tries `merge --abort` (clears
// MERGE_HEAD and resets the index for partially-applied merges) then
// `reset --hard origBranch` (forces the working tree and index to match
// origBranch). Returns nil on success; on failure the caller must flip the
// needs-reconciliation flag since the repo is now in an unknown state.
// Must be called under s.repoMu.
func (s *Server) selfHealAfterFailedRestore(ctx context.Context, origBranch string, restoreErr error) error {
	// merge --abort is expected to succeed in the common case (conflict
	// markers in working tree from the just-failed merge). Even if it fails
	// because no merge is in progress, reset --hard below will force
	// consistency, so we treat abort errors as non-fatal — but log at Info
	// so the recovery sequence is visible without cranking the log level.
	if abortErr := runGit(ctx, s.repoPath, "merge", "--abort"); abortErr != nil {
		s.logger.Info("merge --abort during self-heal returned an error (continuing to reset --hard)",
			"orig_branch", origBranch, "abort_error", abortErr, "restore_error", restoreErr,
		)
	}
	if resetErr := runGit(ctx, s.repoPath, "reset", "--hard", origBranch); resetErr != nil {
		return fmt.Errorf("reset --hard %s (HEAD=%s): %w", origBranch, s.currentHEAD(ctx), resetErr)
	}
	// reset --hard updated the working tree to match origBranch's tree but did
	// not switch the branch pointer — HEAD is still on TargetBranch. Move it.
	if err := runGit(ctx, s.repoPath, "checkout", origBranch); err != nil {
		return fmt.Errorf("post-reset checkout %s (HEAD=%s): %w", origBranch, s.currentHEAD(ctx), err)
	}
	// Post-condition: don't trust the git commands to have done what we asked.
	// Verify HEAD is actually on origBranch before returning nil, otherwise
	// the caller would think the repo is healed when it's not — exactly the
	// "lie about state" failure mode invariant A2 was added to prevent.
	if head := s.currentHEAD(ctx); head != origBranch {
		return fmt.Errorf("self-heal post-condition: HEAD=%s, want %s", head, origBranch)
	}
	return nil
}

// currentHEAD returns the short branch name of HEAD, or "<unknown>" on error.
// Used in self-heal error messages so operators have the exact wedge state
// without needing to exec into the container to run git themselves.
func (s *Server) currentHEAD(ctx context.Context) string {
	out, err := gitOutput(ctx, s.repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "<unknown>"
	}
	return strings.TrimSpace(out)
}

// mergeIntoMainRepo performs the checkout + merge + file-change-parse sequence.
// Must be called under s.repoMu to prevent concurrent mutations.
func (s *Server) mergeIntoMainRepo(ctx context.Context, taskID, hash string, req mergeRequest) (mergeResponse, error) {
	// If target_branch is set, save the current branch so we can restore on failure.
	var origBranch string
	if req.TargetBranch != "" {
		out, err := gitOutput(ctx, s.repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return mergeResponse{}, fmt.Errorf("failed to determine current branch: %w", err)
		}
		origBranch = strings.TrimSpace(out)

		if err := runGit(ctx, s.repoPath, "checkout", req.TargetBranch); err != nil {
			return mergeResponse{}, fmt.Errorf("failed to checkout target branch: %w", err)
		}
	}

	// Build merge commit message with trailers.
	mergeMsg := fmt.Sprintf("merge: agent task %s", taskID)
	if req.CommitMessage != "" {
		mergeMsg = req.CommitMessage
	}
	mergeMsg = appendTrailers(mergeMsg, req.Trailers)

	if err := runGit(ctx, s.repoPath, "merge", hash, "--no-ff", "-m", mergeMsg); err != nil {
		// Restore original branch on merge failure. If the plain checkout
		// fails — typically because the failed merge left conflict markers in
		// tracked files — try to self-heal with `git merge --abort` followed
		// by `git reset --hard <origBranch>`. Only if that also fails do we
		// flip the needs-reconciliation flag, since at that point the repo is
		// genuinely wedged (disk full, corrupt .git, etc.).
		if origBranch != "" {
			if restoreErr := runGit(ctx, s.repoPath, "checkout", origBranch); restoreErr != nil {
				healErr := s.selfHealAfterFailedRestore(ctx, origBranch, restoreErr)
				if healErr != nil {
					s.needsReconciliation.Store(true)
					s.logger.Error("Sandbox repo wedged — self-heal failed after restore failure; needs-reconciliation flag set",
						"task_id", taskID,
						"orig_branch", origBranch,
						"merge_error", err,
						"restore_error", restoreErr,
						"self_heal_error", healErr,
					)
					return mergeResponse{}, fmt.Errorf("%w: merge=%v restore=%v heal=%v",
						errSandboxNeedsReconciliation, err, restoreErr, healErr)
				}
				s.logger.Warn("Sandbox self-healed after failed merge-restore",
					"task_id", taskID,
					"orig_branch", origBranch,
					"merge_error", err,
					"restore_error", restoreErr,
				)
			}
		}
		return mergeResponse{}, err
	}

	// Get merge commit hash for response.
	mergeHash, _ := gitOutput(ctx, s.repoPath, "rev-parse", "HEAD")
	mergeHash = strings.TrimSpace(mergeHash)

	// Parse changed files from the merge commit.
	filesChanged := s.parseChangedFiles(ctx, s.repoPath, mergeHash)

	return mergeResponse{
		Status:       "merged",
		Commit:       mergeHash,
		FilesChanged: filesChanged,
	}, nil
}

// branchCreateRequest is the JSON body for POST /branch.
type branchCreateRequest struct {
	Name string `json:"name"` // branch name (e.g. "semspec/scenario-auth")
	Base string `json:"base"` // base ref (default: HEAD)
}

// handleCreateBranch creates a git branch in the main repository.
// POST /branch  {"name": "semspec/scenario-auth", "base": "HEAD"}
func (s *Server) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	if s.refuseIfNeedsReconciliation(w) {
		return
	}
	var req branchCreateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Name == "" || !isValidBranchName(req.Name) {
		writeError(w, http.StatusBadRequest, "invalid or missing branch name")
		return
	}

	base := req.Base
	if base == "" {
		base = "HEAD"
	}

	ctx := r.Context()

	// Serialize branch creation against main repo.
	s.repoMu.Lock()
	err := runGit(ctx, s.repoPath, "branch", req.Name, base)
	s.repoMu.Unlock()

	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSON(w, http.StatusOK, map[string]string{"status": "exists"})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create branch: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "branch": req.Name})
}

// fileEntry matches tools/sandbox.FileEntry for JSON serialization.
type fileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// handleListWorktreeFiles lists all files tracked in a worktree.
// GET /worktree/{taskID}/files
// Returns []fileEntry (array, not wrapped in object) to match the sandbox client contract.
func (s *Server) handleListWorktreeFiles(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	ctx := r.Context()

	output, err := gitOutput(ctx, worktreePath, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list files: "+err.Error())
		return
	}

	lines := splitLines(output)
	entries := make([]fileEntry, 0, len(lines))
	for _, name := range lines {
		fe := fileEntry{Name: name}
		if info, err := os.Stat(filepath.Join(worktreePath, name)); err == nil {
			fe.IsDir = info.IsDir()
			fe.Size = info.Size()
		}
		entries = append(entries, fe)
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleWriteFile writes content to a file path inside a task's worktree.
// PUT /file  {"task_id": "abc", "path": "main.go", "content": "..."}
func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	var req fileWriteRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	absPath, err := s.resolveTaskPath(req.TaskID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	content := []byte(req.Content)
	if int64(len(content)) > s.maxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("content exceeds max file size (%d bytes)", s.maxFileSize))
		return
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create directory: "+err.Error())
		return
	}

	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"written": len(content)})
}

// handleReadFile reads a file from a task's worktree.
// GET /file?task_id=abc&path=main.go
func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	path := r.URL.Query().Get("path")

	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	absPath, err := s.resolveTaskPath(taskID, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read file: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, fileResponse{
		Content: string(content),
		Size:    len(content),
	})
}

// handleList lists directory entries within a task's worktree.
// POST /list  {"task_id": "abc", "path": "pkg/"}
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	var req listRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	absPath, err := s.resolveTaskPath(req.TaskID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "directory not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list directory: "+err.Error())
		return
	}

	var result []listEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, listEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}
	if result == nil {
		result = []listEntry{}
	}

	writeJSON(w, http.StatusOK, listResponse{Entries: result})
}

// handleSearch performs a grep-style pattern search within a task's worktree.
// POST /search  {"task_id": "abc", "pattern": "func main", "file_glob": "*.go"}
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}
	if req.Pattern == "" {
		writeError(w, http.StatusBadRequest, "pattern is required")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+req.TaskID)
		return
	}

	re, err := regexp.Compile(req.Pattern)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pattern: "+err.Error())
		return
	}

	var matches []searchMatch

	walkErr := filepath.Walk(worktreePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			// Skip .git directory.
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if req.FileGlob != "" {
			matched, _ := filepath.Match(req.FileGlob, info.Name())
			if !matched {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(worktreePath, path)
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, searchMatch{
					File: relPath,
					Line: i + 1,
					Text: line,
				})
			}
		}
		return nil
	})

	if walkErr != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+walkErr.Error())
		return
	}

	if matches == nil {
		matches = []searchMatch{}
	}

	writeJSON(w, http.StatusOK, searchResponse{Matches: matches})
}

// handleGitStatus returns the porcelain git status of a task's worktree.
// POST /git/status  {"task_id": "abc"}
func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+req.TaskID)
		return
	}

	output, err := gitOutput(r.Context(), worktreePath, "status", "--porcelain")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git status failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, gitStatusResponse{Output: output})
}

// handleGitCommit stages all changes in a worktree and commits them.
// POST /git/commit  {"task_id": "abc", "message": "feat: add handler"}
func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	var req gitCommitRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)
	ctx := r.Context()

	if err := runGit(ctx, worktreePath, "add", "-A"); err != nil {
		writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
		return
	}

	commitErr := runGit(ctx, worktreePath, "commit", "-m", req.Message)
	if commitErr != nil {
		if strings.Contains(commitErr.Error(), "nothing to commit") {
			writeJSON(w, http.StatusOK, gitCommitResponse{Status: "nothing_to_commit"})
			return
		}
		writeError(w, http.StatusInternalServerError, "git commit failed: "+commitErr.Error())
		return
	}

	hash, err := gitOutput(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get commit hash: "+err.Error())
		return
	}
	commitHash := strings.TrimSpace(hash)

	// Get changed files for provenance graph entities.
	filesChanged := s.parseChangedFiles(ctx, worktreePath, commitHash)

	writeJSON(w, http.StatusOK, gitCommitResponse{
		Status:       "committed",
		Hash:         commitHash,
		FilesChanged: filesChanged,
	})
}

// handleGitDiff returns the combined unstaged and staged diff for a worktree.
// POST /git/diff  {"task_id": "abc"}
func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+req.TaskID)
		return
	}

	ctx := r.Context()

	// Unstaged changes.
	unstaged, err := gitOutput(ctx, worktreePath, "diff")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff failed: "+err.Error())
		return
	}

	// Staged changes.
	staged, err := gitOutput(ctx, worktreePath, "diff", "--cached")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff --cached failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, gitDiffResponse{Output: unstaged + staged})
}

// handleGitBranchDiff returns per-file stats for the commits on `branch` that
// are not on `base` (i.e. `git diff base...branch --numstat --name-status`).
// Used by the UI to show what an agent actually changed on a requirement's
// branch, not the working tree of a scratch worktree.
//
// POST /git/branch-diff  {"branch": "semspec/requirement-R1", "base": "main"}
func (s *Server) handleGitBranchDiff(w http.ResponseWriter, r *http.Request) {
	var req branchDiffRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidBranchName(req.Branch) {
		writeError(w, http.StatusBadRequest, "invalid branch")
		return
	}
	base := req.Base
	if base == "" {
		base = "main"
	}
	if !isValidBranchName(base) {
		writeError(w, http.StatusBadRequest, "invalid base")
		return
	}

	ctx := r.Context()

	// Verify both refs exist so we return 404 instead of a cryptic 500.
	if _, err := gitOutput(ctx, s.repoPath, "rev-parse", "--verify", req.Branch); err != nil {
		writeError(w, http.StatusNotFound, "branch not found: "+req.Branch)
		return
	}
	if _, err := gitOutput(ctx, s.repoPath, "rev-parse", "--verify", base); err != nil {
		writeError(w, http.StatusNotFound, "base not found: "+base)
		return
	}

	spec := base + "..." + req.Branch

	numstat, err := gitOutput(ctx, s.repoPath, "diff", "--numstat", spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff --numstat failed: "+err.Error())
		return
	}
	namestatus, err := gitOutput(ctx, s.repoPath, "diff", "--name-status", spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff --name-status failed: "+err.Error())
		return
	}

	files := mergeBranchDiff(numstat, namestatus)
	totalIns, totalDel := 0, 0
	for _, f := range files {
		totalIns += f.Insertions
		totalDel += f.Deletions
	}

	writeJSON(w, http.StatusOK, branchDiffResponse{
		Base:            base,
		Branch:          req.Branch,
		Files:           files,
		TotalInsertions: totalIns,
		TotalDeletions:  totalDel,
	})
}

// handleGitBranchFileDiff returns the unified patch for a single file between
// base and branch. Separate endpoint because patches can be large and callers
// usually only want one at a time (file clicked in the UI).
//
// POST /git/branch-file-diff  {"branch": "...", "base": "...", "path": "src/x.go"}
func (s *Server) handleGitBranchFileDiff(w http.ResponseWriter, r *http.Request) {
	var req branchFileDiffRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidBranchName(req.Branch) {
		writeError(w, http.StatusBadRequest, "invalid branch")
		return
	}
	base := req.Base
	if base == "" {
		base = "main"
	}
	if !isValidBranchName(base) {
		writeError(w, http.StatusBadRequest, "invalid base")
		return
	}
	if req.Path == "" || strings.Contains(req.Path, "..") || strings.HasPrefix(req.Path, "/") {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	ctx := r.Context()
	spec := base + "..." + req.Branch

	// `--` separates path args from revision args; prevents a path that looks
	// like a revision from being interpreted as one.
	patch, err := gitOutput(ctx, s.repoPath, "diff", spec, "--", req.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, branchFileDiffResponse{Patch: patch})
}

// mergeBranchDiff joins `git diff --numstat` and `git diff --name-status`
// output into one file list keyed by path. Handles renames where numstat
// reports the new path and name-status reports `R<score>\told\tnew`.
func mergeBranchDiff(numstat, namestatus string) []branchDiffFile {
	type entry struct {
		ins, del int
		binary   bool
		status   string
		oldPath  string
	}
	files := map[string]*entry{}

	for _, line := range splitLines(numstat) {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		ins, del := parts[0], parts[1]
		// Numstat format for renames: "N\tM\told => new" or tab-separated
		// new path alone depending on diff.renames config. Trust the tab split.
		path := parts[len(parts)-1]
		e := &entry{}
		if ins == "-" && del == "-" {
			e.binary = true
		} else {
			e.ins = atoiDefault(ins, 0)
			e.del = atoiDefault(del, 0)
		}
		files[path] = e
	}

	for _, line := range splitLines(namestatus) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		code := parts[0]
		var path, oldPath string
		switch code[0] {
		case 'R', 'C':
			if len(parts) < 3 {
				continue
			}
			oldPath, path = parts[1], parts[2]
		default:
			path = parts[1]
		}
		e, ok := files[path]
		if !ok {
			e = &entry{}
			files[path] = e
		}
		e.status = statusFromCode(code)
		if oldPath != "" {
			e.oldPath = oldPath
		}
	}

	out := make([]branchDiffFile, 0, len(files))
	for path, e := range files {
		status := e.status
		if status == "" {
			if e.binary {
				status = "binary"
			} else {
				status = "modified"
			}
		}
		out = append(out, branchDiffFile{
			Path:       path,
			OldPath:    e.oldPath,
			Status:     status,
			Insertions: e.ins,
			Deletions:  e.del,
			Binary:     e.binary,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func statusFromCode(code string) string {
	if code == "" {
		return ""
	}
	switch code[0] {
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'M':
		return "modified"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "typechange"
	default:
		return "modified"
	}
}

func atoiDefault(s string, def int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// handleExec executes a shell command inside a task's worktree.
// POST /exec  {"task_id": "abc", "command": "go test ./...", "timeout_ms": 30000}
func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req execRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	workDir := s.worktreeFor(req.TaskID)
	if workDir == "" {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+req.TaskID)
		return
	}

	timeout := s.defaultTimeout
	if req.TimeoutMs > 0 {
		timeout = min(time.Duration(req.TimeoutMs)*time.Millisecond, s.maxTimeout)
	}

	stdout, stderr, exitCode, timedOut := execCommand(r.Context(), workDir, req.Command, timeout, s.maxOutputBytes)

	classification, missingCmd := classifyExec(stderr, exitCode, timedOut)

	writeJSON(w, http.StatusOK, execResponse{
		Stdout:         stdout,
		Stderr:         stderr,
		ExitCode:       exitCode,
		TimedOut:       timedOut,
		Classification: string(classification),
		MissingCommand: missingCmd,
	})
}

// handleInstall installs packages inside the sandbox container.
// POST /install  {"task_id": "abc", "package_manager": "apt", "packages": ["cargo"]}
//
// Supported package managers:
//   - apt: runs apt-get install -y <packages>
//   - npm: runs npm install -g <packages>
//   - pip: runs pip3 install <packages>
//   - go:  runs go install <packages> (each must end in @version)
//
// The task_id scopes the working directory. For "go install", the command runs
// in the worktree directory so GOPATH is correct.
func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !isValidID(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}
	if len(req.Packages) == 0 {
		writeError(w, http.StatusBadRequest, "packages is required")
		return
	}
	if len(req.Packages) > 20 {
		writeError(w, http.StatusBadRequest, "too many packages (max 20)")
		return
	}

	// Validate package names to prevent command injection.
	for _, pkg := range req.Packages {
		if !isValidPackageName(pkg) {
			writeError(w, http.StatusBadRequest, "invalid package name: "+pkg)
			return
		}
	}

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+req.TaskID)
		return
	}

	// Build the install command.
	var cmd string
	switch req.PackageManager {
	case "apt":
		cmd = "apt-get install -y " + strings.Join(req.Packages, " ")
	case "npm":
		cmd = "npm install -g " + strings.Join(req.Packages, " ")
	case "pip":
		cmd = "pip3 install " + strings.Join(req.Packages, " ")
	case "go":
		cmd = "go install " + strings.Join(req.Packages, " ")
	default:
		writeError(w, http.StatusBadRequest,
			"unsupported package_manager: "+req.PackageManager+"; valid: apt, npm, pip, go")
		return
	}

	// Use a generous timeout for installs (3 min).
	timeout := 3 * time.Minute

	stdout, stderr, exitCode, timedOut := execCommand(r.Context(), worktreePath, cmd, timeout, s.maxOutputBytes)

	status := "installed"
	if exitCode != 0 {
		status = "failed"
	}

	writeJSON(w, http.StatusOK, installResponse{
		Status:   status,
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		TimedOut: timedOut,
	})
}

// ---------------------------------------------------------------------------
// Workspace browser types
// ---------------------------------------------------------------------------

// workspaceTaskInfo describes a single active worktree in the workspace.
type workspaceTaskInfo struct {
	TaskID    string `json:"task_id"`
	FileCount int    `json:"file_count"`
	Branch    string `json:"branch"`
}

// workspaceEntry is a node in the nested file tree returned by GET /workspace/tree.
type workspaceEntry struct {
	Name     string            `json:"name"`
	Path     string            `json:"path"`
	IsDir    bool              `json:"is_dir"`
	Size     int64             `json:"size"`
	Children []*workspaceEntry `json:"children,omitempty"`
}

// dirNode is a build-time helper for constructing the nested workspaceEntry tree.
type dirNode struct {
	entry    *workspaceEntry
	children map[string]*dirNode
}

// ---------------------------------------------------------------------------
// Workspace browser handlers
// ---------------------------------------------------------------------------

// handleWorkspaceTasks lists all active worktrees with their file counts and branches.
// GET /workspace/tasks
func (s *Server) handleWorkspaceTasks(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.worktreeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []workspaceTaskInfo{})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read worktree root: "+err.Error())
		return
	}

	ctx := r.Context()
	var tasks []workspaceTaskInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID := entry.Name()
		if taskID == ".git" || !isValidID(taskID) {
			continue
		}

		worktreePath := filepath.Join(s.worktreeRoot, taskID)

		// Count all files tracked or untracked in the worktree.
		output, err := gitOutput(ctx, worktreePath, "ls-files", "--cached", "--others", "--exclude-standard")
		fileCount := 0
		if err == nil {
			fileCount = len(splitLines(output))
		}

		// Resolve the current branch name.
		branch := ""
		if b, err := gitOutput(ctx, worktreePath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			branch = strings.TrimSpace(b)
		}

		tasks = append(tasks, workspaceTaskInfo{
			TaskID:    taskID,
			FileCount: fileCount,
			Branch:    branch,
		})
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID
	})

	if tasks == nil {
		tasks = []workspaceTaskInfo{}
	}

	writeJSON(w, http.StatusOK, tasks)
}

// handleWorkspaceTree returns a nested file tree for a worktree.
// GET /workspace/tree?task_id=X
func (s *Server) handleWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+taskID)
		return
	}

	output, err := gitOutput(r.Context(), worktreePath, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list files: "+err.Error())
		return
	}

	files := splitLines(output)

	// Build a nested tree using dirNode helpers.
	root := &dirNode{
		entry:    nil,
		children: make(map[string]*dirNode),
	}

	for _, relPath := range files {
		parts := strings.Split(relPath, "/")
		cur := root
		for i, part := range parts {
			node, exists := cur.children[part]
			if !exists {
				isDir := i < len(parts)-1
				entryPath := strings.Join(parts[:i+1], "/")
				entry := &workspaceEntry{
					Name:  part,
					Path:  entryPath,
					IsDir: isDir,
				}
				if !isDir {
					// Stat the file for its size.
					if info, err := os.Stat(filepath.Join(worktreePath, relPath)); err == nil {
						entry.Size = info.Size()
					}
				}
				node = &dirNode{
					entry:    entry,
					children: make(map[string]*dirNode),
				}
				cur.children[part] = node
			}
			cur = node
		}
	}

	// Recursively collect and sort entries from a dirNode.
	var collect func(n *dirNode) []*workspaceEntry
	collect = func(n *dirNode) []*workspaceEntry {
		if len(n.children) == 0 {
			return nil
		}

		entries := make([]*workspaceEntry, 0, len(n.children))
		for _, child := range n.children {
			child.entry.Children = collect(child)
			entries = append(entries, child.entry)
		}

		// Sort: directories first, then alphabetical within each group.
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir != entries[j].IsDir {
				return entries[i].IsDir
			}
			return entries[i].Name < entries[j].Name
		})

		return entries
	}

	result := collect(root)
	if result == nil {
		result = []*workspaceEntry{}
	}

	writeJSON(w, http.StatusOK, result)
}

// handleWorkspaceDownload streams the worktree as a ZIP archive.
// GET /workspace/download?task_id=X
func (s *Server) handleWorkspaceDownload(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+taskID)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-workspace.zip"`, taskID))
	w.WriteHeader(http.StatusOK)

	zw := zip.NewWriter(w)
	defer zw.Close()

	_ = filepath.Walk(worktreePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(worktreePath, path)
		if err != nil {
			return nil
		}

		// Skip .git entries (both the .git directory in normal repos and the
		// .git file that git uses in worktrees) at any depth.
		if info.Name() == ".git" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Skip files that exceed the per-file size limit.
		if info.Size() > s.maxFileSize {
			return nil
		}

		fw, err := zw.Create(relPath)
		if err != nil {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		_, _ = io.Copy(fw, f)
		return nil
	})
}

// isValidPackageName checks that a package name is safe for shell use.
// Allows alphanumeric, hyphens, underscores, dots, slashes, @, =, and colons
// (for Go module paths like golang.org/x/tools/cmd/goimports@latest).
var validPackageRe = regexp.MustCompile(`^[a-zA-Z0-9._/@:=+~-]{1,256}$`)

func isValidPackageName(name string) bool {
	if strings.HasPrefix(name, "-") {
		return false // prevent flag injection (e.g., --pre-invoke=cmd)
	}
	return validPackageRe.MatchString(name)
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// resolveTaskPath resolves a relative path within a task's worktree to an
// absolute path, guarding against directory traversal attacks.
func (s *Server) resolveTaskPath(taskID, relPath string) (string, error) {
	if !isValidID(taskID) {
		return "", fmt.Errorf("invalid task_id")
	}
	if relPath == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("path must be relative, not absolute")
	}

	base := s.worktreeFor(taskID)
	if base == "" {
		return "", fmt.Errorf("worktree not found for task_id: %s", taskID)
	}
	resolved := filepath.Join(base, filepath.Clean(relPath))

	// Guard against escape outside the working directory.
	if !strings.HasPrefix(resolved+string(filepath.Separator), base+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes worktree boundary")
	}

	return resolved, nil
}

// ---------------------------------------------------------------------------
// Git helpers
// ---------------------------------------------------------------------------

// removeWorktree removes a worktree via git, with os.RemoveAll fallback.
func (s *Server) removeWorktree(ctx context.Context, worktreePath string) error {
	if err := runGit(ctx, s.repoPath, "worktree", "remove", "--force", worktreePath); err != nil {
		// Fallback: forcibly remove the directory and prune stale metadata.
		if _, statErr := os.Stat(worktreePath); statErr == nil {
			if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
				return fmt.Errorf("remove worktree (fallback): %w", removeErr)
			}
		}
		_ = runGit(ctx, s.repoPath, "worktree", "prune")
	}
	return nil
}

// copyGitConfig copies user.name and user.email from the main repo into the
// worktree's local config so commits are properly attributed. Failures are
// silently ignored.
func (s *Server) copyGitConfig(ctx context.Context, worktreePath string) {
	for _, key := range []string{"user.name", "user.email"} {
		val, err := gitOutput(ctx, s.repoPath, "config", key)
		if err != nil || strings.TrimSpace(val) == "" {
			continue
		}
		_ = runGit(ctx, worktreePath, "config", key, strings.TrimSpace(val))
	}
}

// parseChangedFiles runs `git diff-tree` on commitHash to extract the list of
// files modified by the commit and their operation (add, modify, delete, rename,
// copy, type_change). Errors are logged and result in a nil return — callers
// treat this as optional provenance metadata.
func (s *Server) parseChangedFiles(ctx context.Context, worktreePath, commitHash string) []fileChangeInfo {
	// Use -m to handle merge commits (which have multiple parents) — without
	// -m, diff-tree produces no output for merge commits.
	out, err := gitOutput(ctx, worktreePath, "diff-tree", "-m", "--first-parent", "--no-commit-id", "--name-status", "-r", commitHash)
	if err != nil {
		s.logger.Warn("parseChangedFiles: git diff-tree failed", "commit", commitHash, "error", err)
		return nil
	}

	var files []fileChangeInfo
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// Format: "<status>\t<path>" or "<status>\t<old>\t<new>" for renames/copies.
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}

		var op string
		switch {
		case strings.HasPrefix(parts[0], "A"):
			op = "add"
		case strings.HasPrefix(parts[0], "M"):
			op = "modify"
		case strings.HasPrefix(parts[0], "D"):
			op = "delete"
		case strings.HasPrefix(parts[0], "R"):
			op = "rename"
		case strings.HasPrefix(parts[0], "C"):
			op = "copy"
		case strings.HasPrefix(parts[0], "T"):
			op = "type_change"
		default:
			op = strings.ToLower(parts[0])
		}

		path := parts[len(parts)-1] // For renames/copies, use the destination path.
		files = append(files, fileChangeInfo{Path: path, Operation: op})
	}
	return files
}

// ---------------------------------------------------------------------------
// Identifier validation
// ---------------------------------------------------------------------------

// validIDRe matches task IDs: alphanumeric, dots, hyphens, underscores, max 256 chars.
var validIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,256}$`)

// isValidID reports whether id is a safe identifier for use as a directory name
// and git branch name component.
func isValidID(id string) bool {
	return validIDRe.MatchString(id)
}

// validBranchRe matches git branch names: alphanumeric, dots, hyphens,
// underscores, and forward slashes (for hierarchical names like
// "semspec/scenario-auth"). Must not start with "-" or ".", must not contain
// ".." or end with ".lock".
var validBranchRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,255}$`)

// isValidBranchName reports whether name is a safe git branch name.
func isValidBranchName(name string) bool {
	if !validBranchRe.MatchString(name) {
		return false
	}
	if strings.Contains(name, "..") || strings.HasSuffix(name, ".lock") {
		return false
	}
	return true
}

// appendTrailers appends git trailers to a commit message in deterministic
// (sorted) order. Returns the message unchanged if trailers is empty.
func appendTrailers(msg string, trailers map[string]string) string {
	if len(trailers) == 0 {
		return msg
	}
	keys := make([]string, 0, len(trailers))
	for k := range trailers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	msg += "\n"
	for _, k := range keys {
		msg += fmt.Sprintf("\n%s: %s", k, trailers[k])
	}
	return msg
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// splitLines splits output into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
