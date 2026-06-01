// Package tools registers agent tools with the semstreams agentic-tools component.
// Follows the bash-first approach: bash is the universal tool, specialized tools
// only for things bash can't do (graph queries, terminal signals).
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

	"log/slog"

	"github.com/c360studio/semspec/tools/bash"
	"github.com/c360studio/semspec/tools/httptool"
	"github.com/c360studio/semspec/tools/question"
	"github.com/c360studio/semspec/tools/research"
	"github.com/c360studio/semspec/tools/terminal"
	"github.com/c360studio/semspec/tools/websearch"
	"github.com/c360studio/semspec/tools/workflow"
	wf "github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/answerer"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semstreams/component"
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

	// Platform identifies this semspec instance. Required to wire the
	// write_todos tool (ADR-036) — the executor uses Org+Platform to
	// resolve the loop entity ID on which todos are stored as triples.
	// Zero value disables write_todos registration without erroring.
	Platform component.PlatformMeta
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
	// Wire SKG triple emission so bash path-miss recovery hints land
	// in the graph (per ADR-035-style loud-recovery discipline).
	// Falls back to counter+log only when natsClient is nil.
	if deps.NATSClient != nil {
		bashOpts = append(bashOpts, bash.WithTripleEmitter(&graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        slog.Default(),
			ComponentName: "bash",
		}))
	}
	bashExec := bash.NewExecutor(repoRoot, os.Getenv("SANDBOX_URL"), bashOpts...)
	errs = append(errs, reg.RegisterTool("bash", bashExec))

	// submit_work — terminal tool (StopLoop=true) shared across deliverable types.
	// workDir wires the planner scope.include validator (see
	// tools/terminal/scope_validator.go); SKG triple writer makes
	// each fire queryable via tool.recovery.incident.
	termExec := terminal.NewExecutor().WithWorkDir(repoRoot)
	if deps.NATSClient != nil {
		termExec = termExec.WithTripleEmitter(&graphutil.TripleWriter{
			NATSClient:    deps.NATSClient,
			Logger:        slog.Default(),
			ComponentName: "submit_work",
		})
	}
	errs = append(errs, reg.RegisterTool("submit_work", termExec))

	// http_request — with NATS for graph persistence when available.
	var httpOpts []httptool.Option
	if deps.Timeouts.HTTP > 0 {
		httpOpts = append(httpOpts, httptool.WithRequestTimeout(deps.Timeouts.HTTP))
	}
	errs = append(errs, httptool.Register(reg, deps.NATSClient, httpOpts...))

	// graph tools (graph_search, graph_query, graph_summary).
	errs = append(errs, workflow.Register(reg, deps.NATSClient))

	// web_search — only active when BRAVE_SEARCH_API_KEY is set.
	errs = append(errs, websearch.Register(reg))

	// --- Infrastructure-dependent tools ---

	// spawn_agent was deleted in Phase 3 of the task-11 worktree audit. The
	// reactive execution model (ADR-025) — originally decompose_task + serial
	// DAG dispatch by requirement-executor — replaced the LLM-driven
	// child-agent spawning pattern. ADR-043 PR 4g retired the decomposer
	// LLM step itself: requirement-executor now synthesizes the DAG from
	// Sarah-prepared Stories. See docs/audit/task-11-worktree-invariants.md (A4).

	errs = append(errs, registerQuestionTools(reg, deps)...)
	errs = append(errs, registerResearchTools(reg, deps)...)
	errs = append(errs, registerAgentScratchTools(reg, deps)...)

	if joined := errors.Join(errs...); joined != nil {
		return fmt.Errorf("register agentic tools: %w", joined)
	}
	return nil
}

// registerQuestionTools wires ask_question (KV write + answerer dispatch +
// KV watch) and answer_question (terminal tool for answerer agents).
func registerQuestionTools(reg *agentictools.ExecutorRegistry, deps AgenticToolDeps) []error {
	if deps.NATSClient == nil {
		return nil
	}
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
	return []error{
		reg.RegisterTool("ask_question", questionExec),
		reg.RegisterTool("answer_question", question.NewAnswerExecutor(questionStore, nil)),
	}
}

// registerResearchTools wires research (non-terminal dev tool that delegates
// upstream-API-surface investigation to a researcher sub-agent; blocks on
// RESEARCH KV until answer_research lands) and answer_research (terminal
// tool for the researcher sub-agent; validates answer size against
// workflow.MaxResearchAnswerBytes and citation shape).
//
// R2 wires only the tool executors so wire shape and arg validation are
// exercisable in unit + mock e2e tests before paying for the dispatch
// loop. researcher-manager (R3) subscribes to agent.research.requested.>
// and spawns the researcher loop.
func registerResearchTools(reg *agentictools.ExecutorRegistry, deps AgenticToolDeps) []error {
	if deps.NATSClient == nil {
		return nil
	}
	var researchStore *wf.ResearchStore
	if store, storeErr := wf.NewResearchStore(deps.NATSClient); storeErr == nil {
		researchStore = store
	}
	return []error{
		reg.RegisterTool("research", research.NewExecutor(deps.NATSClient, researchStore, nil)),
		reg.RegisterTool("answer_research", research.NewAnswerExecutor(researchStore, nil)),
	}
}

// registerAgentScratchTools wires write_todos and scratchpad — agent-private
// reasoning channels persisted as graph triples on the calling loop's entity
// (semstreams ADR-036 / beta.62). write_todos is cross-iteration list
// semantics; scratchpad is a single-shot reasoning dump. Both survive context
// compaction so multi-step dispatches can hold state across iterations.
//
// Requires NATS (graph mutations) and a non-zero Platform (loop entity ID
// resolution); skipped silently if either is missing.
func registerAgentScratchTools(reg *agentictools.ExecutorRegistry, deps AgenticToolDeps) []error {
	if deps.NATSClient == nil || deps.Platform.Org == "" || deps.Platform.Platform == "" {
		return nil
	}
	todoWriter := agentictools.NewNATSTodoWriter(deps.NATSClient)
	todoExec := agentictools.NewWriteTodosExecutor(todoWriter, deps.Platform)
	todoExec.SetLogger(slog.Default())

	scratchPub := agentictools.NewNATSTriplePublisher(deps.NATSClient)
	scratchExec := agentictools.NewScratchpadExecutor(scratchPub, deps.Platform)
	scratchExec.SetLogger(slog.Default())

	return []error{
		reg.RegisterTool(agentictools.WriteTodosToolName, todoExec),
		reg.RegisterTool(agentictools.ScratchpadToolName, scratchExec),
	}
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
