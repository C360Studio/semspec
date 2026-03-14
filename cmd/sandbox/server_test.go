package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
func createWorktree(t *testing.T, ts *httptest.Server, taskID string) WorktreeCreateResponse {
	t.Helper()

	resp := doRequest(t, ts, http.MethodPost, "/worktree", WorktreeCreateRequest{TaskID: taskID})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /worktree: expected 201, got %d", resp.StatusCode)
	}

	var result WorktreeCreateResponse
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
	writeResp := doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
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

	var mergeResult map[string]string
	decodeJSON(t, mergeResp, &mergeResult)

	if mergeResult["status"] != "merged" {
		t.Errorf("merge status = %q, want %q", mergeResult["status"], "merged")
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
	writeResp := doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
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

	var fileResult FileResponse
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

	resp := doRequest(t, ts, http.MethodPost, "/exec", ExecRequest{
		TaskID:  "test-exec",
		Command: "echo hello",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /exec: expected 200, got %d", resp.StatusCode)
	}

	var result ExecResponse
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

	resp := doRequest(t, ts, http.MethodPost, "/exec", ExecRequest{
		TaskID:    "test-timeout",
		Command:   "sleep 60",
		TimeoutMs: 200, // 200ms — will time out quickly
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /exec: expected 200, got %d", resp.StatusCode)
	}

	var result ExecResponse
	decodeJSON(t, resp, &result)

	if !result.TimedOut {
		t.Error("expected timed_out = true")
	}
}

func TestGitCommitAndStatus(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-git")

	// Write a file into the worktree.
	writeResp := doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
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
	var statusResult GitStatusResponse
	decodeJSON(t, statusResp, &statusResult)
	if !strings.Contains(statusResult.Output, "feature.go") {
		t.Errorf("git status output = %q, expected to contain 'feature.go'", statusResult.Output)
	}

	// Commit the file.
	commitResp := doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-git",
		Message: "feat: add feature",
	})
	if commitResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", commitResp.StatusCode)
	}
	var commitResult GitCommitResponse
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
	var statusResult2 GitStatusResponse
	decodeJSON(t, statusResp2, &statusResult2)
	if statusResult2.Output != "" {
		t.Errorf("git status after commit = %q, want empty (clean)", statusResult2.Output)
	}
}

func TestGitCommitNothingToCommit(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-nothing")

	// Commit with no changes.
	resp := doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-nothing",
		Message: "should be empty",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", resp.StatusCode)
	}

	var result GitCommitResponse
	decodeJSON(t, resp, &result)

	if result.Status != "nothing_to_commit" {
		t.Errorf("status = %q, want %q", result.Status, "nothing_to_commit")
	}
}

func TestGitCommitModifyOperation(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-modify")

	// Write and commit an initial file.
	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-modify",
		Path:    "main.go",
		Content: "package main\n",
	}).Body.Close()
	doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-modify",
		Message: "initial",
	}).Body.Close()

	// Modify the file and commit again.
	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-modify",
		Path:    "main.go",
		Content: "package main\n\nfunc main() {}\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-modify",
		Message: "update main",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", resp.StatusCode)
	}

	var result GitCommitResponse
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
	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-del",
		Path:    "remove_me.go",
		Content: "package main\n",
	}).Body.Close()
	doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-del",
		Message: "add file",
	}).Body.Close()

	// Delete via exec (no DELETE file endpoint).
	doRequest(t, ts, http.MethodPost, "/exec", ExecRequest{
		TaskID:  "test-del",
		Command: "rm remove_me.go",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-del",
		Message: "delete file",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/commit: expected 200, got %d", resp.StatusCode)
	}

	var result GitCommitResponse
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
	commitResp := doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-diff",
		Message: "initial empty",
	})
	commitResp.Body.Close()

	// Write then commit an initial version.
	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-diff",
		Path:    "tracked.go",
		Content: "package main\n",
	}).Body.Close()

	doRequest(t, ts, http.MethodPost, "/git/commit", GitCommitRequest{
		TaskID:  "test-diff",
		Message: "add tracked file",
	}).Body.Close()

	// Now modify the tracked file.
	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-diff",
		Path:    "tracked.go",
		Content: "package main\n\nfunc main() {}\n",
	}).Body.Close()

	// Diff should show the modification.
	diffResp := doRequest(t, ts, http.MethodPost, "/git/diff", map[string]string{"task_id": "test-diff"})
	if diffResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /git/diff: expected 200, got %d", diffResp.StatusCode)
	}

	var result GitDiffResponse
	decodeJSON(t, diffResp, &result)

	if !strings.Contains(result.Output, "tracked.go") {
		t.Errorf("diff output = %q, expected to mention 'tracked.go'", result.Output)
	}
}

func TestPathEscape(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-escape")

	// Attempt directory traversal.
	resp := doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
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

	resp := doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
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
			resp := doRequest(t, ts, http.MethodPost, "/worktree", WorktreeCreateRequest{TaskID: id})
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
		doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
			TaskID:  "test-list-files",
			Path:    name,
			Content: "package main\n",
		}).Body.Close()
	}

	resp := doRequest(t, ts, http.MethodGet, "/worktree/test-list-files/files", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /worktree/test-list-files/files: expected 200, got %d", resp.StatusCode)
	}

	var result map[string][]string
	decodeJSON(t, resp, &result)

	files := result["files"]
	found := make(map[string]bool)
	for _, f := range files {
		found[f] = true
	}

	// The worktree starts with files from HEAD (README.md) plus our new files.
	for _, want := range []string{"alpha.go", "beta.go"} {
		if !found[want] {
			t.Errorf("file %q not found in listing; got %v", want, files)
		}
	}
}

func TestList(t *testing.T) {
	_, ts := newTestServer(t)
	createWorktree(t, ts, "test-list-dir")

	// Write a file in a subdirectory.
	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-list-dir",
		Path:    "pkg/util.go",
		Content: "package pkg\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/list", ListRequest{
		TaskID: "test-list-dir",
		Path:   "pkg",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /list: expected 200, got %d", resp.StatusCode)
	}

	var result ListResponse
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

	doRequest(t, ts, http.MethodPut, "/file", FileWriteRequest{
		TaskID:  "test-search",
		Path:    "main.go",
		Content: "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	}).Body.Close()

	resp := doRequest(t, ts, http.MethodPost, "/search", SearchRequest{
		TaskID:   "test-search",
		Pattern:  "func main",
		FileGlob: "*.go",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /search: expected 200, got %d", resp.StatusCode)
	}

	var result SearchResponse
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
