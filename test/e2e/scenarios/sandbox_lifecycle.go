package scenarios

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/test/e2e/client"
	"github.com/c360studio/semspec/test/e2e/config"
)

// SandboxLifecycleScenario tests the sandbox server's complete lifecycle:
// worktree creation, file operations, git operations, command execution,
// workspace browser, merge, and cleanup.
type SandboxLifecycleScenario struct {
	name        string
	description string
	config      *config.Config
	sandbox     *client.SandboxClient
}

// NewSandboxLifecycleScenario creates a new sandbox lifecycle scenario.
func NewSandboxLifecycleScenario(cfg *config.Config) *SandboxLifecycleScenario {
	return &SandboxLifecycleScenario{
		name:        "sandbox-lifecycle",
		description: "Tests sandbox server lifecycle: worktree CRUD, file ops, git, exec, merge, cleanup",
		config:      cfg,
	}
}

// Name implements Scenario.
func (s *SandboxLifecycleScenario) Name() string { return s.name }

// Description implements Scenario.
func (s *SandboxLifecycleScenario) Description() string { return s.description }

// Setup implements Scenario.
func (s *SandboxLifecycleScenario) Setup(ctx context.Context) error {
	sandboxURL := s.config.SandboxURL
	if sandboxURL == "" {
		sandboxURL = config.DefaultSandboxURL
	}
	s.sandbox = client.NewSandboxClient(sandboxURL)

	if err := s.sandbox.WaitForHealthy(ctx); err != nil {
		return fmt.Errorf("sandbox not healthy: %w", err)
	}

	return nil
}

// Execute implements Scenario.
func (s *SandboxLifecycleScenario) Execute(ctx context.Context) (*Result, error) {
	result := NewResult(s.name)
	defer result.Complete()

	stages := []struct {
		name string
		fn   func(context.Context, *Result) error
	}{
		{"health-check", s.stageHealthCheck},
		{"create-worktree", s.stageCreateWorktree},
		{"write-file", s.stageWriteFile},
		{"read-file", s.stageReadFile},
		{"list-files", s.stageListFiles},
		{"search-files", s.stageSearchFiles},
		{"git-status", s.stageGitStatus},
		{"git-commit", s.stageGitCommit},
		{"exec-command", s.stageExecCommand},
		{"exec-command-not-found", s.stageExecCommandNotFound},
		{"workspace-tasks", s.stageWorkspaceTasks},
		{"workspace-tree", s.stageWorkspaceTree},
		{"merge-worktree", s.stageMergeWorktree},
		{"delete-worktree", s.stageDeleteWorktree},
		{"verify-cleanup", s.stageVerifyCleanup},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		stageCtx, cancel := context.WithTimeout(ctx, s.config.StageTimeout)

		err := stage.fn(stageCtx, result)
		cancel()

		stageDuration := time.Since(stageStart)
		result.SetMetric(fmt.Sprintf("%s_duration_us", stage.name), stageDuration.Microseconds())

		if err != nil {
			result.AddStage(stage.name, false, stageDuration, err.Error())
			result.AddError(fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			return result, nil
		}

		result.AddStage(stage.name, true, stageDuration, "")
	}

	result.Success = true
	return result, nil
}

// Teardown implements Scenario.
func (s *SandboxLifecycleScenario) Teardown(ctx context.Context) error {
	// Best-effort cleanup of worktree if test failed mid-way.
	if s.sandbox != nil {
		_ = s.sandbox.DeleteWorktree(ctx, sandboxTestTaskID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const sandboxTestTaskID = "e2e-sandbox-lifecycle-001"

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

func (s *SandboxLifecycleScenario) stageHealthCheck(ctx context.Context, result *Result) error {
	if err := s.sandbox.HealthCheck(ctx); err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	result.SetDetail("health_status", "ok")
	return nil
}

func (s *SandboxLifecycleScenario) stageCreateWorktree(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.CreateWorktree(ctx, client.WorktreeCreateRequest{
		TaskID: sandboxTestTaskID,
	})
	if err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}
	if resp.Path == "" {
		return fmt.Errorf("worktree path is empty")
	}
	if resp.Branch == "" {
		return fmt.Errorf("worktree branch is empty")
	}

	result.SetDetail("worktree_path", resp.Path)
	result.SetDetail("worktree_branch", resp.Branch)
	result.SetDetail("worktree_status", resp.Status)
	return nil
}

func (s *SandboxLifecycleScenario) stageWriteFile(ctx context.Context, result *Result) error {
	err := s.sandbox.WriteFile(ctx, client.FileWriteRequest{
		TaskID:  sandboxTestTaskID,
		Path:    "api/health.go",
		Content: "package api\n\nfunc HealthCheck() string {\n\treturn \"ok\"\n}\n",
	})
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	result.SetDetail("file_written", "api/health.go")
	return nil
}

func (s *SandboxLifecycleScenario) stageReadFile(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.ReadFile(ctx, sandboxTestTaskID, "api/health.go")
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if resp.Content == "" {
		return fmt.Errorf("read file returned empty content")
	}
	if resp.Size == 0 {
		return fmt.Errorf("read file returned size=0")
	}

	result.SetDetail("file_read_size", resp.Size)
	return nil
}

func (s *SandboxLifecycleScenario) stageListFiles(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.ListDirectory(ctx, client.ListRequest{
		TaskID: sandboxTestTaskID,
		Path:   "api",
	})
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}

	found := false
	for _, entry := range resp.Entries {
		if entry.Name == "health.go" {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("health.go not found in file list (got %d entries)", len(resp.Entries))
	}

	result.SetDetail("list_entry_count", len(resp.Entries))
	return nil
}

func (s *SandboxLifecycleScenario) stageSearchFiles(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.Search(ctx, client.SearchRequest{
		TaskID:  sandboxTestTaskID,
		Pattern: "HealthCheck",
	})
	if err != nil {
		return fmt.Errorf("search files: %w", err)
	}
	if len(resp.Matches) == 0 {
		return fmt.Errorf("search for 'HealthCheck' returned no matches")
	}

	result.SetDetail("search_match_count", len(resp.Matches))
	return nil
}

func (s *SandboxLifecycleScenario) stageGitStatus(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.GitStatus(ctx, sandboxTestTaskID)
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if resp.Output == "" {
		return fmt.Errorf("git status returned empty output")
	}

	result.SetDetail("git_status_output_length", len(resp.Output))
	return nil
}

func (s *SandboxLifecycleScenario) stageGitCommit(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.GitCommit(ctx, client.GitCommitRequest{
		TaskID:  sandboxTestTaskID,
		Message: "feat: add health check endpoint",
	})
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if resp.Hash == "" {
		return fmt.Errorf("commit hash is empty")
	}

	result.SetDetail("commit_hash", resp.Hash)
	result.SetDetail("commit_files_changed", len(resp.FilesChanged))
	return nil
}

func (s *SandboxLifecycleScenario) stageExecCommand(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.Exec(ctx, client.ExecRequest{
		TaskID:  sandboxTestTaskID,
		Command: "echo hello-from-sandbox",
	})
	if err != nil {
		return fmt.Errorf("exec command: %w", err)
	}
	if resp.ExitCode != 0 {
		return fmt.Errorf("expected exit_code=0, got %d (stderr: %s)", resp.ExitCode, resp.Stderr)
	}
	if resp.Classification != "success" {
		return fmt.Errorf("expected classification=success, got %q", resp.Classification)
	}

	result.SetDetail("exec_stdout", resp.Stdout)
	result.SetDetail("exec_classification", resp.Classification)
	return nil
}

func (s *SandboxLifecycleScenario) stageExecCommandNotFound(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.Exec(ctx, client.ExecRequest{
		TaskID:  sandboxTestTaskID,
		Command: "nonexistent_tool_xyz123",
	})
	if err != nil {
		return fmt.Errorf("exec command_not_found: %w", err)
	}
	if resp.Classification != "command_not_found" {
		return fmt.Errorf("expected classification=command_not_found, got %q (exit_code=%d, stderr=%q)",
			resp.Classification, resp.ExitCode, resp.Stderr)
	}

	result.SetDetail("command_not_found_classification", resp.Classification)
	result.SetDetail("missing_command", resp.MissingCommand)
	return nil
}

func (s *SandboxLifecycleScenario) stageWorkspaceTasks(ctx context.Context, result *Result) error {
	tasks, err := s.sandbox.WorkspaceTasks(ctx)
	if err != nil {
		return fmt.Errorf("workspace tasks: %w", err)
	}

	found := false
	for _, t := range tasks {
		if t.TaskID == sandboxTestTaskID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in workspace tasks (got %d tasks)", sandboxTestTaskID, len(tasks))
	}

	result.SetDetail("workspace_task_count", len(tasks))
	return nil
}

func (s *SandboxLifecycleScenario) stageWorkspaceTree(ctx context.Context, result *Result) error {
	tree, err := s.sandbox.WorkspaceTree(ctx, sandboxTestTaskID)
	if err != nil {
		return fmt.Errorf("workspace tree: %w", err)
	}
	if len(tree) == 0 {
		return fmt.Errorf("workspace tree returned empty")
	}

	result.SetDetail("workspace_tree_root_entries", len(tree))
	return nil
}

func (s *SandboxLifecycleScenario) stageMergeWorktree(ctx context.Context, result *Result) error {
	resp, err := s.sandbox.MergeWorktree(ctx, sandboxTestTaskID, client.MergeRequest{
		CommitMessage: "feat: merge health check endpoint from sandbox",
	})
	if err != nil {
		return fmt.Errorf("merge worktree: %w", err)
	}

	result.SetDetail("merge_status", resp.Status)
	result.SetDetail("merge_commit", resp.Commit)
	result.SetDetail("merge_files_changed", len(resp.FilesChanged))
	return nil
}

func (s *SandboxLifecycleScenario) stageDeleteWorktree(ctx context.Context, result *Result) error {
	err := s.sandbox.DeleteWorktree(ctx, sandboxTestTaskID)
	if err != nil {
		return fmt.Errorf("delete worktree: %w", err)
	}

	result.SetDetail("worktree_deleted", true)
	return nil
}

func (s *SandboxLifecycleScenario) stageVerifyCleanup(ctx context.Context, result *Result) error {
	// Reading a file from a deleted worktree should fail.
	_, err := s.sandbox.ReadFile(ctx, sandboxTestTaskID, "api/health.go")
	if err == nil {
		return fmt.Errorf("expected error reading file from deleted worktree, got success")
	}

	result.SetDetail("cleanup_verified", true)
	return nil
}
