// Package tools registers agent tools with the semstreams agentic-tools component.
// Follows the bash-first approach: bash is the universal tool, specialized tools
// only for things bash can't do (graph queries, terminal signals, DAG decomposition).
//
// All registration happens in RegisterAgenticTools, called once during component
// startup. There are no init() registrations — semspec always runs with NATS.
package tools

import (
	"context"
	"os"
	"path/filepath"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/tools/bash"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/httptool"
	"github.com/c360studio/semspec/tools/question"
	"github.com/c360studio/semspec/tools/review"
	"github.com/c360studio/semspec/tools/spawn"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/tools/websearch"
	"github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
)

// AgenticToolDeps carries the infrastructure dependencies required by tools.
type AgenticToolDeps struct {
	// NATSClient is the concrete NATS client.
	NATSClient *natsclient.Client

	// GraphHelper is required by spawn_agent for recording spawn relationships.
	GraphHelper spawn.GraphHelper

	// DefaultModel is the fallback LLM model for spawned agents.
	DefaultModel string

	// MaxDepth overrides the default spawn depth limit (5). Zero uses default.
	MaxDepth int

	// ErrorCategoryRegistry is required by review_scenario for category validation.
	// If nil, the review tool is not registered.
	ErrorCategoryRegistry *wf.ErrorCategoryRegistry
}

// RegisterAgenticTools registers all agent tools. Call once during component startup.
// Uses context.Background — prefer RegisterAgenticToolsWithContext for lifecycle-aware callers.
func RegisterAgenticTools(deps AgenticToolDeps) {
	RegisterAgenticToolsWithContext(context.Background(), deps)
}

// registerAgenticToolsImpl is the real implementation, accepting a context for
// lifecycle-aware operations like KV bucket discovery.
func registerAgenticToolsImpl(ctx context.Context, deps AgenticToolDeps) {
	// --- Stateless tools ---

	// bash — universal shell access (sandbox or local).
	repoRoot := resolveRepoRoot()
	bashExec := bash.NewExecutor(repoRoot, os.Getenv("SANDBOX_URL"))
	_ = agentictools.RegisterTool("bash", bashExec)

	// Terminal tools (StopLoop=true).
	// Each registration wraps the shared executor with singleToolAdapter so
	// ListTools() returns only the registered tool — prevents Gemini's
	// "Duplicate function declaration" error.
	termExec := terminal.NewExecutor()
	_ = agentictools.RegisterTool("submit_work", singleToolAdapter(termExec, "submit_work"))
	_ = agentictools.RegisterTool("submit_review", singleToolAdapter(termExec, "submit_review"))

	// decompose_task — validates LLM-provided TaskDAG.
	decomposeExec := decompose.NewExecutor()
	_ = agentictools.RegisterTool("decompose_task", decomposeExec)

	// http_request — with NATS for graph persistence when available.
	httptool.Register(deps.NATSClient)

	// graph tools (graph_search, graph_query, graph_summary).
	workflow.Register()

	// web_search — only active when BRAVE_SEARCH_API_KEY is set.
	websearch.Register()

	// --- Infrastructure-dependent tools ---

	// spawn_agent — requires NATS + graph + AGENT_LOOPS KV.
	if deps.NATSClient != nil && deps.GraphHelper != nil {
		spawnNC := &spawnNATSAdapter{client: deps.NATSClient}
		spawnOpts := []spawn.Option{}
		if deps.DefaultModel != "" {
			spawnOpts = append(spawnOpts, spawn.WithDefaultModel(deps.DefaultModel))
		}
		if deps.MaxDepth > 0 {
			spawnOpts = append(spawnOpts, spawn.WithMaxDepth(deps.MaxDepth))
		}
		// Wire AGENT_LOOPS KV bucket for watching child loop completion.
		if js, jsErr := deps.NATSClient.JetStream(); jsErr == nil {
			if loopsBucket, kvErr := wf.WaitForKVBucket(ctx, js, "AGENT_LOOPS"); kvErr == nil {
				spawnOpts = append(spawnOpts, spawn.WithLoopsBucket(loopsBucket))
			}
		}
		// Wire WorktreeManager for git-level isolation of child agents.
		if repoRoot != "" {
			spawnOpts = append(spawnOpts, spawn.WithWorktreeManager(spawn.NewWorktreeManager(repoRoot)))
		}
		spawnExec := spawn.NewExecutor(spawnNC, deps.GraphHelper, spawnOpts...)
		_ = agentictools.RegisterTool("spawn_agent", spawnExec)
	}

	// review_scenario — requires graph helper + error category registry.
	if deps.GraphHelper != nil {
		if rg, ok := deps.GraphHelper.(review.GraphHelper); ok && deps.ErrorCategoryRegistry != nil {
			reviewExec := review.NewExecutor(rg, deps.ErrorCategoryRegistry)
			_ = agentictools.RegisterTool("review_scenario", reviewExec)
		}
	}

	// ask_question — blocks until answer arrives via NATS.
	if deps.NATSClient != nil {
		questionExec := question.NewExecutor(deps.NATSClient, nil)
		_ = agentictools.RegisterTool("ask_question", questionExec)
	}
}

// RegisterAgenticToolsWithContext registers all agent tools with a parent context
// for lifecycle-aware operations like KV bucket discovery.
func RegisterAgenticToolsWithContext(ctx context.Context, deps AgenticToolDeps) {
	registerAgenticToolsImpl(ctx, deps)
}

// resolveRepoRoot determines the workspace root from env or cwd.
func resolveRepoRoot() string {
	repoRoot := os.Getenv("SEMSPEC_REPO_PATH")
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			repoRoot = "."
		}
	}
	absRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return repoRoot
	}
	return absRepoRoot
}

// spawnNATSAdapter adapts *natsclient.Client to spawn.NATSClient.
type spawnNATSAdapter struct {
	client *natsclient.Client
}

func (a *spawnNATSAdapter) PublishToStream(ctx context.Context, subject string, data []byte) error {
	return a.client.PublishToStream(ctx, subject, data)
}

// singleToolAdapter wraps a ToolExecutor so ListTools() returns only the
// definition matching the given name. This prevents duplicate function
// declarations when the same executor handles multiple tools (e.g.,
// terminal.Executor handles both submit_work and submit_review).
// Gemini rejects duplicate function names; OpenAI silently accepts them.
func singleToolAdapter(exec agentictools.ToolExecutor, name string) agentictools.ToolExecutor {
	return &filteredExecutor{inner: exec, name: name}
}

type filteredExecutor struct {
	inner agentictools.ToolExecutor
	name  string
}

func (f *filteredExecutor) ListTools() []agentic.ToolDefinition {
	var filtered []agentic.ToolDefinition
	for _, t := range f.inner.ListTools() {
		if t.Name == f.name {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (f *filteredExecutor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return f.inner.Execute(ctx, call)
}
