// Package tools provides file and git operation tools for the Semspec agent.
// Tools are registered globally via init() for use by agentic-tools.
package tools

import (
	"os"
	"path/filepath"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/tools/file"
	"github.com/c360studio/semspec/tools/git"
	"github.com/c360studio/semspec/tools/github"
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
	fileExec := file.NewExecutor(absRepoRoot)
	gitExec := git.NewExecutor(absRepoRoot)

	// Register file tools
	for _, tool := range fileExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, fileExec); err != nil {
			// Log but don't panic - tool might already be registered
			continue
		}
	}

	// Register git tools
	for _, tool := range gitExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, gitExec); err != nil {
			// Log but don't panic - tool might already be registered
			continue
		}
	}

	// Register GitHub tools
	githubExec := github.NewExecutor(absRepoRoot)
	for _, tool := range githubExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, githubExec); err != nil {
			// Log but don't panic - tool might already be registered
			continue
		}
	}
}
