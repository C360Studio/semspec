package workflow

import (
	"os"
	"path/filepath"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

func init() {
	// Determine repo root from environment or current directory
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			repoRoot = "."
		}
	}

	// Resolve to absolute path
	absRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		absRepoRoot = repoRoot
	}

	// Create executors
	graphExec := NewGraphExecutor()
	documentExec := NewDocumentExecutor(absRepoRoot)
	constitutionExec := NewConstitutionExecutor(absRepoRoot)
	grepExec := NewGrepExecutor(absRepoRoot)

	// Register graph tools (primary context source)
	for _, tool := range graphExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, graphExec); err != nil {
			// Log but don't panic - tool might already be registered
			continue
		}
	}

	// Register document tools
	for _, tool := range documentExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, documentExec); err != nil {
			continue
		}
	}

	// Register constitution tools
	for _, tool := range constitutionExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, constitutionExec); err != nil {
			continue
		}
	}

	// Register grep fallback tools (secondary context source)
	for _, tool := range grepExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, grepExec); err != nil {
			continue
		}
	}
}
