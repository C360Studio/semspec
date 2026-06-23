package terminal

import (
	"github.com/c360studio/semspec/workflow/payloads"
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
func ToolsForEndpoint(reg component.ToolRegistryReader, deliverableType string, ep *ssmodel.EndpointConfig, allowedNames ...string) []agentic.ToolDefinition {
	tools := ToolsForDeliverable(reg, deliverableType, allowedNames...)
	if !EndpointSupportsResponseFormat(ep) {
		return tools
	}
	for i := range tools {
		if tools[i].Name == "submit_work" {
			tools[i].Strict = true
		}
	}
	return tools
}

// SchemaForDeliverable exposes the submit_work parameter schema for a
// deliverable type to out-of-package schema↔struct parity tests. The
// requirement-generator and scenario-generator parse the model's submit_work
// payload into UNEXPORTED structs (requirementItem, llmScenario), so their
// parity tests must run in those packages and need the canonical schema from
// here. This is a read-only thin wrapper over schemaForDeliverable; production
// dispatch code uses ToolsForDeliverable / ToolsForEndpoint, never this.
func SchemaForDeliverable(deliverableType string) map[string]any {
	return schemaForDeliverable(deliverableType)
}

// schemaForDeliverable returns a submit_work parameter schema with named
// properties specific to the given deliverable type. Each role gets only
// the fields it needs — no kitchen-sink union.
func schemaForDeliverable(deliverableType string) map[string]any {
	switch deliverableType {
	case "exploration":
		// ADR-040 Move 1: analyst sub-phase deliverable. Returns the
		// Exploration capability shape. CRITICAL: must be a DIFFERENT
		// schema from plan — when the analyst dispatch was wired with
		// deliverableType="plan", gemini-3-flash anchored on the planSchema
		// and emitted goal/context/scope on every retry (runs #1 + #2,
		// 2026-05-30). The tool definition has stronger pull than persona
		// text because it's the literal function signature the model must
		// call. Smoke runs caught this only after the system-prompt
		// fragment audits proved cleanup wasn't enough.
		return explorationSchema()
	case "plan":
		return planSchema()
	case "requirements":
		return requirementsSchema()
	case "scenarios":
		return scenariosSchema()
	case "stories":
		return storiesSchema()
	case "architecture":
		return architectureSchema()
	case "review":
		return reviewSchema()
	case "recovery":
		return recoverySchema()
	case "qa-review":
		return qaReviewSchema()
	case "lesson":
		return lessonSchema()
	default:
		return developerSchema()
	}
}

// explorationSchema defines the submit_work parameters for the analyst
// sub-phase (ADR-040 Move 1). Schema mirrors workflow.Exploration —
// capabilities array with name/lifecycle/description/depends_on per entry,
// plus optional open_questions string array.
func explorationSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"capabilities": map[string]any{
				"type":        "array",
				"description": "Named capabilities this change introduces or modifies. Each capability becomes its own specification file. REQUIRED, non-empty.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Kebab-case capability identifier (e.g. user-auth, mavsdk-bootstrap). Lowercase letters, digits, and hyphens only; no leading or trailing hyphen.",
						},
						"lifecycle": map[string]any{
							"type":        "string",
							"enum":        []string{"new", "modified"},
							"description": "Whether this capability is new (does not exist in openspec/specs/) or modifies an existing spec.",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "1-3 sentence summary of what the capability covers.",
						},
						"depends_on": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Names of other capabilities this one depends on. Multi-valued. Emit [] when no dependencies.",
						},
						"surfaces": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string", "enum": []string{"ui", "api", "background"}},
							"description": "User-observable surface(s) this capability exposes (ADR-041 Move 2). 'ui' for capabilities with a user-visible interface (web/CLI prompt/forms); 'api' for programmatic surfaces other code or systems consume (REST endpoints, library functions, NATS subjects); 'background' for scheduled or event-driven capabilities with no human surface (cron, watchers, reactive consumers). Most capabilities have exactly one surface. Multi-surface is allowed (e.g., an HTTP endpoint with a UI shell). When in doubt, prefer 'api'. The scenario-generator emits @e2e scenarios ONLY for capabilities containing 'ui' — get this right.",
						},
					},
					"required":             []string{"name", "lifecycle", "description", "depends_on", "surfaces"},
					"additionalProperties": false,
				},
			},
			"open_questions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Analyst-flagged ambiguities for the planner sub-phase to resolve. Emit [] when the request is unambiguous.",
			},
		},
		"required":             []string{"capabilities", "open_questions"},
		"additionalProperties": false,
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
			"constraints": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Hard constraints lifted VERBATIM from the request — the must/must-not rules that bind the WHOLE implementation, not any single requirement. Capture every: prohibition (\"do not stub X\", \"do not hand-roll Y\", \"never Z\"), coverage/quality mandate (\"full coverage\", \"machine-checkable inventory\", \"at least one live test\"), and baseline-preservation requirement (\"preserve existing outputs/inputs\"). These are re-injected into developer, reviewer, and QA prompts, which otherwise never see the original request. Emit [] only when the request states no hard constraints.",
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
		"required":             []string{"goal", "context", "constraints", "scope"},
		"additionalProperties": false,
	}
}

func requirementsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"requirements": map[string]any{
				"type":        "array",
				"description": "List of testable requirements — PRD scope per ADR-043 Move 4. Each Requirement carries intent + acceptance criteria; file-path determination lives downstream (Winston declares implementation_files per component, Sarah selects components per Story). The depends_on edges express capability-level ordering — execution sequencing moves to Story.depends_on after Sarah shards.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Short requirement title",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Detailed requirement description — intent + acceptance criteria. 1-3 sentences of what the system MUST do (SHALL/MUST normative language).",
						},
						"depends_on": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Prerequisite requirement titles. Express capability-level intent ordering (auth before session). Execution-time sequencing (parallel vs. serial) lives on Story.depends_on after the architecture + story-prep phases.",
						},
						"capability_name": map[string]any{
							"type":        "string",
							"description": "Kebab-case name of the capability this requirement implements. When the prompt includes a '## Capabilities' block, set this to one of the listed capability names exactly. When no Capabilities block is present, set to empty string. Field is required on the wire even when empty.",
						},
						"acceptance_criteria": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Concrete, verifiable conditions a test could assert — observable outcomes, NOT a restatement of the title or vague prose ('works correctly'). Each entry is one testable condition the downstream scenario-generator can bind a test to. REQUIRED and MUST be non-empty (at least one condition): the ADR-051 requirements review (R-req) rejects any requirement with empty or prose-only acceptance_criteria. Example: ['mavsdk_server starts and reaches CONNECTED within 10s of driver start', 'driver re-establishes the peer connection after a simulated link drop'].",
						},
					},
					"required":             []string{"title", "description", "depends_on", "capability_name", "acceptance_criteria"},
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
						"tags": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "BDD-style scenario tags (ADR-041 Move 1). MUST contain EXACTLY ONE tier tag: @unit (function/class boundary, fakes only), @integration (services-class or testcontainers-class harness running), @smoke (release-staging environment, scheduled), or @e2e (full-system deployment with UI). Operator-defined facet tags (@flaky, @security, @slow) may follow but are informational. Tag bodies are alphanumeric + '-' only — no ':' or '.' (pytest-bdd / behave compat). The prompt context's required_tiers field tells you which tier tags MUST appear across the scenarios you emit.",
						},
						"harness_profile_ids": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Catalog harness profile IDs this scenario binds to (ADR-041 Move 1). REQUIRED non-empty for @integration scenarios — list the profile_id(s) from the prompt context's required_tiers entries. Empty [] for @unit, @smoke, @e2e — those tiers don't bind to catalog profiles in this layer.",
						},
					},
					"required":             []string{"title", "given", "when", "then", "tags", "harness_profile_ids"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"scenarios"},
		"additionalProperties": false,
	}
}

// storiesSchema defines the submit_work parameters for the story-preparer
// dispatch (ADR-043 Move 3). Sarah shards Requirements into ready-for-dev
// Stories with intra-story Task checklists.
//
// ADR-043 PR 4e — positional/labeled wire shape: Sarah never authors entity
// IDs (story.id, task.id, requirement_id). Instead she emits a local
// `label` string for each story/task and references requirements by
// `requirement_index` (0-indexed into the plan's Requirements array as
// shown in her prompt). Cross-story DependsOn references use story labels;
// intra-story task DependsOn references use task labels. The story-preparer
// component resolves labels + indices to canonical IDs server-side before
// publishing the StoriesGeneratedEvent. This mirrors Bob's pattern from
// PR 4b (Scenario.StoryID populated server-side) and eliminates two
// historical pain points: (1) the LLM doesn't have to fabricate the
// entity-ID format, and (2) mock fixtures don't depend on plan slug.
//
// Strict-mode subset: no minItems / patternProperties. Per-Story invariants
// (≥1 source file, ≥1 task, valid component refs, DAG without cycles) are
// enforced server-side after label-to-ID resolution.
func storiesSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"stories": map[string]any{
				"type":        "array",
				"description": "Stories shaped per ADR-044 M:N coverage. Sarah is the product owner: emit one Story per architectural component, covering every Requirement and Capability that component implements. FilesOwned is system-derived from component.implementation_files — you do NOT pick files. Cross-Story DependsOn is system-derived from (a) Requirement prereq closure and (b) file-ownership conflicts — you do NOT pick edges. Your readiness gate: every Capability appears in ≥1 capability_indices; every Requirement appears in ≥1 requirement_indices; component_name resolves to a declared component_boundaries entry; tasks is non-empty.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label": map[string]any{
							"type":        "string",
							"description": "Local label for this story (kebab-case). Used only as a stable handle within this submit_work call. The story-preparer component resolves your labels into canonical Story.ID values server-side.",
						},
						"component_name": map[string]any{
							"type":        "string",
							"description": "The ONE architectural component this Story implements — must match a declared component_boundaries[].name in the architecture. FilesOwned is derived from this component's implementation_files by the system; you do not author files. plan-reviewer R3 rule story.unresolved_component rejects values that don't resolve.",
						},
						"requirement_indices": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "integer", "minimum": 0},
							"description": "Zero-based indices into the prompt's Requirements list — the Requirements this Story covers (ADR-044 M:N). One Story may cover many Requirements when they map to the same component. Every plan Requirement MUST appear in at least one Story's requirement_indices; coverage gaps are rejected at parse time.",
						},
						"capability_indices": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "integer", "minimum": 0},
							"description": "Zero-based indices into the prompt's Capabilities list — the Capabilities this Story carries acceptance evidence for (ADR-044 M:N). Every plan Capability MUST appear in at least one Story's capability_indices.",
						},
						"title": map[string]any{
							"type":        "string",
							"description": "Human-readable story heading (sentence-cased).",
						},
						"intent": map[string]any{
							"type":        "string",
							"description": "1-2 sentence description of what implementing this Story proves.",
						},
						"tasks": map[string]any{
							"type":        "array",
							"description": "Sarah-authored ordered TDD checklist. Typical shape is 3-5 tasks per story: write failing tests, implement to pass, integration smoke, verify scenarios. The execution-manager runs tasks in topo order from intra-story depends_on_labels. Emit at least one task.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label": map[string]any{
										"type":        "string",
										"description": "Local label for this task (kebab-case). Used to express intra-story task DependsOn edges. The story-preparer component resolves your labels into canonical Task.ID values server-side.",
									},
									"description": map[string]any{
										"type":        "string",
										"description": "1-line statement of what this task accomplishes (e.g. 'Write failing test for boot lifecycle').",
									},
									"depends_on_labels": map[string]any{
										"type":        "array",
										"items":       map[string]any{"type": "string"},
										"description": "Other task labels WITHIN this story that must reach complete before this task can dispatch. Emit [] when the task has no intra-story prereqs. Cross-story scheduling is system-derived, not Sarah-authored, so do not use this for cross-story edges.",
									},
								},
								"required":             []string{"label", "description", "depends_on_labels"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"label", "component_name", "requirement_indices", "capability_indices", "title", "intent", "tasks"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"stories"},
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
				"description": "Component definitions with name, responsibility, dependencies, upstream_refs, implementation_files, and capability_indices. ADR-043 Move 1 — Winston declares the BMAD tech-spec scope here: every component owns its file space AND maps to capabilities. Sarah uses this in the next phase to shard requirements into ready-for-dev stories.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":           map[string]any{"type": "string"},
						"responsibility": map[string]any{"type": "string"},
						"dependencies": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"upstream_refs": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Names of upstream_resolutions[] entries this component integrates with. Bidirectional with upstream_resolutions[].used_by — both sides must agree. Emit [] when the component has no external integrations.",
						},
						"implementation_files": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Workspace-relative paths this component owns (ADR-043 Move 1). Source these from plan.scope.create for new components or the existing project tree for modified components. Emit at least one entry, and at least one entry MUST be a source-code file (.java/.go/.ts/.py/.rs/…); companion documentation files (.md/.txt) MAY appear alongside source but never alone. The min-1 + at-least-one-source invariants are enforced by workflow.ValidateComponentImplementationFiles at architecture-generator parse time and by plan-reviewer R2 rules architecture.component_missing_implementation_files and architecture.component_implementation_files_doc_only — strict-mode JSON schema does not support minItems so the cardinality lives in the validator.",
						},
						"capability_indices": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "integer", "minimum": 0},
							"description": "0-based indices into the prompt's '## Capabilities (from analyst)' list — the capabilities this component implements (ADR-043 Move 1; indexed form 2026-06-07). Reference capabilities by INDEX, never re-type their names: paraphrased kebab-case names ('typed-control-streams' → 'typed-controlstreams') fail coverage. The system resolves indices to names. Emit at least one entry. Every analyst capability MUST appear in at least one component's capability_indices, else plan-reviewer R2 rule capability.unresolved_in_architecture rejects; an out-of-range index is rejected at architecture-generator parse time.",
						},
					},
					"required":             []string{"name", "responsibility", "dependencies", "upstream_refs", "implementation_files", "capability_indices"},
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
			"upstream_resolutions": map[string]any{
				"type":        "array",
				"description": "External dependencies the architect resolved to concrete coordinates + API surfaces. Populate one entry per external library named anywhere (technology_choices, integrations, component_boundaries.dependencies). The dev no longer has a research sub-agent (shelved 2026-05-15) — these resolutions are the dev's pre-loaded reading list, so populating this field is what prevents the dev from wedging on re-discovery on hard fixtures (take-23 wedge: 35 external file reads + 0 worktree writes). Emit [] when the project is greenfield with no external integrations.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Human label (e.g. 'OpenSensorHub Core'). Stable across version bumps so cross-references survive. Used for component_boundaries[].upstream_refs linkage.",
						},
						"coordinate": map[string]any{
							"type":        "string",
							"description": "Machine-resolvable identifier the dev pastes into the build manifest. Examples: 'org.sensorhub:sensorhub-core:2.0.0', 'npm:react@18.2.0', 'github.com/opensensorhub/osh-core@v2.0.0'. A vague hint like 'OSH 2.x' is NOT a coordinate — re-fetch and find the specific version.",
						},
						"source_ref": map[string]any{
							"type":        "string",
							"description": "URL or file path proving the coordinate is valid (sonatype page, package-lock entry, github release tag URL).",
						},
						"resolution_kind": map[string]any{
							"type":        []any{"string", "null"},
							"description": "How the dev consumes a CODE artifact, which the SYSTEM re-verifies. 'maven_central' = published jar (coordinate must resolve on Maven Central; gradle GROUP + git tag is NOT proof). 'source_build' = no published jar, dev clones+builds from source (source_ref must be a resolvable git repo/tag). 'kmp_multiplatform' = Kotlin Multiplatform, the -jvm artifact resolves on Central. 'unresolved' = code artifact searched for but none consumable (honest outcome — never fabricate a coordinate). null for non-code entries (e.g. an integration_target service/endpoint like a SITL or broker that has no jar/package). A maven_central coordinate that returns 0 results on Central is rejected.",
							"enum":        []any{"maven_central", "source_build", "kmp_multiplatform", "unresolved", nil},
						},
						"apis": map[string]any{
							"type":        "array",
							"description": "Specific symbols the dev will integrate against (constructors, methods, config fields). At least one entry is the architect's normal contribution — without any APIs this resolution is just a build-manifest pin with no usage guidance, which the reviewer flags as incomplete (criterion 7a).",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"symbol": map[string]any{
										"type":        "string",
										"description": "Name as the dev will reference it in code. For methods include the qualifier: 'Connection.send' not 'send'.",
									},
									"import": map[string]any{
										"type":        []any{"string", "null"},
										"description": "Fully-qualified, paste-ready reference the dev imports — REQUIRED for code-symbol kinds (class, interface, type, function, annotation, constant), verified against the artifact (jar tf / unzip / source). Example: 'io.mavsdk.System', not bare 'System'. null only for config_field. A bare or empty import for a code symbol is rejected by ValidateUpstreamImports at parse time.",
									},
									"artifact": map[string]any{
										"type":        []any{"string", "null"},
										"description": "The coordinate the symbol actually resolves in, when it differs from the parent resolution's coordinate (a library split across artifacts, e.g. mavsdk-server vs mavsdk). null when the symbol lives in the parent coordinate (the common case).",
									},
									"kind": map[string]any{
										"type":        "string",
										"description": "Classifier so the dev knows what shape to expect.",
										"enum":        []string{"class", "method", "interface", "function", "config_field", "constant", "type", "annotation"},
									},
									"signature": map[string]any{
										"type":        "string",
										"description": "Type-level shape. For methods/functions: full signature including parameters and return type. For classes/interfaces: constructor or 'class X extends Y' form. Example: 'protected AbstractSensorModule(SensorConfig config)'.",
									},
									"lifecycle": map[string]any{
										"type":        []any{"string", "null"},
										"description": "Calling convention or expected sequence when the surface has one. Example: 'init(config) -> start() -> stop()'. Set null when the surface is a single-call utility with no lifecycle.",
									},
									"notes": map[string]any{
										"type":        []any{"string", "null"},
										"description": "Constraints or preconditions the signature alone doesn't convey: 'throws X if Y', 'must be called from main thread', 'config must include Z field'. Set null when there are no extra constraints.",
									},
									"citation": map[string]any{
										"type":        "string",
										"description": "File path or URL where the architect verified this surface. REQUIRED. An uncited surface is a guess; reviewer will reject.",
									},
								},
								"required":             []string{"symbol", "import", "artifact", "kind", "signature", "lifecycle", "notes", "citation"},
								"additionalProperties": false,
							},
						},
						"used_by": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "component_boundaries[].name entries that depend on this resolution. Bidirectional with component_boundaries[].upstream_refs — keeps 'what depends on this lib?' answerable without scanning every component.",
						},
						"role": map[string]any{
							"type":        "string",
							"description": "How this dep is consumed at test time. 'build_dep' = compile-time only (annotation processor, type stubs, codegen). 'runtime_dep' = library/framework called in-process; unit tests use it directly. 'integration_target' = separate process the dev talks to over a wire protocol (daemon, broker, database, SITL/autopilot endpoint); requires a selected architecture.harness_profiles[] entry that covers it. Default to 'runtime_dep' when uncertain — it's the most common case.",
							"enum":        []string{"build_dep", "runtime_dep", "integration_target"},
						},
					},
					"required":             []string{"name", "coordinate", "source_ref", "resolution_kind", "apis", "used_by", "role"},
					"additionalProperties": false,
				},
			},
			"harness_profiles": map[string]any{
				"type":        "array",
				"description": "Catalog-backed test environment profile selections. Populate with profile IDs from the catalog only. The catalog owns images, ports, readiness, required test assertions, and runner compatibility. Emit [] when no integration_target upstreams need a test environment.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"profile_id": map[string]any{
							"type":        "string",
							"description": "Stable test environment profile ID from the catalog, for example 'mavlink.px4-sitl.mavsdk-smoke'. Do not invent IDs.",
						},
						"used_by": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "component_boundaries[].name entries that should use this profile.",
						},
						"purpose": map[string]any{
							"type":        "string",
							"description": "Why this profile is selected for this architecture.",
						},
						"covers": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Integration targets, protocol facets, plugin groups, or scenario names this profile covers. Emit [] when used_by is sufficient.",
						},
					},
					"required":             []string{"profile_id", "used_by", "purpose", "covers"},
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
		"required":             []string{"technology_choices", "component_boundaries", "data_flow", "decisions", "actors", "integrations", "upstream_resolutions", "harness_profiles", "test_surface"},
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

func recoverySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Recovery action to apply. Pick one closed-set value and write it first.",
				"enum": []string{
					string(payloads.RecoveryActionRefinePrompt),
					string(payloads.RecoveryActionNarrowScope),
					string(payloads.RecoveryActionSplitReq),
					string(payloads.RecoveryActionStoryReprepare),
					string(payloads.RecoveryActionArchitectureRevise),
					string(payloads.RecoveryActionEscalateHuman),
					string(payloads.RecoveryActionMarkUnrecoverable),
				},
			},
			"diagnosis": map[string]any{
				"type":        "string",
				"description": "2-6 sentences explaining what the trajectory shows, why the prior agent wedged, and why the selected action fits.",
			},
			"recovery_succeeded": map[string]any{
				"type":        "boolean",
				"description": "true when the selected action plausibly fixes the wedge; false for escalate_human or mark_unrecoverable.",
			},
			"contract_impact": map[string]any{
				"type":        "object",
				"description": "How accepting this recovery affects the authoritative contract.",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"description": "preserve keeps the accepted contract; refine changes downstream shape while preserving obligations; change mutates obligations/topology/scope.",
						"enum":        []string{"preserve", "refine", "change"},
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Concise contract-impact summary.",
					},
					"affected_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Requirement, story, scenario, or capability IDs affected by the action. Emit [] when the impact is plan-wide or not ID-specific.",
					},
				},
				"required":             []string{"kind", "summary", "affected_ids"},
				"additionalProperties": false,
			},
			"refined_prompt": map[string]any{
				"type":        []any{"string", "null"},
				"description": "Complete replacement prompt for refine_prompt. Set null for every other action.",
			},
			"scope_changes": map[string]any{
				"type":        "object",
				"description": "Structured narrowing/splitting summary. For non narrow_scope/split_req actions, set summary to empty string and arrays to [].",
				"properties": map[string]any{
					"summary": map[string]any{
						"type":        "string",
						"description": "What scope is being narrowed or split, and why.",
					},
					"keep": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Files, concerns, or requirement slices that remain in scope. Emit [] when not applicable.",
					},
					"drop": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Files, concerns, or requirement slices to remove from the immediate retry. Emit [] when not applicable.",
					},
					"split_requirements": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Proposed smaller requirement titles or slices for split_req. Emit [] when not applicable.",
					},
				},
				"required":             []string{"summary", "keep", "drop", "split_requirements"},
				"additionalProperties": false,
			},
		},
		"required":             []string{"action", "diagnosis", "recovery_succeeded", "contract_impact", "refined_prompt", "scope_changes"},
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
			"file_intents": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Workspace-relative path from files_modified",
						},
						"intent": map[string]any{
							"type": "string",
							"enum": []string{
								"modified_existing",
								"owned_deliverable",
								"companion_test",
								"planning_gap_required_file",
								"scratch_or_probe",
							},
							"description": "Declared purpose of this file. Use scratch_or_probe for throwaway/probe files; use planning_gap_required_file when implementation requires a new source/test file outside the declared story scope.",
						},
						"rationale": map[string]any{
							"type":        "string",
							"description": "Brief reason this file belongs in the selected intent bucket.",
						},
					},
					"required":             []string{"path", "intent", "rationale"},
					"additionalProperties": false,
				},
				"description": "One entry per files_modified path declaring whether the change is an existing-file edit, owned deliverable, companion test, planning-gap required file, or scratch/probe.",
			},
		},
		"required":             []string{"summary", "files_modified", "file_intents"},
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
				"description": "Release-readiness verdict: approved (ship it, all-green); conditionally_approved (build + all executed tests pass, but some tests were SKIPPED because they need an environment this sandbox can't provide — e.g. a live SITL/hardware endpoint — and you judged every such skip a legitimate deferral; terminal but NOT all-green; name the deferred behavior, to be verified in operator-CI e2e; use this INSTEAD of approved whenever tests were skipped for legitimate environmental reasons); needs_changes (fixable with change proposals); rejected (escalate to human — cannot be automatically retried)",
				"enum":        []string{"approved", "conditionally_approved", "needs_changes", "rejected"},
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Concise executive summary of the QA verdict. REQUIRED. State what was assessed, what passed, what failed, and the overall recommendation.",
			},
			"dimensions": map[string]any{
				"type":        "object",
				"description": "Per-axis quality assessment. Populate only the dimensions appropriate to the qa.level (synthesis: requirement_fulfillment + capability_evidence; unit adds coverage/assertion_quality/regression_surface; integration/full add flake_judgment). Emit empty strings (\"\") for dimensions that don't apply to the current level.",
				"properties": map[string]any{
					"requirement_fulfillment": map[string]any{
						"type":        "string",
						"description": "Did the implementation satisfy each requirement's intent? Note any requirement with no test coverage or with a scenario that went unimplemented.",
					},
					"capability_evidence": map[string]any{
						"type":        "string",
						"description": "ADR-044 M:N release-readiness gate. Does every Capability declared by the plan have evidence from at least one shipped Story (Story.Status == complete)? The Capability evidence rollup in the user prompt flags ❌ (no Story claims coverage) and ⚠ (claimed but unshipped). Both are blocking unless the qa.level is synthesis-only. Cite the offending Capability name(s) and recommend a PlanDecision when evidence is missing.",
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
				"required":             []string{"requirement_fulfillment", "capability_evidence", "coverage", "assertion_quality", "regression_surface", "flake_judgment"},
				"additionalProperties": false,
			},
			"plan_decisions": map[string]any{
				"type":        "array",
				"description": "Structured change proposals. Populate ONLY when verdict is needs_changes; emit [] when verdict is approved, conditionally_approved, or rejected.",
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
