package structuralvalidator

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckStubArtifacts_NoJarsModified(t *testing.T) {
	result := CheckStubArtifacts(t.TempDir(), []string{"src/Foo.java", "build.gradle"})
	if !result.Passed {
		t.Errorf("no jars modified should pass: %s", result.Stdout)
	}
	if !result.Required {
		t.Error("stub-artifact-detector must be Required:true (fabrication is ship-stopper)")
	}
}

func TestCheckStubArtifacts_ManifestOnlyStubFails(t *testing.T) {
	dir := t.TempDir()
	// Take-19 fabrication shape: META-INF/MANIFEST.MF and nothing else.
	rel := writeJar(t, dir, "libs/local-maven-repo/com/acme/foo/1.0/foo-1.0.jar",
		[]jarEntry{
			{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
		})
	result := CheckStubArtifacts(dir, []string{rel})
	if result.Passed {
		t.Errorf("MANIFEST-only stub must fail: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, rel) {
		t.Errorf("violation should name the jar path: %s", result.Stdout)
	}
}

func TestCheckStubArtifacts_TinyJarUnderThresholdFails(t *testing.T) {
	dir := t.TempDir()
	// Two empty .class files — count > 0 but total size tiny. Catches
	// the fabrication shape where the agent adds skeleton class files
	// to pass the .class-count gate without writing real bytecode.
	rel := writeJar(t, dir, "libs/local-maven-repo/x/x/1.0/x-1.0.jar",
		[]jarEntry{
			{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
			{"x/A.class", ""},
			{"x/B.class", ""},
		})
	result := CheckStubArtifacts(dir, []string{rel})
	if result.Passed {
		t.Errorf("sub-threshold jar must fail: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "threshold") {
		t.Errorf("violation should reference the size threshold: %s", result.Stdout)
	}
}

func TestCheckStubArtifacts_RealJarPasses(t *testing.T) {
	dir := t.TempDir()
	// Fabricate a "realistic" small JAR: manifest + one .class with
	// enough bytes to exceed the 2 KiB threshold.
	classBytes := bytes.Repeat([]byte{0xCA, 0xFE, 0xBA, 0xBE}, 600) // 2400 bytes
	rel := writeJar(t, dir, "libs/real/lib.jar",
		[]jarEntry{
			{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
			{"com/example/Foo.class", string(classBytes)},
		})
	result := CheckStubArtifacts(dir, []string{rel})
	if !result.Passed {
		t.Errorf("realistic jar should pass: %s", result.Stdout)
	}
}

func TestCheckStubArtifacts_InvalidZipFails(t *testing.T) {
	dir := t.TempDir()
	// Write a non-zip file named .jar.
	rel := "libs/garbage.jar"
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("this is not a zip file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result := CheckStubArtifacts(dir, []string{rel})
	if result.Passed {
		t.Errorf("invalid .jar should fail: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "not a valid JAR") {
		t.Errorf("violation should explain invalid zip: %s", result.Stdout)
	}
}

func TestCheckStubArtifacts_MultipleJarsOneStub(t *testing.T) {
	dir := t.TempDir()
	classBytes := bytes.Repeat([]byte{0xCA, 0xFE, 0xBA, 0xBE}, 600)
	realRel := writeJar(t, dir, "libs/real.jar",
		[]jarEntry{
			{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"},
			{"x/Real.class", string(classBytes)},
		})
	stubRel := writeJar(t, dir, "libs/stub.jar",
		[]jarEntry{{"META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n"}})

	result := CheckStubArtifacts(dir, []string{realRel, stubRel})
	if result.Passed {
		t.Errorf("any stub in the set should fail the whole check: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, stubRel) {
		t.Errorf("violation must name the offending jar (%s): %s", stubRel, result.Stdout)
	}
	if strings.Contains(result.Stdout, realRel) {
		t.Errorf("real jar should not appear in violations: %s", result.Stdout)
	}
}

func TestCheckStubArtifacts_NonJarFilesIgnored(t *testing.T) {
	// Even when files are clearly suspect (.class file on its own,
	// 0-byte text file named "foo.txt"), the detector only inspects
	// .jar files — the rest is out of scope for this check.
	result := CheckStubArtifacts(t.TempDir(), []string{"loose.class", "empty.txt", "foo.java"})
	if !result.Passed {
		t.Errorf("non-jar files should be ignored: %s", result.Stdout)
	}
}

func TestFilterJarFiles_CaseInsensitive(t *testing.T) {
	got := filterJarFiles([]string{"a.jar", "b.JAR", "c.Jar", "d.txt", "e.jar.bak"})
	if len(got) != 3 {
		t.Errorf("expected 3 jars (case-insensitive), got %d: %v", len(got), got)
	}
}

func TestHasJarFiles(t *testing.T) {
	if !hasJarFiles([]string{"a.txt", "b.JAR"}) {
		t.Error("case-insensitive .jar detection failed")
	}
	if hasJarFiles([]string{"a.txt", "build.gradle"}) {
		t.Error("false positive on non-jar files")
	}
	if hasJarFiles(nil) {
		t.Error("nil slice should not match")
	}
}

// --- helpers ---

type jarEntry struct {
	name    string
	content string
}

// writeJar creates a JAR (zip) at the given relative path inside dir
// and returns the relative path the caller passes to the check. Each
// jarEntry becomes one zip entry with the given uncompressed content.
func writeJar(t *testing.T, dir, relPath string, entries []jarEntry) string {
	t.Helper()
	abs := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		t.Fatalf("create jar: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("zip create %q: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatalf("zip write %q: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return relPath
}
