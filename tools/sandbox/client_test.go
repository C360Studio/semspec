package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient returns a Client wired to the given httptest.Server.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := NewClient(srv.URL)
	require.NotNil(t, c)
	return c
}

// respond writes a JSON response with the given status code.
func respond(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func TestNewClient_EmptyURL(t *testing.T) {
	c := NewClient("")
	assert.Nil(t, c, "NewClient(\"\") must return nil")
}

func TestNewClient_ValidURL(t *testing.T) {
	c := NewClient("http://sandbox:8090")
	require.NotNil(t, c)
	assert.Equal(t, "http://sandbox:8090", c.baseURL)
}

func TestNewClient_TrailingSlashStripped(t *testing.T) {
	c := NewClient("http://sandbox:8090/")
	require.NotNil(t, c)
	assert.Equal(t, "http://sandbox:8090", c.baseURL)
}

func TestCreateWorktree(t *testing.T) {
	want := WorktreeInfo{
		Status: "created",
		Path:   "/repo/.semspec/worktrees/task-123",
		Branch: "agent/task-123",
	}

	var gotBody struct {
		TaskID string `json:"task_id"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/worktree", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusCreated, want)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).CreateWorktree(context.Background(), "task-123")
	require.NoError(t, err)
	assert.Equal(t, want.Path, got.Path)
	assert.Equal(t, want.Branch, got.Branch)
	assert.Equal(t, "task-123", gotBody.TaskID)
}

func TestDeleteWorktree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/worktree/task-456", r.URL.Path)
		respond(t, w, http.StatusOK, map[string]string{"status": "deleted"})
	}))
	defer srv.Close()

	err := newTestClient(t, srv).DeleteWorktree(context.Background(), "task-456")
	require.NoError(t, err)
}

func TestMergeWorktree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/worktree/task-789/merge", r.URL.Path)
		respond(t, w, http.StatusOK, map[string]string{"status": "merged"})
	}))
	defer srv.Close()

	err := newTestClient(t, srv).MergeWorktree(context.Background(), "task-789")
	require.NoError(t, err)
}

func TestListWorktreeFiles(t *testing.T) {
	want := []FileEntry{
		{Name: "main.go", IsDir: false, Size: 512},
		{Name: "pkg", IsDir: true, Size: 0},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/worktree/t1/files", r.URL.Path)
		respond(t, w, http.StatusOK, want)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).ListWorktreeFiles(context.Background(), "t1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "main.go", got[0].Name)
	assert.Equal(t, int64(512), got[0].Size)
	assert.True(t, got[1].IsDir)
}

func TestReadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/file", r.URL.Path)
		assert.Equal(t, "t1", r.URL.Query().Get("task_id"))
		assert.Equal(t, "src/main.go", r.URL.Query().Get("path"))
		respond(t, w, http.StatusOK, map[string]string{"content": "package main\n"})
	}))
	defer srv.Close()

	content, err := newTestClient(t, srv).ReadFile(context.Background(), "t1", "src/main.go")
	require.NoError(t, err)
	assert.Equal(t, "package main\n", content)
}

func TestWriteFile(t *testing.T) {
	var gotBody struct {
		TaskID  string `json:"task_id"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/file", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	err := newTestClient(t, srv).WriteFile(context.Background(), "t1", "src/foo.go", "package foo\n")
	require.NoError(t, err)
	assert.Equal(t, "t1", gotBody.TaskID)
	assert.Equal(t, "src/foo.go", gotBody.Path)
	assert.Equal(t, "package foo\n", gotBody.Content)
}

func TestListDir(t *testing.T) {
	want := struct {
		Entries []FileEntry `json:"entries"`
	}{
		Entries: []FileEntry{{Name: "handler.go", IsDir: false, Size: 1024}},
	}

	var gotBody struct {
		TaskID string `json:"task_id"`
		Path   string `json:"path"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/list", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, want)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).ListDir(context.Background(), "t1", "internal/")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "handler.go", got[0].Name)
	assert.Equal(t, "t1", gotBody.TaskID)
	assert.Equal(t, "internal/", gotBody.Path)
}

func TestSearch(t *testing.T) {
	wantResp := struct {
		Matches []SearchMatch `json:"matches"`
	}{
		Matches: []SearchMatch{
			{File: "main.go", Line: 5, Text: "// TODO: fix"},
		},
	}

	var gotBody struct {
		TaskID   string `json:"task_id"`
		Pattern  string `json:"pattern"`
		FileGlob string `json:"file_glob"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/search", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, wantResp)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Search(context.Background(), "t1", "TODO", "*.go")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "main.go", got[0].File)
	assert.Equal(t, 5, got[0].Line)
	assert.Contains(t, got[0].Text, "TODO")
	assert.Equal(t, "t1", gotBody.TaskID)
	assert.Equal(t, "TODO", gotBody.Pattern)
	assert.Equal(t, "*.go", gotBody.FileGlob)
}

func TestGitStatus(t *testing.T) {
	var gotBody struct {
		TaskID string `json:"task_id"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/git/status", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, map[string]string{"output": "M  main.go\n"})
	}))
	defer srv.Close()

	out, err := newTestClient(t, srv).GitStatus(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, "M  main.go\n", out)
	assert.Equal(t, "t1", gotBody.TaskID)
}

func TestGitCommit(t *testing.T) {
	const wantHash = "abc123def456"
	var gotBody struct {
		TaskID  string `json:"task_id"`
		Message string `json:"message"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/git/commit", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, map[string]any{
			"status": "committed",
			"hash":   wantHash,
			"files_changed": []map[string]string{
				{"path": "main.go", "operation": "modify"},
			},
		})
	}))
	defer srv.Close()

	result, err := newTestClient(t, srv).GitCommit(context.Background(), "t1", "feat: add handler")
	require.NoError(t, err)
	assert.Equal(t, wantHash, result.Hash)
	assert.Equal(t, "committed", result.Status)
	require.Len(t, result.FilesChanged, 1)
	assert.Equal(t, "main.go", result.FilesChanged[0].Path)
	assert.Equal(t, "modify", result.FilesChanged[0].Operation)
	assert.Equal(t, "t1", gotBody.TaskID)
	assert.Equal(t, "feat: add handler", gotBody.Message)
}

func TestGitCommit_NothingToCommit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(t, w, http.StatusOK, map[string]string{"status": "nothing_to_commit"})
	}))
	defer srv.Close()

	result, err := newTestClient(t, srv).GitCommit(context.Background(), "t1", "no-op")
	require.NoError(t, err)
	assert.Equal(t, "nothing_to_commit", result.Status)
	assert.Empty(t, result.FilesChanged)
}

func TestGitDiff(t *testing.T) {
	var gotBody struct {
		TaskID string `json:"task_id"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/git/diff", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, map[string]string{"output": "+added line\n"})
	}))
	defer srv.Close()

	out, err := newTestClient(t, srv).GitDiff(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, "+added line\n", out)
	assert.Equal(t, "t1", gotBody.TaskID)
}

func TestExec(t *testing.T) {
	want := ExecResult{
		Stdout:   "ok\n",
		Stderr:   "",
		ExitCode: 0,
		TimedOut: false,
	}
	var gotBody struct {
		TaskID    string `json:"task_id"`
		Command   string `json:"command"`
		TimeoutMs int    `json:"timeout_ms"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/exec", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		respond(t, w, http.StatusOK, want)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Exec(context.Background(), "t1", "go test ./...", 30000)
	require.NoError(t, err)
	assert.Equal(t, "ok\n", got.Stdout)
	assert.Equal(t, 0, got.ExitCode)
	assert.False(t, got.TimedOut)
	assert.Equal(t, "t1", gotBody.TaskID)
	assert.Equal(t, "go test ./...", gotBody.Command)
	assert.Equal(t, 30000, gotBody.TimeoutMs)
}

func TestExec_TimedOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(t, w, http.StatusOK, ExecResult{
			Stdout:   "",
			Stderr:   "signal: killed",
			ExitCode: -1,
			TimedOut: true,
		})
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Exec(context.Background(), "t1", "sleep 9999", 100)
	require.NoError(t, err)
	assert.True(t, got.TimedOut)
	assert.Equal(t, -1, got.ExitCode)
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/health", r.URL.Path)
		respond(t, w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	err := newTestClient(t, srv).Health(context.Background())
	require.NoError(t, err)
}

func TestServerError_StructuredBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(t, w, http.StatusInternalServerError, map[string]string{"error": "disk full"})
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).CreateWorktree(context.Background(), "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
	assert.Contains(t, err.Error(), "500")
}

func TestServerError_PlainStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a non-JSON body so the client falls back to the status code message.
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).CreateWorktree(context.Background(), "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestServerError_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(t, w, http.StatusNotFound, map[string]string{"error": "worktree not found"})
	}))
	defer srv.Close()

	err := newTestClient(t, srv).DeleteWorktree(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worktree not found")
}

func TestContextCancellation(t *testing.T) {
	// Server that blocks indefinitely — cancelled context should abort the request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := newTestClient(t, srv).CreateWorktree(ctx, "t1")
	require.Error(t, err)
}
