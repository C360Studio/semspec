// Package spawn implements the spawn_agent tool executor.
// It publishes a TaskMessage to start a child agentic loop, watches the
// AGENT_LOOPS KV bucket for the child's terminal state, and returns the result.
package spawn

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
)

const (
	// defaultTimeout is used when the caller does not specify a timeout.
	defaultTimeout = 5 * time.Minute

	// defaultMaxDepth caps how many levels of nested agents may be spawned.
	defaultMaxDepth = 5

	// sourceSpawn is the source identifier stamped on BaseMessage envelopes
	// published by this executor.
	sourceSpawn = "semspec.spawn"
)

// NATSClient is the subset of natsclient.Client that Executor needs.
// Depending on this interface rather than the concrete struct keeps the
// executor testable without a live NATS connection.
type NATSClient interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// LoopWatcher abstracts KV watch on the AGENT_LOOPS bucket.
// jetstream.KeyValue satisfies this interface.
type LoopWatcher interface {
	Watch(ctx context.Context, key string, opts ...jetstream.WatchOpt) (jetstream.KeyWatcher, error)
}

// GraphHelper is the subset of agentgraph.Helper that Executor needs.
type GraphHelper interface {
	RecordSpawn(ctx context.Context, parentLoopID, childLoopID, role, model string) error
}

// Executor implements the ToolExecutor interface for the spawn_agent tool.
// It publishes a TaskMessage to start a child agentic loop, watches the
// AGENT_LOOPS KV bucket for the child's terminal state, and returns the result.
type Executor struct {
	nats         NATSClient
	graph        GraphHelper
	loopsBucket  LoopWatcher      // AGENT_LOOPS KV for watching child completion
	worktrees    *WorktreeManager // nil if worktree isolation is not configured
	defaultModel string
	maxDepth     int
}

// Option is a functional option for configuring an Executor.
type Option func(*Executor)

// WithDefaultModel sets the fallback model used when the caller does not
// provide one in the tool arguments.
func WithDefaultModel(model string) Option {
	return func(e *Executor) {
		e.defaultModel = model
	}
}

// WithMaxDepth sets the maximum spawn depth. The default is 5.
func WithMaxDepth(depth int) Option {
	return func(e *Executor) {
		e.maxDepth = depth
	}
}

// WithLoopsBucket sets the AGENT_LOOPS KV bucket used to watch for child
// loop completion. If nil, Execute returns an error.
func WithLoopsBucket(bucket LoopWatcher) Option {
	return func(e *Executor) {
		e.loopsBucket = bucket
	}
}

// WithWorktreeManager enables git worktree isolation for spawned agents.
// Each child agent gets its own worktree; on success the changes are merged
// back, on failure the worktree is discarded.
func WithWorktreeManager(mgr *WorktreeManager) Option {
	return func(e *Executor) {
		e.worktrees = mgr
	}
}

// NewExecutor constructs an Executor with the given NATS client and graph
// helper. Pass functional options to override defaults.
func NewExecutor(n NATSClient, g GraphHelper, opts ...Option) *Executor {
	e := &Executor{
		nats:     n,
		graph:    g,
		maxDepth: defaultMaxDepth,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ListTools returns the tool definitions that this executor exposes to an
// agentic loop's tool registry.
func (e *Executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        "spawn_agent",
		Description: "Spawn a child agent to perform a subtask. The child runs as an independent agentic loop and returns its result when complete.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"prompt", "role"},
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Task prompt for the child agent",
				},
				"role": map[string]any{
					"type":        "string",
					"description": "System role for the child agent",
				},
				"model": map[string]any{
					"type":        "string",
					"description": "LLM model (defaults to parent's model)",
				},
				"timeout": map[string]any{
					"type":        "string",
					"description": "Timeout duration (e.g. '5m', '30s')",
					"default":     "5m",
				},
				"system_context": map[string]any{
					"type":        "string",
					"description": "System prompt for the child agent (sets constructed context)",
				},
				"workflow_slug": map[string]any{
					"type":        "string",
					"description": "Workflow context slug (e.g. 'planning')",
				},
				"workflow_step": map[string]any{
					"type":        "string",
					"description": "Pipeline stage identifier (e.g. 'drafting')",
				},
				"metadata": map[string]any{
					"type":        "object",
					"description": "Key-value metadata to propagate to the child agent",
				},
			},
		},
	}}
}

// Execute runs the spawn_agent tool call. It:
//  1. Parses and validates arguments from call.Arguments.
//  2. Checks the spawn depth against the configured limit.
//  3. Creates an isolated git worktree (if configured).
//  4. Publishes a TaskMessage to agent.task.<taskID>.
//  5. Records the parent→child relationship in the graph.
//  6. Watches AGENT_LOOPS KV for the child loop to reach terminal state.
//  7. Merges or discards the worktree based on outcome.
func (e *Executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	if e.loopsBucket == nil {
		return errorResult(call.ID, call.LoopID, call.TraceID,
			"spawn_agent: AGENT_LOOPS KV bucket not configured"), nil
	}

	args, parseErr := parseArguments(call.Arguments)
	if parseErr != nil {
		return errorResult(call.ID, call.LoopID, call.TraceID, parseErr.Error()), nil
	}

	// Determine current depth and enforce the limit.
	currentDepth := args.depth
	if currentDepth+1 >= e.maxDepth {
		return errorResult(call.ID, call.LoopID, call.TraceID,
			fmt.Sprintf("spawn depth limit reached: current depth %d, max depth %d",
				currentDepth, e.maxDepth)), nil
	}

	// Resolve model: prefer argument, fall back to executor default.
	model := args.model
	if model == "" {
		model = e.defaultModel
	}
	if model == "" {
		return errorResult(call.ID, call.LoopID, call.TraceID,
			"spawn_agent: no model specified and no default model configured"), nil
	}

	childLoopID := uuid.New().String()
	taskID := uuid.New().String()

	// Create isolated worktree for child agent if configured.
	var worktreePath string
	if e.worktrees != nil {
		path, err := e.worktrees.Create(ctx, childLoopID)
		if err != nil {
			return errorResult(call.ID, call.LoopID, call.TraceID,
				fmt.Sprintf("spawn_agent: create worktree: %v", err)), nil
		}
		worktreePath = path
	}

	// Build metadata, merging caller-provided metadata with spawn metadata.
	taskMeta := map[string]any{
		"parent_loop_id": call.LoopID,
	}
	if worktreePath != "" {
		taskMeta["worktree_path"] = worktreePath
	}
	for k, v := range args.metadata {
		taskMeta[k] = v
	}

	// Build the TaskMessage with full context.
	task := &agentic.TaskMessage{
		LoopID:       childLoopID,
		TaskID:       taskID,
		Role:         args.role,
		Model:        model,
		Prompt:       args.prompt,
		ParentLoopID: call.LoopID,
		Depth:        currentDepth + 1,
		MaxDepth:     e.maxDepth,
		WorkflowSlug: args.workflowSlug,
		WorkflowStep: args.workflowStep,
		Metadata:     taskMeta,
	}
	if args.systemContext != "" {
		task.Context = &agentic.ConstructedContext{
			Content: args.systemContext,
		}
	}

	msg := message.NewBaseMessage(task.Schema(), task, sourceSpawn)
	data, marshalErr := json.Marshal(msg)
	if marshalErr != nil {
		e.cleanupWorktree(ctx, worktreePath, false)
		return agentic.ToolResult{}, fmt.Errorf("spawn_agent: marshal task message: %w", marshalErr)
	}

	subject := fmt.Sprintf("agent.task.%s", taskID)
	if pubErr := e.nats.PublishToStream(ctx, subject, data); pubErr != nil {
		e.cleanupWorktree(ctx, worktreePath, false)
		return agentic.ToolResult{}, fmt.Errorf("spawn_agent: publish task: %w", pubErr)
	}

	// Record the spawn relationship in the graph. Best-effort — the child
	// loop is already running so we continue waiting regardless of failure.
	var graphWarning string
	if graphErr := e.graph.RecordSpawn(ctx, call.LoopID, childLoopID, args.role, model); graphErr != nil {
		graphWarning = fmt.Sprintf("graph recording failed (non-fatal): %v", graphErr)
	}

	// Watch AGENT_LOOPS KV for the child loop reaching terminal state.
	result, watchErr := e.watchChildCompletion(ctx, childLoopID, args.timeout)
	if watchErr != nil {
		e.cleanupWorktree(ctx, worktreePath, false)
		return agentic.ToolResult{}, watchErr
	}

	// Build result metadata.
	resultMeta := map[string]any{
		"child_loop_id": childLoopID,
		"task_id":       taskID,
	}
	if graphWarning != "" {
		resultMeta["warning"] = graphWarning
	}

	// Handle worktree: merge on success, discard on failure.
	success := result.err == ""
	e.cleanupWorktree(ctx, worktreePath, success)

	if result.err != "" {
		return errorResult(call.ID, call.LoopID, call.TraceID, result.err), nil
	}

	return agentic.ToolResult{
		CallID:   call.ID,
		Content:  result.content,
		Metadata: resultMeta,
		LoopID:   call.LoopID,
		TraceID:  call.TraceID,
	}, nil
}

// watchChildCompletion watches the AGENT_LOOPS KV bucket for the child loop
// to reach a terminal state (complete, failed, or cancelled). Returns the
// child's result content on success, or an error description on failure.
func (e *Executor) watchChildCompletion(ctx context.Context, childLoopID string, timeout time.Duration) (childResult, error) {
	watcher, err := e.loopsBucket.Watch(ctx, childLoopID)
	if err != nil {
		return childResult{}, fmt.Errorf("spawn_agent: watch AGENT_LOOPS[%s]: %w", childLoopID, err)
	}
	defer watcher.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case entry, ok := <-watcher.Updates():
			if !ok {
				return childResult{err: "spawn_agent: AGENT_LOOPS watcher closed unexpectedly"}, nil
			}
			if entry == nil {
				// End of initial replay — no existing entry for this key yet.
				continue
			}
			if entry.Operation() != jetstream.KeyValuePut {
				continue
			}

			var loop agentic.LoopEntity
			if unmarshalErr := json.Unmarshal(entry.Value(), &loop); unmarshalErr != nil {
				continue // skip malformed entries, wait for next update
			}

			if !loop.State.IsTerminal() {
				continue
			}

			if loop.Outcome == agentic.OutcomeSuccess {
				return childResult{content: loop.Result}, nil
			}

			// Failed or cancelled.
			errMsg := loop.Error
			if errMsg == "" {
				errMsg = fmt.Sprintf("child loop %s reached terminal state: %s", childLoopID, loop.State)
			}
			return childResult{err: errMsg}, nil

		case <-timer.C:
			return childResult{err: fmt.Sprintf("spawn_agent: child loop %s timed out after %s", childLoopID, timeout)}, nil

		case <-ctx.Done():
			return childResult{err: fmt.Sprintf("spawn_agent: context cancelled: %v", ctx.Err())}, nil
		}
	}
}

// cleanupWorktree merges or discards a worktree. No-op if worktreePath is empty.
func (e *Executor) cleanupWorktree(ctx context.Context, worktreePath string, success bool) {
	if e.worktrees == nil || worktreePath == "" {
		return
	}
	if success {
		// Best-effort merge — the result is already captured in the child's
		// LoopEntity so a merge failure doesn't lose work.
		_ = e.worktrees.Merge(ctx, worktreePath)
	} else {
		_ = e.worktrees.Discard(ctx, worktreePath)
	}
}

// childResult carries the outcome of a child agent loop back to the caller.
type childResult struct {
	content string // non-empty on success
	err     string // non-empty on failure
}

// errorResult constructs a ToolResult that signals an error back to the loop.
// Returning a ToolResult with Error set (rather than a Go error) lets the
// agentic loop decide how to handle the failure; it does not crash the loop.
func errorResult(callID, loopID, traceID, msg string) agentic.ToolResult {
	return agentic.ToolResult{
		CallID:  callID,
		Error:   msg,
		LoopID:  loopID,
		TraceID: traceID,
	}
}

// spawnArgs holds parsed and validated arguments from a spawn_agent tool call.
type spawnArgs struct {
	prompt        string
	role          string
	model         string
	depth         int
	timeout       time.Duration
	systemContext string
	workflowSlug  string
	workflowStep  string
	metadata      map[string]any
}

// parseArguments validates the raw arguments map from a ToolCall and returns
// a typed spawnArgs. It returns an error if required fields are absent.
func parseArguments(args map[string]any) (spawnArgs, error) {
	out := spawnArgs{timeout: defaultTimeout}

	prompt, ok := stringArg(args, "prompt")
	if !ok || prompt == "" {
		return spawnArgs{}, fmt.Errorf("spawn_agent: argument 'prompt' is required")
	}
	out.prompt = prompt

	role, ok := stringArg(args, "role")
	if !ok || role == "" {
		return spawnArgs{}, fmt.Errorf("spawn_agent: argument 'role' is required")
	}
	out.role = role

	if model, ok := stringArg(args, "model"); ok {
		out.model = model
	}

	if timeoutStr, ok := stringArg(args, "timeout"); ok && timeoutStr != "" {
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return spawnArgs{}, fmt.Errorf("spawn_agent: invalid timeout %q: %w", timeoutStr, err)
		}
		if d <= 0 {
			return spawnArgs{}, fmt.Errorf("spawn_agent: timeout must be positive, got %s", d)
		}
		if d > 30*time.Minute {
			d = 30 * time.Minute
		}
		out.timeout = d
	}

	// Depth is passed as a numeric argument by the parent agent.
	if rawDepth, exists := args["depth"]; exists && rawDepth != nil {
		switch v := rawDepth.(type) {
		case int:
			out.depth = v
		case float64:
			out.depth = int(v)
		}
	}

	// Optional context passthrough fields.
	if sc, ok := stringArg(args, "system_context"); ok {
		out.systemContext = sc
	}
	if ws, ok := stringArg(args, "workflow_slug"); ok {
		out.workflowSlug = ws
	}
	if ws, ok := stringArg(args, "workflow_step"); ok {
		out.workflowStep = ws
	}
	if raw, exists := args["metadata"]; exists && raw != nil {
		if m, ok := raw.(map[string]any); ok {
			out.metadata = m
		}
	}

	return out, nil
}

// stringArg safely extracts a string value from an arguments map.
func stringArg(args map[string]any, key string) (string, bool) {
	v, exists := args[key]
	if !exists || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
