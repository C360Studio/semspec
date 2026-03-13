// Package tools provides file and git operation tools for the Semspec agent.
// Tools are registered globally via init() for use by agentic-tools.
package tools

import (
	"context"
	"os"
	"path/filepath"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/tools/create"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/doc"
	"github.com/c360studio/semspec/tools/file"
	"github.com/c360studio/semspec/tools/git"
	"github.com/c360studio/semspec/tools/github"
	"github.com/c360studio/semspec/tools/review"
	"github.com/c360studio/semspec/tools/spawn"
	"github.com/c360studio/semspec/tools/tree"
	"github.com/c360studio/semspec/workflow"
	// Register workflow tools via init()
	_ "github.com/c360studio/semspec/tools/workflow"
)

// AgenticToolDeps carries the infrastructure dependencies required by
// spawn_agent and query_agent_tree. These are not available at init() time
// so callers must invoke RegisterAgenticTools explicitly during startup.
type AgenticToolDeps struct {
	// NATSClient is required by spawn_agent for publishing task messages and
	// subscribing to child completion events.
	NATSClient spawn.NATSClient

	// GraphHelper is required by spawn_agent and query_agent_tree for
	// recording spawn relationships and querying the agent hierarchy.
	GraphHelper spawn.GraphHelper

	// TreeQuerier is the graphQuerier interface required by query_agent_tree.
	// If nil and GraphHelper satisfies the interface, it will be used directly.
	TreeQuerier tree.GraphQuerier

	// DefaultModel is the fallback LLM model for spawned agents when the
	// tool call does not specify one.
	DefaultModel string

	// MaxDepth overrides the default spawn depth limit (5). Zero uses default.
	MaxDepth int

	// ErrorCategoryRegistry is required by review_scenario for category validation.
	// If nil, the review tool is not registered.
	ErrorCategoryRegistry *workflow.ErrorCategoryRegistry
}

// RegisterAgenticTools registers the three agentic tool executors that require
// runtime infrastructure (NATS client, graph helper). Call this once during
// component startup after the NATS connection is established.
//
// decompose_task is also registered here because its Executor is stateless
// and doesn't need infrastructure, but grouping all agentic tools together
// makes the startup sequence explicit.
func RegisterAgenticTools(deps AgenticToolDeps) {
	// decompose_task — stateless, no infrastructure dependencies.
	decomposeExec := decompose.NewExecutor()
	for _, tool := range decomposeExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, decomposeExec)
	}

	// create_tool — stateless MVP, no infrastructure dependencies.
	createExec := create.NewExecutor()
	for _, tool := range createExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, createExec)
	}

	if deps.NATSClient == nil || deps.GraphHelper == nil {
		// Infrastructure not available; skip spawn_agent and query_agent_tree.
		return
	}

	// spawn_agent — requires NATS and graph.
	spawnOpts := []spawn.Option{}
	if deps.DefaultModel != "" {
		spawnOpts = append(spawnOpts, spawn.WithDefaultModel(deps.DefaultModel))
	}
	if deps.MaxDepth > 0 {
		spawnOpts = append(spawnOpts, spawn.WithMaxDepth(deps.MaxDepth))
	}
	spawnExec := spawn.NewExecutor(deps.NATSClient, deps.GraphHelper, spawnOpts...)
	for _, tool := range spawnExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, spawnExec)
	}

	// query_agent_tree — requires graph querier.
	querier := deps.TreeQuerier
	if querier == nil {
		if q, ok := deps.GraphHelper.(tree.GraphQuerier); ok {
			querier = q
		}
	}
	if querier != nil {
		treeExec := tree.NewExecutor(querier)
		for _, tool := range treeExec.ListTools() {
			_ = agentictools.RegisterTool(tool.Name, treeExec)
		}
	}

	// review_scenario — requires graph helper + error category registry.
	if rg, ok := deps.GraphHelper.(review.GraphHelper); ok && deps.ErrorCategoryRegistry != nil {
		reviewExec := review.NewExecutor(rg, deps.ErrorCategoryRegistry)
		for _, tool := range reviewExec.ListTools() {
			_ = agentictools.RegisterTool(tool.Name, NewRecordingExecutor(reviewExec))
		}
	}
}

// RegisterAgenticToolsWithContext is RegisterAgenticTools with a context for
// future use (e.g. graceful shutdown). Currently a pass-through.
func RegisterAgenticToolsWithContext(_ context.Context, deps AgenticToolDeps) {
	RegisterAgenticTools(deps)
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

	// Create executors wrapped with recording for trajectory tracking.
	// RecordingExecutor captures tool call metadata (timing, params, result)
	// and stores it in the TOOL_CALLS KV bucket via GlobalToolCallStore().
	fileExec := NewRecordingExecutor(file.NewExecutor(absRepoRoot))
	gitExec := NewRecordingExecutor(git.NewExecutor(absRepoRoot))
	githubExec := NewRecordingExecutor(github.NewExecutor(absRepoRoot))

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
	for _, tool := range githubExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, githubExec); err != nil {
			// Log but don't panic - tool might already be registered
			continue
		}
	}

	// Register doc tools
	// Use sources directory from environment or default to .semspec/sources/docs
	sourcesDir := os.Getenv("SEMSPEC_SOURCES_DIR")
	if sourcesDir == "" {
		sourcesDir = filepath.Join(absRepoRoot, ".semspec", "sources", "docs")
	}
	docExec := doc.NewExecutor(sourcesDir)
	for _, tool := range docExec.ListTools() {
		if err := agentictools.RegisterTool(tool.Name, docExec); err != nil {
			// Log but don't panic - tool might already be registered
			continue
		}
	}
}
