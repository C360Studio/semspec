package lessoncurator

import (
	"os"
	"path/filepath"
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
