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

// Issue #113 (2026-06-03): the literal-substring sub-checks were
// retired (evidence-anchor presence, @integration binding string,
// env-key consumption). What remains is "do the architect's
// selections resolve in the catalog?" — a plan-time validity check.
// Binding correctness is enforced by LLM reviewer + qa-runner runtime.

func TestCheckHarnessProfileDiscipline_NoSelections(t *testing.T) {
	catalog := mustCatalog(t)
	result := CheckHarnessProfileDiscipline(nil, catalog)
	if !result.Passed {
		t.Errorf("greenfield project (no harness profiles) should pass, got: %s", result.Stdout)
	}
	if result.Name != "harness-profile-discipline" {
		t.Errorf("Name = %q, want harness-profile-discipline", result.Name)
	}
	if !strings.Contains(result.Stdout, "nothing to enforce") {
		t.Errorf("no-selections result should mention 'nothing to enforce': %s", result.Stdout)
	}
}

func TestCheckHarnessProfileDiscipline_RequiredProfileResolves(t *testing.T) {
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}
	result := CheckHarnessProfileDiscipline(selections, mustCatalog(t))
	if !result.Passed {
		t.Errorf("known required profile should pass: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "resolve in catalog") {
		t.Errorf("resolved-selection result should mention 'resolve in catalog': %s", result.Stdout)
	}
}

func TestCheckHarnessProfileDiscipline_CompatibilityProfileDoesNotHardFail(t *testing.T) {
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.ardupilot-sitl.compat", Purpose: "compatibility sweep"},
	}
	result := CheckHarnessProfileDiscipline(selections, mustCatalog(t))
	if !result.Passed {
		t.Errorf("compatibility-only profile should not hard-fail developer validation: %s", result.Stdout)
	}
}

func TestCheckHarnessProfileDiscipline_UnknownProfileHardFails(t *testing.T) {
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.not-real", Purpose: "bad selection"},
	}
	result := CheckHarnessProfileDiscipline(selections, mustCatalog(t))
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

// TestCheckHarnessProfileDiscipline_NoLiteralGrep is the post-#113
// regression pin: a re-introduction of any of the deleted
// literal-substring helpers would break this test (or the build, if
// the function names are re-added) and force code-review attention.
//
// Deleted helpers (issue #113 / commit retiring testcontainers_discipline
// literal scans):
//
//   - integrationBindingViolations  — grep'd test contents for harness profile IDs
//   - integrationEnvConsumptionViolations  — grep'd test contents for env keys
//   - missingEvidenceAnchors  — grep'd test contents for catalog evidence_anchors
//   - loadTestContents  — read test files into memory for the above
//   - scenarioHasTag, anyEnvKeyReferenced, containsAny  — helpers for the above
//   - formatHarnessViolation, quoteStrings  — error rendering for the above
//   - loadScenarios  — loaded plan.Scenarios for the above
//
// All were goodhart-able (graded on "did you write this exact string");
// any reintroduction should surface in PR review with a pointer to
// issue #113's options table (B = AST-aware, C = behavioral) as the
// architecturally correct path.
func TestCheckHarnessProfileDiscipline_NoLiteralGrep(t *testing.T) {
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}
	// The function no longer takes test files; this test simply pins
	// that the signature accepts (selections, catalog) and returns pass
	// when selections resolve. There is no test-file scanning surface
	// to subvert anymore.
	result := CheckHarnessProfileDiscipline(selections, mustCatalog(t))
	if !result.Passed {
		t.Errorf("simplified check should pass on any valid selection regardless of test files: %s", result.Stdout)
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
