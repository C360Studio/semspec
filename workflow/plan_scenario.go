package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/c360studio/semspec/vocabulary/semspec"
	"github.com/c360studio/semspec/workflow/graphutil"
)

// ScenariosJSONFile is the filename for machine-readable scenario storage (JSON format).
const ScenariosJSONFile = "scenarios.json"

// SaveScenarios saves scenarios to ENTITY_STATES as triples.
// Each scenario is stored as a separate entity keyed by ScenarioEntityID.
// Multi-valued fields (Then) are written as individual triples.
func SaveScenarios(ctx context.Context, tw *graphutil.TripleWriter, scenarios []Scenario, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range scenarios {
		if err := writeScenarioTriples(ctx, tw, &scenarios[i]); err != nil {
			return fmt.Errorf("save scenario %s: %w", scenarios[i].ID, err)
		}
	}

	return nil
}

// writeScenarioTriples writes all Scenario fields as individual triples.
func writeScenarioTriples(ctx context.Context, tw *graphutil.TripleWriter, s *Scenario) error {
	if tw == nil {
		return nil
	}
	entityID := ScenarioEntityID(s.ID)

	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioGiven, s.Given)
	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioWhen, s.When)
	if err := tw.WriteTriple(ctx, entityID, semspec.ScenarioStatus, string(s.Status)); err != nil {
		return fmt.Errorf("write scenario status: %w", err)
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioRequirement, RequirementEntityID(s.RequirementID))
	_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioCreatedAt, s.CreatedAt.Format(time.RFC3339))

	title := s.When
	if len(title) > 100 {
		title = title[:97] + "..."
	}
	_ = tw.WriteTriple(ctx, entityID, semspec.DCTitle, title)

	// Write each Then clause as an individual triple (proper graph edges).
	for _, clause := range s.Then {
		_ = tw.WriteTriple(ctx, entityID, semspec.ScenarioThen, clause)
	}

	return nil
}
