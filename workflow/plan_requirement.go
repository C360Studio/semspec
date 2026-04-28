package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// RequirementsJSONFile is the filename for machine-readable requirement storage (JSON format).
const RequirementsJSONFile = "requirements.json"

// ValidateRequirementDAG validates that the DependsOn references within the
// provided requirements form a valid directed acyclic graph. It checks that:
//   - All DependsOn entries reference IDs that exist within the slice
//   - No requirement references itself
//   - There are no cycles (detected via DFS with three-color marking)
//
// An empty slice or a slice where no requirement has DependsOn entries is
// always valid. The algorithm is structurally identical to the DAG validation
// in tools/decompose/types.go.
func ValidateRequirementDAG(requirements []Requirement) error {
	// Build an index of requirement IDs for O(1) membership checks.
	idIndex := make(map[string]struct{}, len(requirements))
	for _, r := range requirements {
		idIndex[r.ID] = struct{}{}
	}

	// Validate dependency references and self-references before DFS.
	for _, r := range requirements {
		for _, dep := range r.DependsOn {
			if dep == r.ID {
				return fmt.Errorf("requirement %q depends on itself", r.ID)
			}
			if _, exists := idIndex[dep]; !exists {
				return fmt.Errorf("requirement %q depends on unknown requirement %q", r.ID, dep)
			}
		}
	}

	// Build an adjacency list for cycle detection.
	adj := make(map[string][]string, len(requirements))
	for _, r := range requirements {
		adj[r.ID] = r.DependsOn
	}

	// Detect cycles via recursive DFS with three-color marking:
	//   white (0) = unvisited, gray (1) = in current path, black (2) = done.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(requirements))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("cycle detected: requirement %q and requirement %q are in a cycle", id, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
			// black: already fully explored, no cycle through this path
		}
		color[id] = black
		return nil
	}

	for _, r := range requirements {
		if color[r.ID] == white {
			if err := visit(r.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateFileOwnershipPartition rejects requirement sets where two requirements
// claim the same path in FilesOwned with no dependency edge between them. The
// plan-level merge of independent worktrees can't reconcile two parallel rewrites
// of the same file — catching the conflict here saves the executor from running
// the whole TDD loop and stalling at reviewing_qa.
//
// Two requirements may share a path if one transitively depends on the other
// (the executor sequences them, so the second one rebases on the first's
// changes instead of branching from the same base).
//
// Requirements with empty FilesOwned are not subject to this check — older
// generators may not emit it, and we shouldn't break those plans.
func ValidateFileOwnershipPartition(requirements []Requirement) error {
	if len(requirements) < 2 {
		return nil
	}
	ancestors := transitiveAncestors(requirements)
	idx := make(map[string]*Requirement, len(requirements))
	for i := range requirements {
		idx[requirements[i].ID] = &requirements[i]
	}
	for i := range requirements {
		ri := &requirements[i]
		if len(ri.FilesOwned) == 0 {
			continue
		}
		for j := i + 1; j < len(requirements); j++ {
			rj := &requirements[j]
			if len(rj.FilesOwned) == 0 {
				continue
			}
			conflict := intersectFiles(ri.FilesOwned, rj.FilesOwned)
			if len(conflict) == 0 {
				continue
			}
			// Allow the overlap when one requirement depends on the other,
			// transitively. The executor will sequence them, so the later
			// requirement rebases on the earlier one's merge commit.
			if ancestors[ri.ID][rj.ID] || ancestors[rj.ID][ri.ID] {
				continue
			}
			return fmt.Errorf(
				"requirements %q and %q both claim files_owned %v with no dependency edge between them: parallel writes to the same file deadlock the plan-level merge — either consolidate the requirements or add a depends_on edge",
				ri.ID, rj.ID, conflict,
			)
		}
	}
	return nil
}

// transitiveAncestors returns, for each requirement ID, the set of IDs reachable
// via DependsOn (i.e. the prerequisites). Used by ValidateFileOwnershipPartition
// to allow file overlap between requirements that have an explicit ordering.
//
// Caller is expected to have already passed ValidateRequirementDAG so cycles
// don't lead to non-termination.
func transitiveAncestors(requirements []Requirement) map[string]map[string]bool {
	deps := make(map[string][]string, len(requirements))
	for _, r := range requirements {
		deps[r.ID] = r.DependsOn
	}
	out := make(map[string]map[string]bool, len(requirements))
	for _, r := range requirements {
		seen := make(map[string]bool)
		stack := append([]string(nil), r.DependsOn...)
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[top] {
				continue
			}
			seen[top] = true
			stack = append(stack, deps[top]...)
		}
		out[r.ID] = seen
	}
	return out
}

// intersectFiles returns the set intersection of two path slices, comparing
// canonical (NormalizeFilePath) forms so "./main.go" and "main.go" don't slip
// past as distinct paths. Returns the canonical form of overlapping paths.
// Empty input → nil; no overlap → nil.
func intersectFiles(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	bSet := make(map[string]struct{}, len(b))
	for _, p := range b {
		if np := NormalizeFilePath(p); np != "" {
			bSet[np] = struct{}{}
		}
	}
	var out []string
	seen := make(map[string]struct{}, len(a))
	for _, p := range a {
		np := NormalizeFilePath(p)
		if np == "" {
			continue
		}
		if _, ok := seen[np]; ok {
			continue
		}
		if _, ok := bSet[np]; ok {
			seen[np] = struct{}{}
			out = append(out, np)
		}
	}
	return out
}

// NormalizeFilePath canonicalises a workspace-relative path so equivalent
// spellings collapse to one form. Returns "" for empty input, the workspace
// root (".", ""), or paths that escape the workspace ("../foo") — those
// shouldn't reach the validator and dropping them prevents the validator from
// silently accepting a non-canonical form.
func NormalizeFilePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// NormalizeFilePaths returns a fresh slice with each path canonicalised via
// NormalizeFilePath. Empty/escape entries are dropped. nil-in / nil-out.
func NormalizeFilePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if np := NormalizeFilePath(p); np != "" {
			out = append(out, np)
		}
	}
	return out
}

// SaveRequirements saves requirements to ENTITY_STATES as triples.
// Each requirement is stored as a separate entity keyed by RequirementEntityID.
// Multi-valued fields (DependsOn) are written as individual triples.
func SaveRequirements(ctx context.Context, tw *graphutil.TripleWriter, requirements []Requirement, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := ValidateRequirementDAG(requirements); err != nil {
		return fmt.Errorf("invalid requirement DAG: %w", err)
	}

	if err := ValidateFileOwnershipPartition(requirements); err != nil {
		return fmt.Errorf("invalid requirement file ownership: %w", err)
	}

	planEntityID := PlanEntityID(slug)
	for i := range requirements {
		if requirements[i].PlanID == "" {
			requirements[i].PlanID = planEntityID
		}
		if err := writeRequirementTriples(ctx, tw, &requirements[i]); err != nil {
			return fmt.Errorf("save requirement %s: %w", requirements[i].ID, err)
		}
	}

	return nil
}

// writeRequirementTriples writes all Requirement fields as individual triples.
func writeRequirementTriples(ctx context.Context, tw *graphutil.TripleWriter, req *Requirement) error {
	if tw == nil {
		return nil
	}
	entityID := RequirementEntityID(req.ID)

	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementTitle, req.Title)
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, req.Title)
	if err := tw.WriteTriple(ctx, entityID, semspec.RequirementStatus, string(req.Status)); err != nil {
		return fmt.Errorf("write requirement status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementPlan, req.PlanID)
	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementCreatedAt, req.CreatedAt.Format(time.RFC3339))
	_ = tw.WriteTriple(ctx, entityID, semspec.RequirementUpdatedAt, req.UpdatedAt.Format(time.RFC3339))
	if req.Description != "" {
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementDescription, req.Description)
	}

	// Write each dependency as an individual triple (proper graph edges).
	// Hash each dep ID so the stored value is the entity-ID suffix.
	for _, dep := range req.DependsOn {
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementDependsOn, HashInstanceID(dep))
	}

	// Write each owned file path as an individual triple — multi-valued by
	// design so queries can ask "which requirements own this path?" without
	// parsing a JSON blob.
	for _, path := range req.FilesOwned {
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementFilesOwned, path)
	}

	return nil
}
