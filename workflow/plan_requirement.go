package workflow

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// ErrInvalidRequirementDAG and ErrInvalidFileOwnership are sentinel errors
// returned by the requirement validators. Callers wrap them with %w so
// downstream code can route on errors.Is — used by requirement-generator
// to feed validator rejections back to the agent for regeneration instead
// of failing the plan terminally. The string contracts the requirement-
// generator previously matched on were silently coupled to a Sprintf
// format in plan-manager; sentinel errors make the contract structural.
var (
	ErrInvalidRequirementDAG = errors.New("invalid requirement DAG")
	ErrInvalidFileOwnership  = errors.New("invalid requirement file ownership")
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
				return fmt.Errorf("%w: requirement %q depends on itself", ErrInvalidRequirementDAG, r.ID)
			}
			if _, exists := idIndex[dep]; !exists {
				return fmt.Errorf("%w: requirement %q depends on unknown requirement %q", ErrInvalidRequirementDAG, r.ID, dep)
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
				return fmt.Errorf("%w: cycle detected — requirement %q and requirement %q are in a cycle", ErrInvalidRequirementDAG, id, dep)
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

// ValidateFileOwnershipPartition rejects requirement sets that would deadlock
// at plan-level merge. Two failure modes are caught here:
//
//  1. Missing files_owned. When >1 requirement exists, every requirement must
//     declare files_owned. Without it the executor can't reason about
//     parallelism vs serialization, and the prompt-side promise of "the
//     validator will reject" is meaningless. Empty arrays are not acceptable.
//
//  2. Overlap without dependency. Two requirements may share a file in
//     files_owned, but only when one transitively depends on the other —
//     the executor then sequences them so the later requirement rebases on
//     the earlier one's merge commit. Without that depends_on edge, parallel
//     branches both rewrite the same file and the plan-level merge stalls.
//
// Single-requirement plans skip the whole check (no possible overlap).
//
// Background: 2026-04-29 Gemini @easy run found the previous lenient version
// (skip-if-empty) silently accepted requirements with files_owned=null and
// stalled at reviewing_qa with a merge conflict. See
// project_gemini_easy_2026_04_29 memory.
func ValidateFileOwnershipPartition(requirements []Requirement) error {
	if len(requirements) < 2 {
		return nil
	}
	// Mode 1: every requirement must declare files_owned when there's more
	// than one in play. Catches generators that ignore the prompt.
	for i := range requirements {
		if len(requirements[i].FilesOwned) == 0 {
			return fmt.Errorf(
				"%w: requirement %q has empty files_owned — every requirement in a multi-requirement plan must declare the workspace-relative paths it modifies, so the validator can detect overlap and force depends_on. Regenerate with files_owned set",
				ErrInvalidFileOwnership, requirements[i].ID,
			)
		}
	}
	ancestors := transitiveAncestors(requirements)
	for i := range requirements {
		ri := &requirements[i]
		for j := i + 1; j < len(requirements); j++ {
			rj := &requirements[j]
			conflict := intersectFiles(ri.FilesOwned, rj.FilesOwned)
			if len(conflict) == 0 {
				continue
			}
			// Mode 2: overlap is allowed when there's a dependency edge.
			// Required for the layered case (impl + test for same surface,
			// define + use, etc.) where two reqs legitimately touch the
			// same file. The executor sequences them via depends_on.
			if ancestors[ri.ID][rj.ID] || ancestors[rj.ID][ri.ID] {
				continue
			}
			// Hint includes the two valid resolutions concretely — the
			// abstract "consolidate or add depends_on edge" line was
			// observed to recur on qwen3-moe even AFTER the fan-in
			// prompt fix shipped 2026-05-02. The worked examples
			// below give the model a directive template to copy
			// rather than reasoning about the right shape from
			// scratch on every retry. Same SAP-loud-on-help
			// discipline as graph_query D.8 ("Try entity(id: \"X\")
			// if that's the one you meant").
			return fmt.Errorf(
				"%w: requirements %q and %q both claim files_owned %v with no dependency edge between them — parallel writes to the same file deadlock the plan-level merge.\n\nFIX: choose ONE of these two resolutions.\n\n(a) Consolidate into a single requirement that owns all the conflicting files:\n  {\"title\": \"<merged title>\", \"description\": \"...\", \"files_owned\": %v}\n\n(b) Keep two requirements, add a depends_on edge so the second rebases on the first's merge:\n  {\"title\": %q, \"files_owned\": %v}\n  {\"title\": %q, \"depends_on\": [%q], \"files_owned\": %v}\n\nIf the two requirements are about the SAME surface (impl + its test, define + use), prefer (a). If they're genuinely separate features that happen to touch a shared file (router/main wire-up), use (b).",
				ErrInvalidFileOwnership, ri.ID, rj.ID, conflict,
				conflict, // (a) merged files_owned
				ri.ID, ri.FilesOwned, // (b) first req keeps its files
				rj.ID, ri.ID, rj.FilesOwned, // (b) second req gains depends_on
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
