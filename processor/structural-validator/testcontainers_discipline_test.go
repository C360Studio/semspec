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
	result := CheckHarnessProfileDiscipline(t.TempDir(), []string{"src/Foo.java"}, nil, nil, catalog)
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

	result := CheckHarnessProfileDiscipline(t.TempDir(), []string{"src/main/java/Driver.java", "build.gradle"}, selections, nil, catalog)
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

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, nil, mustCatalog(t))
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

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, nil, mustCatalog(t))
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

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, nil, mustCatalog(t))
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

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, nil, mustCatalog(t))
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

// TestCheckHarnessProfileDiscipline_IntegrationBindingPresent pins ADR-041
// Move 5 check (1): an @integration scenario binding a profile ID must
// surface that ID as a string literal in at least one modified test file.
// Present → pass.
func TestCheckHarnessProfileDiscipline_IntegrationBindingPresent(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/MavlinkDriverTest.java", `
package com.example;

class MavlinkDriverTest {
    static final String PROFILE = "mavlink.px4-sitl.mavsdk-smoke";
    static final String IMAGE = "px4io/px4-sitl:latest";
    static final int PORT = 14540;
    void smoke() {
        // PX4_SIM_MODEL is the env key the catalog declares for this profile.
        String model = System.getenv("PX4_SIM_MODEL");
        assert true : "mavsdk_core_connected";
        assert true : "HEARTBEAT";
    }
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}
	scenarios := []workflow.Scenario{
		{
			ID:                "scn.1",
			RequirementID:     "r1",
			Given:             "the SITL is configured via $PX4_SIM_MODEL",
			When:              "the driver starts",
			Then:              []string{"a HEARTBEAT is received"},
			Tags:              []string{workflow.TierIntegration},
			HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
		},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, scenarios, mustCatalog(t))
	if !result.Passed {
		t.Errorf("test file with profile-id binding + env consumption should pass: %s", result.Stdout)
	}
}

// TestCheckHarnessProfileDiscipline_IntegrationBindingMissing pins that a
// missing harness-binding string literal surfaces a violation naming the
// scenario + the missing profile_id, so the regen LLM has a targeted
// directive to add the literal. ADR-041 Move 5 check (1).
func TestCheckHarnessProfileDiscipline_IntegrationBindingMissing(t *testing.T) {
	dir := t.TempDir()
	// Test file contains all the catalog evidence anchors EXCEPT the
	// profile_id string itself — isolates the binding check from the
	// pre-existing required-assertions check.
	testFile := writeTestFile(t, dir, "src/test/java/MavlinkDriverTest.java", `
package com.example;

class MavlinkDriverTest {
    static final String IMAGE = "px4io/px4-sitl:latest";
    static final int PORT = 14540;
    void smoke() {
        String endpoint = System.getenv("SITL_ENDPOINT");
        assert true : "mavsdk_core_connected";
        assert true : "HEARTBEAT";
    }
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}
	scenarios := []workflow.Scenario{
		{
			ID:                "scn.1",
			Tags:              []string{workflow.TierIntegration},
			HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
		},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, scenarios, mustCatalog(t))
	if result.Passed {
		t.Fatalf("missing harness-binding string literal should fail; got passed with stdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "scn.1") {
		t.Errorf("violation should name the scenario: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "mavlink.px4-sitl.mavsdk-smoke") {
		t.Errorf("violation should name the unbound profile id: %s", result.Stdout)
	}
}

// TestCheckHarnessProfileDiscipline_EnvConsumptionMissing pins ADR-041
// Move 5 check (2): a test file referencing the profile_id but NOT any of
// the catalog's declared env keys is treated as hardcoding the endpoint.
// The mavlink.px4-sitl.mavsdk-smoke catalog entry declares SITL_ENDPOINT in
// its Env map; a test file that contains the profile_id but not
// SITL_ENDPOINT should fail.
func TestCheckHarnessProfileDiscipline_EnvConsumptionMissing(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/MavlinkDriverTest.java", `
package com.example;

class MavlinkDriverTest {
    static final String PROFILE = "mavlink.px4-sitl.mavsdk-smoke";
    static final String IMAGE = "px4io/px4-sitl:latest";
    static final int PORT = 14540;
    void smoke() {
        // Hardcoded endpoint — no env var reference.
        String endpoint = "udp://127.0.0.1:14540";
        assert true : "mavsdk_core_connected";
        assert true : "HEARTBEAT";
    }
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}
	scenarios := []workflow.Scenario{
		{
			ID:                "scn.1",
			Tags:              []string{workflow.TierIntegration},
			HarnessProfileIDs: []string{"mavlink.px4-sitl.mavsdk-smoke"},
		},
	}

	catalog := mustCatalog(t)
	// Only run the env-consumption assertion if the catalog actually
	// declares env keys for this profile. (Catalog evolution should not
	// break this test silently.)
	profile, ok := catalog.Profiles["mavlink.px4-sitl.mavsdk-smoke"]
	if !ok || len(profile.Env) == 0 {
		t.Skipf("mavlink.px4-sitl.mavsdk-smoke profile has no Env keys declared in the built-in catalog; check the env-consumption rule with a catalog override")
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, scenarios, catalog)
	if result.Passed {
		t.Fatalf("hardcoded endpoint (no env key reference) should fail: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "scn.1") {
		t.Errorf("violation should name the scenario: %s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "env") {
		t.Errorf("violation should reference 'env': %s", result.Stdout)
	}
}

// TestCheckHarnessProfileDiscipline_PureFixtureSkipsEnvCheck pins that
// pure-fixture profiles don't trigger the env-consumption check — there's
// no peer process and no endpoint to inject. Binding check still applies
// (the test file should still reference the profile_id as a literal).
func TestCheckHarnessProfileDiscipline_PureFixtureSkipsEnvCheck(t *testing.T) {
	// We need a synthetic catalog with a pure-fixture profile because the
	// built-in catalog may not have a profile shaped this way at this layer.
	pureFixtureCatalog := &harnesscatalog.Catalog{
		Profiles: map[string]harnesscatalog.Profile{
			"test.pure-fixture": {
				ID:                 "test.pure-fixture",
				Tier:               harnesscatalog.TierRequired,
				Orchestration:      harnesscatalog.OrchestrationPureFixture,
				EvidenceAnchors:    []string{"FIXTURE_LOADED"},
				RequiredAssertions: []string{"FIXTURE_LOADED"},
				Env:                map[string]string{"SHOULD_BE_IGNORED": "x"},
			},
		},
	}

	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/FixtureTest.java", `
package com.example;
class FixtureTest {
    static final String PROFILE = "test.pure-fixture";
    void smoke() {
        // No SHOULD_BE_IGNORED reference — fine for pure-fixture.
        assert true : "FIXTURE_LOADED";
    }
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "test.pure-fixture", Purpose: "in-process fixture"},
	}
	scenarios := []workflow.Scenario{
		{
			ID:                "scn.1",
			Tags:              []string{workflow.TierIntegration},
			HarnessProfileIDs: []string{"test.pure-fixture"},
		},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, scenarios, pureFixtureCatalog)
	if !result.Passed {
		t.Errorf("pure-fixture profile should not trigger env-consumption check: %s", result.Stdout)
	}
}

// TestCheckHarnessProfileDiscipline_UnitScenariosIgnored pins that @unit
// scenarios DO NOT trigger the binding or env-consumption checks — those
// fire only for @integration. Without this guard, every @unit scenario
// would falsely demand harness-binding string literals.
func TestCheckHarnessProfileDiscipline_UnitScenariosIgnored(t *testing.T) {
	dir := t.TempDir()
	testFile := writeTestFile(t, dir, "src/test/java/UnitTest.java", `
package com.example;
class UnitTest {
    // No profile_id, no env var — but this is a @unit scenario, so the new
    // checks must NOT fire. Existing evidence-anchor check IS still in effect
    // for the selected profile; we satisfy it by adding the anchor strings.
    static final String PROFILE_ANCHOR = "mavlink.px4-sitl.mavsdk-smoke";
    static final String IMAGE = "px4io/px4-sitl:latest";
    static final int PORT = 14540;
    void test() {
        assert true : "mavsdk_core_connected";
        assert true : "HEARTBEAT";
    }
}
`)
	selections := []workflow.HarnessProfileSelection{
		{ProfileID: "mavlink.px4-sitl.mavsdk-smoke", Purpose: "prove SITL"},
	}
	scenarios := []workflow.Scenario{
		{
			ID:   "scn.unit",
			Tags: []string{workflow.TierUnit},
		},
	}

	result := CheckHarnessProfileDiscipline(dir, []string{testFile}, selections, scenarios, mustCatalog(t))
	if !result.Passed {
		t.Errorf("@unit scenarios should not trigger Move 5 checks: %s", result.Stdout)
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
