// Package scenarioorchestrator provides the scenario-orchestrator component.
// It is the entry point for ADR-025 Phase 4 scenario execution.
//
// The orchestrator:
//  1. Receives an orchestration trigger for a plan (on scenario.orchestrate.<planSlug>).
//  2. Queries the graph for all Scenarios in the plan with status=pending or status=dirty.
//  3. Triggers a scenario-execution-loop workflow for each unmet Scenario.
//
// The actual decomposition and execution are handled by the scenario-executor
// component (processor/scenario-executor). This component is deliberately
// minimal — it dispatches, then ACKs.
package scenarioorchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semspec/workflow"
	"github.com/c360studio/semspec/workflow/graphutil"
	"github.com/c360studio/semspec/workflow/payloads"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	sscache "github.com/c360studio/semstreams/pkg/cache"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the scenario-orchestrator processor.
type Component struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger
	decoder    *message.Decoder

	// repoRoot is resolved once at construction from SEMSPEC_REPO_PATH or cwd.
	// It is used to load requirement and scenario data fresh from disk each dispatch cycle.
	repoRoot     string
	tripleWriter *graphutil.TripleWriter

	// sandbox resolves the per-requirement branch-derivation base. Nil when
	// SandboxURL is unset (tests / plans whose branch base is just the plan
	// base) — resolveRequirementBase then needs a sandbox only for the >1
	// prerequisite merge and fails loud if asked to merge without one.
	sandbox sandboxClient

	// completedReqs caches RequirementExecutionCompleteEvent data keyed by
	// RequirementID. Populated at startup via reconcileCompletedRequirements and
	// kept current by the completion event consumer. Prerequisite context for
	// downstream RequirementExecutionRequests is built from this cache.
	completedReqs sscache.Cache[*workflow.RequirementExecutionCompleteEvent]

	// Lifecycle
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc

	// Metrics
	triggersProcessed     atomic.Int64
	requirementsTriggered atomic.Int64
	triggersFailed        atomic.Int64
	lastActivityMu        sync.RWMutex
	lastActivity          time.Time
}

// NewComponent creates a new scenario-orchestrator processor.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Apply defaults for unset fields.
	defaults := DefaultConfig()
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.ConsumerName == "" {
		config.ConsumerName = defaults.ConsumerName
	}
	if config.TriggerSubject == "" {
		config.TriggerSubject = defaults.TriggerSubject
	}
	if config.ExecutionTimeout == "" {
		config.ExecutionTimeout = defaults.ExecutionTimeout
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = defaults.MaxConcurrent
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}

	return &Component{
		name:       "scenario-orchestrator",
		config:     config,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		decoder:    message.NewDecoder(deps.PayloadRegistry),
		repoRoot:   repoRoot,
		sandbox:    newSandboxClient(config.SandboxURL),
	}, nil
}

// resolveRepoRoot returns the repository root path, preferring SEMSPEC_REPO_PATH
// and falling back to the current working directory.
func resolveRepoRoot() (string, error) {
	if root := os.Getenv("SEMSPEC_REPO_PATH"); root != "" {
		return root, nil
	}
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	return root, nil
}

// Initialize prepares the component.
func (c *Component) Initialize() error {
	c.logger.Debug("initialized scenario-orchestrator",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"trigger_subject", c.config.TriggerSubject,
		"max_concurrent", c.config.MaxConcurrent)
	return nil
}

// Start begins consuming scenario orchestration triggers.
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("component already running")
	}
	if c.natsClient == nil {
		c.mu.Unlock()
		return fmt.Errorf("NATS client required")
	}

	c.running = true
	c.startTime = time.Now()

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	// Initialize completed-requirements cache. 4-hour TTL with 30-minute eviction
	// sweep matches execution-manager's active-execution cache sizing.
	cr, err := sscache.NewTTL[*workflow.RequirementExecutionCompleteEvent](ctx, 4*time.Hour, 30*time.Minute)
	if err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("init completed requirements cache: %w", err)
	}
	c.completedReqs = cr

	// Initialize TripleWriter for workflow state operations.
	c.tripleWriter = &graphutil.TripleWriter{
		NATSClient:    c.natsClient,
		Logger:        c.logger,
		ComponentName: "scenario-orchestrator",
	}

	// Backfill completed requirements from EXECUTION_STATES so prereq context
	// is available immediately after a restart without waiting for replay.
	c.reconcileCompletedRequirements(subCtx)

	// Push-based consumption — messages arrive via callback, no polling delay.
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName,
		ConsumerName:  c.config.ConsumerName,
		FilterSubject: c.config.TriggerSubject,
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AckWait:       c.config.GetExecutionTimeout() + 30*time.Second,
	}
	if err := c.natsClient.ConsumeStreamWithConfig(subCtx, cfg, c.handleTriggerPush); err != nil {
		c.rollbackStart(cancel)
		return fmt.Errorf("consume orchestration triggers: %w", err)
	}

	// KV watcher: EXECUTION_STATES req.> for completion caching.
	// Supplements the stream consumer with durable, replay-safe delivery.
	go c.watchReqCompletions(subCtx)

	c.logger.Info("scenario-orchestrator started",
		"stream", c.config.StreamName,
		"consumer", c.config.ConsumerName,
		"subject", c.config.TriggerSubject,
		"max_concurrent", c.config.MaxConcurrent)

	return nil
}

func (c *Component) rollbackStart(cancel context.CancelFunc) {
	c.mu.Lock()
	c.running = false
	c.cancel = nil
	c.mu.Unlock()
	cancel()
}

// handleTriggerPush is the push-based callback for ConsumeStreamWithConfig.
// Messages arrive immediately when published — no polling delay.
func (c *Component) handleTriggerPush(ctx context.Context, msg jetstream.Msg) {
	c.handleTrigger(ctx, msg)
}

// OrchestratorTrigger is the payload received on scenario.orchestrate.<planSlug>.
// It carries the plan slug. Scenarios and requirements are loaded from disk.
// OrchestratorTrigger is an alias for the registered payload type.
// Using the payload type directly ensures all fields survive deserialization.
type OrchestratorTrigger = payloads.ScenarioOrchestrationTrigger

// handleTrigger processes a single orchestration trigger message.
func (c *Component) handleTrigger(ctx context.Context, msg jetstream.Msg) {
	c.triggersProcessed.Add(1)
	c.updateLastActivity()

	base, err := c.decoder.Decode(msg.Data())
	if err != nil {
		c.logger.Error("failed to parse orchestration trigger envelope", "error", err)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message", "error", err)
		}
		return
	}

	trigger, ok := base.Payload().(*OrchestratorTrigger)
	if !ok {
		c.logger.Error("unexpected payload type in orchestration trigger",
			"type", fmt.Sprintf("%T", base.Payload()))
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message", "error", err)
		}
		return
	}

	if trigger.PlanSlug == "" {
		c.logger.Error("orchestration trigger missing plan_slug")
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message", "error", err)
		}
		return
	}

	c.logger.Info("orchestrating requirements",
		"plan_slug", trigger.PlanSlug,
		"trace_id", trigger.TraceID)

	// Apply execution timeout for the dispatch cycle.
	dispatchCtx, cancel := context.WithTimeout(ctx, c.config.GetExecutionTimeout())
	defer cancel()

	if err := c.dispatchRequirements(dispatchCtx, trigger); err != nil {
		c.logger.Error("requirement dispatch failed",
			"plan_slug", trigger.PlanSlug,
			"error", err)
		c.triggersFailed.Add(1)
		if err := msg.Nak(); err != nil {
			c.logger.Warn("failed to NAK message after dispatch error", "error", err)
		}
		return
	}

	if err := msg.Ack(); err != nil {
		c.logger.Warn("failed to ACK message", "error", err)
	}
}

// dispatchRequirements applies requirement-DAG gating and then triggers a
// requirement-execution-loop for each ready requirement using bounded concurrency
// controlled by config.MaxConcurrent.
//
// DAG gating logic:
//  1. A requirement is "complete" when its EXECUTION_STATES entry has stage
//     == "completed" (cached in c.completedReqs by req_completion_watcher,
//     re-confirmed via reconcileCompletedRequirements at dispatch time).
//  2. A requirement is "ready" when all its DependsOn requirements are complete
//     AND the requirement itself is not yet complete.
func (c *Component) dispatchRequirements(ctx context.Context, trigger *OrchestratorTrigger) error {
	if c.tripleWriter == nil {
		c.logger.Info("no KV store configured, skipping requirement dispatch", "plan_slug", trigger.PlanSlug)
		return nil
	}
	// Requirements and scenarios are populated by plan-manager from its cache.
	// No graph fallback — if the trigger doesn't carry the data, that's a bug.
	requirements := trigger.Requirements
	if len(requirements) == 0 {
		return fmt.Errorf("plan %s trigger has 0 requirements — plan-manager must populate the trigger payload", trigger.PlanSlug)
	}

	allScenarios := trigger.Scenarios
	if len(allScenarios) == 0 {
		return fmt.Errorf("plan %s has %d requirements but 0 scenarios — plan-manager must populate the trigger payload", trigger.PlanSlug, len(requirements))
	}

	// Apply DAG gating — only dispatch requirements whose upstream deps are
	// satisfied. Completion source-of-truth is EXECUTION_STATES.req.>.stage=="completed",
	// surfaced through c.completedReqs (populated by req_completion_watcher).
	//
	// Reconcile from EXECUTION_STATES first to close a race: plan-manager
	// re-fires scenario.orchestrate.<slug> immediately on a requirement
	// completion KV update, and the consumer here may run BEFORE
	// req_completion_watcher's independent goroutine has updated
	// c.completedReqs for that same update. Without the reconcile, a
	// just-completed requirement would look not-completed in cache, its
	// dependents would stay blocked, and the chain would deadlock until the
	// next completion. reconcileCompletedRequirements does a synchronous KV
	// scan so it always reflects the latest committed state.
	c.reconcileCompletedRequirements(ctx)

	completedReqIDs := make(map[string]bool, len(requirements))
	for _, r := range requirements {
		if _, ok := c.completedReqs.Get(r.ID); ok {
			completedReqIDs[r.ID] = true
		}
	}
	toDispatch := filterReadyRequirements(requirements, completedReqIDs)
	preStoryGateCount := len(toDispatch)
	toDispatch = filterByM2NStoryReservations(toDispatch, trigger.Stories)
	postStoryGateCount := len(toDispatch)
	// Branch-derivation gate: a dependent whose branch must fork from a
	// prerequisite owner's branch (including the cross-Story file-overlap edges
	// that live only on Story.DependsOn) is held until that owner completes, so
	// resolveRequirementBase never forks from a missing/empty branch.
	toDispatch = filterByBranchPrereqCompletion(toDispatch, trigger.Stories, completedReqIDs)

	c.logger.Info("requirement DAG gating applied",
		"plan_slug", trigger.PlanSlug,
		"total_requirements", len(requirements),
		"ready_count", len(toDispatch),
		"blocked_by_deps", len(requirements)-preStoryGateCount,
		"blocked_by_m_n_story_reservation", preStoryGateCount-postStoryGateCount,
		"blocked_by_branch_prereq", postStoryGateCount-len(toDispatch))

	if len(toDispatch) == 0 {
		return nil
	}

	// Group scenarios by requirement ID for dispatch.
	scenariosByReq := make(map[string][]workflow.Scenario, len(requirements))
	for _, s := range allScenarios {
		scenariosByReq[s.RequirementID] = append(scenariosByReq[s.RequirementID], s)
	}

	sem := make(chan struct{}, c.config.MaxConcurrent)
	var wg sync.WaitGroup
	errs := make(chan error, len(toDispatch))

	for _, req := range toDispatch {
		if ctx.Err() != nil {
			break
		}

		scenarios := scenariosByReq[req.ID]
		prereqs := c.buildPrereqContext(req, requirements)

		wg.Add(1)
		go func(r workflow.Requirement, sc []workflow.Scenario, deps []payloads.PrereqContext) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			if err := c.dispatchRequirement(ctx, trigger, r, sc, deps); err != nil {
				errs <- err
			} else {
				c.requirementsTriggered.Add(1)
			}
		}(req, scenarios, prereqs)
	}

	wg.Wait()
	close(errs)

	var firstErr error
	for err := range errs {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// dispatchRequirement resolves a requirement's DependsOn-derived branch base
// and triggers its execution. Branch derivation runs here (not in the executor)
// so the >1-prerequisite reqbase merge happens before the mutation and the
// executor sees a single ready base. A resolution failure (e.g. a missing
// prereq branch) is fatal for this requirement — dispatching it anyway would
// fork from the wrong base and collide at plan-level assembly, the exact bug
// this fix removes.
func (c *Component) dispatchRequirement(
	ctx context.Context,
	trigger *OrchestratorTrigger,
	req workflow.Requirement,
	scenarios []workflow.Scenario,
	prereqs []payloads.PrereqContext,
) error {
	base, err := c.resolveRequirementBase(ctx, req, trigger.Stories, trigger.PlanBranch)
	if err != nil {
		c.logger.Error("failed to resolve requirement branch base",
			"requirement_id", req.ID, "error", err)
		return err
	}
	if err := c.triggerRequirementExecution(ctx, trigger.PlanSlug, trigger.TraceID, trigger.PlanBranch, base, req, scenarios, prereqs); err != nil {
		// A "req execution already exists" rejection is a benign idempotency
		// outcome, not a failure (issue #180): when plan-manager re-fires the
		// orchestrator on a completion AND the DAG gate independently re-dispatches
		// a now-ready requirement, both paths race to req.create and the executor's
		// dedup correctly rejects the loser. The requirement got exactly one
		// execution. Treat it as success — Debug-log and return nil — so the
		// orchestrate cycle ACKs (no Error-level noise, and no NAK→redelivery churn
		// that would re-run the whole cycle and re-log on every redelivery).
		if isIdempotentReqRejection(err) {
			c.logger.Debug("requirement already dispatched (idempotent re-fire), skipping",
				"requirement_id", req.ID, "detail", err)
			return nil
		}
		c.logger.Error("failed to trigger requirement execution",
			"requirement_id", req.ID, "error", err)
		return err
	}
	return nil
}

// isIdempotentReqRejection reports whether a dispatch error is the benign
// "execution already exists" rejection the executor returns when a requirement
// is dispatched twice (re-fire + DAG-gate race). The marker crosses a NATS
// request/reply boundary as a plain string (execution-manager mutations.go
// "req execution already exists: <key>"), so it is matched on the message rather
// than a typed sentinel.
func isIdempotentReqRejection(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already exists")
}

// buildPrereqContext assembles PrereqContext for a requirement's DependsOn list
// from the cached completion events. Falls back to requirement metadata only
// when completion data is unavailable.
func (c *Component) buildPrereqContext(req workflow.Requirement, allReqs []workflow.Requirement) []payloads.PrereqContext {
	if len(req.DependsOn) == 0 {
		return nil
	}

	// Index all requirements for metadata lookup.
	reqIndex := make(map[string]workflow.Requirement, len(allReqs))
	for _, r := range allReqs {
		reqIndex[r.ID] = r
	}

	var prereqs []payloads.PrereqContext
	for _, depID := range req.DependsOn {
		pc := payloads.PrereqContext{RequirementID: depID}

		// Try cached completion event first (has files + summary).
		if evt, ok := c.completedReqs.Get(depID); ok {
			pc.Title = evt.Title
			pc.Description = evt.Description
			pc.FilesModified = evt.FilesModified
			pc.Summary = evt.Summary
		} else if dep, ok := reqIndex[depID]; ok {
			// Fallback: requirement metadata only.
			pc.Title = dep.Title
			pc.Description = dep.Description
		}

		prereqs = append(prereqs, pc)
	}
	return prereqs
}

// triggerRequirementExecution sends a req.create mutation to execution-manager,
// which writes the requirement execution to EXECUTION_STATES with stage=pending.
// The requirement-executor's KV watcher picks up the pending entry and starts
// decomposition.
func (c *Component) triggerRequirementExecution(
	ctx context.Context,
	planSlug, traceID, planBranch, baseBranch string,
	req workflow.Requirement,
	scenarios []workflow.Scenario,
	prereqs []payloads.PrereqContext,
) error {
	mutReq := map[string]any{
		"slug":           planSlug,
		"requirement_id": req.ID,
		"title":          req.Title,
		"description":    req.Description,
		"scenarios":      scenarios,
		"depends_on":     prereqs,
		"trace_id":       traceID,
		"plan_branch":    planBranch,
		"base_branch":    baseBranch,
	}

	data, err := json.Marshal(mutReq)
	if err != nil {
		return fmt.Errorf("marshal req.create mutation: %w", err)
	}

	respData, err := c.natsClient.RequestWithRetry(ctx, "execution.mutation.req.create", data, 5*time.Second, natsclient.DefaultRetryConfig())
	if err != nil {
		return fmt.Errorf("req.create mutation failed: %w", err)
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal req.create response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("req.create rejected: %s", resp.Error)
	}

	c.logger.Info("Triggered requirement execution via mutation",
		"requirement_id", req.ID,
		"plan_slug", planSlug,
		"scenario_count", len(scenarios),
		"prereq_count", len(prereqs),
	)
	return nil
}

// reconcileCompletedRequirements backfills completedReqs from EXECUTION_STATES on
// startup. This is best-effort — any failure logs at debug level and returns
// without blocking the component from starting.
func (c *Component) reconcileCompletedRequirements(ctx context.Context) {
	reconcileCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	bucket, err := c.natsClient.GetKeyValueBucket(reconcileCtx, "EXECUTION_STATES")
	if err != nil {
		c.logger.Debug("EXECUTION_STATES not available for reconciliation", "error", err)
		return
	}
	kvStore := c.natsClient.NewKVStore(bucket)

	keys, err := kvStore.Keys(reconcileCtx)
	if err != nil || len(keys) == 0 {
		c.logger.Debug("No execution states to reconcile", "error", err)
		return
	}

	recovered := 0
	// completedNow is the authoritative set of currently-completed requirement
	// IDs from this scan. After the loop we EVICT any cached completion not in
	// it (when the scan was clean — see scanClean), so a requirement that a
	// QA-recovery reopen reset (its KV entry deleted or moved off "completed")
	// stops counting as complete and is re-dispatched. Without eviction the cache
	// is additive-only and a reset dependent would be permanently skipped by
	// filterReadyRequirements — the P3 recovery-staleness bug.
	completedNow := make(map[string]struct{})
	// scanClean stays true only if EVERY req.* key was read + parsed. A single
	// per-key Get/unmarshal failure (or a context-deadline mid-scan) makes
	// completedNow an undercount, so we must NOT evict on it — that would drop
	// still-completed reqs from the SHARED cross-plan cache and wrongly block
	// their dependents in the very same sweep. A partial scan is treated like the
	// empty-keys early return: skip eviction, retry next sweep.
	scanClean := true
	for _, key := range keys {
		if !strings.HasPrefix(key, "req.") {
			continue
		}

		entry, err := kvStore.Get(reconcileCtx, key)
		if err != nil {
			scanClean = false
			continue
		}

		var reqExec workflow.RequirementExecution
		if err := json.Unmarshal(entry.Value, &reqExec); err != nil {
			scanClean = false
			continue
		}

		// Only cache completed requirements — the consumer handles live updates.
		if reqExec.Stage != "completed" {
			continue
		}
		completedNow[reqExec.RequirementID] = struct{}{}

		// Synthesize a completion event from the durable execution state.
		// FilesModified and Summary are aggregated from NodeResults; Title and
		// Description come directly from the execution record.
		var filesModified []string
		var summaries []string
		for _, nr := range reqExec.NodeResults {
			filesModified = append(filesModified, nr.FilesModified...)
			if nr.Summary != "" {
				summaries = append(summaries, nr.Summary)
			}
		}

		evt := &workflow.RequirementExecutionCompleteEvent{
			Slug:          reqExec.Slug,
			RequirementID: reqExec.RequirementID,
			Title:         reqExec.Title,
			Description:   reqExec.Description,
			ProjectID:     reqExec.ProjectID,
			TraceID:       reqExec.TraceID,
			Outcome:       "completed",
			NodeCount:     reqExec.NodeCount,
			FilesModified: filesModified,
			Summary:       strings.Join(summaries, "; "),
		}

		c.completedReqs.Set(reqExec.RequirementID, evt) //nolint:errcheck
		recovered++
	}

	// Evict cached completions whose KV state is no longer "completed" (reset /
	// reopened for recovery) — but ONLY when the scan was clean and complete, so
	// a partial scan can never mass-evict still-completed reqs and strand their
	// dependents until the next trigger. On a partial scan the cache is left
	// as-is (additive); the next clean sweep evicts.
	evicted := 0
	if scanClean && reconcileCtx.Err() == nil {
		for _, id := range staleCompletions(c.completedReqs.Keys(), completedNow) {
			// A stale candidate is absent from the point-in-time key snapshot. It
			// could be genuinely reset/reopened (evict) OR a req that COMPLETED
			// after the snapshot and was cached by the concurrent completion
			// watcher (keep). Re-read live KV to tell them apart, closing a TOCTOU
			// that would otherwise transiently re-block that req's dependents.
			if c.requirementStillCompleted(reconcileCtx, kvStore, id) {
				continue
			}
			if deleted, _ := c.completedReqs.Delete(id); deleted {
				evicted++
			}
		}
	}

	if recovered > 0 || evicted > 0 {
		c.logger.Info("Reconciled completed requirements from EXECUTION_STATES",
			"recovered", recovered, "evicted", evicted)
	}
}

// requirementStillCompleted re-reads the cached requirement's live KV entry and
// reports whether it is currently stage=="completed". Used to spare an eviction
// candidate that completed AFTER the reconcile key snapshot (cached by the
// concurrent completion watcher) — a not-found or non-completed read means the
// entry was genuinely reset/reopened and should be evicted. The cached
// completion event carries Slug + RequirementID (both Set sites populate them),
// so the full KV key can be reconstructed.
func (c *Component) requirementStillCompleted(ctx context.Context, kvStore *natsclient.KVStore, reqID string) bool {
	evt, ok := c.completedReqs.Get(reqID)
	if !ok || evt == nil {
		return false
	}
	entry, err := kvStore.Get(ctx, workflow.RequirementExecutionKey(evt.Slug, evt.RequirementID))
	if err != nil {
		return false
	}
	var re workflow.RequirementExecution
	if err := json.Unmarshal(entry.Value, &re); err != nil {
		return false
	}
	return re.Stage == "completed"
}

// Stop gracefully stops the component.
func (c *Component) Stop(_ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}

	c.running = false
	c.logger.Info("scenario-orchestrator stopped",
		"triggers_processed", c.triggersProcessed.Load(),
		"requirements_triggered", c.requirementsTriggered.Load(),
		"triggers_failed", c.triggersFailed.Load())

	return nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "scenario-orchestrator",
		Type:        "processor",
		Description: "Dispatches scenario-execution-loop workflows for pending Scenarios in a plan (ADR-025 Phase 4)",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions.
func (c *Component) InputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Inputs))
	for i, portDef := range c.config.Ports.Inputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionInput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions.
func (c *Component) OutputPorts() []component.Port {
	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, len(c.config.Ports.Outputs))
	for i, portDef := range c.config.Ports.Outputs {
		ports[i] = component.Port{
			Name:        portDef.Name,
			Direction:   component.DirectionOutput,
			Required:    portDef.Required,
			Description: portDef.Description,
			Config: component.NATSPort{
				Subject: portDef.Subject,
			},
		}
	}
	return ports
}

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return orchestratorSchema
}

// Health returns the current health status.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	running := c.running
	startTime := c.startTime
	c.mu.RUnlock()

	status := "stopped"
	if running {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(c.triggersFailed.Load()),
		Uptime:     time.Since(startTime),
		Status:     status,
	}
}

// DataFlow returns current data flow metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      c.getLastActivity(),
	}
}

// IsRunning returns whether the component is running.
func (c *Component) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

func (c *Component) updateLastActivity() {
	c.lastActivityMu.Lock()
	c.lastActivity = time.Now()
	c.lastActivityMu.Unlock()
}

func (c *Component) getLastActivity() time.Time {
	c.lastActivityMu.RLock()
	defer c.lastActivityMu.RUnlock()
	return c.lastActivity
}
