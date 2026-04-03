package terminal

import (
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"

	"github.com/c360studio/semstreams/agentic"
)

// ToolsForDeliverable returns all registered tools with submit_work's schema
// replaced by a role-specific version matching the deliverable type.
// Pass the result as TaskMessage.Tools so the agentic loop uses per-role
// named parameters instead of the generic global schema.
func ToolsForDeliverable(deliverableType string) []agentic.ToolDefinition {
	allTools := agentictools.ListRegisteredTools()
	schema := schemaForDeliverable(deliverableType)
	for i, t := range allTools {
		if t.Name == "submit_work" {
			allTools[i].Parameters = schema
		}
	}
	return allTools
}

// schemaForDeliverable returns a submit_work parameter schema with named
// properties specific to the given deliverable type. Each role gets only
// the fields it needs — no kitchen-sink union.
func schemaForDeliverable(deliverableType string) map[string]any {
	switch deliverableType {
	case "plan":
		return planSchema()
	case "requirements":
		return requirementsSchema()
	case "scenarios":
		return scenariosSchema()
	case "architecture":
		return architectureSchema()
	case "review":
		return reviewSchema()
	default:
		return developerSchema()
	}
}

func planSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"goal": map[string]any{
				"type":        "string",
				"description": "Specific, actionable goal describing what to build or fix",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Current state, why this matters, key constraints",
			},
			"scope": map[string]any{
				"type":        "object",
				"description": "File scope boundaries",
				"properties": map[string]any{
					"include": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Files to include in the plan",
					},
					"exclude": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Files to exclude from the plan",
					},
					"do_not_touch": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Protected files that must not be modified",
					},
				},
			},
		},
		"required": []string{"goal", "context"},
	}
}

func requirementsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"requirements": map[string]any{
				"type":        "array",
				"description": "List of testable requirements",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Short requirement title",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Detailed requirement description",
						},
					},
					"required": []string{"title", "description"},
				},
			},
		},
		"required": []string{"requirements"},
	}
}

func scenariosSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scenarios": map[string]any{
				"type":        "array",
				"description": "BDD scenarios with Given/When/Then",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Scenario title",
						},
						"given": map[string]any{
							"type":        "string",
							"description": "Precondition state",
						},
						"when": map[string]any{
							"type":        "string",
							"description": "Triggering action",
						},
						"then": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Expected outcomes",
						},
					},
					"required": []string{"title", "given", "when", "then"},
				},
			},
		},
		"required": []string{"scenarios"},
	}
}

func architectureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"technology_choices": map[string]any{
				"type":        "array",
				"description": "Technology choices with category, choice, and rationale",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"category":  map[string]any{"type": "string"},
						"choice":    map[string]any{"type": "string"},
						"rationale": map[string]any{"type": "string"},
					},
					"required": []string{"category", "choice", "rationale"},
				},
			},
			"component_boundaries": map[string]any{
				"type":        "array",
				"description": "Component definitions with name, responsibility, and dependencies",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":           map[string]any{"type": "string"},
						"responsibility": map[string]any{"type": "string"},
						"dependencies": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"name", "responsibility", "dependencies"},
				},
			},
			"data_flow": map[string]any{
				"type":        "string",
				"description": "How data moves between components",
			},
			"decisions": map[string]any{
				"type":        "array",
				"description": "Architecture decisions with id, title, decision, and rationale",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":        map[string]any{"type": "string"},
						"title":     map[string]any{"type": "string"},
						"decision":  map[string]any{"type": "string"},
						"rationale": map[string]any{"type": "string"},
					},
					"required": []string{"id", "title", "decision", "rationale"},
				},
			},
		},
		"required": []string{"technology_choices", "component_boundaries", "data_flow", "decisions"},
	}
}

func reviewSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{
				"type":        "string",
				"description": "Review verdict: approved, rejected, or needs_changes",
			},
			"feedback": map[string]any{
				"type":        "string",
				"description": "Specific, actionable review feedback",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Brief review summary",
			},
			"rejection_type": map[string]any{
				"type":        "string",
				"description": "Rejection category: fixable, misscoped, architectural, or too_big",
			},
			"findings": map[string]any{
				"type":        "array",
				"description": "SOP compliance findings",
			},
			"scenario_verdicts": map[string]any{
				"type":        "array",
				"description": "Per-scenario pass/fail verdicts",
			},
		},
		"required": []string{"verdict", "feedback"},
	}
}

func developerSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "Summary of work completed",
			},
			"files_modified": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "List of files created or modified",
			},
		},
		"required": []string{"summary", "files_modified"},
	}
}
