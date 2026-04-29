package domain

import (
	"fmt"
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
func renderRequirementGeneratorPrompt(rg *prompt.RequirementGeneratorContext) string {
	var sb strings.Builder

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
			if len(r.FilesOwned) > 0 {
				fmt.Fprintf(&sb, "  files_owned: %s\n", strings.Join(r.FilesOwned, ", "))
			}
			if len(r.DependsOn) > 0 {
				fmt.Fprintf(&sb, "  depends_on: %s\n", strings.Join(r.DependsOn, ", "))
			}
		}
		sb.WriteString("\nWhen proposing replacements, do NOT claim a path already in any kept requirement's files_owned unless your replacement lists that requirement's title in depends_on. Otherwise the plan-level merge will deadlock and the entire generation will be rejected.\n\n")
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
		fmt.Fprintf(&sb, "\n## Previous Review Findings (Address These)\n\nThe previous set of requirements was reviewed and rejected. Address ALL of the following findings:\n\n%s\n", rg.ReviewFindings)
	}

	return sb.String()
}

// renderPlannerPrompt produces the planner agent's user message. Two paths:
// fresh creation (Title only) and revision-after-rejection (PreviousPlanJSON
// + RevisionPrompt). PreviousError is appended to either path as a retry note.
// Replaces processor/planner/buildPlannerUserPrompt and the legacy
// workflow/prompts.PlannerPromptWithTitle helper.
func renderPlannerPrompt(p *prompt.PlannerPromptContext) string {
	if p.IsRevision && p.RevisionPrompt != "" {
		var sb strings.Builder
		if p.PreviousPlanJSON != "" {
			sb.WriteString("## Your Previous Plan Output\n\nThis is the plan you produced that was rejected. Update it to address ALL findings below.\n\n```json\n")
			sb.WriteString(p.PreviousPlanJSON)
			sb.WriteString("\n```\n\n")
		}
		sb.WriteString(p.RevisionPrompt)
		if p.PreviousError != "" {
			sb.WriteString("\n\n## RETRY NOTE\n\nYour previous attempt failed with this error:\n")
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
	out := fmt.Sprintf(`Create a committed plan for implementation:

**Title:** %s

Read the codebase to understand the current state. If any critical information is missing for implementation, ask questions. Then produce the Goal/Context/Scope structure.`, p.Title)
	if p.PreviousError != "" {
		out += "\n\n## RETRY NOTE\n\nYour previous attempt failed with this error:\n" + p.PreviousError + "\n\nPlease try again, addressing the issue above."
	}
	return out
}

// renderScenarioGeneratorPrompt produces the scenario-generator agent's user
// message. Mirrors the legacy workflow/prompts.ScenarioGeneratorPrompt body
// byte-for-byte; ArchitectureContext is pre-rendered upstream.
func renderScenarioGeneratorPrompt(p *prompt.ScenarioGeneratorPromptContext) string {
	archSection := ""
	if p.ArchitectureContext != "" {
		archSection = "\n" + p.ArchitectureContext + "\n"
	}

	base := fmt.Sprintf(`You are generating BDD scenarios for a specific requirement.

## Plan: %s

**Goal:** %s

## Requirement: %s

%s
%s
## Your Task

Generate 1-5 BDD scenarios that define the observable behavior for this requirement. Each scenario must:
- Describe ONE observable behavior
- Be independently executable — a QA engineer could run it without additional context
- Use specific, measurable outcomes
- Cover the happy path first, then key edge cases

**Scenario Design Guidelines:**
- **Given**: Precondition state — what exists before the action. Be specific: "a registered user with a valid session" not "a user exists"
- **When**: The triggering action — what the user or system does. One action per scenario, use active voice
- **Then**: Expected outcomes as an ARRAY of assertions — multiple things to verify. Use specific values where possible: "the response status is 200" not "the request succeeds"

Do NOT include implementation details — describe WHAT happens, not HOW it is implemented.

**Good scenario:**
- Given: "an unauthenticated user with a registered account"
- When: "they submit the login form with a valid email and correct password"
- Then: ["the response status is 200", "a JWT token is returned in the response body", "the token expires in 24 hours"]

**Bad scenario (too vague):**
- Given: "a user exists"
- When: "they log in"
- Then: ["it works"]

## Output Format

Return ONLY valid JSON matching this exact structure:

`+"```json"+`
{
  "scenarios": [
    {
      "given": "an unauthenticated user with a registered account",
      "when": "they submit the login form with a valid email and correct password",
      "then": [
        "the response status is 200",
        "a JWT token is returned in the response body",
        "the token expires in 24 hours"
      ]
    },
    {
      "given": "an unauthenticated user",
      "when": "they submit the login form with an incorrect password",
      "then": [
        "the response status is 401",
        "the response body contains the message 'Invalid credentials'",
        "no token is returned"
      ]
    }
  ]
}
`+"```"+`

**Important:** Return ONLY the JSON object, no additional text or explanation.
`, p.PlanTitle, p.PlanGoal, p.RequirementTitle, p.RequirementDescription, archSection)

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
`, p.ReviewFindings)
	}

	return base
}

// renderArchitectPrompt produces the architecture-generator agent's user
// message. Mirrors the legacy workflow/prompts.ArchitectPrompt body.
func renderArchitectPrompt(p *prompt.ArchitectPromptContext) string {
	scopeInclude := formatScopeListLocal(p.ScopeInclude, "all files")
	scopeExclude := formatScopeListLocal(p.ScopeExclude, "none")
	scopeProtected := formatScopeListLocal(p.ScopeProtected, "none")

	var reqSection strings.Builder
	if len(p.Requirements) > 0 {
		reqSection.WriteString("\n## Requirements\n\n")
		for i, r := range p.Requirements {
			fmt.Fprintf(&reqSection, "%d. **%s**: %s\n", i+1, r.Title, r.Description)
		}
	}

	prevErr := ""
	if p.PreviousError != "" {
		prevErr = fmt.Sprintf(`

## Previous Attempt Failed

Your previous output could not be processed: %s

Please fix the issue and ensure your deliverable matches the required structure.`, p.PreviousError)
	}

	return fmt.Sprintf(`Analyze the following plan and its requirements to produce architecture decisions.

**Goal:** %s

**Context:** %s

**Scope:**
- Include: %s
- Exclude: %s
- Protected (do not touch): %s
%s
## Your Task

1. Use bash and graph tools to explore the codebase — understand the existing technology stack, project structure, and patterns already in use.
2. Identify technology choices, component boundaries, data flows, and key architecture decisions.
3. Call submit_work with a summary and a structured deliverable.

**Guidelines:**
- Reuse existing technology stack where possible — do not propose new frameworks when the project already has one
- Focus on structure and boundaries, not implementation details
- Justify every decision with a rationale
- Flag architectural risks or trade-offs
- Component boundaries should reflect natural module/service divisions in the codebase

## Deliverable Structure

Your deliverable must contain:
- **technology_choices**: array of {category, choice, rationale} — e.g., framework, database, messaging
- **component_boundaries**: array of {name, responsibility, dependencies[]} — logical modules or services
- **data_flow**: string describing how data moves between components
- **decisions**: array of {id, title, decision, rationale} — architecture decision records (use IDs like ARCH-001)
- **actors**: array of {name, type, triggers[], permissions[]?} — who or what initiates actions in the system (type: human | system | scheduler | event). Every trigger the system responds to must map to an actor
- **integrations**: array of {name, direction, protocol, contract?, error_mode?} — external boundaries the system touches (direction: inbound | outbound | bidirectional; protocol: http | nats | grpc | db | filesystem). Include error_mode when failure behavior matters
- **test_surface** (optional but strongly recommended): object with two arrays — the test coverage your architecture implies. The developer uses this to know what to test; QA uses it to judge whether coverage is adequate.
  - **integration_flows**: array of {name, components_involved[], description, scenario_refs[]?} — each external integration[] deserves one integration flow that exercises it with a real fixture. components_involved references component_boundaries[].name entries.
  - **e2e_flows**: array of {actor, steps[], success_criteria[]} — each actor[] of type human or system that drives a user-visible outcome deserves one end-to-end flow. Steps describe the actor's actions; success_criteria describe observable post-conditions.

**Deriving test_surface:**
- Walk integrations[]: each entry that goes inbound or bidirectional needs at minimum one integration_flow that validates the contract and the error_mode. Outbound-only integrations (we call them, they don't call us) also need coverage when failure is consequential.
- Walk actors[]: each human or system actor with a trigger that produces user-visible output needs one e2e_flow. Scheduler and event actors only need e2e coverage if the flow touches the UI or external systems; otherwise integration coverage is sufficient.
- It's fine for test_surface.integration_flows to reference requirement scenarios via scenario_refs — reviewers use this to verify the test authors wrote tests that actually implement the declared surface.
%s`, p.Goal, p.PlanContext, scopeInclude, scopeExclude, scopeProtected,
		reqSection.String(), prevErr)
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

const planReviewerCompletenessR1 = `## Completeness Criteria (Round 1 — Plan Document)

In addition to SOP compliance, verify the following structural completeness checks.
Flag failures as error-severity findings with category "completeness".

1. **Goal clarity** — The goal must be specific and actionable. A vague goal like "improve the system" is insufficient. The goal should state what is being built or fixed and what the expected outcome is.
2. **Context sufficiency** — The context must provide enough background for requirements to be derived. It should explain the current state, why this change matters, and any relevant constraints.
3. **Scope validity** — All scope.include paths must either exist in the project or be files the plan intends to create. Hallucinated paths (typos, wrong directories) are error-severity violations.

`

const planReviewerCompletenessR2 = "## Completeness Criteria (Round 2 — Requirements + Scenarios + Architecture)\n\n" +
	"In addition to SOP compliance, verify the following structural completeness checks.\n" +
	"Flag failures as error-severity findings with category \"completeness\".\n" +
	"Include the \"phase\" field on each finding (\"requirements\", \"architecture\", or \"scenarios\") and \"target_id\" when a specific entity is at fault.\n\n" +
	"1. **Goal coverage** — Requirements must collectively address the stated goal. If the goal says \"add a /goodbye endpoint\" but no requirement covers that endpoint, flag it. (phase: \"requirements\")\n" +
	"2. **Requirement→Scenario coverage** — Every requirement must have at least one scenario. Requirements without scenarios cannot be verified. (phase: \"requirements\", target_id: the requirement ID)\n" +
	"3. **Dependency validity** — All depends_on references must point to existing requirement IDs. The dependency graph must be a valid DAG (no cycles, no orphan references). (phase: \"requirements\")\n" +
	"3a. **File-ownership partition** — Two requirements must not both list the same path in `files_owned` unless one transitively depends on the other via `depends_on`. Independent parallel requirements that touch the same file deadlock on the plan-level merge. Emit ONE finding per conflicting pair (not one finding listing all conflicts). For each pair, set verdict=rejected with category \"completeness\", phase: \"requirements\", target_id: the requirement that should be modified to resolve the conflict (typically the later or narrower-scoped one). The `evidence` field should name the conflicting paths and the other requirement's ID; the `suggestion` field should propose either consolidating the two requirements into one or adding a specific `depends_on` edge. Watch for semantic overlap too — two requirements describing the same feature surface (e.g. both about /health) should be flagged here even if their `files_owned` happen not to literally collide today, because the developer agents will reach for the same files at execution time. (phase: \"requirements\")\n" +
	"4. **No orphaned scenarios** — Every scenario must reference an existing requirement ID. (phase: \"scenarios\", target_id: the orphaned scenario ID)\n" +
	"5. **Scope alignment** — Scope files should be relevant to the requirements. Scope entries unrelated to any requirement may indicate stale or incorrect scope. (phase: \"plan\")\n" +
	"6. **Architecture coherence** — If an architecture document is present, technology choices must be internally consistent, component boundaries must not overlap, actors must have distinct trigger sets, and integration points must not contradict component boundaries. (phase: \"architecture\")\n" +
	"7. **Architecture-requirement alignment** — If architecture is present, every requirement must be implementable with the chosen technology stack. Requirements involving external systems should map to declared integration points. Requirements triggered by user actions should map to declared actors. Flag requirements that conflict with architectural decisions. (phase: \"requirements\", target_id: the conflicting requirement ID)\n" +
	"8. **Scenario-actor coverage** — Scenarios should reference the actors declared in the architecture. If the architecture declares an actor (e.g., a \"scheduler\" or \"event\" type) but no scenario has a Given/When involving that actor's triggers, flag as a warning — the plan may have blind spots for that actor's behavior. (phase: \"scenarios\")\n" +
	"9. **Scenario-integration coverage** — Scenarios should exercise the integration points declared in the architecture. If the architecture declares an integration (e.g., an outbound HTTP API or a database) but no scenario verifies that integration's behavior or error handling, flag as a warning — untested integration boundaries are a common source of production failures. (phase: \"scenarios\")\n\n"
