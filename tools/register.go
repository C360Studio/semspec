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
	"time"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/tools/bash"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/httptool"
	"github.com/c360studio/semspec/tools/question"
	"github.com/c360studio/semspec/tools/spawn"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/tools/websearch"
	"github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/natsclient"
)

// ToolTimeouts holds configurable timeouts for agent tools.
// Zero values mean "use the tool's builtin default."
type ToolTimeouts struct {
	Bash     time.Duration // Default 120s — shell command execution.
	Spawn    time.Duration // Default 5m — child agent default timeout.
	SpawnMax time.Duration // Default 30m — max timeout an LLM can request for a child agent.
	HTTP     time.Duration // Default 30s — HTTP fetch requests.
}

// AgenticToolDeps carries the infrastructure dependencies required by tools.
type AgenticToolDeps struct {
	// NATSClient is the concrete NATS client.
	NATSClient *natsclient.Client

	// DefaultModel is the fallback LLM model for spawned agents.
	DefaultModel string

	// MaxDepth overrides the default spawn depth limit (5). Zero uses default.
	MaxDepth int

	// Timeouts overrides default tool execution timeouts. Zero values use builtin defaults.
	Timeouts ToolTimeouts
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
	var bashOpts []bash.Option
	if deps.Timeouts.Bash > 0 {
		bashOpts = append(bashOpts, bash.WithDefaultTimeout(deps.Timeouts.Bash))
	}
	bashExec := bash.NewExecutor(repoRoot, os.Getenv("SANDBOX_URL"), bashOpts...)
	_ = agentictools.RegisterTool("bash", bashExec)

	// Terminal tools (StopLoop=true).
	// Each registration wraps the shared executor with singleToolAdapter so
	// ListTools() returns only the registered tool — prevents Gemini's
	// "Duplicate function declaration" error.
	termExec := terminal.NewExecutor()
	_ = agentictools.RegisterTool("submit_work", termExec)

	// decompose_task — validates LLM-provided TaskDAG.
	decomposeExec := decompose.NewExecutor()
	_ = agentictools.RegisterTool("decompose_task", decomposeExec)

	// http_request — with NATS for graph persistence when available.
	var httpOpts []httptool.Option
	if deps.Timeouts.HTTP > 0 {
		httpOpts = append(httpOpts, httptool.WithRequestTimeout(deps.Timeouts.HTTP))
	}
	httptool.Register(deps.NATSClient, httpOpts...)

	// graph tools (graph_search, graph_query, graph_summary).
	workflow.Register()

	// web_search — only active when BRAVE_SEARCH_API_KEY is set.
	websearch.Register()

	// --- Infrastructure-dependent tools ---

	// spawn_agent — requires NATS + AGENT_LOOPS KV.
	// Graph tracking via TripleWriter is best-effort and non-blocking.
	if deps.NATSClient != nil {
		spawnNC := &spawnNATSAdapter{client: deps.NATSClient}
		spawnOpts := []spawn.Option{}
		if deps.DefaultModel != "" {
			spawnOpts = append(spawnOpts, spawn.WithDefaultModel(deps.DefaultModel))
		}
		if deps.MaxDepth > 0 {
			spawnOpts = append(spawnOpts, spawn.WithMaxDepth(deps.MaxDepth))
		}
		if deps.Timeouts.Spawn > 0 {
			spawnOpts = append(spawnOpts, spawn.WithDefaultTimeout(deps.Timeouts.Spawn))
		}
		if deps.Timeouts.SpawnMax > 0 {
			spawnOpts = append(spawnOpts, spawn.WithMaxTimeout(deps.Timeouts.SpawnMax))
		}
		// Wire AGENT_LOOPS KV bucket for watching child loop completion.
		if js, jsErr := deps.NATSClient.JetStream(); jsErr == nil {
			if loopsBucket, kvErr := wf.WaitForKVBucket(ctx, js, "AGENT_LOOPS"); kvErr == nil {
				spawnOpts = append(spawnOpts, spawn.WithLoopsBucket(loopsBucket))
			}
		}
		// Wire TripleWriter for best-effort spawn relationship tracking in the graph.
		tw := &graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			ComponentName: "spawn-agent",
		}
		spawnOpts = append(spawnOpts, spawn.WithTripleWriter(tw))
		// Wire WorktreeManager for git-level isolation of child agents.
		if repoRoot != "" {
			spawnOpts = append(spawnOpts, spawn.WithWorktreeManager(spawn.NewWorktreeManager(repoRoot)))
		}
		spawnExec := spawn.NewExecutor(spawnNC, spawnOpts...)
		_ = agentictools.RegisterTool("spawn_agent", spawnExec)
	}

	// ask_question — writes to QUESTIONS KV, dispatches answerer agent, blocks on KV watch.
	// answer_question — terminal tool for answerer agents, writes answer to QUESTIONS KV.
	if deps.NATSClient != nil {
		var questionStore *wf.QuestionStore
		if store, storeErr := wf.NewQuestionStore(deps.NATSClient); storeErr == nil {
			questionStore = store
		}
		questionExec := question.NewExecutor(deps.NATSClient, questionStore, nil)
		if deps.DefaultModel != "" {
			questionExec = questionExec.WithDefaultModel(deps.DefaultModel)
		}
		_ = agentictools.RegisterTool("ask_question", questionExec)

		answerExec := question.NewAnswerExecutor(questionStore, nil)
		_ = agentictools.RegisterTool("answer_question", answerExec)
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
