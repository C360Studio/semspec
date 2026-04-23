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
// On success, plan.AssembledBranch and plan.AssembledMergeCommit are populated and
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

	// Order requirements by their DependsOn graph so that a requirement
	// branch is merged after any prerequisites. Without this, merge order
	// is whatever plan-manager's write order happens to be — a hand
	// grenade at plan sizes where overlapping file edits exist. Ties (no
	// dependency relationship) break by the original slice order so the
	// result is deterministic and reproducible. Cycles fall back to slice
	// order with a warning — the decomposer shouldn't produce them, but
	// we won't wedge the merge if it does.
	orderedIDs := topoSortRequirementsByDependsOn(plan.Requirements)
	if len(orderedIDs) != len(plan.Requirements) {
		c.logger.Warn("Requirement topological sort produced unexpected length — falling back to slice order",
			"slug", plan.Slug, "sorted_len", len(orderedIDs), "requirements_len", len(plan.Requirements))
		orderedIDs = orderedIDs[:0]
		for _, r := range plan.Requirements {
			orderedIDs = append(orderedIDs, r.ID)
		}
	}
	branches := make([]string, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		branches = append(branches, "semspec/requirement-"+id)
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
		// can tell the human exactly which two requirements collided. Wrap
		// with %w so callers upstream can errors.Is-match the sentinel
		// (Phase 5's infra_health vs plan_conflict classification keys off it).
		if errors.Is(err, sandbox.ErrMergeBranchesConflict) && result != nil {
			return fmt.Errorf("requirement branch %q conflicts with prior branches on target %q: %w",
				result.ConflictingBranch, target, sandbox.ErrMergeBranchesConflict)
		}
		return err
	}

	plan.AssembledBranch = result.Target
	if n := len(result.MergeCommits); n > 0 {
		plan.AssembledMergeCommit = result.MergeCommits[n-1].Commit
	}
	c.logger.Info("Plan-level merge assembled",
		"slug", plan.Slug,
		"plan_branch", plan.AssembledBranch,
		"merge_commit", plan.AssembledMergeCommit,
		"requirements_merged", len(result.MergeCommits),
	)
	return nil
}

// topoSortRequirementsByDependsOn returns requirement IDs in an order where
// every prerequisite appears before its dependents. Requirements without any
// DependsOn entry — or with dependencies pointing outside the plan — are
// treated as roots. Ties are broken by original slice position so the
// output is deterministic.
//
// A cycle in the DependsOn graph produces a short result (fewer IDs than
// input); the caller falls back to slice order when that happens.
func topoSortRequirementsByDependsOn(reqs []workflow.Requirement) []string {
	if len(reqs) == 0 {
		return nil
	}
	type node struct {
		id       string
		deps     []string
		position int // index in original slice for tie-breaking
	}
	nodes := make(map[string]*node, len(reqs))
	order := make([]string, 0, len(reqs))
	for i, r := range reqs {
		nodes[r.ID] = &node{id: r.ID, deps: r.DependsOn, position: i}
		order = append(order, r.ID)
	}

	// In-plan dependency count — dependencies pointing outside the plan are
	// ignored (treat them as already-satisfied).
	indegree := make(map[string]int, len(nodes))
	for _, n := range nodes {
		for _, d := range n.deps {
			if _, ok := nodes[d]; ok {
				indegree[n.id]++
			}
		}
	}

	// Kahn's algorithm, scanning the original slice order on every pass so
	// ties break deterministically by plan-position rather than map iteration.
	result := make([]string, 0, len(reqs))
	remaining := make(map[string]bool, len(nodes))
	for id := range nodes {
		remaining[id] = true
	}
	for len(remaining) > 0 {
		progressed := false
		for _, id := range order {
			if !remaining[id] || indegree[id] > 0 {
				continue
			}
			result = append(result, id)
			delete(remaining, id)
			// Decrement indegree of everyone depending on this node.
			for _, m := range nodes {
				for _, d := range m.deps {
					if d == id {
						indegree[m.id]--
					}
				}
			}
			progressed = true
		}
		if !progressed {
			break // cycle
		}
	}
	return result
}
