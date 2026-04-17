// Package spawn implements the spawn_agent tool executor.
package spawn

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager handles git worktree lifecycle for isolated agent execution.
// Each worktree is a lightweight checkout that shares the git object database
// with the main repository but has its own working directory.
type WorktreeManager struct {
	repoRoot string // absolute path to the main repository
}

// NewWorktreeManager constructs a WorktreeManager rooted at repoRoot.
// repoRoot must be the top-level directory of a git repository.
func NewWorktreeManager(repoRoot string) *WorktreeManager {
	return &WorktreeManager{repoRoot: repoRoot}
}

// Create creates a new git worktree for isolated execution.
// The worktree is placed at <repoRoot>/.semspec/worktrees/<id>/ and checked
// out at a detached HEAD pointing at the current HEAD of the main repo.
//
// Returns the absolute path to the worktree directory.
func (m *WorktreeManager) Create(ctx context.Context, id string) (string, error) {
	worktreePath := filepath.Join(m.repoRoot, ".semspec", "worktrees", id)

	// Detect whether the path already exists so we can give a clear error
	// rather than letting git emit a confusing message.
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	// Ensure the parent directory exists. `.semspec/` is already in .gitignore
	// so no separate ignore entry is needed.
	parent := filepath.Dir(worktreePath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", fmt.Errorf("create worktree parent dir: %w", err)
	}

	// Validate HEAD exists before attempting worktree creation. An empty
	// repository (no commits) will fail with "fatal: invalid reference: HEAD".
	if err := m.run(ctx, m.repoRoot, "git", "rev-parse", "--verify", "HEAD"); err != nil {
		return "", fmt.Errorf("cannot create worktree: HEAD is invalid (does the repository have at least one commit?): %w", err)
	}

	// Create a detached-HEAD worktree at the current HEAD. Using --detach
	// avoids creating or checking out a branch, which would conflict with
	// any existing branch of the same name in the main repo.
	if err := m.run(ctx, m.repoRoot,
		"git", "worktree", "add", "--detach", worktreePath, "HEAD",
	); err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}

	// Copy the git user config from the main repo into the worktree so that
	// commits made inside the worktree (during Merge) are properly attributed.
	m.copyGitConfig(ctx, worktreePath)

	return worktreePath, nil
}

// Merge merges changes from a completed worktree back into the main repo.
//
// Steps:
//  1. Stage and commit all changes in the worktree.
//  2. Record the resulting commit hash.
//  3. In the main repo, run `git merge --no-ff` to bring the changes over.
//  4. On success, remove the worktree.
//
// If step 3 produces a merge conflict, Merge returns a descriptive error and
// leaves the worktree in place so the caller can inspect it. The caller is
// responsible for calling Discard if it decides to abandon the changes.
func (m *WorktreeManager) Merge(ctx context.Context, worktreePath string) error {
	id := filepath.Base(worktreePath)

	// Stage everything in the worktree.
	if err := m.run(ctx, worktreePath, "git", "-C", worktreePath, "add", "-A"); err != nil {
		return fmt.Errorf("worktree stage changes: %w", err)
	}

	// Commit. If the index is clean (nothing to commit) git exits non-zero
	// with "nothing to commit". We treat that as a no-op: skip to removal.
	commitMsg := fmt.Sprintf("agent: %s task completion", id)
	commitErr := m.run(ctx, worktreePath,
		"git", "-C", worktreePath, "commit", "-m", commitMsg,
	)
	nothingToCommit := commitErr != nil &&
		strings.Contains(commitErr.Error(), "nothing to commit")

	if commitErr != nil && !nothingToCommit {
		return fmt.Errorf("worktree commit: %w", commitErr)
	}

	if nothingToCommit {
		// No changes to merge — just remove the worktree cleanly.
		return m.removeWorktree(ctx, worktreePath)
	}

	// Get the commit hash we just created in the worktree.
	hash, err := m.output(ctx, worktreePath,
		"git", "-C", worktreePath, "rev-parse", "HEAD",
	)
	if err != nil {
		return fmt.Errorf("rev-parse worktree HEAD: %w", err)
	}
	hash = strings.TrimSpace(hash)

	// Merge the worktree commit into the main repo.
	mergeMsg := fmt.Sprintf("merge: agent task %s", id)
	if err := m.run(ctx, m.repoRoot,
		"git", "-C", m.repoRoot, "merge", hash, "--no-ff", "-m", mergeMsg,
	); err != nil {
		// Leave the worktree in place so the caller can inspect or retry.
		return fmt.Errorf("merge worktree commit %s: %w", hash, err)
	}

	return m.removeWorktree(ctx, worktreePath)
}

// Discard removes a worktree and all its uncommitted changes without merging.
// This is the rollback path — called when a task fails and the work should
// be abandoned.
func (m *WorktreeManager) Discard(ctx context.Context, worktreePath string) error {
	// --force removes the worktree even if it has uncommitted changes.
	if err := m.run(ctx, m.repoRoot,
		"git", "worktree", "remove", "--force", worktreePath,
	); err != nil {
		// Best-effort fallback: try without --force, then OS removal.
		_ = m.run(ctx, m.repoRoot, "git", "worktree", "remove", worktreePath)
		if _, statErr := os.Stat(worktreePath); statErr == nil {
			if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
				return fmt.Errorf("discard worktree (fallback os.RemoveAll): %w", removeErr)
			}
		}
		// Prune stale worktree metadata from git's internal list.
		_ = m.run(ctx, m.repoRoot, "git", "worktree", "prune")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// removeWorktree removes the worktree directory and its git metadata using the
// standard `git worktree remove` command.
func (m *WorktreeManager) removeWorktree(ctx context.Context, worktreePath string) error {
	if err := m.run(ctx, m.repoRoot,
		"git", "worktree", "remove", worktreePath,
	); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// copyGitConfig copies user.name and user.email from the main repo into the
// worktree's local config so commits are properly attributed. Failures are
// silently ignored — attribution is a quality-of-life detail, not correctness.
func (m *WorktreeManager) copyGitConfig(ctx context.Context, worktreePath string) {
	for _, key := range []string{"user.name", "user.email"} {
		val, err := m.output(ctx, m.repoRoot, "git", "-C", m.repoRoot, "config", key)
		if err != nil || strings.TrimSpace(val) == "" {
			continue
		}
		_ = m.run(ctx, worktreePath,
			"git", "-C", worktreePath, "config", key, strings.TrimSpace(val),
		)
	}
}

// run executes a git command, combining stdout and stderr into the error
// message on failure. dir is used only for working-directory-aware commands
// that do not already specify -C; pass repoRoot or worktreePath as appropriate.
func (m *WorktreeManager) run(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("%s: %s: %w", name, msg, err)
	}
	return nil
}

// output executes a command and returns its trimmed stdout. Stderr is captured
// and included in the error on failure.
func (m *WorktreeManager) output(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return "", fmt.Errorf("%s: %s: %w", name, msg, err)
	}
	return stdout.String(), nil
}
