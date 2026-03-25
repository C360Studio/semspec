package workflow

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/natsclient"
)

// ScenariosJSONFile is the filename for machine-readable scenario storage (JSON format).
const ScenariosJSONFile = "scenarios.json"

// SaveScenarios saves scenarios to ENTITY_STATES KV bucket.
// Each scenario is stored as a separate entity keyed by ScenarioEntityID.
func SaveScenarios(ctx context.Context, kv *natsclient.KVStore, scenarios []Scenario, slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range scenarios {
		if err := kvPut(ctx, kv, ScenarioEntityID(scenarios[i].ID), scenarios[i]); err != nil {
			return fmt.Errorf("save scenario %s: %w", scenarios[i].ID, err)
		}
	}

	return nil
}

// LoadScenarios loads scenarios for a plan from ENTITY_STATES KV bucket.
// Scans all scenario entities by prefix and filters by plan's requirements.
func LoadScenarios(ctx context.Context, kv *natsclient.KVStore, slug string) ([]Scenario, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}

	if kv == nil {
		return []Scenario{}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// First load requirements to know which requirement IDs belong to this plan
	requirements, err := LoadRequirements(ctx, kv, slug)
	if err != nil {
		return nil, fmt.Errorf("load requirements for scenario filter: %w", err)
	}

	reqIDs := make(map[string]bool, len(requirements))
	for _, req := range requirements {
		reqIDs[req.ID] = true
	}

	prefix := EntityPrefix() + ".wf.plan.scenario."
	keys, err := kv.KeysByPrefix(ctx, prefix)
	if err != nil {
		return []Scenario{}, nil
	}

	var scenarios []Scenario
	for _, key := range keys {
		var s Scenario
		if err := kvGet(ctx, kv, key, &s); err != nil {
			continue
		}

		if reqIDs[s.RequirementID] {
			scenarios = append(scenarios, s)
		}
	}

	if scenarios == nil {
		scenarios = []Scenario{}
	}

	return scenarios, nil
}
