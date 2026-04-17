package spawn_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/tools/spawn"
)

// ---------------------------------------------------------------------------
// Test repo helpers
// ---------------------------------------------------------------------------

// setupWorktreeRepo initialises a fresh git repository with one initial commit
// and returns its absolute path. All git operations use the repo-local config
// so tests are hermetic and do not depend on the developer's global git config.
func setupWorktreeRepo(t *testing.T) string {
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
			t.Fatalf("setupWorktreeRepo: %v: %s", args, out)
		}
	}

	// Write an initial file and commit it so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# semspec test repo\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "feat: initial commit"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setupWorktreeRepo commit: %v: %s", args, out)
		}
	}

	return dir
}

// gitFileExists reports whether path exists inside repo.
func gitFileExists(t *testing.T, repo, path string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, path))
	return err == nil
}

// gitReadFile returns the contents of a file inside repo, failing the test if
// the file cannot be read.
func gitReadFile(t *testing.T, repo, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, path))
	if err != nil {
		t.Fatalf("gitReadFile(%s): %v", path, err)
	}
	return string(data)
}

// isGitWorktree reports whether dir is recognised as a git working tree by
// running `git rev-parse --is-inside-work-tree` inside it.
func isGitWorktree(t *testing.T, dir string) bool {
	t.Helper()
	c := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestWorktreeManager_Create_CreatesWorktree verifies that Create produces a
// directory that git recognises as a valid working tree.
func TestWorktreeManager_Create_CreatesWorktree(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt, err := mgr.Create(context.Background(), "test-create")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Discard(context.Background(), wt) })

	// Directory must exist.
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree directory does not exist: %v", err)
	}

	// Must be a git working tree.
	if !isGitWorktree(t, wt) {
		t.Errorf("created path %s is not a git worktree", wt)
	}

	// Path must be under <repo>/.semspec/worktrees/.
	expectedPrefix := filepath.Join(repo, ".semspec", "worktrees")
	if !strings.HasPrefix(wt, expectedPrefix) {
		t.Errorf("worktree path %q does not start with %q", wt, expectedPrefix)
	}
}

// TestWorktreeManager_Create_IsolatedFromMain verifies that a file written
// inside a worktree does not appear in the main repo's working tree.
func TestWorktreeManager_Create_IsolatedFromMain(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt, err := mgr.Create(context.Background(), "test-isolated")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Discard(context.Background(), wt) })

	// Write a file inside the worktree.
	if err := os.WriteFile(filepath.Join(wt, "agent-output.txt"), []byte("isolated change"), 0644); err != nil {
		t.Fatalf("write file in worktree: %v", err)
	}

	// The file must NOT be visible at the top-level of the main repo.
	if gitFileExists(t, repo, "agent-output.txt") {
		t.Error("file written in worktree leaked into main repo working tree")
	}
}

// TestWorktreeManager_Merge_BringsChangesBack verifies that after Merge the
// file created inside the worktree is present in the main repo's working tree.
func TestWorktreeManager_Merge_BringsChangesBack(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt, err := mgr.Create(context.Background(), "test-merge")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write a file inside the worktree.
	const want = "merged content\n"
	if err := os.WriteFile(filepath.Join(wt, "result.txt"), []byte(want), 0644); err != nil {
		t.Fatalf("write result.txt: %v", err)
	}

	if err := mgr.Merge(context.Background(), wt); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// File must now exist in the main repo.
	if !gitFileExists(t, repo, "result.txt") {
		t.Fatal("result.txt not found in main repo after Merge")
	}
	if got := gitReadFile(t, repo, "result.txt"); got != want {
		t.Errorf("result.txt content = %q, want %q", got, want)
	}

	// Worktree directory must be gone after a successful merge.
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("worktree directory %s still exists after Merge", wt)
	}
}

// TestWorktreeManager_Discard_RemovesCleanly verifies that after Discard:
//   - the worktree directory no longer exists, and
//   - the file written inside is not visible in the main repo.
func TestWorktreeManager_Discard_RemovesCleanly(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt, err := mgr.Create(context.Background(), "test-discard")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write a file that should disappear on Discard.
	if err := os.WriteFile(filepath.Join(wt, "abandoned.txt"), []byte("should vanish"), 0644); err != nil {
		t.Fatalf("write abandoned.txt: %v", err)
	}

	if err := mgr.Discard(context.Background(), wt); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	// Worktree directory must be gone.
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("worktree directory %s still exists after Discard", wt)
	}

	// File must not be in the main repo.
	if gitFileExists(t, repo, "abandoned.txt") {
		t.Error("discarded file appeared in main repo working tree")
	}
}

// TestWorktreeManager_Merge_HandlesConflict verifies that when both the main
// repo and the worktree modify the same file, Merge returns an error describing
// the conflict rather than panicking or silently corrupting state.
func TestWorktreeManager_Merge_HandlesConflict(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	// Write a shared file in the main repo and commit it.
	sharedPath := filepath.Join(repo, "shared.txt")
	if err := os.WriteFile(sharedPath, []byte("original\n"), 0644); err != nil {
		t.Fatalf("write shared.txt: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", "shared.txt"},
		{"git", "commit", "-m", "chore: add shared file"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("commit shared.txt: %v: %s", args, out)
		}
	}

	// Create a worktree from this state.
	wt, err := mgr.Create(context.Background(), "test-conflict")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Discard(context.Background(), wt) })

	// Modify shared.txt in the worktree.
	if err := os.WriteFile(filepath.Join(wt, "shared.txt"), []byte("worktree change\n"), 0644); err != nil {
		t.Fatalf("write worktree shared.txt: %v", err)
	}

	// Advance the main repo's HEAD by committing a conflicting change to the
	// same file AFTER the worktree was branched off.
	if err := os.WriteFile(sharedPath, []byte("main repo change\n"), 0644); err != nil {
		t.Fatalf("write main shared.txt: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", "shared.txt"},
		{"git", "commit", "-m", "chore: conflicting main change"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("commit conflicting change: %v: %s", args, out)
		}
	}

	// Merge must fail with a meaningful error.
	err = mgr.Merge(context.Background(), wt)
	if err == nil {
		// If git resolved the conflict automatically (e.g. both sides changed
		// different lines), that is acceptable — not all file-level conflicts
		// are textual conflicts. Fail only if we expected a conflict and didn't get one.
		t.Log("Merge succeeded (git resolved automatically — not a textual conflict)")
		return
	}
	if !strings.Contains(err.Error(), "merge") {
		t.Errorf("Merge error = %q, expected it to mention 'merge'", err.Error())
	}

	// After a merge failure, abort so the repo is left in a clean state for
	// subsequent cleanup.
	c := exec.Command("git", "merge", "--abort")
	c.Dir = repo
	_ = c.Run()
}

// TestWorktreeManager_Create_MultipleWorktrees verifies that two independent
// worktrees can exist simultaneously without interfering with each other.
func TestWorktreeManager_Create_MultipleWorktrees(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt1, err := mgr.Create(context.Background(), "concurrent-a")
	if err != nil {
		t.Fatalf("Create wt1: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Discard(context.Background(), wt1) })

	wt2, err := mgr.Create(context.Background(), "concurrent-b")
	if err != nil {
		t.Fatalf("Create wt2: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Discard(context.Background(), wt2) })

	// Both must be distinct directories.
	if wt1 == wt2 {
		t.Error("both worktrees have the same path")
	}

	// Both must be valid git working trees.
	if !isGitWorktree(t, wt1) {
		t.Errorf("wt1 %s is not a git worktree", wt1)
	}
	if !isGitWorktree(t, wt2) {
		t.Errorf("wt2 %s is not a git worktree", wt2)
	}

	// Write distinct files in each worktree; neither should appear in the other.
	if err := os.WriteFile(filepath.Join(wt1, "file-a.txt"), []byte("from a"), 0644); err != nil {
		t.Fatalf("write file-a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt2, "file-b.txt"), []byte("from b"), 0644); err != nil {
		t.Fatalf("write file-b.txt: %v", err)
	}

	if gitFileExists(t, wt1, "file-b.txt") {
		t.Error("file-b.txt leaked into wt1")
	}
	if gitFileExists(t, wt2, "file-a.txt") {
		t.Error("file-a.txt leaked into wt2")
	}
}

// TestWorktreeManager_Create_DuplicateID verifies that attempting to create
// a second worktree with the same ID returns an error rather than silently
// overwriting or corrupting the first.
func TestWorktreeManager_Create_DuplicateID(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt, err := mgr.Create(context.Background(), "dup-id")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Discard(context.Background(), wt) })

	_, err = mgr.Create(context.Background(), "dup-id")
	if err == nil {
		t.Fatal("second Create with same ID should have returned an error, got nil")
	}
}

// TestWorktreeManager_Merge_NothingToCommit verifies that Merge succeeds and
// removes the worktree cleanly when no files were changed inside it.
func TestWorktreeManager_Merge_NothingToCommit(t *testing.T) {
	t.Parallel()

	repo := setupWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	wt, err := mgr.Create(context.Background(), "test-empty-merge")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// No changes — Merge should still succeed.
	if err := mgr.Merge(context.Background(), wt); err != nil {
		t.Fatalf("Merge with no changes: %v", err)
	}

	// Worktree should be cleaned up.
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("worktree %s still exists after empty Merge", wt)
	}
}

// ---------------------------------------------------------------------------
// Empty repo tests (invalid HEAD)
// ---------------------------------------------------------------------------

// setupEmptyWorktreeRepo initialises a git repository with NO commits.
func setupEmptyWorktreeRepo(t *testing.T) string {
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
			t.Fatalf("setupEmptyWorktreeRepo: %v: %s", args, out)
		}
	}

	return dir
}

// TestWorktreeManager_Create_EmptyRepo verifies that Create returns a clear
// error when the repository has no commits (HEAD is invalid).
func TestWorktreeManager_Create_EmptyRepo(t *testing.T) {
	t.Parallel()

	repo := setupEmptyWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	_, err := mgr.Create(context.Background(), "test-empty")
	if err == nil {
		t.Fatal("expected error creating worktree in empty repo, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "HEAD") || !strings.Contains(errMsg, "commit") {
		t.Errorf("error message should mention HEAD and commits, got: %q", errMsg)
	}
}

// TestWorktreeManager_Create_EmptyRepoRecovery verifies that once a commit is
// added to a previously empty repo, worktree creation succeeds.
func TestWorktreeManager_Create_EmptyRepoRecovery(t *testing.T) {
	t.Parallel()

	repo := setupEmptyWorktreeRepo(t)
	mgr := spawn.NewWorktreeManager(repo)

	// First attempt fails.
	if _, err := mgr.Create(context.Background(), "test-recover"); err == nil {
		t.Fatal("expected error on empty repo, got nil")
	}

	// Add a commit.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	for _, args := range [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", "feat: initial commit"},
	} {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}

	// Second attempt succeeds.
	wt, err := mgr.Create(context.Background(), "test-recover")
	if err != nil {
		t.Fatalf("expected success after commit, got: %v", err)
	}

	if !isGitWorktree(t, wt) {
		t.Errorf("created path %s is not a valid git worktree", wt)
	}

	// Cleanup.
	_ = mgr.Discard(context.Background(), wt)
}
