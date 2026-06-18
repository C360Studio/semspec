package planmanager

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/c360studio/semspec/workflow"
)

// Level-0 completeness gate (#204).
//
// The planner records the files a plan intends to create in plan.Scope.Create.
// That list survives faithfully through architecture and story preparation, and
// is even carried into each DAG node's file_scope — but as a PERMISSION ("you
// may edit these"), never as an ACCEPTANCE ("this plan MUST deliver these").
// Node synthesis and review are scenario-driven, so a developer can satisfy thin
// scenarios while leaving most declared files untouched, and nothing turns the
// undelivered files into a required deliverable. That is how the 2026-06-16
// hybrid-gpt5 run shipped 8 of ~30 declared files and still reached QA.
//
// This gate runs at implementing-convergence, after the per-requirement branches
// are assembled, and converts scope.create from permission into a minimum
// acceptance contract: every declared file must exist on the assembled branch
// unless it was explicitly removed by a planning decision (which mutates
// scope.create). A gap fails the plan closed to a recoverable rejected state
// (same outcome as a QA needs_changes verdict) rather than advancing to QA on an
// incomplete deliverable. Deterministic — pure set difference over git-tracked
// paths, no LLM.

// undeliveredScopeFiles returns the entries in declared (plan.Scope.Create) that
// have no corresponding path in delivered (files present on the assembled
// branch). Both sides are path.Clean-normalized so "./a" and "a" match. Blank
// entries are ignored. Order is preserved over declared for stable messages.
func undeliveredScopeFiles(declared, delivered []string) []string {
	have := make(map[string]struct{}, len(delivered))
	for _, d := range delivered {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		have[path.Clean(d)] = struct{}{}
	}
	var missing []string
	for _, d := range declared {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if _, ok := have[path.Clean(d)]; !ok {
			missing = append(missing, d)
		}
	}
	return missing
}

// scopeCompletenessGap lists declared scope.create files that were not delivered
// on the assembled branch. Returns (nil, nil) — gate disabled — when there is no
// sandbox, no assembled branch, or an empty scope.create (matching the
// assemble step's own no-op conditions, so test environments that mock less of
// the world are unaffected). A sandbox error is returned so the caller can treat
// it as infra (stall), not as a completeness violation.
func (c *Component) scopeCompletenessGap(ctx context.Context, plan *workflow.Plan) ([]string, error) {
	if c.sandbox == nil || len(plan.Scope.Create) == 0 {
		// No sandbox → can't inspect the tree (test envs / no-sandbox runs). No
		// declared creates → nothing to enforce. Either way the gate is a no-op.
		return nil, nil
	}
	var delivered []string
	if plan.AssembledBranch != "" {
		var err error
		delivered, err = c.assembledTrackedFiles(ctx, plan)
		if err != nil {
			return nil, err
		}
	}
	// AssembledBranch=="" means assembleRequirementBranches merged ZERO owner
	// branches (e.g. mis-derived M:N ownership) — no work was assembled, so
	// delivered stays empty and every declared scope.create file is undelivered.
	// Failing closed here (rather than the prior no-op) is the point: a zero-work
	// deliverable with a non-empty declared scope is the exact false-green the
	// gate exists to stop.
	return undeliveredScopeFiles(plan.Scope.Create, delivered), nil
}

// assembledTrackedFiles returns the git-tracked files on the assembled branch by
// running `git ls-files` in the staged QA worktree (checked out from
// plan.AssembledBranch by assembleAndStageQAWorktree). Ground truth for what the
// deliverable actually contains.
func (c *Component) assembledTrackedFiles(ctx context.Context, plan *workflow.Plan) ([]string, error) {
	qaID := workflow.QAWorktreeID(plan.Slug)
	// -z → NUL-separated paths; core.quotePath=false → non-ASCII/space paths are
	// emitted raw (not octal-escaped or double-quoted), so they string-match the
	// scope.create entries instead of producing false ScopeIncomplete misses.
	res, err := c.sandbox.Exec(ctx, qaID, "git -c core.quotePath=false ls-files -z", lsFilesTimeoutMs)
	if err != nil {
		return nil, fmt.Errorf("list assembled files in worktree %q: %w", qaID, err)
	}
	if res == nil || res.ExitCode != 0 {
		code := -1
		if res != nil {
			code = res.ExitCode
		}
		return nil, fmt.Errorf("git ls-files in worktree %q exited %d", qaID, code)
	}
	raw := strings.Trim(res.Stdout, "\x00")
	if raw == "" {
		return nil, nil // empty tree → no delivered files (NOT []string{""})
	}
	return strings.Split(raw, "\x00"), nil
}

const lsFilesTimeoutMs = 15000

// failPlanOnIncompleteScope fails the plan closed when declared scope.create
// files were not delivered. Mirrors the QA needs_changes outcome: records a
// fixable ScopeIncomplete PlanDecision listing the missing files and sets the
// plan to rejected (retry-eligible — POST /plans/{slug}/retry re-drives
// execution to deliver them, or a planning decision can revise scope). The plan
// does NOT advance to QA. Caller must NOT also transition to ready_for_qa.
func (c *Component) failPlanOnIncompleteScope(ctx context.Context, plan *workflow.Plan, missing []string) {
	now := time.Now()
	plan.LastError = fmt.Sprintf(
		"Level-0 completeness gate: %d of %d declared scope.create file(s) not delivered on the assembled branch: %s",
		len(missing), len(plan.Scope.Create), strings.Join(missing, ", "))
	plan.LastErrorAt = &now

	affected := make([]string, 0, len(plan.Requirements))
	for _, r := range plan.Requirements {
		affected = append(affected, r.ID)
	}
	plan.PlanDecisions = append(plan.PlanDecisions, workflow.PlanDecision{
		ID:             fmt.Sprintf("plan-decision.%s.%d", plan.Slug, len(plan.PlanDecisions)+1),
		PlanID:         workflow.PlanEntityID(plan.Slug),
		Kind:           workflow.PlanDecisionKindScopeIncomplete,
		Title:          "Declared scope.create files not delivered",
		Rationale:      plan.LastError,
		AffectedReqIDs: affected,
		ContractImpact: &workflow.ContractImpact{
			Kind:        workflow.ContractImpactPreserve,
			Summary:     "Declared scope.create files remain required; retry execution must deliver the missing files or propose an explicit scope amendment.",
			AffectedIDs: scopeCompletenessAffectedIDs(missing),
		},
		Status:     workflow.PlanDecisionStatusProposed,
		ProposedBy: "plan-manager",
		CreatedAt:  now,
	})

	c.logger.Warn("Level-0 completeness gate FAILED — failing plan closed (recoverable) instead of advancing to QA",
		"slug", plan.Slug, "missing_count", len(missing), "declared", len(plan.Scope.Create))

	// setPlanStatusCached validates implementing → rejected, persists the plan
	// (including the appended decision + LastError), and emits the status-change
	// event the UI SSE and recovery pickup depend on. On failure, fall back to a
	// direct save so the decision + LastError are not lost. Mirrors
	// failPlanOnAssemblyConflict.
	if err := c.setPlanStatusCached(ctx, plan, workflow.StatusRejected); err != nil {
		c.logger.Error("Failed to transition plan to rejected after Level-0 completeness gate",
			"slug", plan.Slug, "error", err)
		if saveErr := c.savePlanCached(ctx, plan); saveErr != nil {
			c.logger.Error("Failed to persist plan after completeness-gate transition failure",
				"slug", plan.Slug, "error", saveErr)
		}
	}
}

func scopeCompletenessAffectedIDs(missing []string) []string {
	out := make([]string, 0, len(missing))
	for _, file := range missing {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		out = append(out, "contract.scope.create:"+file)
	}
	return out
}
