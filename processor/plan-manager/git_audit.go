package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/c360studio/semspec/workflow"
)

// GitAuditFinding is a single row in the audit report. Branch is the
// semspec/requirement-<id> branch name. ExistsInSandbox is false when
// the sandbox cannot resolve the ref — typically meaning the requirement
// never dispatched, or the branch was pruned. MergedIntoAssembled is the
// load-bearing field: true means the requirement's work is present on
// the plan's assembled branch; false means the plan is "lying about
// state" (the audit's motivating concern).
type GitAuditFinding struct {
	RequirementID       string `json:"requirement_id"`
	Branch              string `json:"branch"`
	ExistsInSandbox     bool   `json:"exists_in_sandbox"`
	MergedIntoAssembled bool   `json:"merged_into_assembled"`
	Notes               string `json:"notes,omitempty"`
}

// GitAuditReport is the JSON payload from GET /plans/{slug}/git-audit.
// Healthy=true iff every requirement branch exists in the sandbox AND
// (when the plan has an AssembledBranch) is reachable from it. The UI
// can surface Healthy as a single traffic light and drill into Findings
// for the per-requirement breakdown.
type GitAuditReport struct {
	Slug            string            `json:"slug"`
	AssembledBranch string            `json:"assembled_branch,omitempty"`
	Healthy         bool              `json:"healthy"`
	Findings        []GitAuditFinding `json:"findings"`
	Warnings        []string          `json:"warnings,omitempty"`
}

// handleGitAudit implements invariant C3 from
// docs/audit/task-11-worktree-invariants.md: let humans ask "does git
// actually agree with what plan-manager claims?" For each requirement in
// the plan, query the sandbox for whether the requirement's branch
// exists and whether it is reachable from the assembled plan branch.
// The report is HTTP-cheap (one sandbox round-trip per requirement) so
// it can be polled from a UI status panel or invoked ad-hoc by
// operators diagnosing "I marked this plan complete — where's the code?"
func (c *Component) handleGitAudit(w http.ResponseWriter, r *http.Request, slug string) {
	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan for git-audit", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}
	report := c.buildGitAuditReport(r.Context(), plan)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

// buildGitAuditReport is the pure logic behind handleGitAudit, extracted
// for direct unit testing without a full HTTP round-trip. Does not
// mutate the plan or write to the graph — audit is strictly read-only.
func (c *Component) buildGitAuditReport(ctx context.Context, plan *workflow.Plan) GitAuditReport {
	report := GitAuditReport{
		Slug:            plan.Slug,
		AssembledBranch: plan.AssembledBranch,
		Healthy:         true,
		Findings:        []GitAuditFinding{},
	}
	if c.sandbox == nil {
		report.Warnings = append(report.Warnings,
			"sandbox client not configured — git state cannot be verified")
		report.Healthy = false
		return report
	}
	if len(plan.Requirements) == 0 {
		// No requirements means no branches to check — vacuously healthy.
		// Common for plans that haven't started execution yet.
		return report
	}

	// When there's no assembled branch yet (plan never completed or pre-B1
	// plan), we verify branch existence only. The audit becomes "do the
	// requirement branches we expect to exist actually exist?" which is
	// still useful for detecting pruning bugs or KV/git drift.
	descendant := plan.AssembledBranch
	checkAncestry := descendant != ""

	for _, r := range plan.Requirements {
		branch := "semspec/requirement-" + r.ID
		finding := GitAuditFinding{
			RequirementID: r.ID,
			Branch:        branch,
		}
		if !checkAncestry {
			// Only probe existence: ancestry-vs-self is trivially true and
			// would mask a missing branch.
			res, err := c.sandbox.Ancestry(ctx, branch, branch)
			if err != nil {
				finding.Notes = "sandbox unreachable: " + err.Error()
				report.Healthy = false
			} else {
				finding.ExistsInSandbox = res.AncestorExists
				if !finding.ExistsInSandbox {
					finding.Notes = "requirement branch not found in sandbox"
					report.Healthy = false
				}
			}
			report.Findings = append(report.Findings, finding)
			continue
		}
		res, err := c.sandbox.Ancestry(ctx, branch, descendant)
		if err != nil {
			finding.Notes = "sandbox unreachable: " + err.Error()
			report.Healthy = false
			report.Findings = append(report.Findings, finding)
			continue
		}
		finding.ExistsInSandbox = res.AncestorExists
		finding.MergedIntoAssembled = res.IsAncestor
		switch {
		case !res.AncestorExists:
			finding.Notes = "requirement branch missing"
			report.Healthy = false
		case !res.DescendantExists:
			finding.Notes = "assembled branch missing — plan record claims " + descendant + " but git has no such branch"
			report.Healthy = false
		case !res.IsAncestor:
			finding.Notes = "requirement commits NOT reachable from assembled branch — plan is lying about state"
			report.Healthy = false
		}
		report.Findings = append(report.Findings, finding)
	}
	return report
}
