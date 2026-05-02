// Package tools registers agent tools with the semstreams agentic-tools component.
// Follows the bash-first approach: bash is the universal tool, specialized tools
// only for things bash can't do (graph queries, terminal signals, DAG decomposition).
//
// All registration happens in RegisterAgenticTools, called once during component
// startup. There are no init() registrations — semspec always runs with NATS.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semspec/tools/bash"
	"github.com/c360studio/semspec/tools/decompose"
	"github.com/c360studio/semspec/tools/httptool"
	"github.com/c360studio/semspec/tools/question"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/tools/websearch"
	"github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semstreams/natsclient"
)

// ToolTimeouts holds configurable timeouts for agent tools.
// Zero values mean "use the tool's builtin default."
type ToolTimeouts struct {
	Bash time.Duration // Default 120s — shell command execution.
	HTTP time.Duration // Default 30s — HTTP fetch requests.
}

// AgenticToolDeps carries the infrastructure dependencies required by tools.
type AgenticToolDeps struct {
	// NATSClient is the concrete NATS client.
	NATSClient *natsclient.Client

	// DefaultModel is the fallback LLM model for agents. Currently only used
	// by the question-answerer dispatch below.
	DefaultModel string

	// AnswererRegistry routes ask_question dispatch by topic. When nil the
	// executor falls back to legacy generic-agent dispatch — see
	// tools/question/executor.go:routeQuestion.
	AnswererRegistry *answerer.Registry

	// Timeouts overrides default tool execution timeouts. Zero values use builtin defaults.
	Timeouts ToolTimeouts
}

// RegisterAgenticTools registers all agent tools onto the supplied registry.
// Call once during component startup. Uses context.Background — prefer
// RegisterAgenticToolsWithContext for lifecycle-aware callers.
func RegisterAgenticTools(reg *agentictools.ExecutorRegistry, deps AgenticToolDeps) error {
	return RegisterAgenticToolsWithContext(context.Background(), reg, deps)
}

// RegisterAgenticToolsWithContext registers all agent tools onto the supplied
// registry. The context is reserved for lifecycle-aware operations like KV
// bucket discovery; currently unused.
//
// Returns an aggregated error covering every per-tool registration failure
// (joined via errors.Join). Duplicate registration is a hard error in beta.16,
// so this surfaces misconfiguration loudly instead of swallowing it.
func RegisterAgenticToolsWithContext(_ context.Context, reg *agentictools.ExecutorRegistry, deps AgenticToolDeps) error {
	if reg == nil {
		return fmt.Errorf("RegisterAgenticToolsWithContext: registry is nil")
	}

	var errs []error

	// --- Stateless tools ---

	// bash — universal shell access (sandbox or local).
	repoRoot := resolveRepoRoot()
	var bashOpts []bash.Option
	if deps.Timeouts.Bash > 0 {
		bashOpts = append(bashOpts, bash.WithDefaultTimeout(deps.Timeouts.Bash))
	}
	bashExec := bash.NewExecutor(repoRoot, os.Getenv("SANDBOX_URL"), bashOpts...)
	errs = append(errs, reg.RegisterTool("bash", bashExec))

	// submit_work — terminal tool (StopLoop=true) shared across deliverable types.
	termExec := terminal.NewExecutor()
	errs = append(errs, reg.RegisterTool("submit_work", termExec))

	// decompose_task — validates LLM-provided TaskDAG.
	decomposeExec := decompose.NewExecutor()
	errs = append(errs, reg.RegisterTool("decompose_task", decomposeExec))

	// http_request — with NATS for graph persistence when available.
	var httpOpts []httptool.Option
	if deps.Timeouts.HTTP > 0 {
		httpOpts = append(httpOpts, httptool.WithRequestTimeout(deps.Timeouts.HTTP))
	}
	errs = append(errs, httptool.Register(reg, deps.NATSClient, httpOpts...))

	// graph tools (graph_search, graph_query, graph_summary).
	errs = append(errs, workflow.Register(reg))

	// web_search — only active when BRAVE_SEARCH_API_KEY is set.
	errs = append(errs, websearch.Register(reg))

	// --- Infrastructure-dependent tools ---

	// spawn_agent was deleted in Phase 3 of the task-11 worktree audit. The
	// reactive execution model (ADR-025) — decompose_task + serial DAG
	// dispatch by requirement-executor — replaced the LLM-driven child-agent
	// spawning pattern. See docs/audit/task-11-worktree-invariants.md (A4).

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
		if deps.AnswererRegistry != nil {
			questionExec = questionExec.WithAnswererRegistry(deps.AnswererRegistry)
		}
		errs = append(errs, reg.RegisterTool("ask_question", questionExec))

		answerExec := question.NewAnswerExecutor(questionStore, nil)
		errs = append(errs, reg.RegisterTool("answer_question", answerExec))
	}

	if joined := errors.Join(errs...); joined != nil {
		return fmt.Errorf("register agentic tools: %w", joined)
	}
	return nil
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
