package structuralvalidator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/harnesscatalog"
	"github.com/c360studio/semspec/workflow/payloads"
)

// CheckHarnessProfileDiscipline scans modified test files for evidence that
// selected required harness profiles are actually exercised AND, per ADR-041
// Move 5, that @integration scenarios' authoring obligations are met:
//
//  1. The catalog's required test assertions (formerly evidence anchors) for
//     each required profile appear somewhere in the modified test files —
//     the existing rule, unchanged in behavior.
//  2. For each @integration scenario with HarnessProfileIDs, at least one
//     modified test file contains each bound profile ID as a string literal.
//     Proves the test code is bound to a catalog profile by name so
//     qa-runner can route via the tag selector.
//  3. For each @integration scenario whose bound profile is services-class
//     or testcontainers-class, at least one modified test file contains an
//     environment-variable key from the catalog Profile's Env map. Proves
//     the test reads its endpoint from the harness-injected env instead of
//     hardcoding a host/port (pure-fixture profiles are exempt — no peer
//     process, no endpoint to inject).
//
// The check name (harness-profile-discipline) stays as the stable operator
// identifier; messaging and behavior expand. ADR-041 Move 5.
//
// No-op cases (pass without enforcement):
//   - No harness profiles selected (greenfield / runtime_dep-only).
//   - filesModified contains no test files (non-test scenarios;
//     integration coverage is verified by other scenarios that DO
//     produce tests).
//
// Unknown profile IDs, missing required-profile evidence, missing harness-
// binding strings, and missing env-var consumption are hard failures.
func CheckHarnessProfileDiscipline(workDir string, filesModified []string, selections []workflow.HarnessProfileSelection, scenarios []workflow.Scenario, catalog *harnesscatalog.Catalog) payloads.CheckResult {
	if len(selections) == 0 {
		return passHarnessResult("no test environment profiles selected — nothing to enforce")
	}
	required, err := catalog.RequiredProfiles(selections)
	if err != nil {
		return failHarnessResult(err.Error())
	}
	if len(required) == 0 {
		return passHarnessResult("no required test environment profiles selected — compatibility/heavy profiles are advisory")
	}

	testFiles := filterTestFiles(filesModified)
	if len(testFiles) == 0 {
		return passHarnessResult("no test files in this scenario — integration coverage deferred to other scenarios")
	}

	contents := loadTestContents(workDir, testFiles)

	var violations []string
	for _, r := range required {
		missing := missingEvidenceAnchors(contents, r.Profile.EvidenceAnchors)
		if len(missing) > 0 {
			violations = append(violations, formatHarnessViolation(r, missing))
		}
	}

	// ADR-041 Move 5: @integration scenario authoring checks.
	violations = append(violations, integrationBindingViolations(scenarios, contents, catalog)...)
	violations = append(violations, integrationEnvConsumptionViolations(scenarios, contents, catalog)...)

	if len(violations) == 0 {
		return payloads.CheckResult{
			Name:     "harness-profile-discipline",
			Passed:   true,
			Required: true,
			Command:  "harness-profile-discipline (internal)",
			Stdout:   fmt.Sprintf("required test environment profiles covered in modified tests: %d", len(required)),
		}
	}

	return payloads.CheckResult{
		Name:     "harness-profile-discipline",
		Passed:   false,
		Required: true,
		Command:  "harness-profile-discipline (internal)",
		Stdout:   strings.Join(violations, "\n"),
	}
}

// integrationBindingViolations returns one violation per @integration scenario
// whose bound HarnessProfileIDs aren't referenced as string literals anywhere
// in the modified test files. Proves the dev's tests carry the catalog
// cross-reference qa-runner needs to route tagged invocations.
//
// ADR-041 Move 5 check (1).
func integrationBindingViolations(scenarios []workflow.Scenario, contents []string, _ *harnesscatalog.Catalog) []string {
	var out []string
	for _, s := range scenarios {
		if !scenarioHasTag(s, workflow.TierIntegration) {
			continue
		}
		var missing []string
		for _, id := range s.HarnessProfileIDs {
			if id == "" {
				continue
			}
			if !containsAny(contents, id) {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			out = append(out, fmt.Sprintf(
				"@integration scenario %q binds harness profile id(s) %s but no modified test file contains the id as a string literal — qa-runner's tag selector cannot route this scenario",
				s.ID, strings.Join(quoteStrings(missing), ", ")))
		}
	}
	return out
}

// integrationEnvConsumptionViolations returns one violation per @integration
// scenario whose bound services/testcontainers profile declares Env keys
// none of which appear in the modified test files. Catches hardcoded
// host/port values that would break qa-runner's harness injection. Pure-
// fixture profiles are exempt — no peer process means no endpoint to
// inject.
//
// Heuristic: at least one of the catalog Profile.Env keys must appear as a
// substring in some modified test file. False negatives (test reads env via
// a wrapper utility that doesn't mention the key) are acceptable —
// operators can override; the rule's purpose is to catch obvious hardcoded
// endpoints. ADR-041 Move 5 check (2).
func integrationEnvConsumptionViolations(scenarios []workflow.Scenario, contents []string, catalog *harnesscatalog.Catalog) []string {
	if catalog == nil {
		return nil
	}
	var out []string
	for _, s := range scenarios {
		if !scenarioHasTag(s, workflow.TierIntegration) {
			continue
		}
		for _, id := range s.HarnessProfileIDs {
			profile, ok := catalog.Profiles[id]
			if !ok {
				continue
			}
			orch := profile.EffectiveOrchestration()
			if orch != harnesscatalog.OrchestrationServices && orch != harnesscatalog.OrchestrationTestcontainers {
				continue
			}
			if len(profile.Env) == 0 {
				continue
			}
			envKeys := make([]string, 0, len(profile.Env))
			for k := range profile.Env {
				envKeys = append(envKeys, k)
			}
			if anyEnvKeyReferenced(contents, envKeys) {
				continue
			}
			out = append(out, fmt.Sprintf(
				"@integration scenario %q binds harness profile %q (orchestration=%s) but no modified test file references any of the profile's declared env keys (%s) — the test appears to be reading hardcoded endpoints instead of consuming the harness-injected env",
				s.ID, id, orch, strings.Join(quoteStrings(envKeys), ", ")))
		}
	}
	return out
}

// scenarioHasTag reports whether the scenario carries the given tag.
func scenarioHasTag(s workflow.Scenario, tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// anyEnvKeyReferenced reports whether any of envKeys appears as a substring
// in any of the test file contents.
func anyEnvKeyReferenced(contents []string, envKeys []string) bool {
	for _, k := range envKeys {
		if k == "" {
			continue
		}
		if containsAny(contents, k) {
			return true
		}
	}
	return false
}

// loadHarnessProfiles reads the plan.json for the given slug and returns the
// architect-selected harness profiles. Returns nil when the plan or architecture
// is absent.
func loadHarnessProfiles(repoPath, slug string) []workflow.HarnessProfileSelection {
	plan := loadPlanForDiscipline(repoPath, slug)
	if plan == nil || plan.Architecture == nil {
		return nil
	}
	return plan.Architecture.HarnessProfiles
}

// loadScenarios reads the plan.json for the given slug and returns the
// scenarios. Returns nil when plan.json is absent or unparseable. The
// scenarios are consumed by CheckHarnessProfileDiscipline's ADR-041 Move 5
// checks (@integration binding + env consumption).
func loadScenarios(repoPath, slug string) []workflow.Scenario {
	plan := loadPlanForDiscipline(repoPath, slug)
	if plan == nil {
		return nil
	}
	return plan.Scenarios
}

// loadPlanForDiscipline is the shared plan.json reader for loadHarnessProfiles
// + loadScenarios. Returns nil on I/O or parse failure — callers treat that
// as "no data, skip enforcement" rather than blocking.
func loadPlanForDiscipline(repoPath, slug string) *workflow.Plan {
	if repoPath == "" || slug == "" {
		return nil
	}
	planPath := filepath.Join(repoPath, ".semspec", "plans", slug, "plan.json")
	data, err := os.ReadFile(planPath)
	if err != nil {
		return nil
	}
	var plan workflow.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil
	}
	return &plan
}

// filterTestFiles returns the subset of files whose names match common
// test-file conventions across Go, Java, Python, Node/TS, .NET, Rust.
// More permissive than executor.go:hasTestFiles which is Go-only.
func filterTestFiles(files []string) []string {
	var out []string
	for _, f := range files {
		if isTestFileMultilang(f) {
			out = append(out, f)
		}
	}
	return out
}

// isTestFileMultilang returns true for test-file naming conventions
// across the languages semspec supports. Conservative — false negatives
// (skipping a file that's actually a test) merely reduce coverage of
// this check; false positives (treating non-test files as tests) would
// pollute the test-content grep with non-test code.
func isTestFileMultilang(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return true
	case strings.HasSuffix(base, "test.java"), strings.HasSuffix(base, "tests.java"),
		strings.HasSuffix(base, "spec.java"):
		// Maven/Gradle conventions: FooTest.java, FooTests.java, FooSpec.java.
		return true
	case strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		// pytest conventions.
		return true
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".spec.ts"),
		strings.HasSuffix(base, ".test.tsx"), strings.HasSuffix(base, ".spec.tsx"),
		strings.HasSuffix(base, ".test.js"), strings.HasSuffix(base, ".spec.js"):
		return true
	case strings.HasSuffix(base, ".test.cs"), strings.HasSuffix(base, "tests.cs"):
		// xUnit/NUnit conventions.
		return true
	case strings.HasSuffix(base, ".rs") && strings.Contains(base, "test"):
		// Rust tests live in src/ alongside production code; this is a coarse
		// heuristic — covers tests/foo_test.rs and integration test files.
		return true
	}
	return false
}

// loadTestContents reads each test file relative to workDir and
// returns the contents. I/O errors are silently skipped so a transient
// read failure doesn't masquerade as a discipline violation.
func loadTestContents(workDir string, testFiles []string) []string {
	var contents []string
	for _, f := range testFiles {
		absPath := f
		if !filepath.IsAbs(f) {
			absPath = filepath.Join(workDir, f)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		contents = append(contents, string(data))
	}
	return contents
}

func containsAny(contents []string, needle string) bool {
	if needle == "" {
		return false
	}
	for _, c := range contents {
		if strings.Contains(c, needle) {
			return true
		}
	}
	return false
}

func missingEvidenceAnchors(contents []string, anchors []string) []string {
	var missing []string
	for _, anchor := range anchors {
		if !containsAny(contents, anchor) {
			missing = append(missing, anchor)
		}
	}
	return missing
}

func formatHarnessViolation(r harnesscatalog.ResolvedSelection, missing []string) string {
	return fmt.Sprintf("required test environment profile %q is selected but no modified test file contains required test assertions: %s",
		r.Profile.ID, strings.Join(quoteStrings(missing), ", "))
}

func quoteStrings(vals []string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, fmt.Sprintf("%q", v))
	}
	return out
}

func passHarnessResult(stdout string) payloads.CheckResult {
	return payloads.CheckResult{
		Name:     "harness-profile-discipline",
		Passed:   true,
		Required: true,
		Command:  "harness-profile-discipline (internal)",
		Stdout:   stdout,
	}
}

func failHarnessResult(stdout string) payloads.CheckResult {
	return payloads.CheckResult{
		Name:     "harness-profile-discipline",
		Passed:   false,
		Required: true,
		Command:  "harness-profile-discipline (internal)",
		Stdout:   stdout,
	}
}
