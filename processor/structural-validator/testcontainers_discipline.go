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
// selected required harness profiles are actually exercised. The architect
// selects catalog profile IDs; the catalog owns the strings that must appear in
// tests (profile ID, image/port/assertion anchors, etc.).
//
// No-op cases (pass without enforcement):
//   - No harness profiles selected (greenfield / runtime_dep-only).
//   - filesModified contains no test files (non-test scenarios;
//     integration coverage is verified by other scenarios that DO
//     produce tests).
//
// Unknown profile IDs and missing required-profile evidence are hard failures.
func CheckHarnessProfileDiscipline(workDir string, filesModified []string, selections []workflow.HarnessProfileSelection, catalog *harnesscatalog.Catalog) payloads.CheckResult {
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

// loadHarnessProfiles reads the plan.json for the given slug and returns the
// architect-selected harness profiles. Returns nil when the plan or architecture
// is absent.
func loadHarnessProfiles(repoPath, slug string) []workflow.HarnessProfileSelection {
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
	return plan.Architecture.HarnessProfiles
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
