package terminal

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/c360studio/semspec/vocabulary/observability"
	"github.com/c360studio/semspec/workflow/recoveryhint"
	"github.com/c360studio/semstreams/metric"
	"github.com/prometheus/client_golang/prometheus"
)

// plannerScopeRecoveryCounter mirrors graph_query and bash recovery
// counter shapes (see tools/workflow/graph.go and tools/bash/recovery.go).
// Labeled by ToolRecoveryOutcome:
//   - suggested: planner submitted scope.include with one or more paths
//     that don't exist on disk; we returned a directive RETRY HINT
//     telling the model to move them to scope.create.
//   - not_suggested: planner submit_work was checked and every
//     scope.include path resolved on disk (no recovery needed).
//
// Operators read deltas via /metrics to see how often planners are
// confusing scope.include with scope.create across runs.
var plannerScopeRecoveryCounter = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "semspec_planner_scope_recovery_total",
		Help: "Total fires of the planner scope.include vs scope.create recovery hint. Labeled by outcome: suggested (RETRY HINT injected — planner listed nonexistent paths in scope.include) or not_suggested (every path resolved on disk; no recovery needed).",
	},
	[]string{"outcome"},
)

func init() {
	plannerScopeRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeSuggested)
	plannerScopeRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeNotSuggested)
}

// RegisterScopeValidatorMetrics registers the planner scope-validator
// recovery counter. Folded into terminal.RegisterMetrics so callers
// don't need a second registration call. Idempotent. Nil-safe.
func registerScopeValidatorMetrics(reg *metric.MetricsRegistry) error {
	if reg == nil {
		return nil
	}
	if err := reg.RegisterCounterVec("planner_scope", "recovery_total", plannerScopeRecoveryCounter); err != nil {
		return fmt.Errorf("register planner scope recovery counter: %w", err)
	}
	return nil
}

// scopeValidatorTripleEmitter is the minimal interface needed to
// emit a tool.recovery.incident triple for planner scope misses.
// Identical to tools/bash's tripleEmitter; kept local to avoid a
// cross-package import.
type scopeValidatorTripleEmitter interface {
	WriteTriple(ctx context.Context, entityID, predicate string, object any) error
}

// extractScopeIncludePaths pulls the scope.include array from a plan
// deliverable. Returns nil when the field is absent, malformed, or
// empty — callers treat all three identically (nothing to validate).
func extractScopeIncludePaths(d map[string]any) []string {
	scope, ok := d["scope"].(map[string]any)
	if !ok {
		return nil
	}
	include, ok := scope["include"].([]any)
	if !ok || len(include) == 0 {
		return nil
	}
	paths := make([]string, 0, len(include))
	for _, p := range include {
		if s, ok := p.(string); ok && s != "" {
			paths = append(paths, s)
		}
	}
	return paths
}

// findMissingScopePaths returns the subset of include paths that do
// not exist on disk under workDir. Paths are resolved relative to
// workDir; absolute paths are checked verbatim. An empty workDir
// disables the check (returns nil) — semspec normally sets
// SEMSPEC_REPO_PATH so this only short-circuits in tests or
// misconfigured deployments.
func findMissingScopePaths(workDir string, paths []string) []string {
	if workDir == "" || len(paths) == 0 {
		return nil
	}
	var missing []string
	for _, p := range paths {
		full := p
		if !filepath.IsAbs(full) {
			full = filepath.Join(workDir, p)
		}
		if _, err := os.Stat(full); err != nil {
			// Treat any stat failure (not-exists, permission, etc.) as
			// "doesn't exist as a usable path" — the model's intent was
			// "this file is part of the scope" and stat-failure means
			// it can't be. False positives on permission errors are
			// extremely rare in our agent fixtures (workDir is the
			// agent's own workspace) and would surface as a directive
			// hint that's still useful to read.
			missing = append(missing, p)
		}
	}
	return missing
}

// formatScopeMissHint builds the directive RETRY HINT the planner
// will see if it submits scope.include paths that don't exist.
// Names every offending path verbatim and tells the model exactly
// what move to make next.
func formatScopeMissHint(missing []string) string {
	return fmt.Sprintf(
		"RETRY HINT: scope.include lists %d path(s) that do not exist on disk: %v. Move these to scope.create if you intend to create them as part of this plan, or remove them if they were a reference error. scope.include is for files that already exist and may be modified; scope.create is for new files.",
		len(missing), missing,
	)
}

// validatePlanScope runs the structural scope check at the planner
// submit_work boundary: every path under args.scope.include must
// exist on disk under workDir. On miss, increments the counter,
// emits a WARN log + SKG triple (best-effort), and returns the
// directive hint string the agent will see.
//
// Returns "" when there's nothing to flag — counter still increments
// the not_suggested label so we have visibility into how often the
// path was clean.
func validatePlanScope(ctx context.Context, workDir string, tw scopeValidatorTripleEmitter, cc CallContext, args map[string]any) string {
	paths := extractScopeIncludePaths(args)
	if len(paths) == 0 {
		// No scope.include to validate — neutral outcome, no counter.
		return ""
	}

	missing := findMissingScopePaths(workDir, paths)
	if len(missing) == 0 {
		plannerScopeRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeNotSuggested).Inc()
		emitScopeIncident(ctx, tw, cc, paths, nil, observability.ToolRecoveryOutcomeNotSuggested)
		return ""
	}

	plannerScopeRecoveryCounter.WithLabelValues(observability.ToolRecoveryOutcomeSuggested).Inc()
	slog.Warn("planner scope.include validation failed",
		"tool", "submit_work",
		"deliverable_type", "plan",
		"missing_paths", missing,
		"role", cc.Role,
		"model", cc.Model,
		"call_id", cc.CallID,
	)
	emitScopeIncident(ctx, tw, cc, paths, missing, observability.ToolRecoveryOutcomeSuggested)
	return formatScopeMissHint(missing)
}

// emitScopeIncident writes a tool.recovery.incident triple set to
// the SKG via recoveryhint.Emit. Best-effort; nil emitter or empty
// CallID is a no-op. Triple-write failures log at WARN only.
func emitScopeIncident(ctx context.Context, tw scopeValidatorTripleEmitter, cc CallContext, allPaths, missing []string, outcome string) {
	if tw == nil || cc.CallID == "" {
		return
	}
	originalQuery := fmt.Sprintf("scope.include=%v", allPaths)
	rc := recoveryhint.RecoveryContext{
		CallID:   cc.CallID,
		Role:     cc.Role,
		Model:    cc.Model,
		ToolName: "submit_work",
	}
	re := recoveryhint.RecoveryEvent{
		Outcome:       outcome,
		OriginalQuery: originalQuery,
		Candidates:    missing, // only populated on suggested
	}
	if _, err := recoveryhint.Emit(ctx, tw, rc, re); err != nil {
		slog.Warn("planner scope recovery triple emit failed",
			"tool", "submit_work",
			"call_id", cc.CallID,
			"error", err,
		)
	}
}
