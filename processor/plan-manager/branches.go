package planmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/c360studio/semspec/workflow"
	"github.com/nats-io/nats.go/jetstream"
)

// PlanRequirementBranch is the per-requirement branch summary returned by
// GET /plans/{slug}/branches. This is the "files view" data source: one
// entry per plan requirement, joined with its git branch diff so the UI
// can show what the agent actually changed (not the fixture tree).
type PlanRequirementBranch struct {
	RequirementID   string           `json:"requirement_id"`
	Title           string           `json:"title"`
	Branch          string           `json:"branch"`
	Stage           string           `json:"stage"`
	Base            string           `json:"base"`
	Files           []BranchDiffFile `json:"files"`
	TotalInsertions int              `json:"total_insertions"`
	TotalDeletions  int              `json:"total_deletions"`
	// DiffError is set when sandbox diff failed for this requirement; UI
	// should surface it instead of silently showing "0 files".
	DiffError string `json:"diff_error,omitempty"`
}

// handlePlanBranches handles GET /plans/{slug}/branches.
// Walks plan.Requirements (canonical list), joins each with its execution
// entry in EXECUTION_STATES KV, and calls sandbox for the branch diff.
func (c *Component) handlePlanBranches(w http.ResponseWriter, r *http.Request, slug string) {
	if c.workspace == nil {
		writeJSONError(w, "sandbox not configured", http.StatusServiceUnavailable)
		return
	}

	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	base := resolvePlanBase(r, plan)

	execsByID, err := c.loadRequirementExecutions(r.Context(), slug)
	if err != nil {
		c.logger.Error("Failed to load requirement executions", "slug", slug, "error", err)
		http.Error(w, "Failed to load requirement executions", http.StatusInternalServerError)
		return
	}

	out := make([]PlanRequirementBranch, 0, len(plan.Requirements))
	for _, req := range plan.Requirements {
		entry := PlanRequirementBranch{
			RequirementID: req.ID,
			Title:         req.Title,
			Base:          base,
		}
		if exec, ok := execsByID[req.ID]; ok {
			entry.Stage = exec.Stage
			entry.Branch = exec.RequirementBranch
			// Exec title is the source of truth once execution has started.
			if exec.Title != "" {
				entry.Title = exec.Title
			}
		}
		if entry.Title == "" {
			entry.Title = req.ID
		}
		if entry.Branch != "" {
			summary, found, err := c.workspace.branchDiff(r.Context(), entry.Branch, base)
			switch {
			case err != nil:
				c.logger.Warn("Branch diff failed", "slug", slug, "branch", entry.Branch, "error", err)
				entry.DiffError = err.Error()
			case !found:
				// Branch was recorded in KV but has since been pruned (e.g. merge cleanup).
				// Still show the row — the UI signals empty state.
			default:
				entry.Files = summary.Files
				entry.TotalInsertions = summary.TotalInsertions
				entry.TotalDeletions = summary.TotalDeletions
			}
		}
		out = append(out, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		c.logger.Warn("Failed to encode branches response", "error", err)
	}
}

// handleRequirementFileDiff handles GET /plans/{slug}/requirements/{reqID}/file-diff?path=X.
// Returns the unified patch for one file on that requirement's branch.
func (c *Component) handleRequirementFileDiff(w http.ResponseWriter, r *http.Request, slug, reqID string) {
	if c.workspace == nil {
		writeJSONError(w, "sandbox not configured", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSONError(w, "path query param is required", http.StatusBadRequest)
		return
	}

	plan, err := c.loadPlanCached(r.Context(), slug)
	if err != nil {
		if errors.Is(err, workflow.ErrPlanNotFound) {
			http.Error(w, "Plan not found", http.StatusNotFound)
			return
		}
		c.logger.Error("Failed to load plan", "slug", slug, "error", err)
		http.Error(w, "Failed to load plan", http.StatusInternalServerError)
		return
	}

	base := resolvePlanBase(r, plan)

	// Resolve branch from the requirement execution. Falls back to the
	// conventional naming if no exec entry exists yet (rare; means the
	// executor already deleted the KV row but the branch still exists).
	execsByID, err := c.loadRequirementExecutions(r.Context(), slug)
	if err != nil {
		c.logger.Error("Failed to load requirement executions", "slug", slug, "error", err)
		http.Error(w, "Failed to load requirement executions", http.StatusInternalServerError)
		return
	}

	branch := ""
	if exec, ok := execsByID[reqID]; ok {
		branch = exec.RequirementBranch
	}
	if branch == "" {
		branch = "semspec/requirement-" + reqID
	}

	patch, found, err := c.workspace.branchFileDiff(r.Context(), branch, base, path)
	if err != nil {
		c.logger.Warn("File diff failed", "slug", slug, "branch", branch, "path", path, "error", err)
		writeJSONError(w, "file diff failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !found {
		http.Error(w, "branch not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Patch string `json:"patch"`
	}{Patch: patch})
}

// resolvePlanBase picks the base branch for diffs against a plan's work.
// Precedence: ?base= query param > plan.GitHub.PlanBranch > "main".
func resolvePlanBase(r *http.Request, plan *workflow.Plan) string {
	if v := r.URL.Query().Get("base"); v != "" {
		return v
	}
	if plan.GitHub != nil && plan.GitHub.PlanBranch != "" {
		return plan.GitHub.PlanBranch
	}
	return "main"
}

// loadRequirementExecutions scans EXECUTION_STATES for all req.<slug>.* keys
// and returns a map keyed by RequirementID. Missing bucket or no keys returns
// an empty map (not an error) — the plan may be pre-execution.
func (c *Component) loadRequirementExecutions(ctx context.Context, slug string) (map[string]*workflow.RequirementExecution, error) {
	result := map[string]*workflow.RequirementExecution{}

	// natsClient can be nil in unit tests; treat as no executions.
	if c.natsClient == nil {
		return result, nil
	}

	bucket, err := c.getExecBucket(ctx)
	if err != nil {
		// No bucket is fine — treat as no executions yet.
		return result, nil
	}

	prefix := "req." + slug + "."
	keys, err := bucket.Keys(ctx, jetstream.MetaOnly())
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return result, nil
		}
		return nil, fmt.Errorf("list execution keys: %w", err)
	}

	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}
		var exec workflow.RequirementExecution
		if err := json.Unmarshal(entry.Value(), &exec); err != nil {
			continue
		}
		if exec.RequirementID == "" {
			continue
		}
		execCopy := exec
		result[exec.RequirementID] = &execCopy
	}

	return result, nil
}
