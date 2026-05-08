package terminal

import (
	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	ssmodel "github.com/c360studio/semstreams/model"
)

// ToolsForDeliverable returns the supplied registry's tools filtered to the
// allowed set, with submit_work's schema replaced by a role-specific version.
// If allowedNames is empty, all registered tools are included (backward compat).
//
// Pass deps.ToolRegistry from the calling component. A nil registry is treated
// as "no tools registered" — the function returns an empty slice rather than
// panicking, matching the behaviour of an unpopulated registry.
//
// This variant does not set submit_work's Strict flag. Dispatch sites that
// know the resolved endpoint should call ToolsForEndpoint instead so the
// strict-mode tool-calling guarantee (ADR-035) is enabled per-endpoint.
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

// ToolsForEndpoint is ToolsForDeliverable plus Strict-mode tool calling
// (ADR-035). When the endpoint honors OpenAI's strict-mode tool calling
// (per the same provider matrix as response_format — see
// EndpointSupportsResponseFormat), the Strict flag is set true on every
// terminal tool whose schema has been audited as strict-mode-compliant
// (additionalProperties:false everywhere, every property in required,
// nullable types for optional fields). On no-op providers (anthropic,
// Gemini OpenAI-compat) Strict stays zero-value and omitempty drops it
// from the wire.
//
// Audited terminals:
//   - submit_work — schemas in schemas.go (TestSchemasNoAdditionalProperties +
//     TestSchemasRequiredCompleteness pin compliance per deliverable type)
//   - decompose_task — schema in tools/decompose/executor.go (audited
//     2026-05-08 take-14 follow-up; required-completeness enforced and
//     additionalProperties:false on both top-level and nested node items)
func ToolsForEndpoint(reg component.ToolRegistryReader, deliverableType string, ep *ssmodel.EndpointConfig, allowedNames ...string) []agentic.ToolDefinition {
	tools := ToolsForDeliverable(reg, deliverableType, allowedNames...)
	if !EndpointSupportsResponseFormat(ep) {
		return tools
	}
	for i := range tools {
		switch tools[i].Name {
		case "submit_work", "decompose_task":
			tools[i].Strict = true
		}
	}
	return tools
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
				"description": "File scope boundaries. Use 'include' for files that already EXIST and may be modified; use 'create' for files this plan will create that don't exist yet. Putting nonexistent paths in 'include' will be rejected at submit_work — see scope.create description. Emit empty arrays for unused fields, never omit them.",
				"properties": map[string]any{
					"include": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Workspace-relative paths to files that ALREADY EXIST on disk and may be modified by this plan. Every path here is checked against the project filesystem at submit time; nonexistent paths are rejected with a directive RETRY HINT telling you to move them to scope.create. Emit [] when the plan only creates new files.",
					},
					"create": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Workspace-relative paths to files this plan will CREATE that do not yet exist on disk. Use this for new pom.xml, new source files, new test files, etc. Reviewers do NOT flag scope.create entries as hallucinated paths — that is the entire point of the field. Emit [] when the plan only modifies existing files.",
					},
					"exclude": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Files to exclude from the plan. Emit [] when nothing needs explicit exclusion.",
					},
					"do_not_touch": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Protected files that must not be modified. Emit [] when no files need protection beyond the default scope boundary.",
					},
				},
				"required":             []string{"include", "create", "exclude", "do_not_touch"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"goal", "context", "scope"},
		"additionalProperties": false,
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
					"required":             []string{"title", "description", "files_owned", "depends_on"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"requirements"},
		"additionalProperties": false,
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
					"required":             []string{"title", "given", "when", "then"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"scenarios"},
		"additionalProperties": false,
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
					"required":             []string{"category", "choice", "rationale"},
					"additionalProperties": false,
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
					"required":             []string{"name", "responsibility", "dependencies"},
					"additionalProperties": false,
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
					"required":             []string{"id", "title", "decision", "rationale"},
					"additionalProperties": false,
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
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Authorization scopes required by this actor. Emit [] when the actor needs no explicit permissions.",
						},
					},
					"required":             []string{"name", "type", "triggers", "permissions"},
					"additionalProperties": false,
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
						"protocol": map[string]any{"type": "string"},
						"contract": map[string]any{
							"type":        []any{"string", "null"},
							"description": "OpenAPI / proto / schema reference that pins the interface. Set null when the contract is implicit (e.g. internal HTTP not yet specified).",
						},
						"error_mode": map[string]any{
							"type":        []any{"string", "null"},
							"description": "Failure semantics: retry policy, timeout behavior, idempotency. Set null when the integration is too simple to need an explicit error contract.",
						},
					},
					"required":             []string{"name", "direction", "protocol", "contract", "error_mode"},
					"additionalProperties": false,
				},
			},
			"test_surface": map[string]any{
				"type":        "object",
				"description": "The test coverage surface this architecture implies. Consumed by developer role to guide integration/e2e test authoring, and by qa-reviewer to judge coverage adequacy. Derive integration_flows from integrations[] (each external boundary deserves an integration test). Derive e2e_flows from actors[] (each human/system actor triggers a user-visible flow worth end-to-end coverage). Emit empty arrays for either flow type when the architecture has none.",
				"properties": map[string]any{
					"integration_flows": map[string]any{
						"type":        "array",
						"description": "Cross-component flows that need integration-level tests (real service fixtures, not unit mocks). Emit [] when integrations[] is empty.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":                map[string]any{"type": "string", "description": "Short flow name, kebab-case"},
								"components_involved": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "component_boundaries[].name entries this flow touches"},
								"scenario_refs":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Scenario IDs that must verify this flow. Emit [] when scenarios haven't been generated yet."},
								"description":         map[string]any{"type": "string", "description": "What the flow does and why it needs integration testing"},
							},
							"required":             []string{"name", "components_involved", "scenario_refs", "description"},
							"additionalProperties": false,
						},
					},
					"e2e_flows": map[string]any{
						"type":        "array",
						"description": "Actor-driven user-visible flows that need end-to-end tests (browser, full stack, real data). Emit [] when actors[] is empty or none drive a user-visible flow.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"actor":            map[string]any{"type": "string", "description": "actors[].name entry that initiates this flow"},
								"steps":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Ordered user actions the test should perform"},
								"success_criteria": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Observable conditions that mean the flow succeeded"},
							},
							"required":             []string{"actor", "steps", "success_criteria"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"integration_flows", "e2e_flows"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"technology_choices", "component_boundaries", "data_flow", "decisions", "actors", "integrations", "test_surface"},
		"additionalProperties": false,
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
				"type":        []any{"string", "null"},
				"description": "Brief review summary. Set null when feedback alone tells the whole story.",
			},
			"rejection_type": map[string]any{
				"type":        []any{"string", "null"},
				"description": "Rejection category: fixable (specific issues, retry with feedback) or restructure (approach is wrong, start over). Set null when verdict is approved.",
				"enum":        []any{"fixable", "restructure", nil},
			},
			"findings": map[string]any{
				"type":        "array",
				"description": "SOP compliance findings. Emit [] when no SOPs apply or no violations were observed.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"sop_id":     map[string]any{"type": "string", "description": "SOP identifier"},
						"sop_title":  map[string]any{"type": "string", "description": "SOP human-readable title"},
						"severity":   map[string]any{"type": "string", "description": "Finding severity", "enum": []string{"error", "warning", "info"}},
						"status":     map[string]any{"type": "string", "description": "Compliance status", "enum": []string{"violation", "compliant", "not_applicable"}},
						"category":   map[string]any{"type": []any{"string", "null"}, "description": "Finding category. Set null for legacy single-category review."},
						"phase":      map[string]any{"type": []any{"string", "null"}, "description": "Plan phase the finding targets (plan/requirements/architecture/scenarios). Set null for whole-plan findings."},
						"target_id":  map[string]any{"type": []any{"string", "null"}, "description": "Specific entity ID (e.g., REQ-2, SCEN-3). Set null for whole-phase findings."},
						"issue":      map[string]any{"type": []any{"string", "null"}, "description": "Concrete issue description. Set null on compliant findings."},
						"suggestion": map[string]any{"type": []any{"string", "null"}, "description": "Proposed fix. Set null on compliant findings."},
						"evidence":   map[string]any{"type": []any{"string", "null"}, "description": "File/line reference grounding the finding. Set null when evidence is implicit."},
					},
					"required":             []string{"sop_id", "sop_title", "severity", "status", "category", "phase", "target_id", "issue", "suggestion", "evidence"},
					"additionalProperties": false,
				},
			},
			"scenario_verdicts": map[string]any{
				"type":        "array",
				"description": "Per-scenario pass/fail verdicts. Emit [] for plan-level reviews (no scenarios) or when scenarios don't apply.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"scenario_id": map[string]any{"type": "string", "description": "Scenario identifier"},
						"passed":      map[string]any{"type": "boolean", "description": "Whether the scenario passed"},
						"feedback": map[string]any{
							"type":        []any{"string", "null"},
							"description": "Per-scenario feedback. Set null when the scenario passed cleanly with nothing to add.",
						},
					},
					"required":             []string{"scenario_id", "passed", "feedback"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"verdict", "feedback", "summary", "rejection_type", "findings", "scenario_verdicts"},
		"additionalProperties": false,
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
		"required":             []string{"summary", "files_modified"},
		"additionalProperties": false,
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
				"description": "Category IDs from the supplied error_categories catalog. Inventing new IDs hurts retirement and ranking — match the closest existing category. Emit [] when no catalog category fits and explain in detail.",
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
					"required":             []string{"loop_id", "step_index"},
					"additionalProperties": false,
				},
			},
			"evidence_files": map[string]any{
				"type":        "array",
				"description": "File-region citations. Used by the retirement sweep (Phase 5) to expire lessons whose cited code has been rewritten or deleted. Emit [] when only trajectory steps cite the failure (the writer requires at least one of evidence_steps OR evidence_files non-empty).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":       map[string]any{"type": "string", "description": "Workspace-relative file path."},
						"line_start": map[string]any{"type": []any{"integer", "null"}, "description": "First cited line (1-based). Set null for whole-file citation."},
						"line_end":   map[string]any{"type": []any{"integer", "null"}, "description": "Last cited line (1-based, inclusive). Set null for whole-file citation."},
						"commit_sha": map[string]any{"type": []any{"string", "null"}, "description": "Commit SHA the citation refers to. Used by the retirement sweep to detect drift. Set null when the citation is HEAD-relative."},
					},
					"required":             []string{"path", "line_start", "line_end", "commit_sha"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"summary", "detail", "injection_form", "root_cause_role", "category_ids", "evidence_steps", "evidence_files"},
		"additionalProperties": false,
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
				"description": "Per-axis quality assessment. Populate only the dimensions appropriate to the qa.level (synthesis: requirement_fulfillment only; unit adds coverage/assertion_quality/regression_surface; integration/full add flake_judgment). Emit empty strings (\"\") for dimensions that don't apply to the current level.",
				"properties": map[string]any{
					"requirement_fulfillment": map[string]any{
						"type":        "string",
						"description": "Did the implementation satisfy each requirement's intent? Note any requirement with no test coverage or with a scenario that went unimplemented.",
					},
					"coverage": map[string]any{
						"type":        "string",
						"description": "Level ≥ unit. Is the test suite's coverage adequate for the risk surface? Are critical paths exercised? Note any obvious gaps. Emit \"\" at synthesis level.",
					},
					"assertion_quality": map[string]any{
						"type":        "string",
						"description": "Level ≥ unit. Are test assertions meaningful and specific, or do they rubber-stamp behavior? Note any tests that can never fail or that assert on irrelevant properties. Emit \"\" at synthesis level.",
					},
					"regression_surface": map[string]any{
						"type":        "string",
						"description": "Level ≥ unit. What existing behavior is at risk from this change? Are the files modified covered by the test suite? Note any change in behavior-sensitive code with no corresponding test. Emit \"\" at synthesis level.",
					},
					"flake_judgment": map[string]any{
						"type":        "string",
						"description": "Level ≥ integration. Do failures look like genuine defects or likely test flakiness (timing, environment, network)? What evidence supports your judgment? Emit \"\" at synthesis or unit level.",
					},
				},
				"required":             []string{"requirement_fulfillment", "coverage", "assertion_quality", "regression_surface", "flake_judgment"},
				"additionalProperties": false,
			},
			"plan_decisions": map[string]any{
				"type":        "array",
				"description": "Structured change proposals. Populate ONLY when verdict is needs_changes; emit [] when verdict is approved or rejected.",
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
							"description": "IDs of requirements this change proposal affects. Emit [] when the proposal targets the plan as a whole.",
						},
						"rejection_type": map[string]any{
							"type":        "string",
							"enum":        []string{"fixable", "restructure"},
							"description": "fixable: specific, targeted fix a developer can apply in one cycle; restructure: the requirement or scenario design needs rethinking",
						},
						"artifact_refs": map[string]any{
							"type":        "array",
							"description": "Workspace-relative artifact paths that evidence the defect. Emit [] when no artifact citation is available (synthesis-level QA, deterministic findings without log capture, etc).",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path":    map[string]any{"type": "string"},
									"type":    map[string]any{"type": "string", "enum": []string{"screenshot", "log", "trace", "coverage-report"}},
									"purpose": map[string]any{"type": "string", "description": "Brief description of why this artifact evidences the defect."},
								},
								"required":             []string{"path", "type", "purpose"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"title", "rationale", "affected_requirement_ids", "rejection_type", "artifact_refs"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"verdict", "summary", "dimensions", "plan_decisions"},
		"additionalProperties": false,
	}
}
