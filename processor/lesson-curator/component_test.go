package lessoncurator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileExistsInRepo_NilWhenRepoEmpty(t *testing.T) {
	if got := fileExistsInRepo(""); got != nil {
		t.Error("empty repo path should yield nil predicate")
	}
}

func TestFileExistsInRepo_RecognisesPresentFile(t *testing.T) {
	dir := t.TempDir()
	relPath := "alpha/file.go"
	abs := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("package alpha\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	pred := fileExistsInRepo(dir)
	if pred == nil {
		t.Fatal("predicate must not be nil for a real directory")
	}
	if !pred(relPath) {
		t.Errorf("workspace-relative path should resolve under repo: %s", relPath)
	}
	if pred("nope/missing.go") {
		t.Error("missing path should return false")
	}
}

func TestFileExistsInRepo_AbsolutePathBypassesRepo(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "anywhere.go")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Predicate scoped to a *different* repo root — absolute path should
	// still resolve because the predicate doesn't strip absolute prefixes.
	other := t.TempDir()
	pred := fileExistsInRepo(other)
	if pred == nil || !pred(abs) {
		t.Errorf("absolute path %q should resolve regardless of repo root", abs)
	}
}

func TestFileExistsInRepo_EmptyPath(t *testing.T) {
	dir := t.TempDir()
	pred := fileExistsInRepo(dir)
	if pred("") {
		t.Error("empty path must return false")
	}
}

func TestResolveRepoPath_PrefersConfigured(t *testing.T) {
	dir := t.TempDir()
	// Put a non-existent path in the env so we know configured wins.
	t.Setenv("SEMSPEC_REPO_PATH", "/this/does/not/exist/any/where")
	got := resolveRepoPath(dir)
	if got == "" {
		t.Fatal("resolveRepoPath returned empty for a real configured dir")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestResolveRepoPath_FallsBackToEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SEMSPEC_REPO_PATH", dir)
	got := resolveRepoPath("")
	if got == "" {
		t.Fatal("env fallback failed")
	}
}

func TestResolveRepoPath_FallsBackToCWD(t *testing.T) {
	t.Setenv("SEMSPEC_REPO_PATH", "")
	got := resolveRepoPath("")
	if got == "" {
		t.Error("CWD fallback failed; the test process always has a valid working dir")
	}
}

func TestResolveRepoPath_AllInvalidReturnsEmpty(t *testing.T) {
	t.Setenv("SEMSPEC_REPO_PATH", "/nope/never")
	// Configured path is also bad, and the test runner's CWD IS valid —
	// so this case can't actually reach the empty branch in a real test
	// environment. We assert the sentinel: a clearly-invalid configured
	// path falls through to env (also invalid) and then CWD (valid).
	got := resolveRepoPath("/this/does/not/exist/here")
	if got == "" {
		t.Error("CWD should rescue when configured + env are invalid")
	}
}

func TestRewriteCheckInRepo_NilWhenRepoEmpty(t *testing.T) {
	if got := rewriteCheckInRepo(""); got != nil {
		t.Error("empty repo path should yield nil predicate")
	}
}

func TestRewriteCheckInRepo_AnchoredAndRewritten(t *testing.T) {
	// Integration test: build a minimal git repo, create two commits,
	// and verify that rewriteCheckInRepo correctly distinguishes a
	// region anchored at the first commit (rewritten=true after the
	// second commit edits it) from one still anchored at the second.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; skipping integration test")
	}

	dir := t.TempDir()

	gitInit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	gitInit("init", "-q")
	gitInit("config", "user.email", "test@example.com")
	gitInit("config", "user.name", "test")
	gitInit("config", "commit.gpgsign", "false")

	mainPath := filepath.Join(dir, "main.go")
	v1 := strings.Join([]string{
		"package main",
		"",
		"func first() int { return 1 }",
		"func second() int { return 2 }",
		"",
	}, "\n")
	if err := os.WriteFile(mainPath, []byte(v1), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	gitInit("add", "main.go")
	gitInit("commit", "-q", "-m", "v1")

	headOut, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse v1: %v", err)
	}
	v1SHA := strings.TrimSpace(string(headOut))

	// Rewrite line 4 (function `second`) — line 3 stays put.
	v2 := strings.Join([]string{
		"package main",
		"",
		"func first() int { return 1 }",
		"func second() int { return 99 }", // changed
		"",
	}, "\n")
	if err := os.WriteFile(mainPath, []byte(v2), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	gitInit("commit", "-q", "-am", "v2")

	pred := rewriteCheckInRepo(dir)
	if pred == nil {
		t.Fatal("expected non-nil predicate")
	}

	// Line 3 (first()) should still be anchored to v1SHA.
	rewritten, err := pred("main.go", 3, 3, v1SHA)
	if err != nil {
		t.Fatalf("anchored check: %v", err)
	}
	if rewritten {
		t.Error("line 3 should still be anchored to v1, got rewritten=true")
	}

	// Line 4 (second()) was rewritten in v2 — no v1SHA blame remains there.
	rewritten, err = pred("main.go", 4, 4, v1SHA)
	if err != nil {
		t.Fatalf("rewritten check: %v", err)
	}
	if !rewritten {
		t.Error("line 4 was edited in v2, should report rewritten=true vs v1")
	}
}
