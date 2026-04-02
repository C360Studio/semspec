package terminal

import "fmt"

// DeliverableValidator validates a structured deliverable from submit_work.
// Returns nil if valid, or an error with a specific, actionable message
// that the LLM can use to fix and retry.
type DeliverableValidator func(deliverable map[string]any) error

// deliverableValidators maps deliverable_type values to their validators.
var deliverableValidators = map[string]DeliverableValidator{
	"plan":         ValidatePlanDeliverable,
	"requirements": ValidateRequirementsDeliverable,
	"scenarios":    ValidateScenariosDeliverable,
	"architecture": ValidateArchitectDeliverable,
}

// GetDeliverableValidator returns the validator for the given deliverable type.
// Returns nil if no validator is registered (deliverable accepted as-is).
func GetDeliverableValidator(deliverableType string) DeliverableValidator {
	return deliverableValidators[deliverableType]
}

// ValidatePlanDeliverable validates a plan deliverable from the planner.
// Required fields: goal, context.
func ValidatePlanDeliverable(d map[string]any) error {
	goal, _ := d["goal"].(string)
	if goal == "" {
		return fmt.Errorf("deliverable.goal is required — provide a specific, actionable goal describing what to build or fix")
	}
	context, _ := d["context"].(string)
	if context == "" {
		return fmt.Errorf("deliverable.context is required — describe the current state, why this matters, and key constraints")
	}
	return nil
}

// ValidateRequirementsDeliverable validates a requirements deliverable.
// Expected: {"requirements": [{"title": "...", "description": "..."}, ...]}.
func ValidateRequirementsDeliverable(d map[string]any) error {
	reqs, ok := d["requirements"].([]any)
	if !ok || len(reqs) == 0 {
		return fmt.Errorf("deliverable.requirements is required — provide an array of requirement objects, each with title and description")
	}
	for i, r := range reqs {
		req, ok := r.(map[string]any)
		if !ok {
			return fmt.Errorf("deliverable.requirements[%d] must be an object with title and description", i)
		}
		title, _ := req["title"].(string)
		if title == "" {
			return fmt.Errorf("deliverable.requirements[%d].title is required", i)
		}
		desc, _ := req["description"].(string)
		if desc == "" {
			return fmt.Errorf("deliverable.requirements[%d].description is required", i)
		}
	}
	return nil
}

// ValidateScenariosDeliverable validates a scenarios deliverable.
// Expected: {"scenarios": [{"title": "...", "given": "...", "when": "...", "then": "..."}, ...]}.
func ValidateScenariosDeliverable(d map[string]any) error {
	scenarios, ok := d["scenarios"].([]any)
	if !ok || len(scenarios) == 0 {
		return fmt.Errorf("deliverable.scenarios is required — provide an array of scenario objects, each with title, given, when, then")
	}
	for i, s := range scenarios {
		sc, ok := s.(map[string]any)
		if !ok {
			return fmt.Errorf("deliverable.scenarios[%d] must be an object with title, given, when, then", i)
		}
		title, _ := sc["title"].(string)
		if title == "" {
			return fmt.Errorf("deliverable.scenarios[%d].title is required", i)
		}
		given, _ := sc["given"].(string)
		when, _ := sc["when"].(string)
		then, _ := sc["then"].(string)
		if given == "" || when == "" || then == "" {
			return fmt.Errorf("deliverable.scenarios[%d] requires given, when, and then clauses", i)
		}
	}
	return nil
}

// ValidateArchitectDeliverable validates an architecture deliverable.
// Expected: {"technology_choices": [...], "component_boundaries": [...], "data_flow": "...", "decisions": [...]}.
func ValidateArchitectDeliverable(d map[string]any) error {
	// technology_choices
	techChoices, ok := d["technology_choices"].([]any)
	if !ok || len(techChoices) == 0 {
		return fmt.Errorf("deliverable.technology_choices is required — provide an array of {category, choice, rationale} objects")
	}
	for i, tc := range techChoices {
		obj, ok := tc.(map[string]any)
		if !ok {
			return fmt.Errorf("deliverable.technology_choices[%d] must be an object with category, choice, rationale", i)
		}
		cat, _ := obj["category"].(string)
		choice, _ := obj["choice"].(string)
		rationale, _ := obj["rationale"].(string)
		if cat == "" || choice == "" || rationale == "" {
			return fmt.Errorf("deliverable.technology_choices[%d] requires category, choice, and rationale strings", i)
		}
	}

	// component_boundaries
	components, ok := d["component_boundaries"].([]any)
	if !ok || len(components) == 0 {
		return fmt.Errorf("deliverable.component_boundaries is required — provide an array of {name, responsibility, dependencies[]} objects")
	}
	for i, cb := range components {
		obj, ok := cb.(map[string]any)
		if !ok {
			return fmt.Errorf("deliverable.component_boundaries[%d] must be an object with name, responsibility, dependencies", i)
		}
		name, _ := obj["name"].(string)
		resp, _ := obj["responsibility"].(string)
		if name == "" || resp == "" {
			return fmt.Errorf("deliverable.component_boundaries[%d] requires name and responsibility strings", i)
		}
		if _, hasDeps := obj["dependencies"]; !hasDeps {
			return fmt.Errorf("deliverable.component_boundaries[%d] requires a dependencies array (may be empty)", i)
		}
	}

	// data_flow
	dataFlow, _ := d["data_flow"].(string)
	if dataFlow == "" {
		return fmt.Errorf("deliverable.data_flow is required — describe how data moves between components")
	}

	// decisions
	decisions, ok := d["decisions"].([]any)
	if !ok || len(decisions) == 0 {
		return fmt.Errorf("deliverable.decisions is required — provide an array of {id, title, decision, rationale} objects")
	}
	for i, dec := range decisions {
		obj, ok := dec.(map[string]any)
		if !ok {
			return fmt.Errorf("deliverable.decisions[%d] must be an object with id, title, decision, rationale", i)
		}
		id, _ := obj["id"].(string)
		title, _ := obj["title"].(string)
		decision, _ := obj["decision"].(string)
		rationale, _ := obj["rationale"].(string)
		if id == "" || title == "" || decision == "" || rationale == "" {
			return fmt.Errorf("deliverable.decisions[%d] requires id, title, decision, and rationale strings", i)
		}
	}

	return nil
}
