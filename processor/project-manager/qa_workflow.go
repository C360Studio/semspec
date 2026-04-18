package projectmanager

import (
	_ "embed"
	"os"
	"path/filepath"
)

// defaultQAWorkflow is the default .github/workflows/qa.yml template shipped
// with the semspec binary. It is embedded at build time so the binary carries
// the template without requiring templates/ on disk at runtime.
//
// Source of truth: templates/qa.yml (repo root). The copy at
// processor/project-manager/templates/qa.yml exists solely because go:embed
// cannot reference paths outside the package tree (no .. allowed).
//
//go:embed templates/qa.yml
var defaultQAWorkflow []byte

// ensureQAWorkflow creates .github/workflows/qa.yml in repoPath if it does
// not already exist. An existing file is left untouched — project owners own
// their QA workflow once it is in place.
//
// On success or when the file already exists, the function returns nil. Write
// errors are returned so the caller can log and continue non-fatally (a missing
// workflow scaffold is a warning, not an init failure).
func ensureQAWorkflow(repoPath string, logger interface {
	Info(string, ...any)
	Warn(string, ...any)
}) error {
	workflowPath := filepath.Join(repoPath, ".github", "workflows", "qa.yml")

	if fileExists(workflowPath) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(workflowPath, defaultQAWorkflow, 0o644); err != nil {
		return err
	}

	logger.Info("Scaffolded default QA workflow — customize at .github/workflows/qa.yml to add integration test services",
		"path", workflowPath)
	return nil
}
