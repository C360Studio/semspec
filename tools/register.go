// Package tools registers agent tools with the semstreams agentic-tools component.
// Follows the bash-first approach: bash is the universal tool, specialized tools
// only for things bash can't do (graph queries, terminal signals, DAG decomposition).
package tools

import (
	"context"
	"os"
	"path/filepath"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/tools/bash"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/httptool"
	"github.com/c360studio/semspec/tools/question"
	"github.com/c360studio/semspec/tools/review"
	"github.com/c360studio/semspec/tools/spawn"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semstreams/natsclient"
	nats "github.com/nats-io/nats.go"
	// Register graph tools (graph_search, graph_query, graph_summary) via init()
	_ "github.com/c360studio/semspec/tools/workflow"
	// Register web search tool via init() — only active when BRAVE_SEARCH_API_KEY is set.
	_ "github.com/c360studio/semspec/tools/websearch"
	// httptool is imported above for RegisterWithNATS; its init() registers http_request.
)

// AgenticToolDeps carries the infrastructure dependencies required by
// spawn_agent and other runtime tools. These are not available at init() time
// so callers must invoke RegisterAgenticTools explicitly during startup.
type AgenticToolDeps struct {
	// NATSClient is the concrete NATS client. Every tool that needs NATS
	// uses this directly. The spawn tool adapts internally.
	NATSClient *natsclient.Client

	// GraphHelper is required by spawn_agent for recording spawn relationships.
	GraphHelper spawn.GraphHelper

	// DefaultModel is the fallback LLM model for spawned agents when the
	// tool call does not specify one.
	DefaultModel string

	// MaxDepth overrides the default spawn depth limit (5). Zero uses default.
	MaxDepth int

	// ErrorCategoryRegistry is required by review_scenario for category validation.
	// If nil, the review tool is not registered.
	ErrorCategoryRegistry *workflow.ErrorCategoryRegistry
}

// RegisterAgenticTools registers tools that require runtime infrastructure
// (NATS client, graph helper). Call once during component startup.
func RegisterAgenticTools(deps AgenticToolDeps) {
	// decompose_task — stateless, no infrastructure dependencies.
	decomposeExec := decompose.NewExecutor()
	for _, tool := range decomposeExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, decomposeExec)
	}

	// http_request — re-register with NATS for graph persistence when available.
	if deps.NATSClient != nil {
		httptool.RegisterWithNATS(deps.NATSClient)
	}

	if deps.NATSClient == nil || deps.GraphHelper == nil {
		// Infrastructure not available; skip spawn_agent.
		registerOptionalTools(deps)
		return
	}

	// spawn_agent — requires NATS and graph.
	// The spawn tool has its own NATSClient interface (different Subscribe signature),
	// so we adapt the concrete client here.
	spawnNC := &spawnNATSAdapter{client: deps.NATSClient}
	spawnOpts := []spawn.Option{}
	if deps.DefaultModel != "" {
		spawnOpts = append(spawnOpts, spawn.WithDefaultModel(deps.DefaultModel))
	}
	if deps.MaxDepth > 0 {
		spawnOpts = append(spawnOpts, spawn.WithMaxDepth(deps.MaxDepth))
	}
	spawnExec := spawn.NewExecutor(spawnNC, deps.GraphHelper, spawnOpts...)
	for _, tool := range spawnExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, spawnExec)
	}

	registerOptionalTools(deps)
}

// registerOptionalTools registers tools that have optional dependencies.
func registerOptionalTools(deps AgenticToolDeps) {
	// review_scenario — requires graph helper + error category registry.
	if deps.GraphHelper != nil {
		if rg, ok := deps.GraphHelper.(review.GraphHelper); ok && deps.ErrorCategoryRegistry != nil {
			reviewExec := review.NewExecutor(rg, deps.ErrorCategoryRegistry)
			for _, tool := range reviewExec.ListTools() {
				_ = agentictools.RegisterTool(tool.Name, reviewExec)
			}
		}
	}

	// ask_question — non-terminal tool that blocks until answer arrives.
	// Publishes question event; question-router handles routing + graph writes.
	if deps.NATSClient != nil {
		questionExec := question.NewExecutor(deps.NATSClient, nil)
		for _, tool := range questionExec.ListTools() {
			_ = agentictools.RegisterTool(tool.Name, questionExec)
		}
	}
}

// RegisterAgenticToolsWithContext is RegisterAgenticTools with a context for
// future use (e.g. graceful shutdown). Currently a pass-through.
func RegisterAgenticToolsWithContext(_ context.Context, deps AgenticToolDeps) {
	RegisterAgenticTools(deps)
}

func init() {
	// Determine repo root from environment or current directory.
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
		absRepoRoot = repoRoot
	}

	// bash — universal shell access (sandbox or local).
	bashExec := bash.NewExecutor(absRepoRoot, os.Getenv("SANDBOX_URL"))
	for _, tool := range bashExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, bashExec)
	}

	// submit_work + ask_question — terminal tools (StopLoop=true).
	termExec := terminal.NewExecutor()
	for _, tool := range termExec.ListTools() {
		_ = agentictools.RegisterTool(tool.Name, termExec)
	}

	// Graph tools (graph_search, graph_query, graph_summary) and web_search
	// are registered via blank imports above.
}

// spawnNATSAdapter adapts *natsclient.Client to spawn.NATSClient.
// The Subscribe signatures differ: natsclient uses func(context.Context, *nats.Msg)
// while spawn uses func(msg []byte).
type spawnNATSAdapter struct {
	client *natsclient.Client
}

func (a *spawnNATSAdapter) PublishToStream(ctx context.Context, subject string, data []byte) error {
	return a.client.PublishToStream(ctx, subject, data)
}

func (a *spawnNATSAdapter) Subscribe(ctx context.Context, subject string, handler func(msg []byte)) (spawn.Subscription, error) {
	sub, err := a.client.Subscribe(ctx, subject, func(_ context.Context, msg *nats.Msg) {
		handler(msg.Data)
	})
	if err != nil {
		return nil, err
	}
	return sub, nil
}
