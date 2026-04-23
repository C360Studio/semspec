package planmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/c360studio/semspec/tools/sandbox"
	"github.com/c360studio/semspec/workflow"
)

// assembleRequirementBranches implements invariant B1 from
// docs/audit/task-11-worktree-invariants.md: at plan-complete time, every
// completed requirement branch is merged into a plan branch
// ("semspec/plan-<slug>") so the aggregated work lives somewhere humans
// can see and review before merge-to-main. The pre-B1 behavior was to
// mark the plan complete while the work sat stranded on sibling
// "semspec/requirement-*" branches that no process ever assembled.
//
// On success, plan.PlanBranch and plan.PlanMergeCommit are populated and
// the caller transitions the plan to complete. On any failure (conflict,
// needs-reconciliation, sandbox unreachable), the caller keeps the plan
// in its current state and surfaces the error so a human can reconcile.
//
// No-op when the sandbox client is not configured or the plan has no
// requirements — both match the pre-B1 shape and are safe for tests that
// mock less of the world.
func (c *Component) assembleRequirementBranches(ctx context.Context, plan *workflow.Plan) error {
	if c.sandbox == nil {
		c.logger.Debug("Plan-level merge skipped — sandbox client not configured",
			"slug", plan.Slug)
		return nil
	}
	if len(plan.Requirements) == 0 {
		c.logger.Debug("Plan-level merge skipped — plan has no requirements",
			"slug", plan.Slug)
		return nil
	}

	branches := make([]string, 0, len(plan.Requirements))
	for _, r := range plan.Requirements {
		branches = append(branches, "semspec/requirement-"+r.ID)
	}
	target := "semspec/plan-" + plan.Slug

	result, err := c.sandbox.MergeBranches(ctx, sandbox.MergeBranchesRequest{
		Target:   target,
		Branches: branches,
		Trailers: map[string]string{"Plan-Slug": plan.Slug},
	})
	if err != nil {
		// A merge conflict is a normal caller-actionable condition — return
		// a descriptive error that names the conflicting branch so the UI
		// can tell the human exactly which two requirements collided.
		if errors.Is(err, sandbox.ErrMergeBranchesConflict) && result != nil {
			return fmt.Errorf("requirement branch %q conflicts with prior branches on target %q",
				result.ConflictingBranch, target)
		}
		return err
	}

	plan.PlanBranch = result.Target
	if n := len(result.MergeCommits); n > 0 {
		plan.PlanMergeCommit = result.MergeCommits[n-1].Commit
	}
	c.logger.Info("Plan-level merge assembled",
		"slug", plan.Slug,
		"plan_branch", plan.PlanBranch,
		"merge_commit", plan.PlanMergeCommit,
		"requirements_merged", len(result.MergeCommits),
	)
	return nil
}
