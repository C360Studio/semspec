package structuralvalidator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
)

func TestCheckTestcontainersDiscipline_NoIntegrationTargets(t *testing.T) {
	result := CheckTestcontainersDiscipline(t.TempDir(), []string{"src/Foo.java"}, nil)
	if !result.Passed {
		t.Errorf("greenfield project (no integration_targets) should pass, got: %s", result.Stdout)
	}
	if result.Name != "testcontainers-discipline" {
		t.Errorf("Name = %q, want testcontainers-discipline", result.Name)
	}
}

func TestCheckTestcontainersDiscipline_NoTestFilesModified(t *testing.T) {
	targets := []workflow.UpstreamResolution{
		integrationTargetFixture("Postgres", "postgres:16-alpine", "testcontainers-java"),
	}
	// filesModified has only production code — no test files.
	result := CheckTestcontainersDiscipline(t.TempDir(), []string{"src/main/java/Repo.java", "build.gradle"}, targets)
	if !result.Passed {
		t.Errorf("scenarios producing no tests should pass (other scenarios cover integration): %s", result.Stdout)
	}
}

func TestCheckTestcontainersDiscipline_BindingAndImagePresent(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/RepoTest.java", `
package com.example;
import org.testcontainers.containers.PostgreSQLContainer;
import org.junit.jupiter.api.Test;

public class RepoTest {
    @Test void connects() {
        var pg = new PostgreSQLContainer<>("postgres:16-alpine");
        pg.start();
    }
}
`)
	targets := []workflow.UpstreamResolution{
		integrationTargetFixture("Postgres", "postgres:16-alpine", "testcontainers-java"),
	}
	result := CheckTestcontainersDiscipline(dir, []string{testFile}, targets)
	if !result.Passed {
		t.Errorf("test file with binding + image should pass: %s", result.Stdout)
	}
}

func TestCheckTestcontainersDiscipline_BindingButNoImage(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/RepoTest.java", `
package com.example;
import org.testcontainers.containers.PostgreSQLContainer;

public class RepoTest {
    void connects() {
        // Wrong image — fabricated stub instead of architect-declared coordinate.
        var pg = new PostgreSQLContainer<>("acme/fake-postgres:1.0");
    }
}
`)
	targets := []workflow.UpstreamResolution{
		integrationTargetFixture("Postgres", "postgres:16-alpine", "testcontainers-java"),
	}
	result := CheckTestcontainersDiscipline(dir, []string{testFile}, targets)
	if result.Passed {
		t.Errorf("test file with wrong image should fail (catches stub-substitution): %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "postgres:16-alpine") {
		t.Errorf("failure message should name the missing image coordinate: %s", result.Stdout)
	}
}

func TestCheckTestcontainersDiscipline_ImageButNoBinding(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/RepoTest.java", `
package com.example;
public class RepoTest {
    // Image string present but no Testcontainers import — agent likely
    // hard-coded the image into a config file without actually using
    // Testcontainers to spawn it.
    static final String IMAGE = "postgres:16-alpine";
}
`)
	targets := []workflow.UpstreamResolution{
		integrationTargetFixture("Postgres", "postgres:16-alpine", "testcontainers-java"),
	}
	result := CheckTestcontainersDiscipline(dir, []string{testFile}, targets)
	if result.Passed {
		t.Errorf("test file with image but no Testcontainers binding should fail: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "org.testcontainers") {
		t.Errorf("failure message should name the missing binding: %s", result.Stdout)
	}
}

func TestCheckTestcontainersDiscipline_NilTestHarnessSkipped(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/RepoTest.java", `// no tests yet`)
	// integration_target declared but TestHarness nil — criterion 7b
	// catches that at the reviewer; this check shouldn't double-flag.
	targets := []workflow.UpstreamResolution{
		{Name: "Postgres", Coordinate: "postgres:16-alpine", Role: "integration_target", TestHarness: nil},
	}
	result := CheckTestcontainersDiscipline(dir, []string{testFile}, targets)
	if !result.Passed {
		t.Errorf("nil TestHarness should be skipped (reviewer's responsibility), got fail: %s", result.Stdout)
	}
}

func TestCheckTestcontainersDiscipline_MultipleTargetsOneMissing(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/RepoTest.java", `
import org.testcontainers.containers.PostgreSQLContainer;
class RepoTest {
    void t() { new PostgreSQLContainer<>("postgres:16-alpine").start(); }
}
`)
	targets := []workflow.UpstreamResolution{
		integrationTargetFixture("Postgres", "postgres:16-alpine", "testcontainers-java"),
		integrationTargetFixture("Kafka", "confluentinc/cp-kafka:7.5.0", "testcontainers-java"),
	}
	result := CheckTestcontainersDiscipline(dir, []string{testFile}, targets)
	if result.Passed {
		t.Errorf("Kafka missing from test should fail: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Kafka") {
		t.Errorf("failure should name the uncovered target: %s", result.Stdout)
	}
}

func TestCheckTestcontainersDiscipline_GoTestcontainers(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "internal/repo/repo_test.go", `
package repo

import (
	"testing"
	"github.com/testcontainers/testcontainers-go"
)

func TestConnects(t *testing.T) {
	req := testcontainers.ContainerRequest{Image: "postgres:16-alpine"}
	_ = req
}
`)
	targets := []workflow.UpstreamResolution{
		integrationTargetFixture("Postgres", "postgres:16-alpine", "testcontainers-go"),
	}
	result := CheckTestcontainersDiscipline(dir, []string{testFile}, targets)
	if !result.Passed {
		t.Errorf("Go test with testcontainers-go binding + image should pass: %s", result.Stdout)
	}
}

func TestIsTestFileMultilang(t *testing.T) {
	cases := []struct {
		path     string
		wantTest bool
	}{
		{"src/foo.go", false},
		{"src/foo_test.go", true},
		{"src/main/java/Foo.java", false},
		{"src/test/java/FooTest.java", true},
		{"src/test/java/FooTests.java", true},
		{"src/test/java/FooSpec.java", true},
		{"foo.py", false},
		{"test_foo.py", true},
		{"foo_test.py", true},
		{"foo.ts", false},
		{"foo.test.ts", true},
		{"foo.spec.ts", true},
		{"foo.test.tsx", true},
		{"FooTests.cs", true},
		{"tests/integration_test.rs", true},
	}
	for _, c := range cases {
		got := isTestFileMultilang(c.path)
		if got != c.wantTest {
			t.Errorf("isTestFileMultilang(%q) = %v, want %v", c.path, got, c.wantTest)
		}
	}
}

func TestLibraryToImportNeedle(t *testing.T) {
	cases := map[string]string{
		"testcontainers-java":   "org.testcontainers",
		"testcontainers-go":     "testcontainers-go",
		"testcontainers-python": "testcontainers",
		"testcontainers-node":   "testcontainers",
		"testcontainers-dotnet": "Testcontainers",
		"testcontainers-rust":   "testcontainers",
		"unknown-library":       "unknown-library",
	}
	for lib, want := range cases {
		got := libraryToImportNeedle(lib)
		if got != want {
			t.Errorf("libraryToImportNeedle(%q) = %q, want %q", lib, got, want)
		}
	}
}

func TestLoadIntegrationTargets_ReadsPlanFromDisk(t *testing.T) {
	dir := t.TempDir()
	plan := workflow.Plan{
		Slug: "test-slug",
		Architecture: &workflow.ArchitectureDocument{
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "Postgres", Role: "integration_target", TestHarness: &workflow.TestHarness{
					Library: "testcontainers-java", Image: "postgres:16-alpine", AccessMethod: "tcp:5432",
				}},
				{Name: "OSH Core", Role: "runtime_dep"},
				{Name: "Annotation processor", Role: "build_dep"},
			},
		},
	}
	planDir := filepath.Join(dir, ".semspec", "plans", "test-slug")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("mkdir plans dir: %v", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "plan.json"), data, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	targets := loadIntegrationTargets(dir, "test-slug")
	if len(targets) != 1 {
		t.Fatalf("expected 1 integration_target, got %d (must filter runtime_dep + build_dep)", len(targets))
	}
	if targets[0].Name != "Postgres" {
		t.Errorf("got target %q, want Postgres", targets[0].Name)
	}
}

func TestLoadIntegrationTargets_MissingPlanReturnsNil(t *testing.T) {
	dir := t.TempDir()
	targets := loadIntegrationTargets(dir, "nonexistent-slug")
	if targets != nil {
		t.Errorf("missing plan should return nil targets, got %v", targets)
	}
}

func TestLoadIntegrationTargets_NoArchitectureReturnsNil(t *testing.T) {
	dir := t.TempDir()
	plan := workflow.Plan{Slug: "test-slug"} // no Architecture
	planDir := filepath.Join(dir, ".semspec", "plans", "test-slug")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(filepath.Join(planDir, "plan.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	targets := loadIntegrationTargets(dir, "test-slug")
	if targets != nil {
		t.Errorf("plan without architecture should return nil, got %v", targets)
	}
}

// integrationTargetFixture builds a populated integration_target for tests.
func integrationTargetFixture(name, image, library string) workflow.UpstreamResolution {
	return workflow.UpstreamResolution{
		Name:       name,
		Coordinate: image,
		SourceRef:  "https://hub.docker.com/_/" + name,
		Role:       "integration_target",
		TestHarness: &workflow.TestHarness{
			Library:      library,
			Image:        image,
			AccessMethod: "tcp:5432",
		},
	}
}

// writeTestFile writes a test file inside dir and returns the relative
// path the caller should pass in filesModified.
func writeTestFile(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	abs := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
	return relPath
}
