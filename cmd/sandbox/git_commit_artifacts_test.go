package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveTransientBuildArtifacts(t *testing.T) {
	root := t.TempDir()

	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	argRoot := mk("javac.20260616_030142.args")  // remove
	argNested := mk("build/tmp/compileJava/x.args") // remove
	keepJava := mk("src/main/java/org/x/Main.java") // keep
	keepGradle := mk("build.gradle")                // keep
	keepGitArg := mk(".git/objects/pack/keep.args") // keep — .git is skipped

	removeTransientBuildArtifacts(root, nil)

	for _, gone := range []string{argRoot, argNested} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("expected %q removed, still present", gone)
		}
	}
	for _, kept := range []string{keepJava, keepGradle, keepGitArg} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("expected %q kept, got: %v", kept, err)
		}
	}
}
