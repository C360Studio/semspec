package workflow

import (
	"context"
	"fmt"
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
	// Hash each dep ID so the stored value is the entity-ID suffix, matching
	// the ID space returned by extractRequirementID on load.
	for _, dep := range req.DependsOn {
		_ = tw.WriteTriple(ctx, entityID, semspec.RequirementDependsOn, HashInstanceID(dep))
	}

	return nil
}

// requirementFromTripleMap reconstructs a Requirement from a predicate→[]values map.
// Single-valued predicates use the first element; DependsOn collects all values.
func requirementFromTripleMap(entityID string, triples map[string][]string) Requirement {
	first := func(pred string) string {
		if vs := triples[pred]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	req := Requirement{
		ID:     extractRequirementID(entityID),
		PlanID: first(semspec.RequirementPlan),
	}

	if v := first(semspec.RequirementTitle); v != "" {
		req.Title = v
	}
	if v := first(semspec.RequirementStatus); v != "" {
		req.Status = RequirementStatus(v)
	}
	if v := first(semspec.RequirementDescription); v != "" {
		req.Description = v
	}
	if v := first(semspec.RequirementCreatedAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			req.CreatedAt = t
		}
	}
	if v := first(semspec.RequirementUpdatedAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			req.UpdatedAt = t
		}
	}
	// DependsOn is written as one triple per dependency; collect all values.
	// Each value is already the hashed entity-ID suffix (written by writeRequirementTriples).
	for _, dep := range triples[semspec.RequirementDependsOn] {
		if dep != "" {
			req.DependsOn = append(req.DependsOn, dep)
		}
	}
	if req.DependsOn == nil {
		req.DependsOn = []string{}
	}

	return req
}

// extractRequirementID extracts the raw requirement ID from the entity ID.
// Entity ID format: {prefix}.wf.plan.req.{id}
func extractRequirementID(entityID string) string {
	prefix := EntityPrefix() + ".wf.plan.req."
	if len(entityID) > len(prefix) {
		return entityID[len(prefix):]
	}
	return entityID
}

// LoadRequirements loads requirements for a plan from ENTITY_STATES triples.
// Scans all requirement entities by prefix and filters by plan.
func LoadRequirements(ctx context.Context, tw *graphutil.TripleWriter, slug string) ([]Requirement, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if tw == nil {
		return []Requirement{}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := EntityPrefix() + ".wf.plan.req."
	entities, err := tw.ReadEntitiesByPrefixMulti(ctx, prefix, 500)
	if err != nil {
		return []Requirement{}, nil
	}

	planEntityID := PlanEntityID(slug)
	var requirements []Requirement

	for entityID, triples := range entities {
		req := requirementFromTripleMap(entityID, triples)
		if req.PlanID == planEntityID {
			requirements = append(requirements, req)
		}
	}

	if requirements == nil {
		requirements = []Requirement{}
	}

	return requirements, nil
}
