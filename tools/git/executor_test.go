package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// setupTestRepo creates a temporary git repository for testing
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create initial commit
	testFile := filepath.Join(tmpDir, "initial.txt")
	os.WriteFile(testFile, []byte("initial"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "feat: initial commit")
	cmd.Dir = tmpDir
	cmd.Run()

	return tmpDir
}

func TestGitStatus(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor := NewExecutor(repoDir)

	t.Run("clean repo", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_status",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
		if result.Content != "Working tree clean" {
			t.Errorf("expected clean status, got: %s", result.Content)
		}
	})

	t.Run("modified file", func(t *testing.T) {
		// Modify a file
		os.WriteFile(filepath.Join(repoDir, "initial.txt"), []byte("modified"), 0644)

		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_status",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
		if result.Content == "Working tree clean" {
			t.Error("expected modified status")
		}

		// Reset for other tests
		cmd := exec.Command("git", "checkout", "initial.txt")
		cmd.Dir = repoDir
		cmd.Run()
	})

	t.Run("not a git repo", func(t *testing.T) {
		nonGitDir := t.TempDir()
		executor := NewExecutor(nonGitDir)

		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_status",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for non-git repo")
		}
	})
}

func TestGitBranch(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor := NewExecutor(repoDir)

	t.Run("create new branch", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_branch",
			Arguments: map[string]any{
				"name": "test-branch",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}

		// Verify we're on the new branch
		cmd := exec.Command("git", "branch", "--show-current")
		cmd.Dir = repoDir
		output, _ := cmd.Output()
		if string(output) != "test-branch\n" {
			t.Errorf("expected to be on test-branch, got: %s", output)
		}
	})

	t.Run("switch to existing branch", func(t *testing.T) {
		// Go back to main/master
		cmd := exec.Command("git", "checkout", "-")
		cmd.Dir = repoDir
		cmd.Run()

		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_branch",
			Arguments: map[string]any{
				"name": "test-branch",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
	})

	t.Run("missing branch name", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_branch",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for missing branch name")
		}
	})
}

func TestGitCommit(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor := NewExecutor(repoDir)

	t.Run("commit staged changes", func(t *testing.T) {
		// Create and stage a file
		testFile := filepath.Join(repoDir, "new.txt")
		os.WriteFile(testFile, []byte("new content"), 0644)

		cmd := exec.Command("git", "add", "new.txt")
		cmd.Dir = repoDir
		cmd.Run()

		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_commit",
			Arguments: map[string]any{
				"message": "feat: add new file",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
	})

	t.Run("commit with stage_all", func(t *testing.T) {
		// Modify existing file
		os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("modified"), 0644)

		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_commit",
			Arguments: map[string]any{
				"message":   "fix: update file",
				"stage_all": true,
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
	})

	t.Run("nothing to commit", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_commit",
			Arguments: map[string]any{
				"message": "feat: empty commit",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for nothing to commit")
		}
	})

	t.Run("invalid commit message", func(t *testing.T) {
		// Create and stage a file
		testFile := filepath.Join(repoDir, "another.txt")
		os.WriteFile(testFile, []byte("content"), 0644)

		cmd := exec.Command("git", "add", "another.txt")
		cmd.Dir = repoDir
		cmd.Run()

		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_commit",
			Arguments: map[string]any{
				"message": "this is not a conventional commit",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for invalid commit message format")
		}

		// Unstage for cleanup
		cmd = exec.Command("git", "reset", "HEAD", "another.txt")
		cmd.Dir = repoDir
		cmd.Run()
	})
}

func TestValidateConventionalCommit(t *testing.T) {
	tests := []struct {
		message string
		valid   bool
	}{
		{"feat: add new feature", true},
		{"fix: resolve bug", true},
		{"docs: update readme", true},
		{"style: format code", true},
		{"refactor: restructure module", true},
		{"test: add unit tests", true},
		{"chore: update deps", true},
		{"feat(auth): add login", true},
		{"fix(api): handle errors", true},
		{"invalid message", false},
		{"Feat: wrong case", false},
		{"feat add feature", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := ValidateConventionalCommit(tt.message)
			if result != tt.valid {
				t.Errorf("ValidateConventionalCommit(%q) = %v, want %v", tt.message, result, tt.valid)
			}
		})
	}
}

func TestListTools(t *testing.T) {
	executor := NewExecutor("/tmp")
	tools := executor.ListTools()

	if len(tools) != 8 {
		t.Errorf("expected 8 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expected := []string{"git_status", "git_branch", "git_commit", "git_clone", "git_pull", "git_fetch", "git_log", "git_ls_remote"}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestValidateGitURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// Valid URLs
		{"https URL", "https://github.com/owner/repo.git", false},
		{"https URL without .git", "https://github.com/owner/repo", false},
		{"git protocol", "git://github.com/owner/repo.git", false},
		{"ssh protocol", "ssh://git@github.com/owner/repo.git", false},
		{"ssh shorthand", "git@github.com:owner/repo.git", false},
		{"ssh shorthand without .git", "git@gitlab.com:owner/repo", false},

		// Invalid URLs
		{"file protocol", "file:///path/to/repo", true},
		{"http (not https)", "http://github.com/owner/repo.git", true},
		{"ftp protocol", "ftp://example.com/repo.git", true},
		{"local path", "/path/to/repo", true},
		{"relative path", "../repo", true},
		{"empty URL", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	baseDir := t.TempDir()

	tests := []struct {
		name    string
		baseDir string
		path    string
		wantErr bool
	}{
		// Valid paths
		{"simple path", baseDir, filepath.Join(baseDir, "repo"), false},
		{"nested path", baseDir, filepath.Join(baseDir, "org", "repo"), false},
		{"no base dir", "", "/some/path", false},

		// Invalid paths
		{"path traversal", baseDir, filepath.Join(baseDir, "..", "escape"), true},
		{"double dot in middle", baseDir, filepath.Join(baseDir, "foo", "..", "..", "bar"), true},
		{"outside base", baseDir, "/tmp/other", true},
		{"empty path", baseDir, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.baseDir, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath(%q, %q) error = %v, wantErr %v", tt.baseDir, tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestGitLog(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor := NewExecutor(repoDir)

	// Create a few more commits for log testing
	for i := 1; i <= 3; i++ {
		testFile := filepath.Join(repoDir, "file"+string(rune('0'+i))+".txt")
		os.WriteFile(testFile, []byte("content"), 0644)

		cmd := exec.Command("git", "add", ".")
		cmd.Dir = repoDir
		cmd.Run()

		cmd = exec.Command("git", "commit", "-m", "feat: add file "+string(rune('0'+i)))
		cmd.Dir = repoDir
		cmd.Run()
	}

	t.Run("default log", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_log",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
		if result.Content == "" {
			t.Error("expected log output")
		}
	})

	t.Run("limited count", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_log",
			Arguments: map[string]any{
				"count": float64(2),
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error != "" {
			t.Errorf("unexpected error: %s", result.Error)
		}
	})

	t.Run("not a git repo", func(t *testing.T) {
		nonGitDir := t.TempDir()
		executor := NewExecutor(nonGitDir)

		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_log",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for non-git repo")
		}
	})
}

func TestGitFetch(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor := NewExecutor(repoDir)

	t.Run("fetch without remote", func(t *testing.T) {
		// This will fail because there's no remote, but we're testing the tool execution
		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_fetch",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		// Without a remote, fetch should still work (just no-op)
		// or return an error about no remote
		_ = result // We just verify it doesn't panic
	})

	t.Run("not a git repo", func(t *testing.T) {
		nonGitDir := t.TempDir()
		executor := NewExecutor(nonGitDir)

		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_fetch",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for non-git repo")
		}
	})
}

func TestGitPull(t *testing.T) {
	repoDir := setupTestRepo(t)
	executor := NewExecutor(repoDir)

	t.Run("not a git repo", func(t *testing.T) {
		nonGitDir := t.TempDir()
		executor := NewExecutor(nonGitDir)

		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_pull",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for non-git repo")
		}
	})

	t.Run("pull without remote", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_pull",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		// Pull without remote should fail
		if result.Error == "" {
			t.Error("expected error for pull without remote")
		}
	})
}

func TestGitClone(t *testing.T) {
	executor := NewExecutor(t.TempDir())

	t.Run("missing url", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_clone",
			Arguments: map[string]any{
				"dest": "/tmp/repo",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for missing url")
		}
	})

	t.Run("missing dest", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_clone",
			Arguments: map[string]any{
				"url": "https://github.com/owner/repo.git",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for missing dest")
		}
	})

	t.Run("invalid url protocol", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_clone",
			Arguments: map[string]any{
				"url":  "file:///tmp/repo",
				"dest": t.TempDir(),
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for file:// protocol")
		}
		if result.Error != "" && !contains(result.Error, "protocol") && !contains(result.Error, "not allowed") {
			t.Errorf("expected protocol error, got: %s", result.Error)
		}
	})
}

func TestGitLsRemote(t *testing.T) {
	executor := NewExecutor(t.TempDir())

	t.Run("missing url", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:        "test-call",
			Name:      "git_ls_remote",
			Arguments: map[string]any{},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for missing url")
		}
	})

	t.Run("invalid url protocol", func(t *testing.T) {
		call := agentic.ToolCall{
			ID:   "test-call",
			Name: "git_ls_remote",
			Arguments: map[string]any{
				"url": "file:///tmp/repo",
			},
		}

		result, _ := executor.Execute(context.Background(), call)
		if result.Error == "" {
			t.Error("expected error for file:// protocol")
		}
	})
}

func TestUnknownTool(t *testing.T) {
	executor := NewExecutor(t.TempDir())

	call := agentic.ToolCall{
		ID:        "test-call",
		Name:      "git_unknown",
		Arguments: map[string]any{},
	}

	result, err := executor.Execute(context.Background(), call)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if result.Error == "" {
		t.Error("expected error in result for unknown tool")
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
