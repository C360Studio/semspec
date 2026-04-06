package terminal

import (
	"fmt"
)

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
	"review":       ValidateReviewDeliverable,
}

// GetDeliverableValidator returns the validator for the given deliverable type.
// Returns nil if no validator is registered (deliverable accepted as-is).
func GetDeliverableValidator(deliverableType string) DeliverableValidator {
	return deliverableValidators[deliverableType]
}

// ExpectedFieldsHint returns a one-line example showing the expected submit_work
// call for the given deliverable type. Used in error messages when arguments are empty.
func ExpectedFieldsHint(deliverableType string) string {
	switch deliverableType {
	case "plan":
		return `Expected JSON: {"goal": "...", "context": "...", "scope": {"include": [...]}}`
	case "requirements":
		return `Expected JSON: {"requirements": [{"title": "...", "description": "..."}]}`
	case "scenarios":
		return `Expected JSON: {"scenarios": [{"title": "...", "given": "...", "when": "...", "then": ["..."]}]}`
	case "architecture":
		return `Expected JSON: {"technology_choices": [...], "component_boundaries": [...], "data_flow": "...", "decisions": [...], "actors": [...], "integrations": [...]}`
	case "review":
		return `Expected JSON: {"verdict": "approved", "feedback": "..."}`
	default:
		return `Expected JSON: {"summary": "...", "files_modified": ["file.go"]}`
	}
}

// ValidatePlanDeliverable validates a plan deliverable from the planner.
// Required fields: goal, context.
func ValidatePlanDeliverable(d map[string]any) error {
	goal, _ := d["goal"].(string)
	if goal == "" {
		return fmt.Errorf("goal is required — provide a specific, actionable goal describing what to build or fix")
	}
	context, _ := d["context"].(string)
	if context == "" {
		return fmt.Errorf("context is required — describe the current state, why this matters, and key constraints")
	}
	return nil
}

// ValidateRequirementsDeliverable validates a requirements // Expected: {"requirements": [{"title": "...", "description": "..."}, ...]}.
func ValidateRequirementsDeliverable(d map[string]any) error {
	reqs, ok := d["requirements"].([]any)
	if !ok || len(reqs) == 0 {
		return fmt.Errorf("requirements is required — provide an array of requirement objects, each with title and description")
	}
	for i, r := range reqs {
		req, ok := r.(map[string]any)
		if !ok {
			return fmt.Errorf("requirements[%d] must be an object with title and description", i)
		}
		title, _ := req["title"].(string)
		if title == "" {
			return fmt.Errorf("requirements[%d].title is required", i)
		}
		desc, _ := req["description"].(string)
		if desc == "" {
			return fmt.Errorf("requirements[%d].description is required", i)
		}
	}
	return nil
}

// ValidateScenariosDeliverable validates a scenarios // Expected: {"scenarios": [{"title": "...", "given": "...", "when": "...", "then": "..."}, ...]}.
func ValidateScenariosDeliverable(d map[string]any) error {
	scenarios, ok := d["scenarios"].([]any)
	if !ok || len(scenarios) == 0 {
		return fmt.Errorf("scenarios is required — provide an array of scenario objects, each with title, given, when, then")
	}
	for i, s := range scenarios {
		sc, ok := s.(map[string]any)
		if !ok {
			return fmt.Errorf("scenarios[%d] must be an object with title, given, when, then", i)
		}
		title, _ := sc["title"].(string)
		if title == "" {
			return fmt.Errorf("scenarios[%d].title is required", i)
		}
		given, _ := sc["given"].(string)
		when, _ := sc["when"].(string)
		// "then" accepts both a string and an array of strings.
		hasThen := false
		if thenStr, ok := sc["then"].(string); ok && thenStr != "" {
			hasThen = true
		} else if thenArr, ok := sc["then"].([]any); ok && len(thenArr) > 0 {
			hasThen = true
		}
		if given == "" || when == "" || !hasThen {
			return fmt.Errorf("scenarios[%d] requires given, when, and then clauses", i)
		}
	}
	return nil
}

// ValidateReviewDeliverable validates a review deliverable from code, scenario, or plan reviewers.
// Required: verdict (approved/rejected/needs_changes).
// When rejected or needs_changes: feedback is required.
// When rejected: rejection_type is also required.
func ValidateReviewDeliverable(d map[string]any) error {
	verdict, _ := d["verdict"].(string)
	if verdict == "" {
		return fmt.Errorf("verdict is required — must be \"approved\", \"rejected\", or \"needs_changes\"")
	}
	validVerdicts := map[string]bool{"approved": true, "rejected": true, "needs_changes": true}
	if !validVerdicts[verdict] {
		return fmt.Errorf("verdict must be \"approved\", \"rejected\", or \"needs_changes\", got %q", verdict)
	}
	feedback, _ := d["feedback"].(string)
	if verdict == "rejected" || verdict == "needs_changes" {
		if feedback == "" {
			return fmt.Errorf("feedback is required when verdict is %s — provide specific, actionable feedback", verdict)
		}
	}
	if verdict == "rejected" {
		rejType, _ := d["rejection_type"].(string)
		validTypes := map[string]bool{"fixable": true, "restructure": true}
		if !validTypes[rejType] {
			return fmt.Errorf("rejection_type is required when verdict is rejected — must be one of: fixable, restructure")
		}
	}
	return nil
}

// ValidateArchitectDeliverable validates an architecture // Expected: {"technology_choices": [...], "component_boundaries": [...], "data_flow": "...", "decisions": [...]}.
func ValidateArchitectDeliverable(d map[string]any) error {
	// technology_choices
	techChoices, ok := d["technology_choices"].([]any)
	if !ok || len(techChoices) == 0 {
		return fmt.Errorf("technology_choices is required — provide an array of {category, choice, rationale} objects")
	}
	for i, tc := range techChoices {
		obj, ok := tc.(map[string]any)
		if !ok {
			return fmt.Errorf("technology_choices[%d] must be an object with category, choice, rationale", i)
		}
		cat, _ := obj["category"].(string)
		choice, _ := obj["choice"].(string)
		rationale, _ := obj["rationale"].(string)
		if cat == "" || choice == "" || rationale == "" {
			return fmt.Errorf("technology_choices[%d] requires category, choice, and rationale strings", i)
		}
	}

	// component_boundaries
	components, ok := d["component_boundaries"].([]any)
	if !ok || len(components) == 0 {
		return fmt.Errorf("component_boundaries is required — provide an array of {name, responsibility, dependencies[]} objects")
	}
	for i, cb := range components {
		obj, ok := cb.(map[string]any)
		if !ok {
			return fmt.Errorf("component_boundaries[%d] must be an object with name, responsibility, dependencies", i)
		}
		name, _ := obj["name"].(string)
		resp, _ := obj["responsibility"].(string)
		if name == "" || resp == "" {
			return fmt.Errorf("component_boundaries[%d] requires name and responsibility strings", i)
		}
		if _, hasDeps := obj["dependencies"]; !hasDeps {
			return fmt.Errorf("component_boundaries[%d] requires a dependencies array (may be empty)", i)
		}
	}

	// data_flow
	dataFlow, _ := d["data_flow"].(string)
	if dataFlow == "" {
		return fmt.Errorf("data_flow is required — describe how data moves between components")
	}

	// decisions
	decisions, ok := d["decisions"].([]any)
	if !ok || len(decisions) == 0 {
		return fmt.Errorf("decisions is required — provide an array of {id, title, decision, rationale} objects")
	}
	for i, dec := range decisions {
		obj, ok := dec.(map[string]any)
		if !ok {
			return fmt.Errorf("decisions[%d] must be an object with id, title, decision, rationale", i)
		}
		id, _ := obj["id"].(string)
		title, _ := obj["title"].(string)
		decision, _ := obj["decision"].(string)
		rationale, _ := obj["rationale"].(string)
		if id == "" || title == "" || decision == "" || rationale == "" {
			return fmt.Errorf("decisions[%d] requires id, title, decision, and rationale strings", i)
		}
	}

	if err := validateActors(d); err != nil {
		return err
	}
	return validateIntegrations(d)
}

func validateActors(d map[string]any) error {
	validTypes := map[string]bool{"human": true, "system": true, "scheduler": true, "event": true}
	actors, ok := d["actors"].([]any)
	if !ok || len(actors) == 0 {
		return fmt.Errorf("actors is required — provide an array of {name, type, triggers[]} objects describing who or what initiates actions")
	}
	for i, a := range actors {
		obj, ok := a.(map[string]any)
		if !ok {
			return fmt.Errorf("actors[%d] must be an object with name, type, triggers", i)
		}
		name, _ := obj["name"].(string)
		actorType, _ := obj["type"].(string)
		if name == "" || actorType == "" {
			return fmt.Errorf("actors[%d] requires name and type strings", i)
		}
		if !validTypes[actorType] {
			return fmt.Errorf("actors[%d] type must be one of: human, system, scheduler, event (got %q)", i, actorType)
		}
		if _, hasTriggers := obj["triggers"]; !hasTriggers {
			return fmt.Errorf("actors[%d] requires a triggers array", i)
		}
	}
	return nil
}

func validateIntegrations(d map[string]any) error {
	validDirections := map[string]bool{"inbound": true, "outbound": true, "bidirectional": true}
	integrations, ok := d["integrations"].([]any)
	if !ok || len(integrations) == 0 {
		return fmt.Errorf("integrations is required — provide an array of {name, direction, protocol} objects describing external boundaries")
	}
	for i, ig := range integrations {
		obj, ok := ig.(map[string]any)
		if !ok {
			return fmt.Errorf("integrations[%d] must be an object with name, direction, protocol", i)
		}
		name, _ := obj["name"].(string)
		direction, _ := obj["direction"].(string)
		protocol, _ := obj["protocol"].(string)
		if name == "" || direction == "" || protocol == "" {
			return fmt.Errorf("integrations[%d] requires name, direction, and protocol strings", i)
		}
		if !validDirections[direction] {
			return fmt.Errorf("integrations[%d] direction must be one of: inbound, outbound, bidirectional (got %q)", i, direction)
		}
	}
	return nil
}
