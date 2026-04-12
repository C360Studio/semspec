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

// scenarioFromTripleMap reconstructs a Scenario from a predicate→[]values map.
// Single-valued predicates use the first element; Then collects all values.
func scenarioFromTripleMap(entityID string, triples map[string][]string) Scenario {
	first := func(pred string) string {
		if vs := triples[pred]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	s := Scenario{
		ID: extractScenarioID(entityID),
	}

	if v := first(semspec.ScenarioGiven); v != "" {
		s.Given = v
	}
	if v := first(semspec.ScenarioWhen); v != "" {
		s.When = v
	}
	if v := first(semspec.ScenarioStatus); v != "" {
		s.Status = ScenarioStatus(v)
	}
	if v := first(semspec.ScenarioCreatedAt); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			s.CreatedAt = t
		}
	}
	// RequirementID stored as full entity ID; extract raw ID.
	if v := first(semspec.ScenarioRequirement); v != "" {
		s.RequirementID = extractRequirementID(v)
	}
	// Then is written as one triple per clause; collect all values.
	for _, clause := range triples[semspec.ScenarioThen] {
		if clause != "" {
			s.Then = append(s.Then, clause)
		}
	}
	if s.Then == nil {
		s.Then = []string{}
	}

	return s
}

// extractScenarioID extracts the raw scenario ID from the entity ID.
// Entity ID format: {prefix}.wf.plan.scenario.{id}
func extractScenarioID(entityID string) string {
	prefix := EntityPrefix() + ".wf.plan.scenario."
	if len(entityID) > len(prefix) {
		return entityID[len(prefix):]
	}
	return entityID
}

// LoadScenarios loads scenarios for a plan from ENTITY_STATES triples.
// Scans all scenario entities by prefix and filters by plan's requirements.
func LoadScenarios(ctx context.Context, tw *graphutil.TripleWriter, slug string) ([]Scenario, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if tw == nil {
		return []Scenario{}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// First load requirements to know which requirement IDs belong to this plan.
	requirements, err := LoadRequirements(ctx, tw, slug)
	if err != nil {
		return nil, fmt.Errorf("load requirements for scenario filter: %w", err)
	}

	reqIDs := make(map[string]bool, len(requirements))
	for _, req := range requirements {
		reqIDs[req.ID] = true
	}

	prefix := EntityPrefix() + ".wf.plan.scenario."
	entities, err := tw.ReadEntitiesByPrefixMulti(ctx, prefix, 500)
	if err != nil {
		return []Scenario{}, nil
	}

	var scenarios []Scenario
	for entityID, triples := range entities {
		s := scenarioFromTripleMap(entityID, triples)
		if reqIDs[s.RequirementID] {
			scenarios = append(scenarios, s)
		}
	}

	if scenarios == nil {
		scenarios = []Scenario{}
	}

	return scenarios, nil
}
