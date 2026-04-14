package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// DefaultProjectSlug is the slug for the auto-created default project.
// Sources and plans without explicit project assignment use this project.
const DefaultProjectSlug = "default"

// Project directory constants.
const (
	// ProjectsDir is the directory name for projects within .semspec.
	ProjectsDir = "projects"
	// ProjectFile is the filename for project metadata.
	ProjectFile = "project.json"
	// PlansDir is the directory name for plans within a project.
	PlansDir = "plans"
)

// ListProjectPlans returns all plans for a specific project from ENTITY_STATES triples.
func ListProjectPlans(ctx context.Context, tw *graphutil.TripleWriter, projectSlug string) (*ListPlansResult, error) {
	result := &ListPlansResult{
		Plans:  []*Plan{},
		Errors: []error{},
	}

	if tw == nil {
		return result, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := EntityPrefix() + ".wf.plan.plan."
	entities, err := tw.ReadEntitiesByPrefix(ctx, prefix, 500)
	if err != nil {
		return result, nil
	}

	projectEntityID := ProjectEntityID(projectSlug)
	for entityID, triples := range entities {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Skip tombstoned plans.
		if triples[semspec.PredicatePlanStatus] == "deleted" {
			continue
		}

		plan := PlanFromTripleMap(entityID, triples)

		// Filter by project
		if projectSlug != "" && plan.ProjectID != "" && plan.ProjectID != projectEntityID {
			continue
		}

		result.Plans = append(result.Plans, plan)
	}

	return result, nil
}

// CreateProjectPlan creates a new plan within a project.
// Uses triple existence check (fails if entity already has triples).
func CreateProjectPlan(ctx context.Context, tw *graphutil.TripleWriter, projectSlug, planSlug, title string) (*Plan, error) {
	if err := ValidateSlug(projectSlug); err != nil {
		return nil, err
	}
	if err := ValidateSlug(planSlug); err != nil {
		return nil, err
	}
	if title == "" {
		return nil, ErrTitleRequired
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check if plan already exists via triple read.
	entityID := PlanEntityID(planSlug)
	if tw != nil {
		triples, err := tw.ReadEntity(ctx, entityID)
		if err == nil && len(triples) > 0 && triples[semspec.PredicatePlanStatus] != "deleted" {
			return nil, fmt.Errorf("%w: %s", ErrPlanExists, planSlug)
		}
	}

	now := time.Now()
	plan := &Plan{
		ID:        entityID,
		Slug:      planSlug,
		Title:     title,
		ProjectID: ProjectEntityID(projectSlug),
		Approved:  false,
		CreatedAt: now,
		Scope: Scope{
			Include:    []string{},
			Exclude:    []string{},
			DoNotTouch: []string{},
		},
	}

	if err := writePlanTriples(ctx, tw, plan); err != nil {
		return nil, err
	}

	return plan, nil
}
