package workflow

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"

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

// ValidateFileOwnershipPartition was a Requirement-level validator that
// rejected requirement sets with empty files_owned or overlapping files
// without a depends_on edge. ADR-043 Move 4 removed Requirement.FilesOwned;
// file ownership now lives on Story (Sarah computes the union of selected
// Components' implementation_files). The equivalent invariants for the
// Story DAG live on workflow.ValidateStories + plan-reviewer R3 rules
// (story.missing_files_owned, story.docs_only_files_owned), and the
// overlap-without-depends_on check itself lives at
// workflow.ValidateStoryFileOwnership (Mode 3 of ValidateStories) as of
// issue #88 (2026-06-03), which fills in this function's explicit "moves
// to ValidateStories in PR 4b" TODO.
//
// This function survives as a no-op so SaveRequirements still compiles
// and existing callers don't fan out into the rest of the codebase.
func ValidateFileOwnershipPartition(requirements []Requirement) error {
	_ = requirements
	return nil
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
		// writeRequirementTriples now folds in the capability edge (ADR-040) so
		// the whole requirement predicate set — including semspec.RequirementCapability
		// — is written in a single UpsertEntityIfChanged call. This ensures the
		// capability predicate is covered by RemoveTriples and the dirty-hash.
		if err := writeRequirementTriples(ctx, tw, &requirements[i], slug); err != nil {
			return fmt.Errorf("save requirement %s: %w", requirements[i].ID, err)
		}
	}

	return nil
}

// writeRequirementTriples writes all Requirement fields — including the
// requirement→capability edge (ADR-040) — as a single atomic batch to
// ENTITY_STATES via UpsertEntityIfChanged.
//
// The capability edge (semspec.RequirementCapability) is folded into the same
// triple slice so it is covered by the derived RemoveTriples and by the
// content-hash. Previously it was emitted in a separate
// writeRequirementCapabilityTriple call, which (a) produced a second NATS
// round-trip to the same entity and (b) left the predicate outside the
// dirty-hash gate.
//
// semspec.RequirementUpdatedAt is excluded from the content-hash via the
// volatilePredicates parameter — it is incremented on every mutation without a
// semantic change and would otherwise defeat dirty-track by always differing.
// The predicate is still written to the graph; it just does not gate dirtiness.
func writeRequirementTriples(ctx context.Context, tw *graphutil.TripleWriter, req *Requirement, planSlug string) error {
	if tw == nil {
		return nil
	}
	entityID := RequirementEntityID(req.ID)

	// Build the complete predicate set for this entity.
	triples := []message.Triple{
		{Subject: entityID, Predicate: semspec.RequirementTitle, Object: req.Title},
		{Subject: entityID, Predicate: semspec.DCTitle, Object: req.Title},
		{Subject: entityID, Predicate: semspec.RequirementStatus, Object: string(req.Status)},
		{Subject: entityID, Predicate: semspec.RequirementPlan, Object: req.PlanID},
		{Subject: entityID, Predicate: semspec.RequirementCreatedAt, Object: req.CreatedAt.Format(time.RFC3339)},
		// RequirementUpdatedAt is written but excluded from the hash (see volatilePredicates below).
		{Subject: entityID, Predicate: semspec.RequirementUpdatedAt, Object: req.UpdatedAt.Format(time.RFC3339)},
	}

	if req.Description != "" {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.RequirementDescription, Object: req.Description})
	}

	// Dependency edges — hash each dep ID to the entity-ID suffix form.
	for _, dep := range req.DependsOn {
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.RequirementDependsOn, Object: HashInstanceID(dep)})
	}

	// ADR-040: requirement→capability edge. Folded here (was writeRequirementCapabilityTriple)
	// so the predicate is covered by RemoveTriples and the dirty-hash in one call.
	if req.CapabilityName != "" && planSlug != "" {
		capEntityID := CapabilityEntityID(planSlug, req.CapabilityName)
		triples = append(triples, message.Triple{Subject: entityID, Predicate: semspec.RequirementCapability, Object: capEntityID})
	}

	// Single batched write. RequirementUpdatedAt is volatile (excluded from hash);
	// OwnedPredicates ensures clearable fields — Description (clearable via PATCH)
	// and DependsOn/Capability (list shrinks to empty) — are included in
	// RemoveTriples even when the current value is absent from the triples slice
	// (C1 stale-on-empty fix).
	_, err := tw.UpsertEntityIfChanged(ctx, RequirementEntityType, entityID, triples, graphutil.UpsertOpts{
		VolatilePredicates: []string{semspec.RequirementUpdatedAt},
		OwnedPredicates: []string{
			semspec.RequirementTitle,
			semspec.DCTitle,
			semspec.RequirementStatus,
			semspec.RequirementPlan,
			semspec.RequirementCreatedAt,
			semspec.RequirementUpdatedAt,
			semspec.RequirementDescription,
			semspec.RequirementDependsOn,
			semspec.RequirementCapability,
		},
	})
	if err != nil {
		return fmt.Errorf("write requirement %s: %w", req.ID, err)
	}
	return nil
}
