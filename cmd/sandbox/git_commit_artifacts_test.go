package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestIsCompilerArgFile(t *testing.T) {
	cases := map[string]bool{
		"javac.20260616_030142.args":      true,
		"jar.123.args":                    true,
		"javadoc.x.args":                  true,
		"build/tmp/compileJava/opts.args": true,
		"sub/build/x.args":                true,
		"src/test/resources/cli.args":     false, // a real deliverable
		"cli.args":                        false, // untracked but not a tool argfile
		"src/main/java/org/x/Foo.java":    false,
	}
	for p, want := range cases {
		if got := isCompilerArgFile(p); got != want {
			t.Errorf("isCompilerArgFile(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestRemoveTransientBuildArtifacts(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "t")

	mk := func(rel, content string) string {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// A TRACKED deliverable that happens to end in .args — must survive (the
	// over-broad-deletion bug this test guards against).
	tracked := mk("src/test/resources/cli.args", "tracked fixture")
	mk("src/main/java/org/x/Main.java", "package x;")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "init")

	// Untracked files created after the commit.
	argRoot := mk("javac.20260616_030142.args", "x")       // remove (javac. tool argfile)
	argBuild := mk("build/tmp/compileJava/opts.args", "x") // remove (under build/)
	devArgs := mk("cli2.args", "x")                        // keep (untracked, NOT a tool argfile)
	devJava := mk("FindClass.java", "x")                   // keep (not .args; gate's job)

	removeTransientBuildArtifacts(context.Background(), dir, nil)

	for _, gone := range []string{argRoot, argBuild} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("expected %q removed, still present", gone)
		}
	}
	for _, kept := range []string{tracked, devArgs, devJava} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("expected %q kept, got: %v", kept, err)
		}
	}
}
