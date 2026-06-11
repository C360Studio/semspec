package bash

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/c360studio/semspec/vocabulary/observability"
	"github.com/c360studio/semspec/workflow/recoveryhint"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// maxTrackedTasks caps the per-task path-miss tracker. Realistic
// concurrent-loop counts run ~50; 1024 absorbs short bursts and
// stale entries without unbounded growth. When exceeded, eviction
// drops one arbitrary entry per insert (no LRU machinery — staleness
// resolves itself on the next call from that task).
const maxTrackedTasks = 1024

// bashRecoveryCounter mirrors graph_query's recovery counter shape
// (tools/workflow/graph.go). Labeled by ToolRecoveryOutcome:
//   - suggested: a 2nd-occurrence path-miss matched and we injected
//     a RETRY HINT into the agent-facing error.
//   - not_suggested: a path-miss was detected but it was the first
//     occurrence for this (task_id, command, path) tuple — we recorded
//     state but did not yet inject a hint.
//
// Operators read deltas via /metrics to see how often the bash tool
// is helping the agent recover from wrong-path bash calls vs how often
// it just sees the first miss and waits.
var bashRecoveryCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "semspec_bash_recovery_total",
		Help: "Total fires of bash path-miss recovery hints. Labeled by outcome: suggested (RETRY HINT injected on a 2nd-occurrence path-miss for this task) or not_suggested (path-miss detected but it was the first occurrence — state recorded, no hint injected).",
	},
	[]string{"outcome"},
)

func init() {
	bashRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeSuggested)
	bashRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeNotSuggested)
}

// RegisterMetrics registers the bash path-miss recovery counter with
// the given metrics registry so per-fire telemetry surfaces at
// /metrics. Call once during process startup. Idempotent. Nil-safe.
func RegisterMetrics(reg *metric.MetricsRegistry) error {
	if reg == nil {
		return nil
	}
	if err := reg.RegisterCounterVec("bash", "recovery_total", bashRecoveryCounter); err != nil {
		return fmt.Errorf("register bash recovery counter: %w", err)
	}
	return nil
}

// pathMissPatterns matches the common Unix shapes of "the path you
// asked for doesn't exist." Order matters: the most specific shapes
// come first; a generic "<prog>: <path>: No such file or directory"
// fallback handles tools that don't use one of the named verbs.
//
// All patterns capture the path in submatch index 1.
var pathMissPatterns = []*regexp.Regexp{
	// ls: cannot access 'path': No such file or directory
	// (also matches double-quoted and unquoted forms)
	regexp.MustCompile(`cannot access ['"]?(.+?)['"]?: No such file or directory`),
	// head/tail: cannot open 'path' for reading: No such file or directory
	regexp.MustCompile(`cannot open ['"]?(.+?)['"]? for reading: No such file or directory`),
	// shell builtin cd: /bin/sh: line 1: cd: bad/dir: No such file or directory
	regexp.MustCompile(`cd: ([^:\n]+?): No such file or directory`),
	// generic: <prog>: <path>: No such file or directory
	// Restrict the leading <prog> to a simple identifier so we don't
	// match "main.go:5:2: undefined: foo: No such..." style compile
	// errors that share the suffix but aren't path misses.
	regexp.MustCompile(`(?m)^[a-zA-Z_][a-zA-Z0-9_-]*: ([^:\n]+?): No such file or directory$`),
}

// classifyPathMiss extracts the missing path from stderr if the
// stderr matches a known "No such file or directory" shape. Returns
// "" if the stderr is not a path-miss class.
func classifyPathMiss(stderr string) string {
	if stderr == "" {
		return ""
	}
	for _, re := range pathMissPatterns {
		if m := re.FindStringSubmatch(stderr); len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// pathMissEntry is the per-task state the detector needs to recognize
// a 2nd-occurrence path-miss: the exact command the agent ran and the
// path it was looking for.
type pathMissEntry struct {
	command string
	path    string
}

// tripleEmitter is the minimal interface PathMissDetector needs to
// emit a tool.recovery.incident triple. graphutil.TripleWriter
// satisfies both methods. recoveryhint.Emit requires UpsertEntity
// (added in issue #154 slice #2 — migration off triple.add must-exist).
type tripleEmitter interface {
	WriteTriple(ctx context.Context, entityID, predicate string, object any) error
	UpsertEntity(ctx context.Context, entityType message.Type, entityID string, triples []message.Triple) error
}

// PathMissDetector tracks recent path-miss bash failures per task
// and emits a RETRY HINT when the same (command, path) repeats in
// immediate succession for a given task. Concurrency-safe.
//
// State is keyed by task_id from the agentic.ToolCall metadata. A
// successful command, a different path-miss, or a non-path-miss
// failure all clear the per-task entry — only an exact (cmd, path)
// repeat triggers the hint.
//
// When a tripleEmitter is configured (via WithTripleEmitter), each
// fire also emits a tool.recovery.incident triple set to the SKG so
// operators can query (role, model) recovery rates by tool. Nil-safe.
type PathMissDetector struct {
	mu      sync.Mutex
	entries map[string]pathMissEntry
	tw      tripleEmitter
}

// NewPathMissDetector returns a ready-to-use detector with empty state.
func NewPathMissDetector() *PathMissDetector {
	return &PathMissDetector{
		entries: make(map[string]pathMissEntry),
	}
}

// WithTripleEmitter installs a triple writer so per-fire SKG triples
// get written alongside the Prom counter increment and WARN log.
// When unset, fires still increment the counter and log at WARN, just
// without the SKG attribution. Nil-safe; returns the receiver for
// chaining.
func (d *PathMissDetector) WithTripleEmitter(tw tripleEmitter) *PathMissDetector {
	if d == nil {
		return nil
	}
	d.tw = tw
	return d
}

// CallContext carries the per-call attribution needed to emit a
// faithful tool.recovery.incident triple set. CallID is required
// when a triple emitter is configured; the rest are best-effort.
// All fields are sourced from agentic.ToolCall (LoopID + Metadata)
// at the executor boundary.
type CallContext struct {
	CallID string // loop ID making the call — required for triple emission
	Role   string // planner | developer | reviewer | ...
	Model  string // endpoint name from the registry
}

// Inspect classifies the current bash result and returns a RETRY HINT
// to prepend to the agent-facing error if the same command + path-miss
// has occurred in immediate succession for this taskID. Returns "" when
// no hint should be injected. Always updates per-task state for the
// next call.
//
// On a "suggested" fire, also emits a WARN log line and a
// tool.recovery.incident triple (best-effort; nil emitter is a no-op).
// Triple write failures are logged at WARN but do not propagate to the
// agent — the counter already reflects the fire.
//
// Nil-safe: a nil receiver returns "" without side effects.
func (d *PathMissDetector) Inspect(ctx context.Context, cc CallContext, taskID, command string, exitCode int, stderr string) string {
	if d == nil {
		return ""
	}
	if exitCode == 0 {
		d.mu.Lock()
		delete(d.entries, taskID)
		d.mu.Unlock()
		return ""
	}
	path := classifyPathMiss(stderr)
	if path == "" {
		d.mu.Lock()
		delete(d.entries, taskID)
		d.mu.Unlock()
		return ""
	}

	d.mu.Lock()
	prev, hadPrev := d.entries[taskID]
	if hadPrev && prev.command == command && prev.path == path {
		d.mu.Unlock()
		bashRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeSuggested).Inc()
		slog.Warn("bash path-miss recovery hint injected",
			"tool", "bash",
			"task_id", taskID,
			"path", path,
			"command", command,
			"role", cc.Role,
			"model", cc.Model,
			"call_id", cc.CallID,
		)
		d.emitIncident(ctx, cc, command, path, observability.ToolRecoveryOutcomeSuggested)
		return formatPathMissHint(path)
	}
	d.entries[taskID] = pathMissEntry{command: command, path: path}
	d.evictIfFull()
	d.mu.Unlock()
	bashRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeNotSuggested).Inc()
	d.emitIncident(ctx, cc, command, path, observability.ToolRecoveryOutcomeNotSuggested)
	return ""
}

// emitIncident writes a tool.recovery.incident triple set via
// recoveryhint.Emit. Best-effort; nil emitter is a no-op. Emit
// failures are logged at WARN only — the Prom counter and WARN log
// in Inspect already reflect the fire.
func (d *PathMissDetector) emitIncident(ctx context.Context, cc CallContext, command, path, outcome string) {
	if d.tw == nil || cc.CallID == "" {
		return
	}
	rc := recoveryhint.RecoveryContext{
		CallID:   cc.CallID,
		Role:     cc.Role,
		Model:    cc.Model,
		ToolName: "bash",
	}
	re := recoveryhint.RecoveryEvent{
		Outcome:       outcome,
		OriginalQuery: command,
	}
	if outcome == observability.ToolRecoveryOutcomeSuggested {
		re.Candidates = []string{path}
	}
	if _, err := recoveryhint.Emit(ctx, d.tw, rc, re); err != nil {
		slog.Warn("bash recovery triple emit failed",
			"tool", "bash",
			"call_id", cc.CallID,
			"error", err,
		)
	}
}

// evictIfFull drops one arbitrary entry when the tracker exceeds the
// hard cap. Caller holds d.mu. Map iteration order is randomized in
// Go, so the eviction victim is effectively arbitrary — fine for a
// staleness fallback that's only there to prevent unbounded growth.
func (d *PathMissDetector) evictIfFull() {
	if len(d.entries) <= maxTrackedTasks {
		return
	}
	for k := range d.entries {
		delete(d.entries, k)
		return
	}
}

// formatPathMissHint builds the RETRY HINT text the agent will see
// prepended to the original bash error. The hint names the missing
// path verbatim and offers two concrete commands the model can run
// next: a tracked-files grep (catches versioned files including
// renamed ones via path patterns) and a filesystem find (catches
// untracked / generated files the agent itself may have written).
func formatPathMissHint(path string) string {
	base := filepath.Base(path)
	return fmt.Sprintf(
		"RETRY HINT: path %q not found. Locate the real path before retrying:\n  git ls-files | grep %s\n  find . -type f -name %q\n",
		path, base, base,
	)
}
