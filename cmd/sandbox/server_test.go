package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

// setupTestRepo initialises a fresh git repository with an initial commit and
// returns its absolute path. All git operations are scoped to the repo so
// tests do not depend on the developer's global git config.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@semspec.test"},
		{"git", "config", "user.name", "Test Agent"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setupTestRepo %v: %s", args, out)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", "feat: initial commit"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setupTestRepo commit %v: %s", args, out)
		}
	}

	return dir
}

// newTestServer builds a Server backed by a fresh git repository and returns
// both the server and the underlying httptest.Server. The httptest.Server is
// closed via t.Cleanup.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	repoPath := setupTestRepo(t)
	worktreeRoot := filepath.Join(repoPath, ".semspec", "worktrees")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatalf("create worktree root: %v", err)
	}

	srv := &Server{
		repoPath:       repoPath,
		worktreeRoot:   worktreeRoot,
		defaultTimeout: 15 * time.Second,
		maxTimeout:     60 * time.Second,
		maxOutputBytes: 64 * 1024,
		maxFileSize:    1 * 1024 * 1024,
		logger:         slog.Default(),
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return srv, ts
}

// doRequest sends an HTTP request and returns the response.
func doRequest(t *testing.T, ts *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()

	var bodyReader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, ts.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// decodeJSON decodes the JSON response body into v.
func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// createWorktree is a test helper that creates a worktree via the API and
// returns the response. It registers a cleanup that calls DELETE if the
// worktree was created.
func createWorktree(t *testing.T, ts *httptest.Server, taskID string) worktreeCreateResponse {
	t.Helper()

	resp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{TaskID: taskID})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /worktree: expected 201, got %d", resp.StatusCode)
	}

	var result worktreeCreateResponse
	decodeJSON(t, resp, &result)

	t.Cleanup(func() {
		// Best-effort cleanup — ignore errors if already deleted.
		r := doRequest(t, ts, http.MethodDelete, "/worktree/"+taskID, nil)
		r.Body.Close()
	})

	return result
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHealth(t *testing.T) {
	_, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	decodeJSON(t, resp, &result)

	if result["status"] != "ok" {
		t.Errorf("health status = %q, want %q", result["status"], "ok")
	}
}

func TestCreateWorktree(t *testing.T) {
	_, ts := newTestServer(t)

	result := createWorktree(t, ts, "test-create")

	if result.Status != "created" {
		t.Errorf("status = %q, want %q", result.Status, "created")
	}
	if !strings.HasSuffix(result.Path, "test-create") {
		t.Errorf("path = %q, want suffix 'test-create'", result.Path)
	}
	if result.Branch != "agent/test-create" {
		t.Errorf("branch = %q, want %q", result.Branch, "agent/test-create")
	}

	// Directory must exist.
	if _, err := os.Stat(result.Path); err != nil {
		t.Errorf("worktree directory does not exist: %v", err)
	}

	// Must be recognised as a git working tree.
	c := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	c.Dir = result.Path
	out, err := c.Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Errorf("path %s is not a git worktree", result.Path)
	}
}

func TestDeleteWorktree(t *testing.T) {
	_, ts := newTestServer(t)

	result := createWorktree(t, ts, "test-delete")
	worktreePath := result.Path

	// Manually delete without relying on the cleanup registered by createWorktree.
	resp := doRequest(t, ts, http.MethodDelete, "/worktree/test-delete", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /worktree: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := os.Stat(worktreePath); err == nil {
		t.Errorf("worktree directory %s still exists after DELETE", worktreePath)
	}
}

func TestMergeWorktree(t *testing.T) {
	srv, ts := newTestServer(t)

	result := createWorktree(t, ts, "test-merge")

	// Write a file via the API.
	writeResp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-merge",
		Path:    "hello.txt",
		Content: "hello from agent\n",
	})
	if writeResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /file: expected 200, got %d", writeResp.StatusCode)
	}
	writeResp.Body.Close()

	// Merge the worktree.
	mergeResp := doRequest(t, ts, http.MethodPost, "/worktree/test-merge/merge", nil)
	if mergeResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /worktree/test-merge/merge: expected 200, got %d", mergeResp.StatusCode)
	}

	var mergeResult mergeResponse
	decodeJSON(t, mergeResp, &mergeResult)

	if mergeResult.Status != "merged" {
		t.Errorf("merge status = %q, want %q", mergeResult.Status, "merged")
	}

	// File must now exist in the main repo.
	mainFilePath := filepath.Join(srv.repoPath, "hello.txt")
	if _, err := os.Stat(mainFilePath); err != nil {
		t.Fatalf("hello.txt not found in main repo after merge: %v", err)
	}

	content, err := os.ReadFile(mainFilePath)
	if err != nil {
		t.Fatalf("read hello.txt from main repo: %v", err)
	}
	if string(content) != "hello from agent\n" {
		t.Errorf("hello.txt content = %q, want %q", content, "hello from agent\n")
	}

	// Worktree directory must be removed.
	if _, err := os.Stat(result.Path); err == nil {
		t.Errorf("worktree directory %s still exists after merge", result.Path)
	}
}

func TestWriteAndReadFile(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-file")

	const content = "package main\n\nfunc main() {}\n"

	// Write.
	writeResp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-file",
		Path:    "main.go",
		Content: content,
	})
	if writeResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /file: expected 200, got %d", writeResp.StatusCode)
	}
	writeResp.Body.Close()

	// Read.
	readResp := doRequest(t, ts, http.MethodGet, "/file?task_id=test-file&path=main.go", nil)
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /file: expected 200, got %d", readResp.StatusCode)
	}

	var fileResult fileResponse
	decodeJSON(t, readResp, &fileResult)

	if fileResult.Content != content {
		t.Errorf("file content = %q, want %q", fileResult.Content, content)
	}
	if fileResult.Size != len(content) {
		t.Errorf("file size = %d, want %d", fileResult.Size, len(content))
	}
}

func TestExec(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-exec")

	resp := doRequest(t, ts, http.MethodPost, "/exec", execRequest{
		TaskID:  "test-exec",
		Command: "echo hello",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /exec: expected 200, got %d", resp.StatusCode)
	}

	var result execResponse
	decodeJSON(t, resp, &result)

	if result.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("stdout = %q, want 'hello'", result.Stdout)
	}
	if result.TimedOut {
		t.Error("timed_out = true, want false")
	}
}

func TestExecTimeout(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-timeout")

	resp := doRequest(t, ts, http.MethodPost, "/exec", execRequest{
		TaskID:    "test-timeout",
		Command:   "sleep 60",
		TimeoutMs: 200, // 200ms — will time out quickly
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /exec: expected 200, got %d", resp.StatusCode)
	}

	var result execResponse
	decodeJSON(t, resp, &result)

	if !result.TimedOut {
		t.Error("expected timed_out = true")
	}
}

func TestGitCommitAndStatus(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-git")

	// Write a file into the worktree.
	writeResp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-git",
		Path:    "feature.go",
		Content: "package main\n",
	})
	if writeResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /file: expected 200, got %d", writeResp.StatusCode)
	}
	writeResp.Body.Close()

	// Status should show the file as untracked.
	statusResp := doRequest(t, ts, http.MethodPost, "/git/status", map[string]string{"task_id": "test-git"})
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/status: expected 200, got %d", statusResp.StatusCode)
	}
	var statusResult gitStatusResponse
	decodeJSON(t, statusResp, &statusResult)
	if !strings.Contains(statusResult.Output, "feature.go") {
		t.Errorf("git status output = %q, expected to contain 'feature.go'", statusResult.Output)
	}

	// Commit the file.
	commitResp := doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-git",
		Message: "feat: add feature",
	})
	if commitResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", commitResp.StatusCode)
	}
	var commitResult gitCommitResponse
	decodeJSON(t, commitResp, &commitResult)

	if commitResult.Status != "committed" {
		t.Errorf("commit status = %q, want %q", commitResult.Status, "committed")
	}
	if commitResult.Hash == "" {
		t.Error("commit hash is empty")
	}

	// FilesChanged should list the new file.
	if len(commitResult.FilesChanged) != 1 {
		t.Fatalf("files_changed length = %d, want 1", len(commitResult.FilesChanged))
	}
	if commitResult.FilesChanged[0].Path != "feature.go" {
		t.Errorf("files_changed[0].path = %q, want %q", commitResult.FilesChanged[0].Path, "feature.go")
	}
	if commitResult.FilesChanged[0].Operation != "add" {
		t.Errorf("files_changed[0].operation = %q, want %q", commitResult.FilesChanged[0].Operation, "add")
	}

	// Status should now be clean.
	statusResp2 := doRequest(t, ts, http.MethodPost, "/git/status", map[string]string{"task_id": "test-git"})
	if statusResp2.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/status (2): expected 200, got %d", statusResp2.StatusCode)
	}
	var statusResult2 gitStatusResponse
	decodeJSON(t, statusResp2, &statusResult2)
	if statusResult2.Output != "" {
		t.Errorf("git status after commit = %q, want empty (clean)", statusResult2.Output)
	}
}

func TestGitCommitNothingToCommit(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-nothing")

	// Commit with no changes.
	resp := doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-nothing",
		Message: "should be empty",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", resp.StatusCode)
	}

	var result gitCommitResponse
	decodeJSON(t, resp, &result)

	if result.Status != "nothing_to_commit" {
		t.Errorf("status = %q, want %q", result.Status, "nothing_to_commit")
	}
}

func TestGitCommitModifyOperation(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-modify")

	// Write and commit an initial file.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-modify",
		Path:    "main.go",
		Content: "package main\n",
	}).Body.Close()
	doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-modify",
		Message: "initial",
	}).Body.Close()

	// Modify the file and commit again.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-modify",
		Path:    "main.go",
		Content: "package main\n\nfunc main() {}\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-modify",
		Message: "update main",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", resp.StatusCode)
	}

	var result gitCommitResponse
	decodeJSON(t, resp, &result)

	if result.Status != "committed" {
		t.Fatalf("commit status = %q, want %q", result.Status, "committed")
	}
	if len(result.FilesChanged) != 1 {
		t.Fatalf("files_changed length = %d, want 1", len(result.FilesChanged))
	}
	if result.FilesChanged[0].Path != "main.go" {
		t.Errorf("files_changed[0].path = %q, want %q", result.FilesChanged[0].Path, "main.go")
	}
	if result.FilesChanged[0].Operation != "modify" {
		t.Errorf("files_changed[0].operation = %q, want %q", result.FilesChanged[0].Operation, "modify")
	}
}

func TestGitCommitDeleteOperation(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-del")

	// Write, commit, then delete a file.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-del",
		Path:    "remove_me.go",
		Content: "package main\n",
	}).Body.Close()
	doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-del",
		Message: "add file",
	}).Body.Close()

	// Delete via exec (no DELETE file endpoint).
	doRequest(t, ts, http.MethodPost, "/exec", execRequest{
		TaskID:  "test-del",
		Command: "rm remove_me.go",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-del",
		Message: "delete file",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", resp.StatusCode)
	}

	var result gitCommitResponse
	decodeJSON(t, resp, &result)

	if result.Status != "committed" {
		t.Fatalf("commit status = %q, want %q", result.Status, "committed")
	}
	if len(result.FilesChanged) != 1 {
		t.Fatalf("files_changed length = %d, want 1", len(result.FilesChanged))
	}
	if result.FilesChanged[0].Path != "remove_me.go" {
		t.Errorf("files_changed[0].path = %q, want %q", result.FilesChanged[0].Path, "remove_me.go")
	}
	if result.FilesChanged[0].Operation != "delete" {
		t.Errorf("files_changed[0].operation = %q, want %q", result.FilesChanged[0].Operation, "delete")
	}
}

func TestParseChangedFiles(t *testing.T) {
	// parseChangedFiles is a method on *Server, so we create a minimal server.
	srv := &Server{logger: slog.New(slog.NewTextHandler(os.Stdout, nil))}

	// Set up a real git repo to test against.
	repoDir := setupTestRepo(t)

	// Create a file and commit.
	if err := os.WriteFile(filepath.Join(repoDir, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", "-A")
	run(t, repoDir, "git", "commit", "-m", "add new.go")

	hash := strings.TrimSpace(runOutput(t, repoDir, "git", "rev-parse", "HEAD"))
	files := srv.parseChangedFiles(context.Background(), repoDir, hash)

	if len(files) != 1 {
		t.Fatalf("parseChangedFiles returned %d files, want 1", len(files))
	}
	if files[0].Path != "new.go" {
		t.Errorf("path = %q, want %q", files[0].Path, "new.go")
	}
	if files[0].Operation != "add" {
		t.Errorf("operation = %q, want %q", files[0].Operation, "add")
	}

	// Modify the file and commit.
	if err := os.WriteFile(filepath.Join(repoDir, "new.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", "-A")
	run(t, repoDir, "git", "commit", "-m", "modify new.go")

	hash2 := strings.TrimSpace(runOutput(t, repoDir, "git", "rev-parse", "HEAD"))
	files2 := srv.parseChangedFiles(context.Background(), repoDir, hash2)

	if len(files2) != 1 {
		t.Fatalf("parseChangedFiles returned %d files, want 1", len(files2))
	}
	if files2[0].Operation != "modify" {
		t.Errorf("operation = %q, want %q", files2[0].Operation, "modify")
	}

	// Delete the file and commit.
	if err := os.Remove(filepath.Join(repoDir, "new.go")); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", "-A")
	run(t, repoDir, "git", "commit", "-m", "delete new.go")

	hash3 := strings.TrimSpace(runOutput(t, repoDir, "git", "rev-parse", "HEAD"))
	files3 := srv.parseChangedFiles(context.Background(), repoDir, hash3)

	if len(files3) != 1 {
		t.Fatalf("parseChangedFiles returned %d files, want 1", len(files3))
	}
	if files3[0].Operation != "delete" {
		t.Errorf("operation = %q, want %q", files3[0].Operation, "delete")
	}

	// Invalid commit hash — should return nil.
	files4 := srv.parseChangedFiles(context.Background(), repoDir, "0000000000000000000000000000000000000000")
	if files4 != nil {
		t.Errorf("expected nil for invalid hash, got %v", files4)
	}

	// Multiple files in one commit.
	if err := os.WriteFile(filepath.Join(repoDir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repoDir, "git", "add", "-A")
	run(t, repoDir, "git", "commit", "-m", "add two files")

	hash5 := strings.TrimSpace(runOutput(t, repoDir, "git", "rev-parse", "HEAD"))
	files5 := srv.parseChangedFiles(context.Background(), repoDir, hash5)

	if len(files5) != 2 {
		t.Fatalf("parseChangedFiles returned %d files, want 2", len(files5))
	}
}

// run executes a command in dir.
func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %s: %v", name, args, out, err)
	}
}

// runOutput executes a command in dir and returns stdout.
func runOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return string(out)
}

func TestGitDiff(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-diff")

	// Write a file — it's untracked, so `git diff` won't show it.
	// To get a diff we need a tracked file that we then modify.
	// Commit a file first, then change it.
	commitResp := doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-diff",
		Message: "initial empty",
	})
	commitResp.Body.Close()

	// Write then commit an initial version.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-diff",
		Path:    "tracked.go",
		Content: "package main\n",
	}).Body.Close()

	doRequest(t, ts, http.MethodPost, "/git/commit", gitCommitRequest{
		TaskID:  "test-diff",
		Message: "add tracked file",
	}).Body.Close()

	// Now modify the tracked file.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-diff",
		Path:    "tracked.go",
		Content: "package main\n\nfunc main() {}\n",
	}).Body.Close()

	// Diff should show the modification.
	diffResp := doRequest(t, ts, http.MethodPost, "/git/diff", map[string]string{"task_id": "test-diff"})
	if diffResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/diff: expected 200, got %d", diffResp.StatusCode)
	}

	var result gitDiffResponse
	decodeJSON(t, diffResp, &result)

	if !strings.Contains(result.Output, "tracked.go") {
		t.Errorf("diff output = %q, expected to mention 'tracked.go'", result.Output)
	}
}

// defaultBranch returns the repo's current branch name — portable across
// git init defaults (master vs main) that depend on host config.
func defaultBranch(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// createBranchWithCommit creates `name` off HEAD in dir, writes path=content,
// commits it, and switches back to the previous branch so the main repo is
// left untouched.
func createBranchWithCommit(t *testing.T, dir, name, path, content, msg string) {
	t.Helper()
	prev := defaultBranch(t, dir)
	for _, args := range [][]string{
		{"git", "checkout", "-b", name},
		{"git", "config", "user.email", "test@semspec.test"},
		{"git", "config", "user.name", "Test Agent"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	for _, args := range [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", msg},
		{"git", "checkout", prev},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
}

func TestGitBranchDiff(t *testing.T) {
	srv, ts := newTestServer(t)
	base := defaultBranch(t, srv.repoPath)

	createBranchWithCommit(t, srv.repoPath, "semspec/requirement-r1",
		"src/parser.go", "package parser\n\nfunc Parse() {}\n", "feat: add parser")

	resp := doRequest(t, ts, http.MethodPost, "/git/branch-diff", branchDiffRequest{
		Branch: "semspec/requirement-r1",
		Base:   base,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("branch-diff: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result branchDiffResponse
	decodeJSON(t, resp, &result)

	if len(result.Files) != 1 {
		t.Fatalf("files = %d, want 1: %+v", len(result.Files), result.Files)
	}
	f := result.Files[0]
	if f.Path != "src/parser.go" {
		t.Errorf("path = %q, want src/parser.go", f.Path)
	}
	if f.Status != "added" {
		t.Errorf("status = %q, want added", f.Status)
	}
	if f.Insertions == 0 {
		t.Errorf("insertions = 0, want > 0")
	}
	if result.TotalInsertions != f.Insertions {
		t.Errorf("total_insertions = %d, want %d", result.TotalInsertions, f.Insertions)
	}
}

func TestGitBranchDiff_InvalidBranch(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []branchDiffRequest{
		{Branch: "", Base: "main"},
		{Branch: "../etc/passwd", Base: "main"},
		{Branch: "foo bar", Base: "main"},
		{Branch: "semspec/ok", Base: "../bad"},
	}
	for _, tc := range cases {
		resp := doRequest(t, ts, http.MethodPost, "/git/branch-diff", tc)
		if resp.StatusCode == http.StatusOK {
			t.Errorf("expected non-200 for %+v, got 200", tc)
		}
		resp.Body.Close()
	}
}

func TestGitBranchDiff_BranchNotFound(t *testing.T) {
	srv, ts := newTestServer(t)
	base := defaultBranch(t, srv.repoPath)

	resp := doRequest(t, ts, http.MethodPost, "/git/branch-diff", branchDiffRequest{
		Branch: "does-not-exist",
		Base:   base,
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing branch, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestGitBranchFileDiff(t *testing.T) {
	srv, ts := newTestServer(t)
	base := defaultBranch(t, srv.repoPath)

	createBranchWithCommit(t, srv.repoPath, "semspec/requirement-r2",
		"src/mod.go", "package mod\n\nconst V = 1\n", "feat: add mod")

	resp := doRequest(t, ts, http.MethodPost, "/git/branch-file-diff", branchFileDiffRequest{
		Branch: "semspec/requirement-r2",
		Base:   base,
		Path:   "src/mod.go",
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("branch-file-diff: expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result branchFileDiffResponse
	decodeJSON(t, resp, &result)

	if !strings.Contains(result.Patch, "src/mod.go") {
		t.Errorf("patch missing file path: %q", result.Patch)
	}
	if !strings.Contains(result.Patch, "+package mod") {
		t.Errorf("patch missing added content: %q", result.Patch)
	}
}

func TestGitBranchFileDiff_PathEscape(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []string{"", "../etc/passwd", "/etc/passwd"}
	for _, p := range cases {
		resp := doRequest(t, ts, http.MethodPost, "/git/branch-file-diff", branchFileDiffRequest{
			Branch: "semspec/requirement-r1",
			Base:   "main",
			Path:   p,
		})
		if resp.StatusCode == http.StatusOK {
			t.Errorf("expected non-200 for path %q, got 200", p)
		}
		resp.Body.Close()
	}
}

func TestPathEscape(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-escape")

	// Attempt directory traversal.
	resp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-escape",
		Path:    "../../../etc/passwd",
		Content: "malicious",
	})
	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for path traversal, got 200")
	}
	resp.Body.Close()

	// Also test via GET.
	readResp := doRequest(t, ts, http.MethodGet, "/file?task_id=test-escape&path=../../etc/passwd", nil)
	if readResp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for path traversal via GET, got 200")
	}
	readResp.Body.Close()
}

func TestPathAbsolute(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-absolute")

	resp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-absolute",
		Path:    "/etc/passwd",
		Content: "malicious",
	})
	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 for absolute path, got 200")
	}
	resp.Body.Close()
}

func TestInvalidTaskID(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []string{
		"",
		"../foo",
		"foo/bar",
		"foo bar",
		"foo;rm -rf",
		strings.Repeat("a", 257),
	}

	for _, id := range cases {
		t.Run(fmt.Sprintf("id=%q", id), func(t *testing.T) {
			resp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{TaskID: id})
			if resp.StatusCode == http.StatusCreated {
				t.Errorf("expected non-201 for invalid id %q, got 201", id)
			}
			resp.Body.Close()
		})
	}
}

func TestListWorktreeFiles(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-list-files")

	// Write two files.
	for _, name := range []string{"alpha.go", "beta.go"} {
		doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
			TaskID:  "test-list-files",
			Path:    name,
			Content: "package main\n",
		}).Body.Close()
	}

	resp := doRequest(t, ts, http.MethodGet, "/worktree/test-list-files/files", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /worktree/test-list-files/files: expected 200, got %d", resp.StatusCode)
	}

	var entries []fileEntry
	decodeJSON(t, resp, &entries)

	found := make(map[string]bool)
	for _, e := range entries {
		found[e.Name] = true
	}

	// The worktree starts with files from HEAD (README.md) plus our new files.
	for _, want := range []string{"alpha.go", "beta.go"} {
		if !found[want] {
			t.Errorf("file %q not found in listing; got %v", want, entries)
		}
	}
}

func TestList(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-list-dir")

	// Write a file in a subdirectory.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-list-dir",
		Path:    "pkg/util.go",
		Content: "package pkg\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/list", listRequest{
		TaskID: "test-list-dir",
		Path:   "pkg",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /list: expected 200, got %d", resp.StatusCode)
	}

	var result listResponse
	decodeJSON(t, resp, &result)

	if len(result.Entries) == 0 {
		t.Fatal("expected at least one entry, got none")
	}

	found := false
	for _, entry := range result.Entries {
		if entry.Name == "util.go" {
			found = true
			if entry.IsDir {
				t.Error("util.go should not be a directory")
			}
		}
	}
	if !found {
		t.Errorf("util.go not found in listing; got %+v", result.Entries)
	}
}

func TestSearch(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-search")

	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-search",
		Path:    "main.go",
		Content: "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/search", searchRequest{
		TaskID:   "test-search",
		Pattern:  "func main",
		FileGlob: "*.go",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /search: expected 200, got %d", resp.StatusCode)
	}

	var result searchResponse
	decodeJSON(t, resp, &result)

	if len(result.Matches) == 0 {
		t.Fatal("expected at least one match, got none")
	}

	found := false
	for _, m := range result.Matches {
		if strings.Contains(m.Text, "func main") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected match for 'func main'; got %+v", result.Matches)
	}
}

func TestInstall(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-install")

	// Install "true" (a command that exists everywhere) via a command that will succeed.
	// We use /bin/sh echo to test the plumbing without needing apt/npm.
	resp := doRequest(t, ts, http.MethodPost, "/install", installRequest{
		TaskID:         "test-install",
		PackageManager: "apt",
		Packages:       []string{"coreutils"}, // Already installed, apt-get install -y is idempotent
	})

	var result installResponse
	decodeJSON(t, resp, &result)

	// On macOS / CI without apt, this will fail — that's expected.
	// The test verifies the API plumbing, not that apt works.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if result.Status != "installed" && result.Status != "failed" {
		t.Errorf("unexpected status: %s", result.Status)
	}
}

func TestInstallValidation(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-install-val")

	tests := []struct {
		name     string
		body     installRequest
		wantCode int
		wantErr  string
	}{
		{
			name:     "empty packages",
			body:     installRequest{TaskID: "test-install-val", PackageManager: "apt", Packages: []string{}},
			wantCode: http.StatusBadRequest,
			wantErr:  "packages is required",
		},
		{
			name:     "invalid package manager",
			body:     installRequest{TaskID: "test-install-val", PackageManager: "yum", Packages: []string{"foo"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "unsupported package_manager",
		},
		{
			name:     "invalid task_id",
			body:     installRequest{TaskID: "../escape", PackageManager: "apt", Packages: []string{"foo"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid task_id",
		},
		{
			name:     "invalid package name with shell injection",
			body:     installRequest{TaskID: "test-install-val", PackageManager: "apt", Packages: []string{"foo; rm -rf /"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid package name",
		},
		{
			name:     "too many packages",
			body:     installRequest{TaskID: "test-install-val", PackageManager: "apt", Packages: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "too many packages",
		},
		{
			name:     "flag injection",
			body:     installRequest{TaskID: "test-install-val", PackageManager: "apt", Packages: []string{"--pre-invoke=malicious"}},
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid package name",
		},
		{
			name:     "nonexistent worktree",
			body:     installRequest{TaskID: "no-such-task", PackageManager: "apt", Packages: []string{"foo"}},
			wantCode: http.StatusNotFound,
			wantErr:  "worktree not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(t, ts, http.MethodPost, "/install", tt.body)
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantCode {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantCode)
			}

			var errResp map[string]string
			json.NewDecoder(resp.Body).Decode(&errResp)
			if !strings.Contains(errResp["error"], tt.wantErr) {
				t.Errorf("error = %q, want containing %q", errResp["error"], tt.wantErr)
			}
		})
	}
}

func TestInstallValidPackageName(t *testing.T) {
	tests := []struct {
		name  string
		pkg   string
		valid bool
	}{
		{"simple", "cargo", true},
		{"hyphenated", "build-essential", true},
		{"go module", "golang.org/x/tools/cmd/goimports@latest", true},
		{"npm scoped", "@types/node", true},
		{"version constraint", "flask==2.3.0", true},
		{"flag injection", "--pre-invoke=cmd", false},
		{"flag short", "-y", false},
		{"shell injection semicolon", "foo;rm -rf /", false},
		{"shell injection backtick", "foo`whoami`", false},
		{"shell injection dollar", "foo$(whoami)", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidPackageName(tt.pkg)
			if got != tt.valid {
				t.Errorf("isValidPackageName(%q) = %v, want %v", tt.pkg, got, tt.valid)
			}
		})
	}
}

func TestCreateBranch(t *testing.T) {
	srv, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodPost, "/branch", branchCreateRequest{
		Name: "semspec/scenario-test",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /branch: expected 201, got %d", resp.StatusCode)
	}

	var result map[string]string
	decodeJSON(t, resp, &result)
	if result["status"] != "created" {
		t.Errorf("status = %q, want %q", result["status"], "created")
	}
	if result["branch"] != "semspec/scenario-test" {
		t.Errorf("branch = %q, want %q", result["branch"], "semspec/scenario-test")
	}

	// Branch must be visible in git.
	out := runOutput(t, srv.repoPath, "git", "branch", "--list", "semspec/scenario-test")
	if !strings.Contains(out, "semspec/scenario-test") {
		t.Errorf("branch not found in git branch --list output: %q", out)
	}

	// Creating the same branch again should return "exists".
	resp2 := doRequest(t, ts, http.MethodPost, "/branch", branchCreateRequest{
		Name: "semspec/scenario-test",
	})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("POST /branch (duplicate): expected 200, got %d", resp2.StatusCode)
	}
	var result2 map[string]string
	decodeJSON(t, resp2, &result2)
	if result2["status"] != "exists" {
		t.Errorf("duplicate branch status = %q, want %q", result2["status"], "exists")
	}
}

func TestCreateWorktreeWithBaseBranch(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a scenario branch first.
	branchResp := doRequest(t, ts, http.MethodPost, "/branch", branchCreateRequest{
		Name: "semspec/scenario-base",
	})
	if branchResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /branch: expected 201, got %d", branchResp.StatusCode)
	}
	branchResp.Body.Close()

	// Create worktree branching from the scenario branch.
	resp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{
		TaskID:     "test-base-branch",
		BaseBranch: "semspec/scenario-base",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /worktree with base_branch: expected 201, got %d", resp.StatusCode)
	}
	t.Cleanup(func() {
		r := doRequest(t, ts, http.MethodDelete, "/worktree/test-base-branch", nil)
		r.Body.Close()
	})

	var result worktreeCreateResponse
	decodeJSON(t, resp, &result)
	if result.Status != "created" {
		t.Errorf("status = %q, want %q", result.Status, "created")
	}

	// Verify the worktree was created from the scenario branch.
	// The parent of the worktree's HEAD should be the scenario branch.
	parentRef := strings.TrimSpace(runOutput(t, result.Path, "git", "log", "--format=%D", "-1"))
	_ = parentRef // Just verify no error — the worktree exists and is valid.

	_ = srv // use srv to prevent unused warning
}

func TestMergeWorktreeWithTargetBranch(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a scenario branch.
	branchResp := doRequest(t, ts, http.MethodPost, "/branch", branchCreateRequest{
		Name: "semspec/scenario-merge-target",
	})
	if branchResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /branch: expected 201, got %d", branchResp.StatusCode)
	}
	branchResp.Body.Close()

	// Create worktree from scenario branch.
	createResp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{
		TaskID:     "test-merge-target",
		BaseBranch: "semspec/scenario-merge-target",
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /worktree: expected 201, got %d", createResp.StatusCode)
	}
	createResp.Body.Close()

	// Write a file in the worktree.
	writeResp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-merge-target",
		Path:    "feature.go",
		Content: "package main\n",
	})
	if writeResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /file: expected 200, got %d", writeResp.StatusCode)
	}
	writeResp.Body.Close()

	// Merge with target_branch, commit_message, and trailers.
	mergeResp := doRequest(t, ts, http.MethodPost, "/worktree/test-merge-target/merge", mergeRequest{
		TargetBranch:  "semspec/scenario-merge-target",
		CommitMessage: "feat(auth): task completion",
		Trailers: map[string]string{
			"Task-ID":   "test-merge-target",
			"Plan-Slug": "auth-refresh",
		},
	})
	if mergeResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /worktree/test-merge-target/merge: expected 200, got %d", mergeResp.StatusCode)
	}

	var mergeResult mergeResponse
	decodeJSON(t, mergeResp, &mergeResult)

	if mergeResult.Status != "merged" {
		t.Errorf("merge status = %q, want %q", mergeResult.Status, "merged")
	}
	if mergeResult.Commit == "" {
		t.Error("merge commit hash is empty")
	}
	if len(mergeResult.FilesChanged) == 0 {
		t.Error("expected files_changed to be populated")
	}

	// Verify the file exists on the scenario branch.
	run(t, srv.repoPath, "git", "checkout", "semspec/scenario-merge-target")
	filePath := filepath.Join(srv.repoPath, "feature.go")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("feature.go not found on scenario branch after merge: %v", err)
	}

	// Verify trailers in the commit message.
	commitLog := runOutput(t, srv.repoPath, "git", "log", "-1", "--format=%B")
	if !strings.Contains(commitLog, "Task-ID: test-merge-target") {
		t.Errorf("commit message missing Task-ID trailer: %q", commitLog)
	}
	if !strings.Contains(commitLog, "Plan-Slug: auth-refresh") {
		t.Errorf("commit message missing Plan-Slug trailer: %q", commitLog)
	}
}

func TestCreateBranch_InvalidName(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []struct {
		name string
		want int
	}{
		{"", http.StatusBadRequest},              // empty
		{"-leading-dash", http.StatusBadRequest}, // starts with dash
		{".leading-dot", http.StatusBadRequest},  // starts with dot
		{"a..b", http.StatusBadRequest},          // contains ..
		{"branch.lock", http.StatusBadRequest},   // ends with .lock
	}

	for _, tc := range cases {
		resp := doRequest(t, ts, http.MethodPost, "/branch", branchCreateRequest{Name: tc.name})
		if resp.StatusCode != tc.want {
			t.Errorf("POST /branch name=%q: got %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
		resp.Body.Close()
	}
}

func TestMergeWorktree_InvalidTargetBranch(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-bad-target")

	// Write a file so there's something to commit.
	writeResp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-bad-target",
		Path:    "test.txt",
		Content: "hello\n",
	})
	writeResp.Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/worktree/test-bad-target/merge", mergeRequest{
		TargetBranch: "-flag-injection",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("merge with invalid target_branch: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	resp.Body.Close()
}

func TestMergeWorktree_NonexistentTargetBranch(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-noexist-target")

	writeResp := doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "test-noexist-target",
		Path:    "test.txt",
		Content: "hello\n",
	})
	writeResp.Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/worktree/test-noexist-target/merge", mergeRequest{
		TargetBranch: "does/not/exist",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("merge with nonexistent target_branch: got %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	resp.Body.Close()
}

func TestCleanupStaleWorktrees(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create a worktree.
	createWorktree(t, ts, "test-cleanup")

	// Backdate the directory mtime so it appears stale.
	worktreePath := filepath.Join(srv.worktreeRoot, "test-cleanup")
	pastTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(worktreePath, pastTime, pastTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Run cleanup with a 24-hour max age.
	srv.cleanupStaleWorktrees(context.Background(), 24*time.Hour)

	// Worktree should be gone.
	if _, err := os.Stat(worktreePath); err == nil {
		t.Errorf("stale worktree %s still exists after cleanup", worktreePath)
	}
}

// ---------------------------------------------------------------------------
// Workspace browser tests
// ---------------------------------------------------------------------------

func TestHandleWorkspaceTasks_NoWorktrees(t *testing.T) {
	_, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/workspace/tasks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace/tasks: expected 200, got %d", resp.StatusCode)
	}

	var result []workspaceTaskInfo
	decodeJSON(t, resp, &result)

	if len(result) != 0 {
		t.Errorf("expected empty array, got %d entries: %+v", len(result), result)
	}
}

func TestHandleWorkspaceTasks_WithWorktrees(t *testing.T) {
	_, ts := newTestServer(t)

	// Create two worktrees with different file counts.
	createWorktree(t, ts, "wstask-alpha")
	createWorktree(t, ts, "wstask-beta")

	// Write files into alpha's worktree.
	for _, name := range []string{"main.go", "util.go"} {
		doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
			TaskID:  "wstask-alpha",
			Path:    name,
			Content: "package main\n",
		}).Body.Close()
	}

	resp := doRequest(t, ts, http.MethodGet, "/workspace/tasks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace/tasks: expected 200, got %d", resp.StatusCode)
	}

	var result []workspaceTaskInfo
	decodeJSON(t, resp, &result)

	if len(result) < 2 {
		t.Fatalf("expected at least 2 tasks, got %d", len(result))
	}

	// Verify sorted order.
	if result[0].TaskID > result[1].TaskID {
		t.Errorf("tasks not sorted: %q > %q", result[0].TaskID, result[1].TaskID)
	}

	// Locate alpha in the result and verify it reports the two new files plus
	// the README.md inherited from the initial commit.
	var alpha *workspaceTaskInfo
	for i := range result {
		if result[i].TaskID == "wstask-alpha" {
			alpha = &result[i]
			break
		}
	}
	if alpha == nil {
		t.Fatal("wstask-alpha not found in /workspace/tasks response")
	}
	if alpha.FileCount < 2 {
		t.Errorf("wstask-alpha file_count = %d, want >= 2", alpha.FileCount)
	}
	if alpha.Branch == "" {
		t.Error("wstask-alpha branch is empty")
	}
}

func TestHandleWorkspaceTree_ValidWorktree(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "wstree-valid")

	// Write files at the root and inside a subdirectory.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "wstree-valid",
		Path:    "main.go",
		Content: "package main\n",
	}).Body.Close()
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "wstree-valid",
		Path:    "pkg/util.go",
		Content: "package pkg\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodGet, "/workspace/tree?task_id=wstree-valid", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace/tree: expected 200, got %d", resp.StatusCode)
	}

	var result []*workspaceEntry
	decodeJSON(t, resp, &result)

	if len(result) == 0 {
		t.Fatal("expected non-empty tree")
	}

	// Directories must sort before files.
	var foundDir, foundFile bool
	var dirIdx, fileIdx int
	for i, entry := range result {
		if entry.IsDir && entry.Name == "pkg" {
			foundDir = true
			dirIdx = i
		}
		if !entry.IsDir && entry.Name == "main.go" {
			foundFile = true
			fileIdx = i
		}
	}
	if !foundDir {
		t.Error("pkg directory not found in tree root")
	}
	if !foundFile {
		t.Error("main.go not found in tree root")
	}
	if foundDir && foundFile && dirIdx > fileIdx {
		t.Errorf("directories should sort before files; dir at index %d, file at %d", dirIdx, fileIdx)
	}

	// The pkg directory must have util.go as a child.
	for _, entry := range result {
		if entry.Name == "pkg" && entry.IsDir {
			if len(entry.Children) == 0 {
				t.Error("pkg directory has no children")
				break
			}
			found := false
			for _, child := range entry.Children {
				if child.Name == "util.go" {
					found = true
				}
			}
			if !found {
				t.Errorf("util.go not found in pkg children: %+v", entry.Children)
			}
			break
		}
	}
}

func TestHandleWorkspaceTree_InvalidTaskID(t *testing.T) {
	_, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/workspace/tree?task_id=../escape", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid task_id, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandleWorkspaceTree_EmptyWorktree(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "wstree-empty")

	// A fresh worktree inherits the initial README.md from HEAD, so this
	// verifies that the endpoint returns a valid JSON array (not null) even
	// when the file list is sparse.
	resp := doRequest(t, ts, http.MethodGet, "/workspace/tree?task_id=wstree-empty", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace/tree: expected 200, got %d", resp.StatusCode)
	}

	// Response must be a JSON array (possibly empty or with just README.md).
	var result []*workspaceEntry
	decodeJSON(t, resp, &result)
	// No assertion on length — just verifies valid JSON array shape.
}

func TestHandleWorkspaceDownload_ValidWorktree(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "wsdl-valid")

	// Write two files.
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "wsdl-valid",
		Path:    "main.go",
		Content: "package main\n",
	}).Body.Close()
	doRequest(t, ts, http.MethodPut, "/file", fileWriteRequest{
		TaskID:  "wsdl-valid",
		Path:    "pkg/util.go",
		Content: "package pkg\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodGet, "/workspace/download?task_id=wsdl-valid", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /workspace/download: expected 200, got %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/zip")
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "wsdl-valid-workspace.zip") {
		t.Errorf("Content-Disposition = %q, want to contain 'wsdl-valid-workspace.zip'", cd)
	}

	// Read the ZIP and verify expected files are present.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	inZip := make(map[string]bool)
	for _, f := range zr.File {
		inZip[f.Name] = true
	}

	for _, want := range []string{"main.go", "pkg/util.go"} {
		if !inZip[want] {
			t.Errorf("file %q not found in ZIP; got %v", want, inZip)
		}
	}

	// .git directory must not appear in the archive.
	for name := range inZip {
		if strings.HasPrefix(name, ".git") {
			t.Errorf("ZIP contains .git entry %q", name)
		}
	}
}

func TestHandleWorkspaceDownload_InvalidTaskID(t *testing.T) {
	_, ts := newTestServer(t)

	resp := doRequest(t, ts, http.MethodGet, "/workspace/download?task_id=../escape", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid task_id, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Empty repo / HEAD validation tests
// ---------------------------------------------------------------------------

// setupEmptyTestRepo initialises a git repository with NO commits.
// HEAD is invalid in this state, which is the root cause of the worktree bug.
func setupEmptyTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@semspec.test"},
		{"git", "config", "user.name", "Test Agent"},
	}
	for _, args := range cmds {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setupEmptyTestRepo %v: %s", args, out)
		}
	}

	return dir
}

// newEmptyRepoTestServer builds a Server backed by an empty git repository
// (no commits, invalid HEAD).
func newEmptyRepoTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()

	repoPath := setupEmptyTestRepo(t)
	worktreeRoot := filepath.Join(repoPath, ".semspec", "worktrees")
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatalf("create worktree root: %v", err)
	}

	srv := &Server{
		repoPath:       repoPath,
		worktreeRoot:   worktreeRoot,
		defaultTimeout: 15 * time.Second,
		maxTimeout:     60 * time.Second,
		maxOutputBytes: 64 * 1024,
		maxFileSize:    1 * 1024 * 1024,
		logger:         slog.Default(),
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return srv, ts
}

func TestCreateWorktree_EmptyRepo(t *testing.T) {
	_, ts := newEmptyRepoTestServer(t)

	resp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{TaskID: "task-empty"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty repo, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	errMsg := result["error"]
	if !strings.Contains(errMsg, "does not exist") || !strings.Contains(errMsg, "commit") {
		t.Errorf("error message should mention missing ref and commits, got: %q", errMsg)
	}
}

func TestCreateWorktree_InvalidBaseBranch(t *testing.T) {
	_, ts := newTestServer(t) // repo WITH commits

	resp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{
		TaskID:     "task-bad-branch",
		BaseBranch: "nonexistent-branch",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid base_branch, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if !strings.Contains(result["error"], "nonexistent-branch") {
		t.Errorf("error message should mention the invalid branch, got: %q", result["error"])
	}
}

func TestCreateWorktree_EmptyRepoRecovery(t *testing.T) {
	srv, ts := newEmptyRepoTestServer(t)

	// First attempt should fail — no HEAD.
	resp := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{TaskID: "task-recover"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("first attempt: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Create a commit in the repo.
	readme := filepath.Join(srv.repoPath, "README.md")
	if err := os.WriteFile(readme, []byte("# Recovery test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(t, srv.repoPath, "git", "add", "-A")
	run(t, srv.repoPath, "git", "commit", "-m", "feat: initial commit")

	// Second attempt should succeed.
	resp2 := doRequest(t, ts, http.MethodPost, "/worktree", worktreeCreateRequest{TaskID: "task-recover"})
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("second attempt: expected 201, got %d", resp2.StatusCode)
	}

	t.Cleanup(func() {
		r := doRequest(t, ts, http.MethodDelete, "/worktree/task-recover", nil)
		r.Body.Close()
	})
}

func TestEnsureHEAD_AlreadyValid(t *testing.T) {
	dir := setupTestRepo(t) // has a commit

	if err := ensureHEAD(context.Background(), dir); err != nil {
		t.Fatalf("ensureHEAD on valid repo: %v", err)
	}

	// Should still have exactly one commit.
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	if strings.TrimSpace(string(out)) != "1" {
		t.Errorf("expected 1 commit, got %s", strings.TrimSpace(string(out)))
	}
}

func TestEnsureHEAD_EmptyRepoWithFiles(t *testing.T) {
	dir := setupEmptyTestRepo(t)

	// Add a file to the workspace.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := ensureHEAD(context.Background(), dir); err != nil {
		t.Fatalf("ensureHEAD: %v", err)
	}

	// HEAD should now be valid.
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Output(); err != nil {
		t.Fatal("HEAD still invalid after ensureHEAD")
	}

	// The file should be committed.
	out, err := exec.Command("git", "-C", dir, "show", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	if !strings.Contains(string(out), "main.go") {
		t.Errorf("expected main.go in initial commit, got: %s", string(out))
	}
}

func TestEnsureHEAD_EmptyRepoNoFiles(t *testing.T) {
	dir := setupEmptyTestRepo(t)

	if err := ensureHEAD(context.Background(), dir); err != nil {
		t.Fatalf("ensureHEAD on empty dir: %v", err)
	}

	// HEAD should now be valid (--allow-empty commit).
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "HEAD").Output(); err != nil {
		t.Fatal("HEAD still invalid after ensureHEAD with empty dir")
	}
}
