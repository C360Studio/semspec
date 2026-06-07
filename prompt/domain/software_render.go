package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/c360studio/semspec/prompt"
)

// renderRequirementGeneratorPrompt produces the user message for the
// requirement-generator agent. Identical content as the legacy
// processor/requirement-generator/c.buildUserPrompt the registry replaces;
// keeping byte-equivalence is the canary for the broader user-prompt
// migration (Plan B). When this renderer's output drifts from the legacy
// builder's, the migration test in software_render_test.go fails before
// the LLM ever sees a different prompt.
//
//revive:disable-next-line:function-length // structured prompt builder; splitting would obscure the byte-equivalence contract.
func renderRequirementGeneratorPrompt(rg *prompt.RequirementGeneratorContext) string {
	var sb strings.Builder

	// Project file tree — ground truth for files_owned partitioning. Must
	// appear BEFORE the plan so the model reads the inventory before
	// deciding which files each requirement should claim. Empty for
	// greenfield (silently omitted) — model then relies on scope.include
	// + scope.create alone, same as before this section was wired.
	writeRequirementGeneratorProjectFileTree(&sb, rg.ProjectFileTree)

	sb.WriteString("## Plan to Decompose\n\n")
	if rg.Title != "" {
		fmt.Fprintf(&sb, "**Title**: %s\n\n", rg.Title)
	}
	if rg.Goal != "" {
		fmt.Fprintf(&sb, "**Goal**: %s\n\n", rg.Goal)
	}
	if rg.Context != "" {
		fmt.Fprintf(&sb, "**Context**: %s\n\n", rg.Context)
	}
	if len(rg.ScopeInclude) > 0 {
		fmt.Fprintf(&sb, "**Scope Include**: %s\n\n", strings.Join(rg.ScopeInclude, ", "))
	}
	if len(rg.ScopeExclude) > 0 {
		fmt.Fprintf(&sb, "**Scope Exclude**: %s\n\n", strings.Join(rg.ScopeExclude, ", "))
	}
	if len(rg.ScopeDoNotTouch) > 0 {
		fmt.Fprintf(&sb, "**Do Not Touch**: %s\n\n", strings.Join(rg.ScopeDoNotTouch, ", "))
	}

	// ADR-040 Move 2: when the analyst sub-phase ran, render the capability
	// list so John produces 1 Requirement per Capability with capability_name
	// set. Absent on legacy plans (no analyst sub-phase) — back-compat path
	// renders the same as before.
	//
	// SKIPPED on partial-regen flows (ReplaceRequirementIDs populated). The
	// existing-approved requirements block doesn't surface CapabilityName
	// today, so the LLM can't reconcile "produce one per capability" with
	// "regenerate only rejected IDs" — go-reviewer flagged this in PR 2.
	// Follow-up: extend ExistingRequirementSummary with CapabilityName and
	// reframe the directive so partial regen preserves capability discipline.
	// Until then, partial regen runs without the Capabilities block; the
	// plan-manager and plan-reviewer rules still catch missing coverage.
	if len(rg.Capabilities) > 0 && len(rg.ReplaceRequirementIDs) == 0 {
		sb.WriteString("## Capabilities (one Requirement per capability)\n\n")
		sb.WriteString("The analyst classified the user's request into these capabilities. Produce EXACTLY ONE Requirement per capability and set the requirement's `capability_name` field to the capability's `name`.\n\n")
		for _, c := range rg.Capabilities {
			fmt.Fprintf(&sb, "- **%s** (`lifecycle: %s`): %s", c.Name, c.Lifecycle, c.Description)
			if len(c.DependsOn) > 0 {
				fmt.Fprintf(&sb, " *(depends_on: %s)*", strings.Join(c.DependsOn, ", "))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\nRules:\n")
		sb.WriteString("- ONE Requirement per capability — no merging two capabilities into one Requirement, no splitting one capability across multiple Requirements.\n")
		sb.WriteString("- `capability_name` is REQUIRED on every Requirement; it MUST exactly match one of the capability names above.\n")
		sb.WriteString("- Each Requirement carries INTENT + acceptance criteria — what the system MUST do. Implementation file paths are NOT your concern (the architect declares files per component, and the product owner assigns files to stories downstream). Do NOT emit files_owned; the field has been removed from your schema.\n")
		sb.WriteString("- Use SHALL or MUST in the Requirement title and description (RFC 2119 normative language).\n")
		sb.WriteString("- Documentation content (READMEs, coverage matrices, tradeoff write-ups) is NOT a standalone Requirement. It attaches as scenarios under the implementation Requirement that produces the documented behavior.\n")
		sb.WriteString("- Capability `depends_on` relationships translate to Requirement `depends_on` automatically — set the Requirement's `depends_on` to the requirement IDs of the capabilities it depends on (preserve the DAG).\n\n")
	}

	if len(rg.ReplaceRequirementIDs) > 0 {
		sb.WriteString("## Existing Approved Requirements (DO NOT regenerate these)\n\n")
		// Status filter mirrors the legacy builder — only Active surviving
		// requirements are surfaced to the LLM. Deprecated/superseded reqs
		// stay hidden so the LLM can't accidentally depend on them.
		for _, r := range rg.ExistingRequirements {
			if r.Status != "active" {
				continue
			}
			fmt.Fprintf(&sb, "- %s — title: %q\n", r.ID, r.Title)
			if len(r.DependsOn) > 0 {
				fmt.Fprintf(&sb, "  depends_on: %s\n", strings.Join(r.DependsOn, ", "))
			}
		}
		sb.WriteString("\nWhen proposing replacements, preserve the depends_on edges and intent that kept requirements rely on; downstream phases (Winston's architecture, Sarah's stories) bind files to components per ADR-043 Move 4 — Requirements no longer carry files_owned.\n\n")
		sb.WriteString("## Rejected Requirements (regenerate replacements for these only)\n\n")
		for _, id := range rg.ReplaceRequirementIDs {
			reason := rg.RejectionReasons[id]
			if reason == "" {
				reason = "no reason provided"
			}
			fmt.Fprintf(&sb, "- %s: rejected because: %s\n", id, reason)
		}
		sb.WriteString("\nGenerate ONLY replacement requirements for the rejected IDs above.\n")
	} else {
		sb.WriteString("Extract testable requirements from the above plan. Each requirement should represent a distinct behavioral intent that can be independently verified.\n")
	}

	if rg.PreviousError != "" {
		fmt.Fprintf(&sb, "\n## Previous Attempt Failed\n\nYour previous output could not be processed: %s\n\nPlease fix the issue and ensure your response is valid JSON matching the required format.\n", rg.PreviousError)
	}

	if rg.ReviewFindings != "" {
		fmt.Fprintf(&sb, "\n## Previous Review Findings (Address These)\n\nThe previous set of requirements was reviewed and rejected. Address ALL of the following findings:\n\n%s\n%s", rg.ReviewFindings, reviewFindingsActionDirective())
	}

	return sb.String()
}

// reviewFindingsActionDirective is the meta-rule appended to every
// regen prompt's review-findings block. It tells the LLM to anchor on
// the structured "Action: VERB `value` (TO|FROM|IN) `field`" line that
// writeViolationFinding renders before any prose. The prose Suggestion
// can drift toward bidirectional language ("ensure consistency between
// A and B"); the directive is the reviewer's committed direction. Take-24
// hybrid/hard (2026-05-14) escalated because a prose suggestion was
// satisfied in the WRONG direction (regen REMOVED a file from
// requirement.files_owned when the directive said ADD to scope.create).
// Including this rule directly under the findings block keeps the
// guidance close to where the model needs it — adding it to the system
// prompt was tried in a draft and lost attention by the time the
// findings block scrolled into the model's window.
func reviewFindingsActionDirective() string {
	return `
**HOW TO READ FINDINGS:** Each error-severity violation begins with an
` + "`Action:`" + ` line in the form ` + "`Action: VERB `value` (TO|FROM|IN) `field``" + `.
The Action line is the reviewer's committed remediation direction —
EXECUTE IT VERBATIM. Do NOT infer the inverse direction from the prose
Suggestion ("remove X from files_owned to make it consistent with
scope.create" is not a valid satisfaction of "Action: ADD X TO
scope.create"). When in doubt, do exactly what the Action line says
and ignore any prose that points the other way. If the directive
itself is wrong (the reviewer asked for the opposite of what the
plan needs), apply it anyway — the next review round will surface a
new finding pointing at the bad directive, which is the right place
to dispute it.

`
}

// renderAnalystPrompt produces the analyst sub-phase user message (ADR-040
// Move 1). Mary's job here is to identify CAPABILITIES from the user's
// request; her system prompt already carries the persona-specific instructions
// (kebab-case naming, new|modified lifecycle, anti-pattern guards). This
// renderer supplies the per-dispatch title/description grounding plus the
// project file tree so Mary can distinguish new vs modified capabilities
// when an openspec/specs/ directory exists.
func renderAnalystPrompt(p *prompt.AnalystPromptContext) string {
	var sb strings.Builder
	writeProjectFileTree(&sb, p.ProjectFileTree)

	if p.Title == "" {
		return ""
	}

	fmt.Fprintf(&sb, `Identify the CAPABILITIES this change will introduce or modify.

**Title:** %s
`, p.Title)
	if p.Description != "" {
		fmt.Fprintf(&sb, "\n**User request:**\n%s\n", p.Description)
	}

	sb.WriteString(`
## Output schema (REQUIRED)

When your capability list is ready, call submit_work with these JSON fields:

` + "```json" + `
{
  "capabilities": [
    {"name": "mavsdk-bootstrap", "lifecycle": "new", "description": "Boot mavsdk_server and manage peer connection lifecycle."},
    {"name": "telemetry-stream", "lifecycle": "new", "description": "Surface MAVSDK telemetry as a CS API DataStream."}
  ],
  "open_questions": ["Should the coverage matrix be runtime-checked or static?"]
}
` + "```" + `

Rules:
- capabilities is REQUIRED, non-empty.
- name MUST be kebab-case (lowercase, hyphen-separated).
- lifecycle MUST be exactly "new" or "modified".
- description is 1-3 sentences.
- open_questions is optional; emit empty array or omit when the user's request is unambiguous.

Do NOT propose files, scope, requirements, or implementation steps — the planner sub-phase derives all of that from your capabilities.

Respond ONLY via the submit_work tool call. No markdown, no preamble.
`)

	if p.PreviousError != "" {
		sb.WriteString("\n## RETRY NOTE\n\nYour previous attempt failed with this error:\n")
		sb.WriteString(p.PreviousError)
		sb.WriteString("\n\nPlease try again, addressing the issue above.")
	}
	return sb.String()
}

// renderPlannerPrompt produces the planner agent's user message. Two paths:
// fresh creation (Title only) and revision-after-rejection (PreviousPlanJSON
// + RevisionPrompt). PreviousError is appended to either path as a retry note.
// Replaces processor/planner/buildPlannerUserPrompt and the legacy
// workflow/prompts.PlannerPromptWithTitle helper.
func renderPlannerPrompt(p *prompt.PlannerPromptContext) string {
	var sb strings.Builder
	writeProjectFileTree(&sb, p.ProjectFileTree)

	if p.IsRevision && p.RevisionPrompt != "" {
		if p.PreviousPlanJSON != "" {
			sb.WriteString("## Your Previous Plan Output\n\nThis is the plan you produced that was rejected. Update it to address ALL findings below.\n\n```json\n")
			sb.WriteString(p.PreviousPlanJSON)
			sb.WriteString("\n```\n\n")
		}
		sb.WriteString(p.RevisionPrompt)
		// Action-directive meta-rule appended after the findings block so
		// the planner anchors on the structured directive over prose.
		// Same fix-cause as requirement-generator/scenario-generator/
		// architect — see reviewFindingsActionDirective() comments.
		sb.WriteString("\n")
		sb.WriteString(reviewFindingsActionDirective())
		// Re-anchor the revision flow against two failure modes seen on
		// 2026-05-03 v8: (1) the planner ignoring scope.create even when
		// the reviewer suggested it by name, (2) the planner panicking
		// under multi-round rejection and dumping every visible path
		// into scope.include. Repeat the schema rule here in the
		// revision prompt so it fires fresh on every revision turn —
		// the system-prompt fragment alone is too far away in a long
		// conversation for example-anchoring to stick.
		sb.WriteString("\n## Scope Schema Reminder (ALWAYS APPLIES)\n\n")
		sb.WriteString("scope.include = files that ALREADY EXIST and the plan will read or modify.\n")
		sb.WriteString("scope.create  = files the plan will CREATE that don't exist yet.\n")
		sb.WriteString("scope.exclude / scope.do_not_touch = boundaries (rarely used).\n\n")
		sb.WriteString("If the reviewer asks for a new file (test fixture, new module, etc.), put it in scope.create — NEVER in scope.include. Putting non-existent files in include is the most common rejection reason; this field exists exactly for that case.\n\n")
		sb.WriteString("Do NOT enlarge scope to satisfy unrelated criticism. If a finding says 'missing test file', add ONE entry to scope.create (the test file), not the whole project tree. The Project Files block above is for path correctness, not a checklist.\n")
		if p.PreviousError != "" {
			sb.WriteString("\n## RETRY NOTE\n\nYour previous attempt failed with this error:\n")
			sb.WriteString(p.PreviousError)
			sb.WriteString("\n\nPlease try again, addressing the issue above.")
		}
		return sb.String()
	}
	if p.Title == "" {
		// Match legacy semantics: fresh creation requires a title; empty
		// returns an empty user message so the dispatcher can short-circuit.
		return ""
	}
	fmt.Fprintf(&sb, `Create a committed plan for implementation:

**Title:** %s

Read the codebase to understand the current state. If any critical information is missing for implementation, ask questions. Then produce the Goal/Context/Scope structure.`, p.Title)

	// Scope-schema reminder on first-pass planning (mirror of the revision-
	// prompt block above). Caught take 12 (2026-05-08): qwen3-MoE planner
	// produced scope: {include:[], create:[], exclude:[], do_not_touch:[]}
	// for an "Add /health endpoint" task. req-gen then invented
	// files_owned: [health.go, health_test.go]; plan-reviewer correctly
	// flagged the inconsistency; rollback cascaded to drafting; planner
	// re-produced the same empty scope; thrash. Small models skim past
	// the tool-definition schema description — the rule has to appear in
	// the user prompt to anchor first-pass attention.
	sb.WriteString("\n\n## Scope Schema (REQUIRED)\n\n")
	sb.WriteString("scope.include = files that ALREADY EXIST and the plan will read or modify.\n")
	sb.WriteString("scope.create  = files the plan will CREATE that don't exist yet.\n")
	sb.WriteString("scope.exclude / scope.do_not_touch = boundaries (rarely used).\n\n")
	sb.WriteString("**If the plan will create ANY new files, list them in scope.create.** Empty scope.create + scope.include is only valid for read-only / inspection-only plans. A plan that says 'add /health endpoint' will create at least one file (the implementation, the test, or both) — name them.\n\n")
	sb.WriteString("Example shape for a typical 'add a feature' plan:\n")
	sb.WriteString("  scope.include: [\"main.go\"]                  ← existing files we'll modify\n")
	sb.WriteString("  scope.create:  [\"main_test.go\"]             ← test file we'll add\n\n")
	sb.WriteString("Downstream req-gen will only own files that appear in include + create. If you leave scope empty, req-gen still names files for itself and the reviewer rejects the plan for inconsistency. Avoid the loop by being explicit up front.\n")

	if p.PreviousError != "" {
		sb.WriteString("\n\n## RETRY NOTE\n\nYour previous attempt failed with this error:\n")
		sb.WriteString(p.PreviousError)
		sb.WriteString("\n\nPlease try again, addressing the issue above.")
	}
	return sb.String()
}

// writeProjectFileTree renders the workspace file inventory at the top of the
// planner's user prompt. The tree is captured at dispatch time via
// `git ls-files` and is the authoritative grounding for any path the planner
// puts into scope — paths NOT in this list either do not exist or have not
// been committed, and the planner must declare them explicitly as
// "intends-to-create" to be a valid scope entry. Bug-#7 fix from the
// 2026-05-03 /health postmortem: prior runs hallucinated cmd/server/main.go
// despite the actual root having main.go directly, then failed to recover
// across three revision rounds because the planner never re-ran bash.
func writeProjectFileTree(sb *strings.Builder, tree string) {
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return
	}
	sb.WriteString("## Project Files (ground truth — captured at dispatch via git ls-files)\n\n")
	sb.WriteString("Any path you put into scope.include MUST appear in this list, OR be a file the plan explicitly intends to CREATE (new test files, new modules). Hallucinating directories that look idiomatic but don't exist is the most common cause of plan rejection — verify with bash if uncertain.\n\n```\n")
	sb.WriteString(tree)
	if !strings.HasSuffix(tree, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n\n")
}

// writeRequirementGeneratorProjectFileTree renders the `git ls-files`
// snapshot for the requirement-generator as scope-awareness context. Post
// ADR-043 Move 4 John no longer authors file paths — but the tree still
// helps him reason about what capabilities make sense given the existing
// project shape (e.g. recognizing that a Spring-flavored Java project has
// AbstractSensorModule conventions vs a flat Python script). Empty input
// silently omits the section.
func writeRequirementGeneratorProjectFileTree(sb *strings.Builder, tree string) {
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return
	}
	sb.WriteString("## Project Files (ground truth — captured at dispatch via git ls-files)\n\n")
	sb.WriteString("Use this tree to ground your understanding of the project's existing shape — what languages, frameworks, and conventions are in play. You do NOT need to author file paths on Requirements; that work moved to the architect (Winston) and the product owner (Sarah) per ADR-043 Move 4. Use the tree to confirm that the capabilities you describe map onto something real in the codebase.\n\n```\n")
	sb.WriteString(tree)
	if !strings.HasSuffix(tree, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n\n")
}

// writePlanReviewerPriorRound surfaces the reviewer's own previous verdict
// on this plan when it's a revision round (ReviewIteration > 0). Closes the
// take-22 wedge: the reviewer was stateless across revision rounds, so a
// non-deterministic model would re-fire the same complaint shape on the
// revised plan even when the planner addressed it. Surfacing the prior
// findings + iteration budget anchors the reviewer to "did the revision
// resolve what I asked for" rather than re-evaluating from scratch.
//
// No-op on the first review pass (ReviewIteration == 0) and when no
// PreviousFindings text is available — degrades cleanly.
func writePlanReviewerPriorRound(sb *strings.Builder, p *prompt.PlanReviewerPromptContext) {
	if p.ReviewIteration <= 0 || strings.TrimSpace(p.PreviousFindings) == "" {
		return
	}
	sb.WriteString("## Previous Review Round (this is a revision)\n\n")
	if p.MaxReviewIterations > 0 {
		fmt.Fprintf(sb, "This is review iteration %d of %d. After the iteration cap is reached the plan is escalated rather than re-revised.\n\n",
			p.ReviewIteration+1, p.MaxReviewIterations)
	} else {
		fmt.Fprintf(sb, "This is review iteration %d.\n\n", p.ReviewIteration+1)
	}
	sb.WriteString("You previously rejected this plan with the findings below. The planner has revised the plan to address them. Verify the revised plan resolves the prior findings — if it does, approve this round, even if you can imagine further improvements (you have a bounded budget; see iteration count above).\n\n")
	sb.WriteString("**Re-rejecting on the same complaint shape after the planner has attempted to address it produces a wedge.** If your prior finding was \"goal lacks specifics\" and the planner added specifics, the right verdict on this round is approved with whatever residual concerns logged at info severity (not error). Reserve error-severity rejection for a NEW class of issue you didn't flag last round, or a clear failure to address what you asked for.\n\n")
	sb.WriteString("Previous findings:\n\n")
	sb.WriteString("<previous-review trust=\"semspec-internal\">\n")
	sb.WriteString(strings.TrimSpace(p.PreviousFindings))
	sb.WriteString("\n</previous-review>\n\n")
}

// writePlanReviewerProjectFileTree renders the same `git ls-files` snapshot
// for the plan-reviewer with reviewer-appropriate framing. The reviewer's job
// is to verify the planner's scope.include against ground truth — paths in
// scope.include that appear here are valid, paths that don't appear are
// hallucinations UNLESS they're declared in scope.create (creation intent).
// Without this section the reviewer's path-check criterion is asked to
// validate against a tree it never received and weak models default to
// "flag it" on real files. Caught 2026-05-08 take 20: llama-3.3-70b
// false-positived "Hallucinated paths in scope.include" on main.go (a real
// file) two rounds running.
func writePlanReviewerProjectFileTree(sb *strings.Builder, tree string) {
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return
	}
	sb.WriteString("## Project Files (ground truth — captured at dispatch via git ls-files)\n\n")
	sb.WriteString("Use this list to verify the plan's scope.include paths. A path in scope.include that appears in this list IS valid — do NOT flag it as hallucinated. A path in scope.include that does NOT appear here AND is NOT in scope.create is a hallucination (error-severity finding). Paths in scope.create are creation-intent declarations and never need to appear in this list.\n\n```\n")
	sb.WriteString(tree)
	if !strings.HasSuffix(tree, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n\n")
}

// renderStoryPreparerPrompt produces the story-preparer (Sarah) agent's user
// message (ADR-043 Move 3). Sarah's system_prompt + readiness gate prose
// live in configs/presets/bmad.json; this fragment renders her dispatch
// inputs — the plan goal/context, the analyst capabilities, the architect
// component map, and the requirement summaries she's sharding.
//
//revive:disable-next-line:function-length // sequential prompt builder; mirrors renderArchitectPrompt + renderScenarioGeneratorPrompt size.
func renderStoryPreparerPrompt(p *prompt.StoryPreparerPromptContext) string {
	var sb strings.Builder
	sb.WriteString("You are sharding requirements into ready-for-dev Stories with Task checklists.\n\n")
	fmt.Fprintf(&sb, "## Plan: %s\n\n", p.PlanTitle)
	if p.PlanGoal != "" {
		fmt.Fprintf(&sb, "**Goal:** %s\n\n", p.PlanGoal)
	}
	if p.PlanContext != "" {
		fmt.Fprintf(&sb, "**Context:** %s\n\n", p.PlanContext)
	}

	if len(p.Capabilities) > 0 {
		sb.WriteString("## Capabilities (from analyst)\n\n")
		sb.WriteString("Reference these by zero-based `capability_index` in your output (index 0 = first entry below).\n\n")
		for i, c := range p.Capabilities {
			fmt.Fprintf(&sb, "### Capability #%d — %s\n", i, c.Name)
			if c.Description != "" {
				fmt.Fprintf(&sb, "%s\n\n", c.Description)
			}
		}
	}

	if len(p.ArchitectureComponents) > 0 {
		sb.WriteString("## Architecture Components (Winston's tech-spec)\n\n")
		sb.WriteString("Each component declares the files it owns and the capabilities it implements. Pick ONE component per Story via `component_name`; FilesOwned is derived from that component's implementation_files by the system.\n\n")
		for _, comp := range p.ArchitectureComponents {
			fmt.Fprintf(&sb, "### %s\n", comp.Name)
			if comp.Responsibility != "" {
				fmt.Fprintf(&sb, "**Responsibility:** %s\n\n", comp.Responsibility)
			}
			if len(comp.ImplementationFiles) > 0 {
				sb.WriteString("**Implementation files:**\n")
				for _, f := range comp.ImplementationFiles {
					fmt.Fprintf(&sb, "- `%s`\n", f)
				}
				sb.WriteString("\n")
			}
			if len(comp.Capabilities) > 0 {
				fmt.Fprintf(&sb, "**Implements capabilities:** %s\n\n", strings.Join(comp.Capabilities, ", "))
			}
			if len(comp.UpstreamRefs) > 0 {
				fmt.Fprintf(&sb, "**Depends on upstream:** %s\n\n", strings.Join(comp.UpstreamRefs, ", "))
			}
		}
	}

	if archCtx := prompt.FormatArchitectureContext(prompt.ArchitectureProjection{
		Integrations: p.Integrations,
		Upstreams:    p.Upstreams,
	}); archCtx != "" {
		sb.WriteString(archCtx)
		sb.WriteString("\n")
	}

	if len(p.Requirements) > 0 {
		sb.WriteString("## Requirements (John's PRD)\n\n")
		sb.WriteString("Reference these by zero-based `requirement_index` in your output.\n\n")
		for i, r := range p.Requirements {
			fmt.Fprintf(&sb, "### Requirement #%d — %s\n", i, r.Title)
			fmt.Fprintf(&sb, "*(canonical id: %s)*\n\n", r.ID)
			if r.Description != "" {
				fmt.Fprintf(&sb, "%s\n\n", r.Description)
			}
			if len(r.DependsOn) > 0 {
				fmt.Fprintf(&sb, "*Depends on requirements:* %s\n\n", strings.Join(r.DependsOn, ", "))
			}
		}
	}

	sb.WriteString("\n## Your Task\n\n")
	sb.WriteString("Produce Stories that satisfy ALL of these constraints (ADR-044 M:N coverage):\n\n")
	sb.WriteString("1. **Coverage closure** — every Capability appears in at least one Story's `capability_indices`; every Requirement appears in at least one Story's `requirement_indices`.\n")
	sb.WriteString("2. **One component per Story** — `component_name` is a single declared component from the Architecture Components list. FilesOwned is derived from that component's implementation_files by the system; you do NOT pick files.\n")
	sb.WriteString("3. **Cross-Story DependsOn is NOT yours** — the system derives Story.DependsOn from (a) Requirement prerequisite closure and (b) file-ownership conflicts. Focus on coverage joins; the dispatch graph follows.\n\n")
	sb.WriteString("Canonical shape: for each architectural component, emit ONE Story covering every requirement whose capabilities map into that component. For cohesive modules (one component implementing N capabilities), one Story covers all N capabilities + their requirements — no artificial fan-out. For naturally disjoint components, one Story per component.\n\n")
	sb.WriteString("For each Story:\n")
	sb.WriteString("- `label`: any short kebab-case local string (used only as a stable handle within this dispatch).\n")
	sb.WriteString("- `component_name`: ONE declared component from the Architecture list.\n")
	sb.WriteString("- `requirement_indices`: list of 0-based indices into the Requirements list this Story covers.\n")
	sb.WriteString("- `capability_indices`: list of 0-based indices into the Capabilities list this Story covers.\n")
	sb.WriteString("- `title`: human-readable sentence-cased heading.\n")
	sb.WriteString("- `intent`: 1-2 sentences on what implementing the Story proves.\n")
	sb.WriteString("- `tasks`: ordered TDD checklist of 3-5 tasks (write failing test, implement to pass, integration smoke, verify scenarios). Each task has its own `label` for intra-story task DependsOn.\n\n")
	sb.WriteString("Apply your readiness gate before signing off each Story. Any Story that doesn't pass — empty requirement_indices, missing component_name, unresolved component reference, indices out of range — must be flagged back rather than emitted.\n\n")
	sb.WriteString("If a requirement spans multiple components, that is a plan-shape bug: the requirement should be split into per-component requirements upstream. Flag it back rather than authoring a Story with multiple components.\n\n")

	if p.PreviousError != "" {
		fmt.Fprintf(&sb, "## Previous Attempt Failed\n\nYour previous output could not be processed: %s\n\nPlease fix the issue and ensure your response is valid JSON matching the required format.\n\n", p.PreviousError)
	}

	if p.ReviewFindings != "" {
		fmt.Fprintf(&sb, "## Previous Review Findings (Address These)\n\nThe previous set of stories was reviewed and rejected. Address ALL of the following findings:\n\n%s\n%s",
			p.ReviewFindings, reviewFindingsActionDirective())
	}

	// Train C step 5: surface recovery diagnoses on Stories that wedged.
	// plan-manager writes Story.RecoveryHint at PlanDecision-accept time
	// when a story_reprepare proposal is approved; Sarah re-shapes the
	// Story set from scratch, accounting for each diagnosis.
	//
	// Note: the prompt does NOT carry the prior Stories' content (only
	// the IDs the recovery-agent flagged). Sarah re-derives the full
	// Story set from the Requirements + Architecture inputs above, so
	// her emission is fresh — story IDs may change across re-preps.
	// Downstream consumers (Bob's scenarios) re-key on Sarah's new IDs
	// via the handleStoriesMutation wipe-and-replace contract.
	if len(p.StoryRecoveryHints) > 0 {
		sb.WriteString("## Recovery Diagnoses for Previously Wedged Stories\n\n")
		sb.WriteString("Each entry below identifies a Story whose prior execution wedged, plus the recovery-agent's diagnosis of WHY. Re-shape the Story set from scratch using the Requirements + Architecture above, accounting for each diagnosis as you re-author. The prior Story IDs are reference points only — your new emission may use different IDs.\n\n")
		for _, h := range p.StoryRecoveryHints {
			fmt.Fprintf(&sb, "- **%s**: %s\n", h.StoryID, h.Hint)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderScenarioGeneratorPrompt produces the scenario-generator agent's user
// message. Mirrors the legacy workflow/prompts.ScenarioGeneratorPrompt body;
// ArchitectureContext is pre-rendered upstream. ADR-041 Move 3 added the
// "Required tiers" section + tier-tagged JSON output shape.
//
//revive:disable-next-line:function-length // sequential prompt builder; byte-equivalence is the contract.
func renderScenarioGeneratorPrompt(p *prompt.ScenarioGeneratorPromptContext) string {
	archSection := ""
	if p.ArchitectureContext != "" {
		archSection = "\n" + p.ArchitectureContext + "\n"
	}

	contextSection := ""
	if p.PlanContext != "" {
		contextSection = fmt.Sprintf("\n**Context:** %s\n", p.PlanContext)
	}

	tiersSection := renderRequiredTiers(p.RequiredTiers)
	storySection := renderScenarioStorySection(p)
	taskScope := "requirement"
	if p.StoryID != "" {
		taskScope = "story (one slice of the requirement)"
	}

	base := fmt.Sprintf(`You are generating BDD scenarios for a specific %s.

## Plan: %s

**Goal:** %s
%s
## Requirement: %s

%s
%s%s%s
## Your Task

Generate scenarios that define the observable behavior for this %s. Emit AT LEAST ONE scenario for each tier listed in "Required tiers" above; you may emit additional scenarios per tier for edge cases. Each scenario must:
- Describe ONE observable behavior
- Be independently executable — a QA engineer could run it without additional context at the tier's level
- Use specific, measurable outcomes
- Cover the happy path first, then key edge cases
- Carry EXACTLY ONE tier tag (one of @unit / @integration / @smoke / @e2e) matching the tier it describes

**Scenario Design Guidelines:**
- **Given**: Precondition state — what exists before the action. Be specific: "a registered user with a valid session" not "a user exists"
- **When**: The triggering action — what the user or system does. One action per scenario, use active voice
- **Then**: Expected outcomes as an ARRAY of assertions — multiple things to verify. Use specific values where possible: "the response status is 200" not "the request succeeds"

**Tier discipline (load-bearing):**
- @unit scenarios describe in-process behavior with fakes/fixtures. NEVER mention real services, real network endpoints, SITL containers, databases, or peer processes in @unit text.
- @integration scenarios assume the bound harness is RUNNING and its endpoint is read from environment variables. Reference the profile IDs from "Required tiers" verbatim in harness_profile_ids. The test code does NOT start the harness — the operator's CI (or, for testcontainers profiles, the test fixture) does that.
- @e2e scenarios describe a real user driving a real UI through a real browser session.

Do NOT include implementation details — describe WHAT happens at the tier's observation point, not HOW it is implemented.

**Good @unit scenario:**
- tags: ["@unit"]
- given: "a Config struct initialized with the default values"
- when: "the connection-string builder is called with an empty endpoint override"
- then: ["the builder returns the env-fallback string 'udp://0.0.0.0:14540'"]
- harness_profile_ids: []

**Good @integration scenario:**
- tags: ["@integration"]
- given: "the SITL endpoint at env $SITL_ENDPOINT, the mavsdk_server process started by the test harness"
- when: "the MAVSDK driver is configured with that endpoint and started"
- then: ["a MAVLink HEARTBEAT is received within 10 seconds", "the MAVSDK Core connection state transitions to mavsdk_core_connected"]
- harness_profile_ids: ["mavlink.px4-sitl.mavsdk-smoke"]

**Bad scenario (tier crossover — would be flagged scenario.unit_mentions_services):**
- tags: ["@unit"]
- given: "the SITL container is running"  ← SITL is not observable at @unit
- when: ...

## Output Format

Return ONLY valid JSON matching this exact structure:

`+"```json"+`
{
  "scenarios": [
    {
      "title": "config builder returns env fallback when override is empty",
      "given": "a Config struct initialized with the default values",
      "when": "the connection-string builder is called with an empty endpoint override",
      "then": [
        "the builder returns the env-fallback string 'udp://0.0.0.0:14540'"
      ],
      "tags": ["@unit"],
      "harness_profile_ids": []
    },
    {
      "title": "HEARTBEAT observed after driver start against SITL",
      "given": "the SITL endpoint at env $SITL_ENDPOINT, the mavsdk_server process started by the test harness",
      "when": "the MAVSDK driver is configured with that endpoint and started",
      "then": [
        "a MAVLink HEARTBEAT is received within 10 seconds",
        "the MAVSDK Core connection state transitions to mavsdk_core_connected"
      ],
      "tags": ["@integration"],
      "harness_profile_ids": ["mavlink.px4-sitl.mavsdk-smoke"]
    }
  ]
}
`+"```"+`

**Important:** Return ONLY the JSON object, no additional text or explanation.
`, taskScope, p.PlanTitle, p.PlanGoal, contextSection, p.RequirementTitle, p.RequirementDescription, archSection, tiersSection, storySection, taskScope)

	if p.PreviousError != "" {
		base += fmt.Sprintf(`
## Previous Attempt Failed

Your previous output could not be processed: %s

Please fix the issue and ensure your response is valid JSON matching the required format.
`, p.PreviousError)
	}

	if p.ReviewFindings != "" {
		base += fmt.Sprintf(`
## Previous Review Findings (Address These)

The previous set of scenarios was reviewed and rejected. Address ALL of the following findings:

%s
%s`, p.ReviewFindings, reviewFindingsActionDirective())
	}

	return base
}

// renderScenarioStorySection produces the per-Story scope block injected
// into Bob's user prompt when the dispatcher is operating in per-Story
// mode (ADR-043 PR 4j). When StoryID is empty (legacy plans / mock
// fixtures without Sarah Stories) the section is omitted and Bob authors
// scenarios for the whole Requirement. Otherwise the section names the
// Story's title, intent, files_owned, and components so Bob can scope
// scenarios to this specific slice of work.
func renderScenarioStorySection(p *prompt.ScenarioGeneratorPromptContext) string {
	if p.StoryID == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Story (your authoring scope — a slice of the requirement above)\n\n")
	if p.StoryTitle != "" {
		fmt.Fprintf(&sb, "**Title:** %s\n", p.StoryTitle)
	}
	if p.StoryIntent != "" {
		fmt.Fprintf(&sb, "**Intent:** %s\n", p.StoryIntent)
	}
	if p.StoryComponentName != "" {
		fmt.Fprintf(&sb, "**Component:** %s\n", p.StoryComponentName)
	}
	if len(p.StoryFilesOwned) > 0 {
		fmt.Fprintf(&sb, "**Files owned by this story:** %s\n", strings.Join(p.StoryFilesOwned, ", "))
	}
	sb.WriteString("\nAuthor scenarios for THIS Story only — do not duplicate scenarios that belong to sibling Stories on the same Requirement. The reviewer will validate Story-scoped scenarios independently.\n")
	return sb.String()
}

// renderRequiredTiers formats the classifier's tier-emission output as a
// markdown bullet list the scenario-generator persona reads to know which
// tier tags to emit. Empty input yields an empty string (legacy callers
// without classifier wiring fall through silently to the legacy single-
// tier prompt body). ADR-041 Move 3.
func renderRequiredTiers(tiers []prompt.RequiredTier) string {
	if len(tiers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Required tiers\n\nYou MUST emit at least one scenario tagged with each of the following tier tags. Use the listed harness_profile_ids verbatim for any @integration scenarios; copy them into each scenario's harness_profile_ids array.\n")
	for _, t := range tiers {
		if len(t.HarnessProfileIDs) == 0 {
			fmt.Fprintf(&sb, "- `%s` — no harness binding\n", t.Tag)
			continue
		}
		fmt.Fprintf(&sb, "- `%s` — harness profile ids: ", t.Tag)
		for i, id := range t.HarnessProfileIDs {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "`%s`", id)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// renderArchitectPrompt produces the architecture-generator agent's user
// message. Mirrors the legacy workflow/prompts.ArchitectPrompt body.
func renderArchitectPrompt(p *prompt.ArchitectPromptContext) string {
	scopeInclude := formatScopeListLocal(p.ScopeInclude, "all files")
	scopeExclude := formatScopeListLocal(p.ScopeExclude, "none")
	scopeProtected := formatScopeListLocal(p.ScopeProtected, "none")

	var capSection strings.Builder
	if len(p.Capabilities) > 0 {
		capSection.WriteString("\n## Capabilities (from analyst)\n\n")
		capSection.WriteString("Map every capability to a component. In each component_boundaries[] entry, set `capability_indices` to the 0-based indices below (index 0 = first entry) — do NOT re-type capability names; reference them by index so the mapping can't drift. Every capability MUST appear in at least one component's `capability_indices`.\n\n")
		for i, c := range p.Capabilities {
			fmt.Fprintf(&capSection, "### Capability #%d — %s\n", i, c.Name)
			if c.Description != "" {
				fmt.Fprintf(&capSection, "%s\n\n", c.Description)
			}
		}
	}

	var reqSection strings.Builder
	if len(p.Requirements) > 0 {
		reqSection.WriteString("\n## Requirements\n\n")
		for i, r := range p.Requirements {
			fmt.Fprintf(&reqSection, "%d. **%s**: %s\n", i+1, r.Title, r.Description)
		}
	}
	harnessSection := renderArchitectHarnessCards(p.HarnessProfiles)

	prevErr := ""
	if p.PreviousError != "" {
		prevErr = fmt.Sprintf(`

## Previous Attempt Failed

Your previous output could not be processed: %s

Please fix the issue and ensure your deliverable matches the required structure.`, p.PreviousError)
	}

	if p.ReviewFindings != "" {
		// Plan-reviewer rejected the prior round; surface the formatted
		// findings so the architect can avoid re-introducing the
		// architectural shape (actors / integrations / triggers) that
		// the scenarios then hallucinated around. Take 9 (2026-05-08)
		// confirmed arch-gen would otherwise reproduce the same shape
		// every revision round.
		prevErr += fmt.Sprintf(`

## Previous Review Findings (Address These)

The previous round was reviewed and rejected. Read every finding before deciding actor / integration / test_surface shape — repeating the same shape will fail the next review the same way.

%s
%s`, p.ReviewFindings, reviewFindingsActionDirective())
	}

	if p.PreviousArchitectureJSON != "" {
		// Give the architect its prior design so it REVISES rather than
		// rewrites from scratch — a fresh rewrite tends to re-introduce the
		// exact shape the reviewer just rejected. Mirrors the planner's
		// PreviousPlanJSON revision base.
		prevErr += fmt.Sprintf(`

## Previous Architecture (Revise This — Do Not Rewrite From Scratch)

Below is the architecture you produced last round. Start from it and make the MINIMAL changes that address the findings above. Preserve every part that was not flagged — especially resolved upstream_resolutions coordinates and component boundaries that already passed.

`+"```json\n%s\n```", p.PreviousArchitectureJSON)
	}

	return fmt.Sprintf(`Analyze the following plan and its requirements to produce architecture decisions.

**Goal:** %s

**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s
%s
%s
%s
## Your Task

1. Use bash and graph tools to explore the codebase — understand the existing technology stack, project structure, and patterns already in use.
2. Identify technology choices, component boundaries, data flows, and key architecture decisions.
3. Select test harness profiles from the catalog by profile_id when an upstream resolution is an integration_target; do not author images, ports, env, startup order, or readiness inline.
4. Call submit_work with a summary and a structured deliverable.

**Guidelines:**
- Reuse existing technology stack where possible — do not propose new frameworks when the project already has one
- Focus on structure and boundaries, not implementation details
- Justify every decision with a rationale
- Flag architectural risks or trade-offs
- Component boundaries should reflect natural module/service divisions in the codebase

## Deliverable Structure

**Required fields** — downstream code depends on these:

- **actors**: array of {name, type, triggers[], permissions[]?} — who or what initiates actions in the system (type: human | system | scheduler | event). Every trigger the system responds to must map to an actor. scenario-generator reads this to seed e2e scenarios.
- **integrations**: array of {name, direction, protocol, contract?, error_mode?} — external boundaries the system touches (direction: inbound | outbound | bidirectional; protocol: http | nats | grpc | db | filesystem). Include error_mode when failure behavior matters. scenario-generator reads this to seed integration scenarios.
- **harness_profiles**: array of {profile_id, used_by[], purpose, covers[]?} — catalog profile selections that prove integration_target upstreams. Use profile IDs from the catalog section only. Every upstream_resolutions[] entry with role="integration_target" must be covered by at least one selected profile, either by covers[] naming the target/facet or used_by[] naming the component that reaches it.
- **test_surface**: object describing the test coverage your architecture implies. execution-manager uses it to guide developer agents; qa-reviewer uses it to judge whether actual coverage matches the architectural expectation.
  - **integration_flows**: array of {name, components_involved[], description, scenario_refs[]?} — each integration[] (especially inbound/bidirectional) deserves one integration flow with a real fixture. components_involved references component_boundaries[].name when those exist.
  - **e2e_flows**: array of {actor, steps[], success_criteria[]} — each human/system actor that drives a user-visible outcome deserves one end-to-end flow.
  - At least one of integration_flows or e2e_flows must be non-empty.

**Optional fields** — human documentation in plan.md; only include when they add real value:

- **technology_choices**: array of {category, choice, rationale} — when introducing or formally endorsing a stack choice. Skip when reusing whatever the project already has.
- **component_boundaries**: array of {name, responsibility, dependencies[], implementation_files[], capability_indices[]} — every component maps to capabilities via capability_indices (0-based indices into the Capabilities list above), NOT re-typed names. Every analyst capability must appear in at least one component's capability_indices.
- **data_flow**: string — when data movement between components is non-obvious. Skip for trivial flows.
- **decisions**: array of {id, title, decision, rationale} — architecture decision records (use IDs like ARCH-001) for trade-offs future contributors will want to understand. Skip for routine choices.

**Deriving test_surface:**
- Walk integrations[]: each entry that goes inbound or bidirectional needs at minimum one integration_flow that validates the contract and the error_mode. Outbound-only integrations (we call them, they don't call us) also need coverage when failure is consequential.
- Walk actors[]: each human or system actor with a trigger that produces user-visible output needs one e2e_flow. Scheduler and event actors only need e2e coverage if the flow touches the UI or external systems; otherwise integration coverage is sufficient.
- It's fine for test_surface.integration_flows to reference requirement scenarios via scenario_refs — reviewers use this to verify the test authors wrote tests that actually implement the declared surface.
%s`, p.Goal, p.PlanContext, scopeInclude, scopeExclude, scopeProtected,
		capSection.String(), reqSection.String(), harnessSection, prevErr)
}

func renderArchitectHarnessCards(cards []prompt.HarnessProfileCard) string {
	if len(cards) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available test environments\n\n")
	sb.WriteString("Select by profile_id only. These test environment profiles are system-owned; do not invent or copy their images, ports, env vars, startup order, readiness probes, or required test assertions into the architecture document.\n\n")
	for _, card := range cards {
		fmt.Fprintf(&sb, "### %s\n\n", card.ID)
		fmt.Fprintf(&sb, "- **Tier:** `%s`\n", card.Tier)
		if card.Cost != "" {
			fmt.Fprintf(&sb, "- **Cost:** %s\n", card.Cost)
		}
		writeJoinedLine(&sb, "Proves", card.Proves)
		writeCoversLine(&sb, card.Covers)
		writeJoinedLine(&sb, "Runner support", card.RunnerSupport)
		writeJoinedLine(&sb, "Constraints", card.Constraints)
		writeJoinedLine(&sb, "Required assertions", card.RequiredAssertions)
		sb.WriteString("\n")
	}
	return sb.String()
}

func writeJoinedLine(sb *strings.Builder, label string, vals []string) {
	if len(vals) == 0 {
		return
	}
	fmt.Fprintf(sb, "- **%s:** %s\n", label, strings.Join(vals, "; "))
}

func writeCoversLine(sb *strings.Builder, covers map[string][]string) {
	if len(covers) == 0 {
		return
	}
	keys := make([]string, 0, len(covers))
	for key := range covers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, strings.Join(covers[key], ", ")))
	}
	fmt.Fprintf(sb, "- **Covers:** %s\n", strings.Join(parts, "; "))
}

const harnessProfilesIntro = "Use these details when your node touches the selected integration. `services`-orchestrated profiles (e.g. live SITL) are operator-tier: the operator's CI brings the stack up as qa.yml services from this catalog metadata — your test reads the endpoint from the env the CI injects rather than starting its own container, and semspec does NOT gate your work on that tier. For `testcontainers` or `pure-fixture` profiles, the test fixture owns the integration peer and runs in the sandbox.\n\n"

// renderTaskScenarios renders the BDD scenarios this task is responsible
// for satisfying. The role-specific intro changes the framing — the
// developer reads the scenarios as a test-writing contract; the per-task
// code-reviewer and validator read them as the conformance bar each
// scenario_id must pass before the task can be approved.
//
// Output shape per scenario:
//
//	### scenario.<id>
//	- Given: <given>
//	- When: <when>
//	- Then:
//	  - <then[0]>
//	  - <then[1]>
//	  ...
//
// Returns "" when scenarios is empty so the fragment-condition gate
// handles the elide-cleanly path for legacy plans.
func renderTaskScenarios(role prompt.Role, scenarios []prompt.ScenarioSpec) string {
	if len(scenarios) == 0 {
		return ""
	}
	var sb strings.Builder
	switch role {
	case prompt.RoleDeveloper:
		sb.WriteString("ACCEPTANCE SCENARIOS — your tests MUST exercise the given/when/then of EACH scenario below.\n\n")
		sb.WriteString("For every scenario you do not have a test covering, the per-task reviewer will reject your work as fixable. Do not skip a scenario because it looks similar to another — each one's specific Given/When/Then is the contract.\n\n")
	case prompt.RoleReviewer:
		sb.WriteString("ACCEPTANCE SCENARIOS — for EACH scenario below, the developer's test suite MUST contain a test exercising its Given/When/Then.\n\n")
		sb.WriteString("Verification protocol — execute this for EVERY scenario:\n")
		sb.WriteString("1. Read the scenario's Given/When/Then.\n")
		sb.WriteString("2. Use bash cat / git diff to find a test that exercises that specific behavior.\n")
		sb.WriteString("3. If no test exists, or the test asserts something other than what Then specifies, that scenario is a violation.\n")
		sb.WriteString("4. Set verdict=rejected with rejection_type=fixable. Quote the scenario_id and the missing/incorrect assertion in feedback.\n\n")
		sb.WriteString("You CANNOT approve if any scenario lacks a test exercising its Then assertions. \"The code looks right and the dev's tests pass\" is NOT sufficient — the dev's tests may pass while missing the scenario contract entirely.\n\n")
	case prompt.RoleValidator:
		sb.WriteString("ACCEPTANCE SCENARIOS — the structural-validator should confirm that test files in the worktree reference each scenario below by ID or by the Given/When/Then assertions.\n\n")
	default:
		sb.WriteString("ACCEPTANCE SCENARIOS — the BDD contract this task must satisfy.\n\n")
	}
	for _, s := range scenarios {
		if s.ID != "" {
			fmt.Fprintf(&sb, "### %s\n", s.ID)
		} else {
			sb.WriteString("### (unnamed scenario)\n")
		}
		if s.Given != "" {
			fmt.Fprintf(&sb, "- **Given:** %s\n", s.Given)
		}
		if s.When != "" {
			fmt.Fprintf(&sb, "- **When:** %s\n", s.When)
		}
		if len(s.Then) > 0 {
			sb.WriteString("- **Then:**\n")
			for _, t := range s.Then {
				fmt.Fprintf(&sb, "  - %s\n", t)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func renderResolvedHarnessProfiles(title string, profiles []prompt.ResolvedHarnessProfileContext) string {
	if len(profiles) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteString("\n\n")
	sb.WriteString(harnessProfilesIntro)
	for _, p := range profiles {
		renderHarnessProfile(&sb, p)
	}
	return sb.String()
}

func renderHarnessProfile(sb *strings.Builder, p prompt.ResolvedHarnessProfileContext) {
	fmt.Fprintf(sb, "### %s (%s)\n\n", p.ProfileID, p.Tier)
	if p.Orchestration != "" {
		fmt.Fprintf(sb, "- **Orchestration:** %s\n", p.Orchestration)
	}
	writeJoinedLine(sb, "Used by", p.UsedBy)
	if p.Purpose != "" {
		fmt.Fprintf(sb, "- **Purpose:** %s\n", p.Purpose)
	}
	writeJoinedLine(sb, "Covers", p.Covers)
	writeJoinedLine(sb, "Proves", p.Proves)
	writeJoinedLine(sb, "Runner support", p.RunnerSupport)
	if p.Cost != "" {
		fmt.Fprintf(sb, "- **Cost:** %s\n", p.Cost)
	}
	writeJoinedLine(sb, "Constraints", p.Constraints)
	writeJoinedLine(sb, "Required assertions", p.RequiredAssertions)
	writeJoinedLine(sb, "Evidence anchors", p.EvidenceAnchors)
	renderHarnessImages(sb, p.Images)
	renderHarnessPorts(sb, p.Ports)
	renderHarnessEnv(sb, p.Env)
	writeJoinedLine(sb, "Readiness", p.Readiness)
	writeJoinedLine(sb, "Test guidance", p.TestGuidance)
	sb.WriteString("\n")
}

func renderHarnessImages(sb *strings.Builder, images []prompt.HarnessImageContext) {
	if len(images) == 0 {
		return
	}
	parts := make([]string, 0, len(images))
	for _, img := range images {
		if img.Purpose == "" {
			parts = append(parts, img.Name)
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", img.Name, img.Purpose))
		}
	}
	fmt.Fprintf(sb, "- **Images:** %s\n", strings.Join(parts, "; "))
}

func renderHarnessPorts(sb *strings.Builder, ports []prompt.HarnessPortContext) {
	if len(ports) == 0 {
		return
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		value := fmt.Sprintf("%s/%d/%s", port.Name, port.ContainerPort, port.Protocol)
		if port.Purpose != "" {
			value = fmt.Sprintf("%s (%s)", value, port.Purpose)
		}
		parts = append(parts, value)
	}
	fmt.Fprintf(sb, "- **Ports:** %s\n", strings.Join(parts, "; "))
}

func renderHarnessEnv(sb *strings.Builder, env map[string]string) {
	if len(env) == 0 {
		return
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, env[key]))
	}
	fmt.Fprintf(sb, "- **Env:** %s\n", strings.Join(parts, "; "))
}

// renderPlanReviewerPrompt produces the plan-reviewer agent's user message.
// Mirrors the legacy workflow/prompts.PlanReviewerUserPrompt body — including
// the round-2 file-ownership criterion 3a we shipped earlier in dial #1.
func renderPlanReviewerPrompt(p *prompt.PlanReviewerPromptContext) string {
	var sb strings.Builder

	// Failure context from the prior dispatch — appears first so the model
	// sees what to fix before it sees the plan again. Empty on the first
	// attempt; non-empty on parse-error / structural-validation retries.
	if p.PreviousError != "" {
		sb.WriteString("## Previous attempt failed\n\n")
		sb.WriteString("Your previous response could not be processed:\n\n```\n")
		sb.WriteString(p.PreviousError)
		sb.WriteString("\n```\n\nProduce a valid response this time. Address the failure mode above before reviewing the plan content.\n\n")
	}

	// Prior-round context — fires on revision rounds (ReviewIteration > 0)
	// so the reviewer sees its own previous verdict on this plan. Without
	// this the reviewer is stateless across revisions and can re-fire the
	// same complaint even when the planner addressed it (take 22 wedge).
	writePlanReviewerPriorRound(&sb, p)

	// Project file tree — ground truth for the scope-validity check. Must
	// appear BEFORE plan content so the reviewer reads the source-of-truth
	// before judging. Empty for greenfield (silently omitted); the renderer
	// then leaves the path-check criterion to fire on the planner's
	// scope.create declarations alone.
	writePlanReviewerProjectFileTree(&sb, p.ProjectFileTree)

	if p.HasStandards {
		sb.WriteString("Review the following plan against the project standards and completeness criteria.\n\n")
	} else {
		sb.WriteString("No project standards are configured. Review the following plan for structural completeness and quality.\n\n")
	}

	sb.WriteString("## Plan to Review\n\n")
	fmt.Fprintf(&sb, "**Slug:** `%s`\n\n", p.Slug)
	sb.WriteString("```json\n")
	sb.WriteString(p.PlanContent)
	sb.WriteString("\n```\n\n")

	switch p.Round {
	case 1:
		sb.WriteString(planReviewerCompletenessR1)
	case 2:
		sb.WriteString(planReviewerCompletenessR2)
	}

	sb.WriteString("Analyze the plan and produce your verdict with findings.\n")
	if p.Round > 0 {
		sb.WriteString("Also evaluate the completeness criteria above. Completeness failures are error-severity findings with category \"completeness\".\n")
	}

	return sb.String()
}

// renderQAReviewerPrompt produces the QA reviewer agent's user message.
// Replaces processor/qa-reviewer/buildUserPrompt; pulls QA-run pass/fail from
// the workflow.Plan since the agent reviews the whole plan + execution.
func renderQAReviewerPrompt(p *prompt.QAReviewerPromptContext) string {
	if p.Plan == nil {
		return ""
	}
	plan := p.Plan
	var sb strings.Builder

	// Failure context from the prior dispatch, if any. Renders before the
	// release-readiness ask so the model addresses the failure mode first.
	if p.PreviousError != "" {
		sb.WriteString("## Previous attempt failed\n\nYour previous response could not be processed:\n\n```\n")
		sb.WriteString(p.PreviousError)
		sb.WriteString("\n```\n\nProduce a valid response this time. Address the failure mode above.\n\n")
	}

	fmt.Fprintf(&sb, "Render a release-readiness verdict for plan: %s\n\n", plan.Slug)
	fmt.Fprintf(&sb, "QA level: %s\n", plan.EffectiveQALevel())

	if plan.QARun != nil {
		if plan.QARun.Passed {
			sb.WriteString("Test execution: PASSED\n")
		} else {
			fmt.Fprintf(&sb, "Test execution: FAILED (%d failures)\n", len(plan.QARun.Failures))
		}
		if plan.QARun.RunnerError != "" {
			fmt.Fprintf(&sb, "Runner error: %s\n", plan.QARun.RunnerError)
		}
	} else if plan.EffectiveQALevel() != "synthesis" {
		sb.WriteString("Warning: QA executor result unavailable — assess based on plan artifacts only.\n")
	}

	// ADR-044 release-readiness contract shift: surface the M:N capability
	// evidence rollup. Empty CoveringStoryIDs = declared-but-uncovered
	// capability (release-blocking). Zero ShippedCount = covered but no
	// Story shipped (release-blocking unless QA-level synthesis).
	if ctx := p.QAReviewContext; ctx != nil && len(ctx.Capabilities) > 0 {
		sb.WriteString("\n## Capability evidence rollup (ADR-044)\n\n")
		sb.WriteString("Release-readiness under ADR-044 requires every Capability to have evidence from ≥1 shipped Story (Story.Status == complete). Gaps below are blocking unless the QA level is synthesis-only.\n\n")
		for _, c := range ctx.Capabilities {
			fmt.Fprintf(&sb, "- **%s** — %d covering Stor(ies), %d shipped",
				c.Name, len(c.CoveringStoryIDs), c.ShippedCount)
			switch {
			case len(c.CoveringStoryIDs) == 0:
				sb.WriteString(" ❌ no Story claims coverage")
			case c.ShippedCount == 0:
				sb.WriteString(" ⚠ claimed but unshipped")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\nUse the system context for detailed plan and test information. Call submit_work with your verdict.")
	return sb.String()
}

// formatScopeListLocal mirrors workflow/prompts.formatScopeList; inlined here
// during migration so software_render.go can stand alone.
func formatScopeListLocal(items []string, defaultValue string) string {
	if len(items) == 0 {
		return defaultValue
	}
	return strings.Join(items, ", ")
}

// renderLessonDecomposerPrompt produces the lesson-decomposer agent's user
// message. Lays out the rejection signal as a structured incident report:
// what the reviewer said, what scenario the work was supposed to satisfy,
// what the developer's trajectory looked like, and which lessons already
// exist for the role. The decomposer's job is to turn that into one
// evidence-cited Lesson — see software.go for the persona and output-format
// fragments. Renderer never invents data; missing inputs simply skip their
// section so the prompt scales down for early lessons that have less
// trajectory captured.
func renderLessonDecomposerPrompt(p *prompt.LessonDecomposerPromptContext) string {
	var sb strings.Builder

	source := p.Source
	if source == "" {
		source = "execution-manager"
	}
	target := p.TargetRole
	if target == "" {
		target = "developer"
	}

	if p.Positive {
		fmt.Fprintf(&sb, "## First-Try Success\n\nA %s pipeline run reached %q on the first attempt. Decompose it into a single auditable BEST PRACTICE lesson for the %q role — capture what worked so future agents can replicate the pattern.\n\n",
			source, p.Verdict, target)
	} else {
		fmt.Fprintf(&sb, "## Incident\n\nA %s rejection occurred during the %s pipeline. Decompose it into a single auditable lesson for the %q role.\n\n",
			p.Verdict, source, target)
	}

	renderDecomposerFeedback(&sb, p.Feedback)
	renderDecomposerScenario(&sb, p.Scenario)
	renderDecomposerDeveloperTrajectory(&sb, p.DeveloperLoopID, p.DeveloperSteps)
	renderDecomposerReviewerTrajectory(&sb, p.ReviewerLoopID, p.ReviewerSteps)
	renderDecomposerWorktree(&sb, p.WorktreeDiffSummary, p.FilesModified)
	renderDecomposerCatalog(&sb, p.CategoryCatalog)
	renderDecomposerExistingLessons(&sb, target, p.ExistingLessons)
	renderDecomposerCommitSHA(&sb, p.CommitSHA)
	renderDecomposerTaskInstructionsBranched(&sb, p.Positive)

	return sb.String()
}

func renderDecomposerFeedback(sb *strings.Builder, feedback string) {
	if feedback == "" {
		return
	}
	sb.WriteString("## Reviewer Feedback (verbatim)\n\n")
	sb.WriteString(feedback)
	sb.WriteString("\n\nDo not parrot this back. Identify the underlying root cause and frame the lesson around the pattern, not the specific reviewer wording.\n\n")
}

func renderDecomposerScenario(sb *strings.Builder, sc *prompt.DecomposerScenarioContext) {
	if sc == nil || (sc.Given == "" && sc.When == "" && len(sc.Then) == 0) {
		return
	}
	sb.WriteString("## Scenario the work was supposed to satisfy\n\n")
	if sc.ID != "" {
		fmt.Fprintf(sb, "**ID:** %s\n", sc.ID)
	}
	if sc.Given != "" {
		fmt.Fprintf(sb, "**Given:** %s\n", sc.Given)
	}
	if sc.When != "" {
		fmt.Fprintf(sb, "**When:** %s\n", sc.When)
	}
	if len(sc.Then) > 0 {
		sb.WriteString("**Then:**\n")
		for _, t := range sc.Then {
			fmt.Fprintf(sb, "  - %s\n", t)
		}
	}
	sb.WriteString("\n")
}

func renderDecomposerDeveloperTrajectory(sb *strings.Builder, loopID string, steps []prompt.TrajectoryStepSummary) {
	if len(steps) > 0 {
		fmt.Fprintf(sb, "## Developer Trajectory (loop %s)\n\nEach line is one step of the agentic loop that produced the rejected code. Cite step indices in `evidence_steps` to ground the lesson.\n\n",
			loopID)
		for _, s := range steps {
			fmt.Fprintf(sb, "- [%d] %s\n", s.Index, s.Summary)
		}
		sb.WriteString("\n")
		return
	}
	if loopID != "" {
		fmt.Fprintf(sb, "## Developer Trajectory\n\nLoop %s exists but its trajectory could not be retrieved. Build the lesson from the reviewer feedback alone; leave `evidence_steps` empty if you cannot cite specific steps.\n\n",
			loopID)
	}
}

func renderDecomposerReviewerTrajectory(sb *strings.Builder, loopID string, steps []prompt.TrajectoryStepSummary) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintf(sb, "## Reviewer Trajectory (loop %s)\n\nReviewer's reasoning is included so you can understand *how* the rejection was reached. Cite reviewer steps only when the reviewer's behaviour itself is the lesson (rare).\n\n",
		loopID)
	for _, s := range steps {
		fmt.Fprintf(sb, "- [%d] %s\n", s.Index, s.Summary)
	}
	sb.WriteString("\n")
}

func renderDecomposerWorktree(sb *strings.Builder, diffSummary string, files []string) {
	if diffSummary != "" {
		sb.WriteString("## Worktree State at Rejection\n\n")
		sb.WriteString(diffSummary)
		sb.WriteString("\n\n")
	}
	if len(files) > 0 {
		sb.WriteString("## Files Modified by Developer\n\nUse these paths in `evidence_files` when the lesson points to a specific code region.\n\n")
		for _, f := range files {
			fmt.Fprintf(sb, "- %s\n", f)
		}
		sb.WriteString("\n")
	}
}

func renderDecomposerCatalog(sb *strings.Builder, catalog []string) {
	if len(catalog) == 0 {
		return
	}
	sb.WriteString("## Available Error Categories\n\nPick `category_ids` from this list. Inventing new IDs makes the lesson harder to retire and rank — match the closest existing category.\n\n")
	for _, cat := range catalog {
		fmt.Fprintf(sb, "- %s\n", cat)
	}
	sb.WriteString("\n")
}

func renderDecomposerExistingLessons(sb *strings.Builder, target string, lessons []string) {
	if len(lessons) == 0 {
		return
	}
	fmt.Fprintf(sb, "## Existing Lessons for the %s role\n\nThese lessons are already in the graph. If your new lesson would say substantially the same thing, prefer matching the existing categorisation; if the rejection is a fresh pattern, file a distinct lesson.\n\n",
		target)
	for _, l := range lessons {
		fmt.Fprintf(sb, "- %s\n", l)
	}
	sb.WriteString("\n")
}

func renderDecomposerCommitSHA(sb *strings.Builder, sha string) {
	if sha == "" {
		return
	}
	fmt.Fprintf(sb, "## Commit SHA\n\nUse this in `evidence_files[].commit_sha` so the retirement sweep can detect when the cited code has been rewritten:\n\n%s\n\n",
		sha)
}

// renderDecomposerTaskInstructionsBranched is the Phase 6-aware version
// of the task instructions block. The negative branch retains the
// original "root cause" framing; the positive branch swaps to "what
// worked" framing while keeping the same evidence and shape requirements.
func renderDecomposerTaskInstructionsBranched(sb *strings.Builder, positive bool) {
	sb.WriteString("## Your Task\n\n")
	if positive {
		sb.WriteString("Produce ONE BEST PRACTICE Lesson via submit_work. The lesson must:\n\n")
		sb.WriteString("- Identify the *replicable pattern* that made this attempt succeed on the first try, not just \"the developer did the right thing\".\n")
		sb.WriteString("- Be auditable: every claim in `detail` must trace back to a step in the developer trajectory or a region in evidence_files.\n")
		sb.WriteString("- Be useful next time: `injection_form` will be rendered into a future agent's prompt, so it must read as concrete prescriptive advice (\"Read the existing test framework before writing the first test\"), not retrospective narration (\"The developer read the test framework\").\n")
		sb.WriteString("- Cite at least one of `evidence_steps` (preferred) or `evidence_files`. A lesson with no evidence is rejected by the writer.\n")
		sb.WriteString("- Set `root_cause_role` to the role whose upstream decision enabled the success — usually the same as the target role, but sometimes the planner / scenario-generator when the success was downstream of a clear plan.\n")
		return
	}
	sb.WriteString("Produce ONE Lesson via submit_work. The lesson must:\n\n")
	sb.WriteString("- Identify the *root cause* pattern, not just the surface symptom the reviewer named.\n")
	sb.WriteString("- Be auditable: every claim in `detail` must trace back to a step in the developer trajectory or a region in evidence_files.\n")
	sb.WriteString("- Be useful next time: `injection_form` will be rendered into a future agent's prompt, so it must read as concrete advice (\"Run the test before submitting\"), not retrospective narration (\"The developer didn't run the test\").\n")
	sb.WriteString("- Cite at least one of `evidence_steps` (preferred) or `evidence_files`. A lesson with no evidence will be rejected by the writer in Phase 3 and is hard to evaluate even now.\n")
	sb.WriteString("- Set `root_cause_role` to the role whose upstream defect created the failure — usually the same as the target role, but sometimes the planner / scenario-generator if the work was set up to fail.\n")
}

const planReviewerCompletenessR1 = `## Completeness Criteria (Round 1 — Plan Document)

**Phase boundaries** — You are reviewing the plan artifact: goal + context + scope. Implementation form — function signatures, response schemas, struct fields, library choices, file layout — is produced by the requirements and architecture phases that run AFTER this review.

In addition to SOP compliance, verify the following structural completeness checks.
Flag failures as error-severity findings with category "completeness".

1. **Goal clarity** — The goal must be specific and actionable. A vague goal like "improve the system" is insufficient. The goal should state what is being built or fixed and what the expected outcome is.
2. **Context sufficiency** — The context must provide enough background for requirements to be derived. It should name the current state, why this change matters, and any constraints. Sufficient means a downstream requirement-generator could derive at least one testable requirement from it; a context naming "build a /health endpoint returning JSON, in a project with no existing endpoints" is sufficient even without specifying response shape.
3. **Scope validity** — All scope.include paths must either exist in the project or be files the plan intends to create. Hallucinated paths (typos, wrong directories) are error-severity violations.

`

const planReviewerCompletenessR2 = "## Completeness Criteria (Round 2 — Requirements + Scenarios + Architecture)\n\n" +
	"In addition to SOP compliance, verify the following structural completeness checks.\n" +
	"Flag failures as error-severity findings with category \"completeness\".\n" +
	"Include the \"phase\" field on each finding (\"requirements\", \"architecture\", or \"scenarios\") and \"target_id\" when a specific entity is at fault.\n\n" +
	"1. **Goal coverage** — Requirements must collectively address the stated goal. If the goal says \"add a /goodbye endpoint\" but no requirement covers that endpoint, flag it. (phase: \"requirements\")\n" +
	"2. **Requirement→Scenario coverage** — Every requirement must have at least one scenario. Requirements without scenarios cannot be verified. (phase: \"requirements\", target_id: the requirement ID)\n" +
	"3. **Dependency validity** — All depends_on references must point to existing requirement IDs. The dependency graph must be a valid DAG (no cycles, no orphan references). (phase: \"requirements\")\n" +
	"3a. **Semantic overlap between requirements** — Two requirements describing the same feature surface (e.g. both about /health, both about the same component) signal a sharding error: they should be consolidated or one should depend on the other. Flag as completeness with category=\"completeness\", phase=\"requirements\", target_id pointing to the later requirement. ADR-043 Move 4 moved file-level partition checking to story preparation (Sarah computes files_owned from selected components), so file-path overlaps no longer surface here — but semantic duplication still matters because Sarah will produce conflicting Stories from semantically-duplicate Requirements. (phase: \"requirements\")\n" +
	"4. **No orphaned scenarios** — Every scenario must reference an existing requirement ID. (phase: \"scenarios\", target_id: the orphaned scenario ID)\n" +
	"5. **Scope alignment** — Scope files should be relevant to the requirements. Scope entries unrelated to any requirement may indicate stale or incorrect scope. (phase: \"plan\")\n" +
	"6. **Architecture coherence** — If an architecture document is present, technology choices must be internally consistent, component boundaries must not overlap, actors must have distinct trigger sets, and integration points must not contradict component boundaries. (phase: \"architecture\")\n" +
	"7. **Architecture-requirement alignment** — If architecture is present, every requirement must be implementable with the chosen technology stack. Requirements involving external systems should map to declared integration points. Requirements triggered by user actions should map to declared actors. Flag requirements that conflict with architectural decisions. (phase: \"requirements\", target_id: the conflicting requirement ID)\n" +
	"7a. **Upstream resolution discipline** — When the architecture references an external library, API, or framework (anywhere in technology_choices, integrations, or component_boundaries), the architect MUST have populated `architecture.upstream_resolutions[]` with the resolved coordinate + the API surface the developer will integrate against. The dev no longer has a research sub-agent (shelved 2026-05-15) — the architect's resolutions are the developer's pre-loaded reading list, and missing resolutions reproduce the take-23 wedge (35 external file reads, 0 worktree writes, iter budget exhausted on re-discovery). Apply the following STRUCTURAL checks; emit Path B-shape findings (action + target_field + target_value) so the architect's revision is unambiguous: (a) For every external library named in technology_choices, integrations, or any component_boundaries[].dependencies entry that is NOT also a component_boundaries[].name (i.e., it points outside the system), there MUST be a matching `architecture.upstream_resolutions[]` entry whose `name` matches. Missing resolution → action=\"add\", target_field=\"architecture.upstream_resolutions\", target_value=\"<lib-name> with coordinate, source_ref, and apis from the canonical docs you fetched in inspection step 4\". (b) Every `upstream_resolutions[]` entry MUST have non-empty `coordinate` (machine-resolvable: groupId:artifactId:version, name@version, github URL@tag — vague hints like \"OSH 2.x\" do NOT satisfy), non-empty `source_ref` (the URL or file path proving the coordinate), and at least one `apis[]` entry. Missing field → action=\"add\", target_field=\"architecture.upstream_resolutions[<name>].<field>\", target_value=\"<concrete value to populate from your inspection notes>\". (c) Every `apis[]` entry MUST have non-empty `citation` (file path or URL where the signature was verified). An uncited surface is a guess; mark it action=\"add\", target_field=\"architecture.upstream_resolutions[<name>].apis[<symbol>].citation\", target_value=\"<URL or path>\". (c2) Every `apis[]` entry whose `kind` is a code symbol (class, interface, type, function, annotation, constant) MUST ALSO have non-empty `import` — the fully-qualified, paste-ready reference the dev imports (e.g. \"io.mavsdk.System\", NOT bare \"System\"). A bare symbol forces the dev to guess the package and rediscover it (the 2026-06-07 javap-thrash wedge). Missing → action=\"add\", target_field=\"architecture.upstream_resolutions[<name>].apis[<symbol>].import\", target_value=\"<fully-qualified import verified against the artifact>\". (d) Bidirectional invariant: for every `component_boundaries[].upstream_refs` entry, the named resolution MUST exist in `upstream_resolutions[]`. For every `upstream_resolutions[].used_by` entry, the named component MUST exist in `component_boundaries[]` AND that component's `upstream_refs[]` MUST list the resolution back. Mismatch → action=\"add\", target_field=\"architecture.<the side missing the back-link>\", target_value=\"<the name to add>\". (e) Goodhart guard: do NOT reject for \"the apis section seems short\" or \"more notes would help\" — those are subjective and unenforceable. Only reject for STRUCTURAL violations (missing field, missing citation, missing import on a code-symbol kind, missing back-link, vague coordinate). If you cannot name the SPECIFIC missing field per Path B's directive shape, the finding doesn't pass the bar. (phase: \"architecture\", target_id: the upstream_resolutions entry name OR the component name OR \"<missing>\" when the entry doesn't exist yet)\n" +
	"7b. **Integration-target harness profile discipline** — Every `upstream_resolutions[]` entry whose `role` field is \"integration_target\" MUST be covered by at least one valid `architecture.harness_profiles[]` selection. The catalog is the integration tier's structural anchor: the architect selects a profile ID and the developer resolves system-owned images, ports, readiness, required test assertions, and runner compatibility from the catalog. Apply Path B-shape findings: (a) Unknown profile ID → action=\"replace\", target_field=\"architecture.harness_profiles[<profile_id>].profile_id\", target_value=\"<valid catalog profile_id>\". Do not accept invented IDs or inline runner topology. (b) Missing coverage for an integration_target → action=\"add\", target_field=\"architecture.harness_profiles\", target_value=\"{profile_id: <catalog profile_id>, used_by: [<component>], purpose: <why this proves the integration>, covers: [<integration_target name or facet>]}\". (c) `harness_profiles[]` entries MUST have non-empty `profile_id`, `used_by`, and `purpose`; `covers` is optional but SHOULD name the integration target, protocol facet, plugin group, or scenario when the mapping is not obvious. (d) The architect MUST NOT author `image`, `port`, `access_method`, `env`, startup order, or readiness fields in architecture JSON; those details belong only to the catalog and are rendered downstream for developers. (e) Inverse goodhart guard: do NOT reject `runtime_dep` or `build_dep` resolutions for missing harness profiles. Only flag the integration_target shape. (f) Architect bias correction: when the architect classifies as `runtime_dep` something that's clearly a separate process (database driver, message-queue client library, gRPC stub for a remote service, MAVLink SITL/autopilot endpoint), the role is wrong. action=\"replace\", target_field=\"architecture.upstream_resolutions[<name>].role\", target_value=\"integration_target\" plus a paired harness profile selection. (g) Scenario binding source (ADR-041 Move 6): the `architecture.harness_profiles[].profile_id` values established by this rule are the IDs that downstream `Scenario.HarnessProfileIDs` reference. The scenario-generator binds @integration scenarios by copying profile_ids from this list verbatim; the structural-validator's harness-profile-discipline check verifies the dev's test files contain those exact ID strings; the operator's CI routes via the same IDs. Treat each profile_id as a stable wire-level identifier — renames cascade across plan + scenarios + tests + qa.yml. (phase: \"architecture\", target_id: the upstream_resolutions entry name OR the harness_profiles entry profile_id)\n" +
	"8. **Scenario-actor coverage** — Scenarios should reference the actors declared in the architecture. If the architecture declares an actor (e.g., a \"scheduler\" or \"event\" type) but no scenario has a Given/When involving that actor's triggers, flag as a warning — the plan may have blind spots for that actor's behavior. (phase: \"scenarios\")\n" +
	"9. **Scenario-integration coverage** — Scenarios should exercise the integration points declared in the architecture. If the architecture declares an integration (e.g., an outbound HTTP API or a database) but no scenario verifies that integration's behavior or error handling, flag as a warning — untested integration boundaries are a common source of production failures. (phase: \"scenarios\")\n\n"

// renderRecoveryAgentPrompt produces the user message for the recovery-agent
// dispatch. Replaces the legacy hand-rolled buildUserPrompt in
// processor/recovery-agent/prompt.go (deleted 2026-05-11). The recovery
// agent's diagnostic context — escalation reason, last failure feedback,
// trajectory steps — is rendered here so it sits alongside the persona
// fragments (system-base + closed-action-set + rules) that come from the
// fragment registry.
func renderRecoveryAgentPrompt(r *prompt.RecoveryPromptContext) string {
	var sb strings.Builder

	sb.WriteString("# RECOVERY REQUEST\n\n")
	if r.Layer != "" {
		fmt.Fprintf(&sb, "**Layer**: %s\n", r.Layer)
	}
	fmt.Fprintf(&sb, "**Plan slug**: %s\n", r.Slug)
	if r.RequirementID != "" {
		fmt.Fprintf(&sb, "**Requirement ID**: %s\n", r.RequirementID)
	}
	if r.TaskID != "" {
		fmt.Fprintf(&sb, "**Task ID**: %s\n", r.TaskID)
	}
	if r.LoopID != "" {
		fmt.Fprintf(&sb, "**Wedged agent loop ID**: %s\n", r.LoopID)
	}
	if r.PriorRecoveryID != "" {
		fmt.Fprintf(&sb, "**Prior recovery attempt**: %s (this is a coordinator-layer retry — pick a different action shape than the prior layer)\n", r.PriorRecoveryID)
	}

	sb.WriteString("\n## Escalation Reason\n\n")
	sb.WriteString(r.EscalationReason)
	sb.WriteString("\n")

	if r.LastFailureFeedback != "" {
		sb.WriteString("\n## Last Failure Feedback (what the wedged agent was responding to before escalation)\n\n")
		sb.WriteString(r.LastFailureFeedback)
		sb.WriteString("\n")
	}

	if r.ArchitectureContext != "" {
		sb.WriteString("\n")
		sb.WriteString(r.ArchitectureContext)
		sb.WriteString("\nUse the architecture above to diagnose the ROOT of the wedge. A missing or mis-resolved upstream dependency, a wrong component boundary, or an integration target the dev cannot satisfy is an ARCHITECTURE problem — name it precisely in your diagnosis and choose architecture_revise so the architect re-runs and revises the architecture against your diagnosis (do NOT mark_unrecoverable, which abandons the work, and do NOT escalate_human, which only surfaces the problem without fixing it). Reserve story_reprepare for cases where the architecture is sound but Sarah's Story-shaping is wrong.\n")
	}

	if len(r.TrajectorySteps) == 0 {
		sb.WriteString("\n## Trajectory\n\n(no trajectory available — work from the escalation reason and last failure feedback)\n")
	} else {
		fmt.Fprintf(&sb, "\n## Trajectory (%d steps, may be capped)\n\n", len(r.TrajectorySteps))
		for i, summary := range r.TrajectorySteps {
			fmt.Fprintf(&sb, "  [%d] %s\n", i, summary)
		}
	}

	sb.WriteString("\n---\nDiagnose the wedge from the evidence above and call submit_work with your chosen RecoveryAction. Do not call any other tool except scratchpad (which is for your own reasoning before you commit).")
	return sb.String()
}

// renderResearcherPrompt builds the researcher's user prompt from the
// asking developer's research() tool call. The renderer is intentionally
// terse — the system prompt carries the role description; the user
// prompt carries the specific request the researcher needs to answer.
func renderResearcherPrompt(r *prompt.ResearcherPromptContext) string {
	var sb strings.Builder

	sb.WriteString("# RESEARCH REQUEST\n\n")
	fmt.Fprintf(&sb, "**Research ID**: %s (pass verbatim to answer_research)\n", r.ResearchID)
	if r.AskingPlanSlug != "" {
		fmt.Fprintf(&sb, "**Asking plan**: %s\n", r.AskingPlanSlug)
	}
	if r.AskingTaskID != "" {
		fmt.Fprintf(&sb, "**Asking task**: %s\n", r.AskingTaskID)
	}

	sb.WriteString("\n## Question\n\n")
	sb.WriteString(r.Question)
	sb.WriteString("\n")

	if len(r.Sources) > 0 {
		sb.WriteString("\n## Source hints (developer's starting points)\n\n")
		for _, s := range r.Sources {
			fmt.Fprintf(&sb, "- %s\n", s)
		}
	} else {
		sb.WriteString("\n## Source hints\n\n(none — the developer did not narrow the starting points; use web_search to discover canonical upstream sources before fetching content)\n")
	}

	sb.WriteString("\n---\nRead just enough to answer the question concretely, then call answer_research with the answer + citations. If the question is too broad to answer concretely from what you can read, return what you have plus a brief note describing what's still ambiguous so the developer can ask a follow-up.")
	return sb.String()
}
