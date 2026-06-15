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

// CheckHarnessProfileDiscipline verifies the architect's harness profile
// selections resolve in the catalog. Issue #113 (2026-06-03): the literal-
// substring sub-checks (evidence-anchor presence, @integration binding
// string, env-key consumption) were retired because they were goodhart-
// able — any check that grades on "did you write this exact string" can
// be satisfied with a dead string literal. PR #109 (#90) surfaced the
// expected literals to the dev's first-cycle prompt, which made the
// stub-passing attack visible and easy to construct.
//
// Binding correctness is now enforced by:
//
//   - **LLM reviewer** — judges whether the test actually exercises the
//     bound harness (catches "UDP probe facade is not real mavsdk_server
//     lifecycle" — the smoke-9 reviewer's call).
//   - **Executable QA runtime** — qa_level=integration runs the configured
//     project QA command in the sandbox; full/e2e orchestration remains in
//     operator CI for MVP. A test that just references the literal but doesn't
//     open the service fails at that runtime gate.
//
// This check now does one thing: verify that the architect didn't name
// a profile that doesn't exist in the catalog. That's a plan-time
// validity check, not a behavioral one. Unknown profile IDs surface as
// a hard failure so plan-reviewer / Sarah can route on it.
//
// Future direction: see issue #113 for the path to AST-aware or
// behavioral evidence (Option B / Option C). This commit ships
// Option A — delete the literal checks; rely on LLM reviewer plus executable
// QA runtime.
func CheckHarnessProfileDiscipline(selections []workflow.HarnessProfileSelection, catalog *harnesscatalog.Catalog) payloads.CheckResult {
	if len(selections) == 0 {
		return passHarnessResult("no test environment profiles selected — nothing to enforce")
	}
	required, err := catalog.RequiredProfiles(selections)
	if err != nil {
		return failHarnessResult(err.Error())
	}
	return passHarnessResult(fmt.Sprintf(
		"required test environment profiles resolve in catalog: %d (binding correctness enforced by LLM reviewer + executable QA runtime, not literal-grep — see issue #113)",
		len(required)))
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

// loadPlanForDiscipline is the shared plan.json reader. Returns nil on
// I/O or parse failure — callers treat that as "no data, skip
// enforcement" rather than blocking.
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
// Kept after #113 because executor.go still uses it as a gate to skip
// non-test-touching dispatches entirely. More permissive than
// executor.go:hasTestFiles which is Go-only.
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
// the gate above; false positives (treating non-test files as tests)
// would over-eagerly run the discipline check on non-test-touching PRs.
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
