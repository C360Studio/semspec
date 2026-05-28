package structuralvalidator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
)

func TestCheckHarnessProfileDiscipline_NoSelections(t *testing.T) {
	catalog := mustCatalog(t)
	result := CheckHarnessProfileDiscipline(t.TempDir(), []string{"src/Foo.java"}, nil, catalog)
	if !result.Passed {
		t.Errorf("greenfield project (no harness profiles) should pass, got: %s", result.Stdout)
	}
	if result.Name != "harness-profile-discipline" {
		t.Errorf("Name = %q, want harness-profile-discipline", result.Name)
	}
}

func TestCheckHarnessProfileDiscipline_NoTestFilesModified(t *testing.T) {
	catalog := mustCatalog(t)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}

	result := CheckHarnessProfileDiscipline(t.TempDir(), []string{"src/main/java/Driver.java", "build.gradle"}, selections, catalog)
	if !result.Passed {
		t.Errorf("scenarios producing no tests should pass (other scenarios cover integration): %s", result.Stdout)
	}
}

func TestCheckHarnessProfileDiscipline_RequiredEvidencePresent(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/MavlinkDriverTest.java", `
package com.example;

class MavlinkDriverTest {
    static final String PROFILE = "mavlink.px4-sitl.mavsdk-smoke";
    static final String IMAGE = "px4io/px4-sitl:latest";
    static final int PORT = 14540;
    void smoke() {
        assert true : "mavsdk_core_connected";
        assert true : "HEARTBEAT";
    }
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, mustCatalog(t))
	if !result.Passed {
		t.Errorf("test file with required profile evidence should pass: %s", result.Stdout)
	}
}

func TestCheckHarnessProfileDiscipline_RequiredEvidenceMissing(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/MavlinkDriverTest.java", `
package com.example;
class MavlinkDriverTest {
    static final String PROFILE = "mavlink.px4-sitl.mavsdk-smoke";
    // Missing image, port, and connection/assertion anchors.
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, mustCatalog(t))
	if result.Passed {
		t.Errorf("test file missing required evidence should fail: %s", result.Stdout)
	}
	if !result.Required {
		t.Error("missing required profile evidence should be a hard failure")
	}
	for _, want := range []string{"px4io/px4-sitl", "14540", "mavsdk_core_connected", "HEARTBEAT"} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("failure should name missing anchor %q: %s", want, result.Stdout)
		}
	}
}

func TestCheckHarnessProfileDiscipline_CompatibilityProfileDoesNotHardFail(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/CompatTest.java", `class CompatTest {}`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.ardupilot-sitl.compat", Purpose: "compatibility sweep"},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, mustCatalog(t))
	if !result.Passed {
		t.Errorf("compatibility-only profile should not hard-fail developer validation: %s", result.Stdout)
	}
}

func TestCheckHarnessProfileDiscipline_UnknownProfileHardFails(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/MavlinkDriverTest.java", `class MavlinkDriverTest {}`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.not-real", Purpose: "bad selection"},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, mustCatalog(t))
	if result.Passed {
		t.Errorf("unknown harness profile should fail: %s", result.Stdout)
	}
	if !result.Required {
		t.Error("unknown harness profile should be a hard failure")
	}
	if !strings.Contains(result.Stdout, "unknown harness profile") {
		t.Errorf("failure should name unknown profile: %s", result.Stdout)
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

func TestLoadHarnessProfiles_ReadsPlanFromDisk(t *testing.T) {
	dir := t.TempDir()
	plan := workflow.Plan{
		Slug: "test-slug",
		Architecture: &workflow.ArchitectureDocument{
			UpstreamResolutions: []workflow.UpstreamResolution{
				{Name: "PX4 SITL", Role: "integration_target"},
				{Name: "OSH Core", Role: "runtime_dep"},
			},
			HarnessProfiles: []workflow.HarnessProfileSelection{
				{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
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

	selections := loadHarnessProfiles(dir, "test-slug")
	if len(selections) != 1 {
		t.Fatalf("expected 1 harness profile, got %d", len(selections))
	}
	if selections[0].ProfileID != "mavlink.px4-sitl.mavsdk-smoke" {
		t.Errorf("got profile %q, want mavlink.px4-sitl.mavsdk-smoke", selections[0].ProfileID)
	}
}

func TestLoadHarnessProfiles_MissingPlanReturnsNil(t *testing.T) {
	dir := t.TempDir()
	selections := loadHarnessProfiles(dir, "nonexistent-slug")
	if selections != nil {
		t.Errorf("missing plan should return nil selections, got %v", selections)
	}
}

func TestLoadHarnessProfiles_NoArchitectureReturnsNil(t *testing.T) {
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
	selections := loadHarnessProfiles(dir, "test-slug")
	if selections != nil {
		t.Errorf("plan without architecture should return nil, got %v", selections)
	}
}

func mustCatalog(t *testing.T) *harnesscatalog.Catalog {
	t.Helper()
	catalog, err := harnesscatalog.LoadBuiltIn()
	if err != nil {
		t.Fatalf("LoadBuiltIn: %v", err)
	}
	return catalog
}

// writeTestFile writes a test file inside dir and returns the relative path
// the caller should pass in filesModified.
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
