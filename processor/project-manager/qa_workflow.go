package projectmanager

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/c360studio/semspec/workflow"
)

// EnsureQAWorkflow writes .github/workflows/qa.yml to workspacePath when
// missing, generating language-appropriate YAML from projectConfig. An
// existing file is left untouched — project owners (and devs) own the
// workflow once it is on disk; we only scaffold a default.
//
// On success or when the file already exists, returns nil. Write errors
// are returned so the caller can log and continue non-fatally (a missing
// scaffold is a warning, not a fatal failure).
//
// Called from:
//   - processor/project-manager HTTP /init: at project initialization.
//   - processor/plan-manager.publishQARequestIfNeeded: just before
//     publishing a QARequestedEvent so qa-runner finds the workflow even
//     when the project was hand-authored (e2e fixtures) rather than
//     init-ed via project-manager.
func EnsureQAWorkflow(workspacePath string, projectConfig *workflow.ProjectConfig, logger Logger) error {
	workflowPath := filepath.Join(workspacePath, ".github", "workflows", "qa.yml")
	if fileExists(workflowPath) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		return fmt.Errorf("create .github/workflows: %w", err)
	}
	body := BuildQAWorkflow(projectConfig)
	if err := os.WriteFile(workflowPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write qa.yml: %w", err)
	}
	if logger != nil {
		logger.Info("Scaffolded default QA workflow — keep catalog harness orchestration in project tests",
			"path", workflowPath, "language", primaryLanguage(projectConfig))
	}
	return nil
}

// Logger is the minimal interface EnsureQAWorkflow uses for diagnostics.
// Matches the surface of slog.Logger and component.Dependencies.GetLogger
// without forcing a hard import.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

// BuildQAWorkflow returns the qa.yml body for the given project shape.
// Switches on the primary language to pick the right toolchain setup
// + test command. Falls back to the Go template when language is
// unknown — it's the most permissive choice (act runners ship with
// most build tools available, so the wrong language template at worst
// produces a "no tests found" rather than a hard fail).
//
// All variants emit two jobs: `integration` (run at qa_level=integration
// AND qa_level=full) and `e2e` (run at qa_level=full only via the
// absence of --job filter). qa-runner uses --job integration at
// qa_level=integration to skip the e2e job for faster feedback.
//
// Note: Testcontainers usage doesn't need any qa.yml-level service
// declaration — the dev's test code spins up containers via the
// Testcontainers library, which uses the docker socket that act
// mounts into runner containers. The qa.yml just needs the language
// toolchain + the test command.
func BuildQAWorkflow(pc *workflow.ProjectConfig) string {
	switch primaryLanguage(pc) {
	case "Java", "Kotlin":
		return javaQAWorkflow(pc)
	case "Python":
		return pythonQAWorkflow(pc)
	case "TypeScript", "JavaScript":
		return nodeQAWorkflow(pc)
	case "Rust":
		return rustQAWorkflow(pc)
	default:
		return goQAWorkflow(pc)
	}
}

// primaryLanguage returns the name of the primary language declared in
// projectConfig, or empty when none is set.
func primaryLanguage(pc *workflow.ProjectConfig) string {
	if pc == nil {
		return ""
	}
	for _, l := range pc.Languages {
		if l.Primary {
			return l.Name
		}
	}
	return ""
}

// testCommand returns pc.EffectiveTestCommand() with a per-language
// fallback so a misconfigured project still gets a runnable line.
func testCommand(pc *workflow.ProjectConfig, fallback string) string {
	if pc != nil {
		if cmd := pc.EffectiveTestCommand(); cmd != "" {
			return cmd
		}
	}
	return fallback
}

func javaQAWorkflow(pc *workflow.ProjectConfig) string {
	cmd := testCommand(pc, "./gradlew test")
	return fmt.Sprintf(`name: QA
# Default QA workflow scaffolded by semspec. Customize as needed.
#
# integration: runs at qa_level=integration AND qa_level=full.
# e2e:         runs at qa_level=full only (Playwright browser flows).
#
# Testcontainers usage in test code does not require services: blocks
# here — the dev's tests spawn containers via the Testcontainers
# library, which uses the docker socket that act mounts into runner
# containers.
on: [push, pull_request]
jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-java@v4
        with:
          distribution: temurin
          java-version: '21'
      - name: Integration tests
        run: %s
      - name: Archive test reports
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: test-reports
          path: |
            build/reports/tests/
            build/test-results/
            target/surefire-reports/
            *.log
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
      - name: Install Playwright
        run: npm ci && npx playwright install --with-deps chromium
      - name: Run Playwright tests
        run: npx playwright test
      - name: Archive Playwright artifacts
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: playwright-report
          path: |
            playwright-report/
            test-results/
`, cmd)
}

func pythonQAWorkflow(pc *workflow.ProjectConfig) string {
	cmd := testCommand(pc, "pytest")
	return fmt.Sprintf(`name: QA
on: [push, pull_request]
jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: '3.12'
      - name: Install dependencies
        run: |
          python -m pip install --upgrade pip
          if [ -f pyproject.toml ]; then pip install -e ".[dev,test]" || pip install -e .; fi
          if [ -f requirements.txt ]; then pip install -r requirements.txt; fi
          if [ -f requirements-dev.txt ]; then pip install -r requirements-dev.txt; fi
      - name: Integration tests
        run: %s
      - name: Archive test reports
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: test-reports
          path: |
            .pytest_cache/
            *.log
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
      - name: Install Playwright
        run: npm ci && npx playwright install --with-deps chromium
      - name: Run Playwright tests
        run: npx playwright test
`, cmd)
}

func nodeQAWorkflow(pc *workflow.ProjectConfig) string {
	cmd := testCommand(pc, "npm test")
	return fmt.Sprintf(`name: QA
on: [push, pull_request]
jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
      - name: Install dependencies
        run: npm ci
      - name: Integration tests
        run: %s
      - name: Archive test reports
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: test-reports
          path: |
            coverage/
            *.log
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
      - name: Install Playwright
        run: npm ci && npx playwright install --with-deps chromium
      - name: Run Playwright tests
        run: npx playwright test
`, cmd)
}

func rustQAWorkflow(pc *workflow.ProjectConfig) string {
	cmd := testCommand(pc, "cargo test")
	return fmt.Sprintf(`name: QA
on: [push, pull_request]
jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@stable
      - name: Integration tests
        run: %s
      - name: Archive test reports
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: test-reports
          path: |
            target/debug/
            *.log
`, cmd)
}

func goQAWorkflow(pc *workflow.ProjectConfig) string {
	cmd := testCommand(pc, "go test ./... -tags=integration -v")
	return fmt.Sprintf(`name: QA
# Default QA workflow scaffolded by semspec project-manager.
#
# Two jobs:
#   - integration: run at qa_level=integration and qa_level=full
#   - e2e:        run at qa_level=full (Playwright browser flows)
#
# qa-runner invokes this file via nektos/act, passing --job integration at
# qa_level=integration so the e2e job is skipped for faster feedback. At
# qa_level=full act runs the full workflow, exercising both jobs.
#
# Catalog harness profiles are test-code responsibilities, not GitHub Actions
# services: entries. Keep Testcontainers/SITL orchestration in the project test
# suite so qa-runner, act, and GitHub-hosted runners execute the same path.
on: [push, pull_request]
jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: false
      - name: Integration tests
        run: %s
      - name: Archive coverage
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: |
            coverage.out
            *.log

  e2e:
    runs-on: ubuntu-latest
    # Runs at qa_level=full. For Go-only projects without a browser UI you can
    # delete this job. Projects with a Playwright suite should adapt the steps
    # to their actual dev-server startup, test command, and artifact paths.
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - name: Install dependencies
        run: npm ci
      - name: Install Playwright browsers
        run: npx playwright install --with-deps chromium
      - name: Run Playwright tests
        run: npx playwright test
      - name: Archive Playwright artifacts
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: playwright-report
          path: |
            playwright-report/
            test-results/
`, cmd)
}
