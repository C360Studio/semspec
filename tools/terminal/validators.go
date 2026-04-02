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
