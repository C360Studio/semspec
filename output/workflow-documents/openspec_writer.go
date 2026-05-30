package workflowdocuments

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semspec/output/workflow-documents/openspec"
	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// openSpecDebounceInterval is how long writeOpenSpecArtifacts coalesces
// EXECUTION_STATES updates before re-rendering tasks.md for a given slug.
// Per ADR-040 Q3 the operator-confirmed cadence is 1s — long enough to
// absorb a burst from per-node completion of a decomposed requirement,
// short enough to feel live in a tail of tasks.md.
const openSpecDebounceInterval = 1 * time.Second

// executionStatesBucket is the KV bucket name execution-manager writes to
// for per-requirement execution state. Lifted from the literal in
// snapshotRequirementExecutions per go-reviewer PR 3 suggestion (#7) for
// consistency with the planStateBucket field pattern.
const executionStatesBucket = "EXECUTION_STATES"

// writeOpenSpecArtifacts renders the OpenSpec directory under
// `.semspec/plans/<slug>/openspec/` from the current plan state.
// Returns the list of files written (relative to planDir) so the
// surrounding writePlanDocuments can include them in its info log.
//
// Skipped (returns nil) when the plan has no Exploration — legacy plans
// produce no OpenSpec output, which is the back-compat contract.
//
// Per-file behavior:
//   - proposal.md / specs/<cap>/spec.md / design.md re-emit on every call
//     so plan-state mutations land immediately. .openspec.yaml writes
//     once (idempotent — written only when absent).
//   - tasks.md is rendered with empty execs map here (pre-execution
//     snapshot). The EXECUTION_STATES watcher (watchExecutionStates) is
//     what flips checkboxes live; writePlanDocuments runs on PLAN_STATES
//     milestones which precede execution.
//
// Write errors are logged but don't abort the rest of the artifacts.
func (c *Component) writeOpenSpecArtifacts(plan *workflow.Plan, planDir string) []string {
	if plan == nil || plan.Exploration == nil || len(plan.Exploration.Capabilities) == 0 {
		return nil
	}
	osDir := filepath.Join(planDir, "openspec")
	if err := os.MkdirAll(osDir, 0o755); err != nil {
		c.logger.Error("openspec: create dir failed", "slug", plan.Slug, "error", err)
		c.writeErrors.Add(1)
		return nil
	}
	var written []string

	writeIf := func(name, content string) {
		if content == "" {
			return
		}
		full := filepath.Join(osDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			c.logger.Error("openspec: create dir for artifact failed",
				"slug", plan.Slug, "file", name, "error", err)
			c.writeErrors.Add(1)
			return
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			c.logger.Error("openspec: write artifact failed",
				"slug", plan.Slug, "file", name, "error", err)
			c.writeErrors.Add(1)
			return
		}
		written = append(written, filepath.Join("openspec", name))
	}

	// proposal.md, design.md, tasks.md (initial unchecked snapshot).
	writeIf("proposal.md", openspec.RenderProposal(plan))
	writeIf("design.md", openspec.RenderDesign(plan))
	writeIf("tasks.md", openspec.RenderTasks(plan, c.snapshotRequirementExecutions(plan.Slug)))

	// Per-capability specs/<name>/spec.md
	for _, capName := range openspec.ListCapabilityNames(plan) {
		writeIf(filepath.Join("specs", capName, "spec.md"), openspec.RenderSpec(plan, capName))
	}

	// .openspec.yaml — idempotent metadata, write only when missing.
	yamlPath := filepath.Join(osDir, ".openspec.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		if err := os.WriteFile(yamlPath, []byte(openspec.RenderOpenSpecYAML()), 0o644); err != nil {
			c.logger.Error("openspec: write .openspec.yaml failed",
				"slug", plan.Slug, "error", err)
			c.writeErrors.Add(1)
		} else {
			written = append(written, "openspec/.openspec.yaml")
		}
	}

	return written
}

// openSpecDebouncer coalesces per-slug tasks.md re-renders triggered by
// EXECUTION_STATES updates. Per-slug entries gate against concurrent
// renders of the same slug — a schedule arriving during an active render
// re-arms the existing goroutine rather than spawning a parallel one.
// Different slugs run in parallel.
//
// Concurrency contract (go-reviewer PR 3 audit):
//   - At most ONE goroutine per slug. The entry stays in `d.pending` for
//     the entire lifetime of that goroutine (wait + render + recheck).
//   - A schedule that arrives during the wait phase sends on `entry.kick`
//     to reset the debounce window.
//   - A schedule that arrives during the render phase ALSO sends on
//     `entry.kick`. The goroutine drains the kick channel after render
//     and loops for another wait+render cycle — guarantees the latest
//     state is materialized.
//   - The stored render closure is captured at first schedule. Subsequent
//     schedules during the same goroutine lifetime do NOT replace it.
//     Callers must pass closures that re-resolve their inputs at fire
//     time (e.g. via `func() { c.rerenderOpenSpecTasks(ctx, slug) }`)
//     rather than baking state into the closure body.
type openSpecDebouncer struct {
	mu      sync.Mutex
	pending map[string]*pendingDebounceEntry
}

// pendingDebounceEntry holds the kick channel for one slug's goroutine.
// Kept simple — kick is the sole signal; map presence is the lifecycle.
type pendingDebounceEntry struct {
	kick chan struct{}
}

func newOpenSpecDebouncer() *openSpecDebouncer {
	return &openSpecDebouncer{pending: make(map[string]*pendingDebounceEntry)}
}

// schedule arms a debounced render for slug. See openSpecDebouncer for
// the per-slug serialization contract.
func (d *openSpecDebouncer) schedule(slug string, render func()) {
	d.mu.Lock()
	if entry, ok := d.pending[slug]; ok {
		// Goroutine already running for this slug — kick it.
		select {
		case entry.kick <- struct{}{}:
		default:
			// Channel full (capacity 1). Existing kick is unconsumed;
			// goroutine will pick it up on next loop iteration.
		}
		d.mu.Unlock()
		return
	}
	entry := &pendingDebounceEntry{kick: make(chan struct{}, 1)}
	d.pending[slug] = entry
	d.mu.Unlock()

	go func() {
		for {
			// Wait phase — reset on every kick, fire when the window elapses.
			waiting := true
			for waiting {
				select {
				case <-entry.kick:
					// Reset the wait window.
				case <-time.After(openSpecDebounceInterval):
					waiting = false
				}
			}

			// Fire phase — entry STAYS in d.pending so late schedulers
			// re-arm this same goroutine rather than spawning a parallel one.
			render()

			// Post-render check — did a schedule land while we were rendering?
			d.mu.Lock()
			select {
			case <-entry.kick:
				// Yes. Loop back for another wait+render cycle.
				d.mu.Unlock()
				continue
			default:
				// No. Clean up and exit.
				delete(d.pending, slug)
				d.mu.Unlock()
				return
			}
		}
	}()
}

// snapshotRequirementExecutions reads EXECUTION_STATES and returns the
// current RequirementExecution map keyed by RequirementID for the given
// plan slug. Returns nil on error or when no NATS access (test paths) so
// the renderer falls back to all-unchecked output.
func (c *Component) snapshotRequirementExecutions(slug string) map[string]workflow.RequirementExecution {
	if c.natsClient == nil {
		return nil
	}
	js, err := c.natsClient.JetStream()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	bucket, err := js.KeyValue(ctx, executionStatesBucket)
	if err != nil {
		return nil
	}
	keys, err := bucket.ListKeys(ctx)
	if err != nil {
		return nil
	}
	prefix := "req." + slug + "."
	out := make(map[string]workflow.RequirementExecution)
	for k := range keys.Keys() {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		entry, err := bucket.Get(ctx, k)
		if err != nil {
			continue
		}
		var re workflow.RequirementExecution
		if err := json.Unmarshal(entry.Value(), &re); err != nil {
			continue
		}
		out[re.RequirementID] = re
	}
	return out
}

// watchExecutionStates watches EXECUTION_STATES KV for requirement
// execution updates. On each update, schedules a debounced tasks.md
// re-render for the affected slug. Runs in its own goroutine; exits
// when ctx is cancelled.
//
// ADR-040 Move 3 Q4: tasks.md checkboxes flip live as execution-manager
// completes nodes. This watcher is what makes that happen.
func (c *Component) watchExecutionStates(ctx context.Context, js jetstream.JetStream) {
	bucket, err := workflow.WaitForKVBucket(ctx, js, executionStatesBucket)
	if err != nil {
		c.logger.Warn("EXECUTION_STATES not available — openspec tasks.md will not flip live",
			"error", err)
		return
	}
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("Failed to watch EXECUTION_STATES", "error", err)
		return
	}
	defer watcher.Stop()

	c.logger.Info("Watching EXECUTION_STATES for openspec tasks.md live-flip")

	for entry := range watcher.Updates() {
		if entry == nil {
			continue
		}
		if entry.Operation() != jetstream.KeyValuePut {
			continue
		}
		// Keys are "req.<slug>.<requirementID>" or "task.<slug>.<taskID>".
		// Only the req.* keys move the tasks.md checkbox state at the
		// requirement granularity we render — skip task.* updates.
		key := entry.Key()
		if !strings.HasPrefix(key, "req.") {
			continue
		}
		parts := strings.SplitN(key, ".", 3)
		if len(parts) < 3 {
			continue
		}
		slug := parts[1]
		c.openSpecDebouncer.schedule(slug, func() {
			c.rerenderOpenSpecTasks(ctx, slug)
		})
	}
}

// rerenderOpenSpecTasks loads the plan + current execution snapshot and
// rewrites tasks.md. No-ops when the plan can't be loaded (e.g. KV blip)
// — the next mutation re-arms the debouncer.
func (c *Component) rerenderOpenSpecTasks(ctx context.Context, slug string) {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return
	}
	bucket, err := js.KeyValue(ctx, c.planStateBucket)
	if err != nil {
		return
	}
	entry, err := bucket.Get(ctx, slug)
	if err != nil {
		return
	}
	var plan workflow.Plan
	if err := json.Unmarshal(entry.Value(), &plan); err != nil {
		return
	}
	if plan.Exploration == nil {
		return // legacy plan
	}
	content := openspec.RenderTasks(&plan, c.snapshotRequirementExecutions(slug))
	if content == "" {
		return
	}
	tasksPath := filepath.Join(c.baseDir, ".semspec", "plans", slug, "openspec", "tasks.md")
	if err := os.MkdirAll(filepath.Dir(tasksPath), 0o755); err != nil {
		c.logger.Warn("openspec live-flip: mkdir failed", "slug", slug, "error", err)
		return
	}
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		c.logger.Warn("openspec live-flip: write failed", "slug", slug, "error", err)
		return
	}
	c.logger.Debug("openspec tasks.md re-rendered", "slug", slug)
}
