package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

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
}

// RegisterRoutes binds all HTTP handlers to the mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)

	// Worktree lifecycle.
	mux.HandleFunc("POST /worktree", s.handleCreateWorktree)
	mux.HandleFunc("DELETE /worktree/{taskID}", s.handleDeleteWorktree)
	mux.HandleFunc("POST /worktree/{taskID}/merge", s.handleMergeWorktree)
	mux.HandleFunc("GET /worktree/{taskID}/files", s.handleListWorktreeFiles)

	// File operations (scoped to worktree).
	mux.HandleFunc("PUT /file", s.handleWriteFile)
	mux.HandleFunc("GET /file", s.handleReadFile)
	mux.HandleFunc("POST /list", s.handleList)
	mux.HandleFunc("POST /search", s.handleSearch)

	// Git operations (scoped to worktree).
	mux.HandleFunc("POST /git/status", s.handleGitStatus)
	mux.HandleFunc("POST /git/commit", s.handleGitCommit)
	mux.HandleFunc("POST /git/diff", s.handleGitDiff)

	// Command execution (scoped to worktree).
	mux.HandleFunc("POST /exec", s.handleExec)
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type WorktreeCreateRequest struct {
	TaskID string `json:"task_id"`
}

type WorktreeCreateResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

type FileWriteRequest struct {
	TaskID  string `json:"task_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type FileResponse struct {
	Content string `json:"content"`
	Size    int    `json:"size"`
}

type ExecRequest struct {
	TaskID    string `json:"task_id"`
	Command   string `json:"command"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type ExecResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

type ListRequest struct {
	TaskID string `json:"task_id"`
	Path   string `json:"path"`
}

type ListEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type ListResponse struct {
	Entries []ListEntry `json:"entries"`
}

type SearchRequest struct {
	TaskID   string `json:"task_id"`
	Pattern  string `json:"pattern"`
	FileGlob string `json:"file_glob,omitempty"`
}

type SearchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchResponse struct {
	Matches []SearchMatch `json:"matches"`
}

type GitCommitRequest struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

type GitCommitResponse struct {
	Status       string           `json:"status"`
	Hash         string           `json:"hash,omitempty"`
	FilesChanged []FileChangeInfo `json:"files_changed,omitempty"`
}

// FileChangeInfo describes a file changed in a commit.
type FileChangeInfo struct {
	Path      string `json:"path"`      // relative to worktree root
	Operation string `json:"operation"` // add, modify, delete, rename
}

type GitStatusResponse struct {
	Output string `json:"output"`
}

type GitDiffResponse struct {
	Output string `json:"output"`
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
	var req WorktreeCreateRequest
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

	if err := runGit(ctx, s.repoPath, "worktree", "add", "-b", branch, worktreePath, "HEAD"); err != nil {
		s.logger.Error("git worktree add failed", "task_id", req.TaskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create worktree: "+err.Error())
		return
	}

	// Copy git user config into worktree for proper commit attribution.
	s.copyGitConfig(ctx, worktreePath)

	writeJSON(w, http.StatusCreated, WorktreeCreateResponse{
		Status: "created",
		Path:   worktreePath,
		Branch: branch,
	})
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

// handleMergeWorktree commits any pending changes in the worktree and merges
// them into the main repository's HEAD via --no-ff.
// POST /worktree/{taskID}/merge
func (s *Server) handleMergeWorktree(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	ctx := r.Context()

	// Stage all changes.
	if err := runGit(ctx, worktreePath, "-C", worktreePath, "add", "-A"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stage changes: "+err.Error())
		return
	}

	// Commit — skip if nothing to commit.
	commitMsg := fmt.Sprintf("agent: %s task completion", taskID)
	commitErr := runGit(ctx, worktreePath, "-C", worktreePath, "commit", "-m", commitMsg)
	nothingToCommit := commitErr != nil && strings.Contains(commitErr.Error(), "nothing to commit")

	if commitErr != nil && !nothingToCommit {
		writeError(w, http.StatusInternalServerError, "failed to commit: "+commitErr.Error())
		return
	}

	if nothingToCommit {
		// Nothing to merge — clean up and return success.
		if err := s.removeWorktree(ctx, worktreePath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove worktree: "+err.Error())
			return
		}
		_ = runGit(ctx, s.repoPath, "branch", "-D", "agent/"+taskID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "merged", "note": "nothing_to_commit"})
		return
	}

	// Get the commit hash from the worktree.
	hash, err := gitOutput(ctx, worktreePath, "-C", worktreePath, "rev-parse", "HEAD")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get commit hash: "+err.Error())
		return
	}
	hash = strings.TrimSpace(hash)

	// Merge into main repo.
	mergeMsg := fmt.Sprintf("merge: agent task %s", taskID)
	if err := runGit(ctx, s.repoPath, "-C", s.repoPath, "merge", hash, "--no-ff", "-m", mergeMsg); err != nil {
		// Leave worktree in place so caller can inspect the conflict.
		writeError(w, http.StatusConflict, "merge conflict: "+err.Error())
		return
	}

	// Clean up worktree and branch on success.
	if err := s.removeWorktree(ctx, worktreePath); err != nil {
		s.logger.Warn("failed to remove worktree after successful merge", "task_id", taskID, "error", err)
	}
	_ = runGit(ctx, s.repoPath, "branch", "-D", "agent/"+taskID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "merged", "commit": hash})
}

// handleListWorktreeFiles lists all files tracked in a worktree.
// GET /worktree/{taskID}/files
func (s *Server) handleListWorktreeFiles(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if !isValidID(taskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id")
		return
	}

	worktreePath := filepath.Join(s.worktreeRoot, taskID)
	ctx := r.Context()

	output, err := gitOutput(ctx, worktreePath, "-C", worktreePath, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list files: "+err.Error())
		return
	}

	lines := splitLines(output)
	writeJSON(w, http.StatusOK, map[string][]string{"files": lines})
}

// handleWriteFile writes content to a file path inside a task's worktree.
// PUT /file  {"task_id": "abc", "path": "main.go", "content": "..."}
func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	var req FileWriteRequest
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

	writeJSON(w, http.StatusOK, FileResponse{
		Content: string(content),
		Size:    len(content),
	})
}

// handleList lists directory entries within a task's worktree.
// POST /list  {"task_id": "abc", "path": "pkg/"}
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	var req ListRequest
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

	var result []ListEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, ListEntry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}
	if result == nil {
		result = []ListEntry{}
	}

	writeJSON(w, http.StatusOK, ListResponse{Entries: result})
}

// handleSearch performs a grep-style pattern search within a task's worktree.
// POST /search  {"task_id": "abc", "pattern": "func main", "file_glob": "*.go"}
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
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

	var matches []SearchMatch

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
				matches = append(matches, SearchMatch{
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
		matches = []SearchMatch{}
	}

	writeJSON(w, http.StatusOK, SearchResponse{Matches: matches})
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

	output, err := gitOutput(r.Context(), worktreePath, "-C", worktreePath, "status", "--porcelain")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git status failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, GitStatusResponse{Output: output})
}

// handleGitCommit stages all changes in a worktree and commits them.
// POST /git/commit  {"task_id": "abc", "message": "feat: add handler"}
func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	var req GitCommitRequest
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

	if err := runGit(ctx, worktreePath, "-C", worktreePath, "add", "-A"); err != nil {
		writeError(w, http.StatusInternalServerError, "git add failed: "+err.Error())
		return
	}

	commitErr := runGit(ctx, worktreePath, "-C", worktreePath, "commit", "-m", req.Message)
	if commitErr != nil {
		if strings.Contains(commitErr.Error(), "nothing to commit") {
			writeJSON(w, http.StatusOK, GitCommitResponse{Status: "nothing_to_commit"})
			return
		}
		writeError(w, http.StatusInternalServerError, "git commit failed: "+commitErr.Error())
		return
	}

	hash, err := gitOutput(ctx, worktreePath, "-C", worktreePath, "rev-parse", "HEAD")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get commit hash: "+err.Error())
		return
	}
	commitHash := strings.TrimSpace(hash)

	// Get changed files for provenance graph entities.
	filesChanged := s.parseChangedFiles(ctx, worktreePath, commitHash)

	writeJSON(w, http.StatusOK, GitCommitResponse{
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
	unstaged, err := gitOutput(ctx, worktreePath, "-C", worktreePath, "diff")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff failed: "+err.Error())
		return
	}

	// Staged changes.
	staged, err := gitOutput(ctx, worktreePath, "-C", worktreePath, "diff", "--cached")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "git diff --cached failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, GitDiffResponse{Output: unstaged + staged})
}

// handleExec executes a shell command inside a task's worktree.
// POST /exec  {"task_id": "abc", "command": "go test ./...", "timeout_ms": 30000}
func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
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

	worktreePath := filepath.Join(s.worktreeRoot, req.TaskID)
	if _, err := os.Stat(worktreePath); err != nil {
		writeError(w, http.StatusNotFound, "worktree not found for task_id: "+req.TaskID)
		return
	}

	timeout := s.defaultTimeout
	if req.TimeoutMs > 0 {
		timeout = min(time.Duration(req.TimeoutMs)*time.Millisecond, s.maxTimeout)
	}

	stdout, stderr, exitCode, timedOut := execCommand(r.Context(), worktreePath, req.Command, timeout, s.maxOutputBytes)

	writeJSON(w, http.StatusOK, ExecResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		TimedOut: timedOut,
	})
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

	worktreeBase := filepath.Join(s.worktreeRoot, taskID)
	resolved := filepath.Join(worktreeBase, filepath.Clean(relPath))

	// Guard against escape outside the worktree.
	if !strings.HasPrefix(resolved+string(filepath.Separator), worktreeBase+string(filepath.Separator)) {
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
		val, err := gitOutput(ctx, s.repoPath, "-C", s.repoPath, "config", key)
		if err != nil || strings.TrimSpace(val) == "" {
			continue
		}
		_ = runGit(ctx, worktreePath, "-C", worktreePath, "config", key, strings.TrimSpace(val))
	}
}

// parseChangedFiles runs `git diff-tree` on commitHash to extract the list of
// files modified by the commit and their operation (add, modify, delete, rename,
// copy, type_change). Errors are logged and result in a nil return — callers
// treat this as optional provenance metadata.
func (s *Server) parseChangedFiles(ctx context.Context, worktreePath, commitHash string) []FileChangeInfo {
	out, err := gitOutput(ctx, worktreePath, "-C", worktreePath, "diff-tree", "--no-commit-id", "--name-status", "-r", commitHash)
	if err != nil {
		s.logger.Warn("parseChangedFiles: git diff-tree failed", "commit", commitHash, "error", err)
		return nil
	}

	var files []FileChangeInfo
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
		files = append(files, FileChangeInfo{Path: path, Operation: op})
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
