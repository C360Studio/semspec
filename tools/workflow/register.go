package workflow

import (
	"os"
	"path/filepath"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// manifestClient is the package-level singleton for graph manifest fetching.
var manifestClient *ManifestClient

// GetManifestClient returns the package-level manifest client singleton.
// Returns nil if the graph gateway URL is not configured.
func GetManifestClient() *ManifestClient {
	return manifestClient
}

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

	// Initialize manifest client for graph knowledge summaries.
	manifestClient = NewManifestClient(getGatewayURL(), nil)

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
