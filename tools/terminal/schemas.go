package terminal

import (
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
)

// ToolsForDeliverable returns the supplied registry's tools filtered to the
// allowed set, with submit_work's schema replaced by a role-specific version.
// If allowedNames is empty, all registered tools are included (backward compat).
//
// Pass deps.ToolRegistry from the calling component. A nil registry is treated
// as "no tools registered" — the function returns an empty slice rather than
// panicking, matching the behaviour of an unpopulated registry.
func ToolsForDeliverable(reg component.ToolRegistryReader, deliverableType string, allowedNames ...string) []agentic.ToolDefinition {
	var allTools []agentic.ToolDefinition
	if reg != nil {
		allTools = reg.ListTools()
	}
	schema := schemaForDeliverable(deliverableType)

	var allowed map[string]bool
	if len(allowedNames) > 0 {
		allowed = make(map[string]bool, len(allowedNames))
		for _, n := range allowedNames {
			allowed[n] = true
		}
	}

	var result []agentic.ToolDefinition
	for _, t := range allTools {
		if allowed != nil && !allowed[t.Name] {
			continue
		}
		if t.Name == "submit_work" {
			t.Parameters = schema
		}
		result = append(result, t)
	}
	return result
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
	case "qa-review":
		return qaReviewSchema()
	case "lesson":
		return lessonSchema()
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
				"description": "List of testable requirements. Each requirement runs in a parallel git worktree at execution time; files_owned and depends_on are how the planner tells the executor whether two requirements can run concurrently or must serialize. Without these, the validator rejects.",
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
						"files_owned": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "MANDATORY when there's more than one requirement. Workspace-relative paths this requirement is allowed to modify, drawn from plan.scope.include. Required by the partition validator: the executor uses this to decide whether two requirements can run in parallel branches or must serialize. Empty arrays are rejected.",
						},
						"depends_on": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Optional list of prerequisite requirement titles. Use when one requirement must finish before another, or when two requirements legitimately need to write to the same file (impl + its test, define + use). The executor sequences depends_on chains so the dependent rebases on the prerequisite's merge commit.",
						},
					},
					"required": []string{"title", "description", "files_owned"},
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
			"actors": map[string]any{
				"type":        "array",
				"description": "Who or what initiates actions in the system",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"type": map[string]any{
							"type": "string",
							"enum": []string{"human", "system", "scheduler", "event"},
						},
						"triggers": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"permissions": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{"name", "type", "triggers"},
				},
			},
			"integrations": map[string]any{
				"type":        "array",
				"description": "External boundaries the system touches",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"direction": map[string]any{
							"type": "string",
							"enum": []string{"inbound", "outbound", "bidirectional"},
						},
						"protocol":   map[string]any{"type": "string"},
						"contract":   map[string]any{"type": "string"},
						"error_mode": map[string]any{"type": "string"},
					},
					"required": []string{"name", "direction", "protocol"},
				},
			},
			"test_surface": map[string]any{
				"type":        "object",
				"description": "The test coverage surface this architecture implies. Consumed by developer role to guide integration/e2e test authoring, and by qa-reviewer to judge coverage adequacy. Derive integration_flows from integrations[] (each external boundary deserves an integration test). Derive e2e_flows from actors[] (each human/system actor triggers a user-visible flow worth end-to-end coverage).",
				"properties": map[string]any{
					"integration_flows": map[string]any{
						"type":        "array",
						"description": "Cross-component flows that need integration-level tests (real service fixtures, not unit mocks)",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":                map[string]any{"type": "string", "description": "Short flow name, kebab-case"},
								"components_involved": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "component_boundaries[].name entries this flow touches"},
								"scenario_refs":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Scenario IDs that must verify this flow"},
								"description":         map[string]any{"type": "string", "description": "What the flow does and why it needs integration testing"},
							},
							"required": []string{"name", "components_involved", "description"},
						},
					},
					"e2e_flows": map[string]any{
						"type":        "array",
						"description": "Actor-driven user-visible flows that need end-to-end tests (browser, full stack, real data)",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"actor":            map[string]any{"type": "string", "description": "actors[].name entry that initiates this flow"},
								"steps":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Ordered user actions the test should perform"},
								"success_criteria": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Observable conditions that mean the flow succeeded"},
							},
							"required": []string{"actor", "steps", "success_criteria"},
						},
					},
				},
			},
		},
		"required": []string{"technology_choices", "component_boundaries", "data_flow", "decisions", "actors", "integrations"},
	}
}

func reviewSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{
				"type":        "string",
				"description": "Review verdict: approved, rejected, or needs_changes",
				"enum":        []string{"approved", "rejected", "needs_changes"},
			},
			"feedback": map[string]any{
				"type":        "string",
				"description": "Specific, actionable review feedback. REQUIRED on rejected/needs_changes: detail WHAT must change and WHY",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Brief review summary",
			},
			"rejection_type": map[string]any{
				"type":        "string",
				"description": "Rejection category: fixable (specific issues, retry with feedback) or restructure (approach is wrong, start over)",
				"enum":        []string{"fixable", "restructure"},
			},
			"findings": map[string]any{
				"type":        "array",
				"description": "SOP compliance findings",
			},
			"scenario_verdicts": map[string]any{
				"type":        "array",
				"description": "Per-scenario pass/fail verdicts",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"scenario_id": map[string]any{"type": "string", "description": "Scenario identifier"},
						"passed":      map[string]any{"type": "boolean", "description": "Whether the scenario passed"},
						"feedback":    map[string]any{"type": "string", "description": "Per-scenario feedback"},
					},
					"required": []string{"scenario_id", "passed"},
				},
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

// lessonSchema returns the submit_work parameter schema for the
// lesson-decomposer role (ADR-033 Phase 2b). The decomposer emits one
// audited lesson per rejection — Detail traces the root cause with file:line
// evidence, InjectionForm is the ≤80-token form rendered into future agent
// prompts. Required fields match what workflow/lessons.Writer enforces in
// Phase 3: at least one of evidence_steps or evidence_files must be
// populated by the decomposer (the writer's strict mode rejects otherwise).
func lessonSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "1-2 sentence actionable summary of the lesson — short title used for indexing and counts.",
			},
			"detail": map[string]any{
				"type":        "string",
				"description": "Long-form root-cause narrative. Used for audit and human review. Every claim should trace to evidence_steps or evidence_files.",
			},
			"injection_form": map[string]any{
				"type":        "string",
				"description": "Compressed case-study text (≤80 tokens) rendered into future agent prompts. Frame as concrete advice for the next agent, not retrospective narration about this run.",
			},
			"category_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Category IDs from the supplied error_categories catalog. Inventing new IDs hurts retirement and ranking — match the closest existing category.",
			},
			"root_cause_role": map[string]any{
				"type":        "string",
				"description": "Role responsible for the upstream defect. Often the same as the role that surfaced the failure, but may be planner / scenario-generator / architect when the work was set up to fail.",
			},
			"evidence_steps": map[string]any{
				"type":        "array",
				"description": "Trajectory step citations. Each entry points to a step in the developer (or reviewer) loop that captured the failure pattern.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"loop_id": map[string]any{
							"type":        "string",
							"description": "Agentic-loop ID — typically the developer's loop that produced the rejected code.",
						},
						"step_index": map[string]any{
							"type":        "integer",
							"description": "Zero-based step index inside the trajectory.",
						},
					},
					"required": []string{"loop_id", "step_index"},
				},
			},
			"evidence_files": map[string]any{
				"type":        "array",
				"description": "File-region citations. Used by the retirement sweep (Phase 5) to expire lessons whose cited code has been rewritten or deleted.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":       map[string]any{"type": "string", "description": "Workspace-relative file path."},
						"line_start": map[string]any{"type": "integer", "description": "First cited line (1-based). Omit for whole-file citation."},
						"line_end":   map[string]any{"type": "integer", "description": "Last cited line (1-based, inclusive). Omit for whole-file citation."},
						"commit_sha": map[string]any{"type": "string", "description": "Commit SHA the citation refers to. Used by the retirement sweep to detect drift."},
					},
					"required": []string{"path"},
				},
			},
		},
		"required": []string{"summary", "detail", "injection_form", "root_cause_role"},
	}
}

func qaReviewSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{
				"type":        "string",
				"description": "Release-readiness verdict: approved (ship it), needs_changes (fixable with change proposals), or rejected (escalate to human — cannot be automatically retried)",
				"enum":        []string{"approved", "needs_changes", "rejected"},
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Concise executive summary of the QA verdict. REQUIRED. State what was assessed, what passed, what failed, and the overall recommendation.",
			},
			"dimensions": map[string]any{
				"type":        "object",
				"description": "Per-axis quality assessment. Populate only the dimensions appropriate to the qa.level (synthesis: requirement_fulfillment only; unit adds coverage/assertion_quality/regression_surface; integration/full add flake_judgment). Leave unpopulated dimensions as empty strings.",
				"properties": map[string]any{
					"requirement_fulfillment": map[string]any{
						"type":        "string",
						"description": "Did the implementation satisfy each requirement's intent? Note any requirement with no test coverage or with a scenario that went unimplemented.",
					},
					"coverage": map[string]any{
						"type":        "string",
						"description": "Level ≥ unit. Is the test suite's coverage adequate for the risk surface? Are critical paths exercised? Note any obvious gaps.",
					},
					"assertion_quality": map[string]any{
						"type":        "string",
						"description": "Level ≥ unit. Are test assertions meaningful and specific, or do they rubber-stamp behavior? Note any tests that can never fail or that assert on irrelevant properties.",
					},
					"regression_surface": map[string]any{
						"type":        "string",
						"description": "Level ≥ unit. What existing behavior is at risk from this change? Are the files modified covered by the test suite? Note any change in behavior-sensitive code with no corresponding test.",
					},
					"flake_judgment": map[string]any{
						"type":        "string",
						"description": "Level ≥ integration. Do failures look like genuine defects or likely test flakiness (timing, environment, network)? What evidence supports your judgment?",
					},
				},
			},
			"plan_decisions": map[string]any{
				"type":        "array",
				"description": "Structured change proposals. Populate ONLY when verdict is needs_changes. Each proposal targets a specific fixable defect that a developer can address in a subsequent execution cycle.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Short imperative title for the change (e.g., 'Add error-path test for payment failure')",
						},
						"rationale": map[string]any{
							"type":        "string",
							"description": "Why this change is needed — reference the specific failure, gap, or risk that motivated it",
						},
						"affected_requirement_ids": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "IDs of requirements this change proposal affects",
						},
						"rejection_type": map[string]any{
							"type":        "string",
							"enum":        []string{"fixable", "restructure"},
							"description": "fixable: specific, targeted fix a developer can apply in one cycle; restructure: the requirement or scenario design needs rethinking",
						},
						"artifact_refs": map[string]any{
							"type":        "array",
							"description": "Optional workspace-relative artifact paths that evidence the defect",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path":    map[string]any{"type": "string"},
									"type":    map[string]any{"type": "string", "enum": []string{"screenshot", "log", "trace", "coverage-report"}},
									"purpose": map[string]any{"type": "string"},
								},
								"required": []string{"path", "type"},
							},
						},
					},
					"required": []string{"title", "rationale", "rejection_type"},
				},
			},
		},
		"required": []string{"verdict", "summary"},
	}
}
