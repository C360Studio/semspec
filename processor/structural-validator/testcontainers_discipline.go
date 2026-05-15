package structuralvalidator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/payloads"
)

// CheckTestcontainersDiscipline scans modified test files for evidence
// that the architect's declared integration_targets are exercised via
// Testcontainers. For each upstream_resolutions entry with
// role=="integration_target", at least one modified test file in this
// validation run must reference BOTH:
//   - The Testcontainers binding for the project's language (e.g.,
//     "org.testcontainers" for testcontainers-java).
//   - The architect's declared image coordinate as a substring (so a
//     fabricated stub image cannot satisfy the check — the agent must
//     use the real coordinate).
//
// Closes the take-19 / take-29 stub-JAR fabrication shape on the dev
// side: the architect declared an integration_target (criterion 7a),
// the reviewer enforced TestHarness completeness (criterion 7b), and
// now the dev must actually exercise that target via Testcontainers
// or the scenario fails structural validation.
//
// No-op cases (pass without enforcement):
//   - No integration_targets declared (greenfield / runtime_dep-only).
//   - filesModified contains no test files (non-test scenarios;
//     integration coverage is verified by other scenarios that DO
//     produce tests).
//
// Advisory (Required: false) initially so real-world hit rates can be
// measured before promoting to hard reject. Pair with criterion 7b
// in plan-reviewer.
func CheckTestcontainersDiscipline(workDir string, filesModified []string, integrationTargets []workflow.UpstreamResolution) payloads.CheckResult {
	if len(integrationTargets) == 0 {
		return passResult("no integration_targets declared — nothing to enforce")
	}

	testFiles := filterTestFiles(filesModified)
	if len(testFiles) == 0 {
		return passResult("no test files in this scenario — integration coverage deferred to other scenarios")
	}

	contents := loadTestContents(workDir, testFiles)

	var violations []string
	for _, t := range integrationTargets {
		if t.TestHarness == nil {
			// Reviewer criterion 7b catches this — defensive skip here so we
			// don't double-report the same defect.
			continue
		}
		bindingFound := containsAny(contents, libraryToImportNeedle(t.TestHarness.Library))
		imageFound := containsAny(contents, t.TestHarness.Image)

		if !bindingFound || !imageFound {
			violations = append(violations, formatTcViolation(t, bindingFound, imageFound))
		}
	}

	if len(violations) == 0 {
		return payloads.CheckResult{
			Name:     "testcontainers-discipline",
			Passed:   true,
			Required: false,
			Command:  "testcontainers-discipline (internal)",
			Stdout:   fmt.Sprintf("integration_targets covered in modified tests: %d", len(integrationTargets)),
		}
	}

	return payloads.CheckResult{
		Name:     "testcontainers-discipline",
		Passed:   false,
		Required: false,
		Command:  "testcontainers-discipline (internal)",
		Stdout:   strings.Join(violations, "\n"),
	}
}

// loadIntegrationTargets reads the plan.json for the given slug and
// returns the architect's role=="integration_target" resolutions.
// Returns nil when the plan, architecture, or resolutions are absent
// (greenfield project, pre-architecture phase, runtime_dep-only).
func loadIntegrationTargets(repoPath, slug string) []workflow.UpstreamResolution {
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
	if plan.Architecture == nil {
		return nil
	}
	var targets []workflow.UpstreamResolution
	for _, r := range plan.Architecture.UpstreamResolutions {
		if r.Role == "integration_target" {
			targets = append(targets, r)
		}
	}
	return targets
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

// libraryToImportNeedle maps a TestHarness.Library value to the
// substring an import statement would contain for that binding.
// Conservative: prefers the unique fragment that identifies the
// binding without over-constraining the syntactic shape.
func libraryToImportNeedle(lib string) string {
	switch lib {
	case "testcontainers-java":
		// import org.testcontainers.{containers,...}
		return "org.testcontainers"
	case "testcontainers-go":
		// "github.com/testcontainers/testcontainers-go" or "/modules/<x>"
		return "testcontainers-go"
	case "testcontainers-python":
		// from testcontainers.<modules> import ...
		return "testcontainers"
	case "testcontainers-node":
		// import { GenericContainer } from "testcontainers"
		return "testcontainers"
	case "testcontainers-dotnet":
		// using Testcontainers.Containers;
		return "Testcontainers"
	case "testcontainers-rust":
		// use testcontainers::Container;
		return "testcontainers"
	default:
		// Best-effort fallback for novel bindings.
		return lib
	}
}

// containsAny returns true when ANY content blob contains the needle.
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

// formatTcViolation builds a directive-shape violation message
// identifying what the dev's tests must reference to satisfy the
// integration_target.
func formatTcViolation(t workflow.UpstreamResolution, bindingFound, imageFound bool) string {
	var missing []string
	if !bindingFound {
		missing = append(missing, fmt.Sprintf("Testcontainers binding (substring %q from library=%q)",
			libraryToImportNeedle(t.TestHarness.Library), t.TestHarness.Library))
	}
	if !imageFound {
		missing = append(missing, fmt.Sprintf("image coordinate %q", t.TestHarness.Image))
	}
	return fmt.Sprintf("integration_target %q has TestHarness but no modified test file references %s",
		t.Name, strings.Join(missing, " and "))
}

// passResult is a tiny helper to keep the no-op pass branches readable.
func passResult(stdout string) payloads.CheckResult {
	return payloads.CheckResult{
		Name:     "testcontainers-discipline",
		Passed:   true,
		Required: false,
		Command:  "testcontainers-discipline (internal)",
		Stdout:   stdout,
	}
}
