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

	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expected := []string{"git_status", "git_branch", "git_commit"}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}
