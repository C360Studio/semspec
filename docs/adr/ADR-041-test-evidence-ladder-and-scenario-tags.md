# ADR-041: Test Evidence Ladder + Scenario Tags (BDD-Compatible Tier Ownership)

**Status:** Proposed (2026-05-31, revision 1)
**Deciders:** Coby, Claude
**Builds on:** ADR-029 (plan completeness + retry), ADR-030 (BMAD persona alignment — keeps QA persona Murat), ADR-037 (wedge recovery), ADR-039 (test environment catalog → qa.yml rendering), ADR-040 (capability vocabulary alignment + analyst sub-phase)
**Does not change:** Plan / PlanState / PLAN_STATES, ENTITY_STATES, the semstreams substrate, the test environment catalog, the SKG, the dev → validator → reviewer → QA pipeline, the autonomous recovery cascade, persona names (BMAD-aligned per ADR-030), JSON wire field names (`architecture.harness_profiles[]` etc — hard rename deferred to a future PR), `qa-reviewer` / `qa-runner` component names.
**Defers to ADR-042 (forthcoming):** Ops persona / harness-manager agent, `HarnessRun` runtime state machine (preflight → allocating → starting → ready → tests → collecting → teardown), project-manager harness-doctor / availability declaration. These are decoupled from ADR-041 — both can ship; ADR-042's runtime owner consumes `Scenario.HarnessProfileIDs` (introduced here) without requiring it to exist at a different shape.

## Context

### The triggering failure

The mavlink-hard run on 2026-05-31 ran for ~60 minutes against the new ADR-040 planning pipeline before operator-killed for forensics. Token spend at kill: ~$40-50 on one of four requirements. The pipeline produced ~50-67 KB of real MAVSDK driver Java code (qualitatively unlike run #3, which produced ~0 KB) and the req-level reviewer gave domain-specific, substantive feedback. But it never converged:

> "Rejected. The implementation compiles and its unit tests pass, but it does not satisfy the lifecycle acceptance contract against a **real MAVSDK/PX4 SITL setup**. The tests use a SleepingServer fixture and locally injected UDP datagrams, so they do not demonstrate that mavsdk_server establishes a MAVLink/MAVSDK connection to PX4 SITL."

The req-reviewer is correct. The scenarios for `mavsdk-lifecycle` were written against PX4 SITL behavior:

- "the MAVSDK Driver is configured with a valid connection string to a PX4 SITL instance"
- "the mavsdk_server process is spawned and running"
- "a MAVSDK heartbeat is received from the Autopilot"

These are correctly written for the `mavlink.px4-sitl.mavsdk-smoke` test environment profile that ADR-039 introduced — i.e., behavior observable in the QA tier when qa-runner brings up the SITL container. They are **structurally impossible to satisfy in the dev sandbox** where there is no PX4 SITL. The dev produces unit tests with stubs; the req-reviewer reads the integration-tier scenarios; both behave correctly within their own contract; the contract between them is broken.

This is a category error: a tier-flat scenario model handed to a tier-flat req-reviewer with no language to say "this assertion is observed somewhere I can't see." The infinite-reject loop combined with autonomous QA-recovery (issue #36) turns the category error into ~$100/req of paid LLM burn.

### What's already in place

ADR-039 introduced the test environment profile catalog with three orchestration types (`services` / `testcontainers` / `pure-fixture`) and per-profile metadata including images, ports, env, readiness, and required test assertions. The architect already selects profiles by `profile_id` based on which integration targets components expose. The structural-validator already checks that the dev's tests carry the catalog's required test assertions. The plumbing for tier-aware verification exists end-to-end.

ADR-040 added the analyst sub-phase that classifies capabilities up front. Each capability is `new | modified` and described in 1-3 sentences before scope/files/requirements are derived.

PR #40 (open) aligned docs/code prose with ADR-039's services rendering. PR #41 (open) renamed user-facing surfaces from semspec-bespoke vocabulary to standard CI/CD vocabulary (harness profile → test environment profile, evidence anchors → required test assertions, etc.).

### What's missing

There is no language in the scenario model for "who can observe this assertion." Scenarios are tier-flat. The scenario-generator doesn't classify by tier. The architecture-generator selects test environment profiles based on capability concepts, not tier responsibility. The req-reviewer reads everything as if observable at dev-completion time.

The fix is not to teach the req-reviewer to be lenient. The fix is to make tier ownership a structural property of each scenario — and to use boring industry vocabulary for that ownership, not roll our own.

### The boring industry answer

BDD/Cucumber has decades-old convention for tier-tagging scenarios. `@unit`, `@integration`, `@smoke`, `@e2e` are tags every BDD adopter recognizes. Cucumber's `--tags @integration and not @smoke` selector language gives free runtime filtering. Tags compose: a scenario can carry `@integration @slow @flaky` — multiple orthogonal facts. OpenSpec spec.md is markdown, and Gherkin tag lines round-trip cleanly through it.

The principle (operator-named during ADR-041 design):

> "Public/operator-facing terms should be boring industry words; semspec-specific names can exist internally."

The same principle drove ADR-040's OpenSpec vocab alignment. ADR-041 applies it to the test surface.

### Why colon-bearing tags fail

Cucumber tags are `@` + non-whitespace, but pytest-bdd rejects `.` and `:`, behave has documented issues with `:`, and karate uses parametric `@tag(value)` which isn't tag syntax at all. The pragmatic answer: keep tag names alphanumeric + hyphens, lift profile binding off the tag into a structured field on the Scenario entity.

## Decision

**Six additive moves. No persona renames, no substrate changes, no breaking wire changes. Each move is independently shippable as a PR; the sequence matters for review but not for technical correctness.**

### Move 1: Add `Scenario.Tags` and `Scenario.HarnessProfileIDs`

Extend the `workflow.Scenario` type:

```go
type Scenario struct {
    // ... existing fields (ID, RequirementID, Title, Given, When, Then) ...
    Tags              []string `json:"tags"`                          // required: exactly one tier tag
    HarnessProfileIDs []string `json:"harness_profile_ids,omitempty"` // required when tier == "@integration" and architecture binds profiles to this capability
}
```

**Tier tags (exactly one required per scenario):**

- `@unit` — observable at function/class boundary with fakes, in-process state, or `pure-fixture` test environments
- `@integration` — observable when a `services` or `testcontainers` test environment is up
- `@smoke` — observable in a release-staging environment; scheduled, not per-PR
- `@e2e` — observable in a full-system deployment with UI + persistence + network

**Harness binding (structured, not tag-embedded):** `Scenario.HarnessProfileIDs` references the catalog by stable ID. Validation enforces every ID resolves to a `harnesscatalog.Profile`. Multi-binding (one scenario exercising two profiles) is trivial: `["a", "b"]`.

**Operator-extensible facet tags** pass through validation but aren't structurally interpreted: `@flaky`, `@slow`, `@security`, `@performance`, project-specific tags from `standards.json`. The structurally-validated set is `{@unit, @integration, @smoke, @e2e}`; everything else is informational metadata.

### Move 2: Add `Capability.surfaces` for clean `@e2e` classification

Extend `workflow.Capability` (added in ADR-040):

```go
type Capability struct {
    // ... existing fields (Name, Lifecycle, Description, DependsOn) ...
    Surfaces []CapabilitySurface `json:"surfaces,omitempty"`
}

type CapabilitySurface string
const (
    SurfaceUI         CapabilitySurface = "ui"          // user-visible interface
    SurfaceAPI        CapabilitySurface = "api"         // programmatic surface
    SurfaceBackground CapabilitySurface = "background"  // scheduled/event-driven
)
```

Mary-analyst (introduced in ADR-040) populates `surfaces` as part of the exploration sub-phase. Adopters who don't want UI tests at all leave `ui` off every capability. `@e2e` scenarios only emit for capabilities where `ui` is in `surfaces` — replacing the fragile "user prompt contains the word 'user flow'" heuristic with an analyst-classified signal source.

### Move 3: Tier-emission classifier in scenario-generator

The scenario-generator's persona (John, in ADR-030) gains a classifier that maps from the architecture's resolved test environment profiles to the tier surface for each requirement, then dispatches one LLM call per tier:

```
for each requirement R with capability C:
    relevant_profiles = profiles bound to C's components via architecture.harness_profiles[].used_by
    services_or_testcontainers = any(p.orchestration in {"services","testcontainers"} for p in relevant_profiles)

    emit @unit scenarios:   always; ≥1 per requirement; observable at function/class boundary
    emit @integration scenarios: when services_or_testcontainers; ≥1 per bound services-class profile
    emit @e2e scenarios:    when "ui" in C.surfaces
    emit @smoke scenarios:  only when operator/architect explicitly directs (rare)

each scenario carries:
    tier tag (exactly one of @unit/@integration/@smoke/@e2e)
    HarnessProfileIDs (required when tier == @integration AND services_or_testcontainers)
    given/when/then prose appropriate to the tier
```

**Anti-pattern enforcement in the persona prompt:** `@unit` scenarios MUST NOT mention real services, network endpoints, SITL containers, databases, or any peer that requires a process beyond the test runtime. `@integration` scenarios MUST assume the harness endpoint is environment-injected (e.g., "Given the SITL endpoint at env `$SITL_ENDPOINT`...") and MUST NOT instruct test code to start the harness — qa-runner does that per ADR-039.

The classifier is deterministic at its boundary (which profiles are relevant per capability is a graph lookup); the LLM is dispatched per-tier so each call produces tier-appropriate prose at the right level of abstraction. Unit scenarios talk about command-line builders and state machines; integration scenarios talk about MAVLink HEARTBEAT and MAVSDK connection state; the persona is told the tier and produces the prose to match.

### Move 4: Plan-reviewer rules (additive to ADR-040's capability rules)

The plan-reviewer gains five structural rules layered on top of ADR-040's capability rules. All fire only after scenario-generation (R2 round), gated on `len(plan.Scenarios) > 0` to avoid the R1-firing bug that issue #2fcbe5a caught for ADR-040:

| Rule ID | Catches |
|---|---|
| `scenario.missing_tier_tag` | Scenario has zero or more-than-one of `@unit / @integration / @smoke / @e2e` |
| `scenario.missing_unit_coverage` | Requirement has zero `@unit` scenarios — every requirement needs unit coverage as a baseline |
| `scenario.missing_integration_for_services` | Requirement bound to a `services` or `testcontainers` test environment has zero `@integration` scenarios tagging that environment |
| `scenario.harness_id_unresolved` | A `HarnessProfileIDs` entry doesn't resolve to a catalog profile |
| `scenario.unit_mentions_services` (warn) | `@unit` scenario text contains real-service tokens (`SITL`, profile names, "real", "live", "container") — heuristic; warn-level so the human can decide |

These are deterministic checks invoked after the LLM-driven verdict the same way ADR-040's capability rules are. The LLM verdict is advisory; structural violations are non-negotiable and override "approved" to `needs_changes`.

### Move 5: Structural-validator rule for `@integration` test scaffolding via catalog lookup

For every `@integration` scenario, the dev's tagged test file (matching the scenario name) must:

1. Reference at least one of the scenario's `HarnessProfileIDs` as a string literal (so qa-runner can route it via the tag selector)
2. Read the harness endpoint from an environment variable matching the catalog profile's `env` declaration (NOT a hardcoded host/port)
3. Contain at least one assertion referencing each `required_assertion` declared by every bound catalog profile

This extends the existing `CheckHarnessProfileDiscipline` (in `processor/structural-validator/testcontainers_discipline.go`) — the assertions check is already there. The additions are (1) the harness-binding string presence and (2) the env-var consumption pattern. The check name (`harness-profile-discipline`) stays as the stable operator identifier; the messaging and behavior expand.

### Move 6: Req-reviewer tier-aware contract

The requirement-level reviewer's contract changes from "do all tests satisfy all scenarios?" (ill-defined when scenarios cross tiers the dev can't observe) to "do the dev's tests satisfy the obligations at the dev tier?":

```
for scenario in requirement.scenarios:
    if "@unit" in scenario.tags:
        find a unit test method exercising this scenario.
        reject if missing.

    elif "@integration" in scenario.tags:
        find a tagged test file that:
          - references at least one HarnessProfileID as a string literal
          - reads endpoint from env per the catalog profile's env declaration
          - asserts on each required_assertion of each bound profile
        reject if any of the above is missing.
        the test does NOT need to PASS at dev-completion (no harness running in
        the dev sandbox); it needs to be authored correctly. qa-runner gates
        passing later, per ADR-039.

    elif "@smoke" or "@e2e" in scenario.tags:
        find a stub test file OR a documented release-gating plan.
        do not block dev approval. these are deferred to scheduled tiers.
```

The req-reviewer's prompt is rewritten to make this contract explicit. The persona is told: "you are looking at evidence of correct authoring at the dev tier. You are NOT looking at evidence of integration behavior — the test environment isn't running. Your verdict is about whether the dev's tests are correctly scaffolded for the tier each scenario claims, not about whether the system behavior holds end-to-end."

This is the load-bearing change for issue #37. The infinite-reject loop becomes structurally impossible because the req-reviewer is no longer asked to confirm assertions it can't observe.

## Architecture

### Predicate additions (3-part dotted, no slugs or instance IDs in the predicate)

Following ADR-040's predicate convention:

| Predicate | Subject (6-part EntityID) | Object | Cardinality | Notes |
|---|---|---|---|---|
| `semspec.scenario.tag` | `…wf.plan.scenario.{hash}` | string (e.g., `"@unit"`, `"@integration"`, `"@flaky"`) | N | One triple per tag; tier tag MUST appear exactly once per scenario |
| `semspec.scenario.harness_profile` | `…wf.plan.scenario.{hash}` | string (profile_id) | N | Multi-valued; cross-reference into the test environment catalog. Underscore in property segment matches existing `semspec.requirement.depends_on` convention. |
| `semspec.capability.surface` | `…wf.plan.capability.{hash}` | `"ui"` \| `"api"` \| `"background"` | N | One triple per declared surface |

These satisfy the rev-5 ADR-040 predicate convention: strictly 3-part `domain.category.property`, no slugs or instance IDs in the predicate itself, underscores in property segment allowed.

### Component changes

| Component | Change | LOC est. |
|---|---|---|
| `workflow` | Extend `Scenario` with `Tags []string` + `HarnessProfileIDs []string`. Extend `Capability` with `Surfaces []CapabilitySurface`. Add validators (`ValidateScenarioTags`, `ValidateCapabilitySurfaces`). | ~120 |
| `vocabulary/semspec/` | Add the three predicates above. | ~30 |
| `processor/planner` analyst sub-phase | Update persona prompt to also classify `surfaces` per capability. | ~30 (prompt-only) |
| `processor/scenario-generator` | Add classifier + tier-emission dispatch. Per-requirement: walk architecture → bound profiles, compute `must_emit` set, dispatch one LLM call per tier with tier-appropriate persona instructions. Validate output (one tier tag, harness binding consistency, anti-pattern checks). | ~200 |
| `processor/plan-reviewer` | Add five scenario-tag rules + targeted-regen wiring. | ~150 |
| `processor/structural-validator` | Extend `CheckHarnessProfileDiscipline` with the harness-binding string presence check + env-var consumption pattern check. | ~80 |
| `processor/requirement-executor` | Update `buildReviewPrompt` to communicate the tier-aware contract to the req-reviewer persona. Update verdict-parsing if needed. | ~60 |
| `prompt/domain/software.go` | Update the scenario-generator persona prompt (John) with the tier-emission rules + anti-pattern examples. Update the req-reviewer persona prompt with the tier-aware contract. Update rule 7b (`Integration-target test environment discipline`) to reference scenarios' `HarnessProfileIDs` as the binding source, not implicit architectural inference. | ~200 (prompt-only) |
| `output/workflow-documents/openspec/spec.go` | OpenSpec emitter renders scenario tag line + harness binding line in spec.md. Inbound importer (folded from ADR-038 / ADR-040 Move 4) parses both. | ~40 |

**Total: ~910 LOC additive. Zero deletions of existing functionality.**

### OpenSpec round-trip syntax

Tags and harness bindings round-trip through OpenSpec spec.md as adjacent prose lines:

```markdown
#### Scenario: MAVSDK heartbeat observed after driver start

`@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`

**GIVEN** the MAVSDK Driver is configured with $SITL_ENDPOINT from the env
**WHEN** the driver starts
**THEN** a MAVLink HEARTBEAT is received within 10 seconds AND the MAVSDK
 Core connection state transitions to `mavsdk_core_connected`
```

The tag line on its own row is Cucumber-friendly. The harness binding lives in a small inline-code prose line right below it — visible to readers, parseable by the OpenSpec emitter/importer, doesn't require the tag namespace to swallow dots or colons.

Adopter tools (`openspec validate`) continue to parse the file as standard markdown. Their parsers don't have to know about tags; the lines are valid markdown prose. Semspec's emitter writes them; semspec's importer (ADR-040 Move 4) reads them back into `Scenario.Tags` and `Scenario.HarnessProfileIDs`.

### What stays exactly the same

- Plan / PlanState enum / PLAN_STATES KV bucket
- EXECUTION_STATES KV bucket + execution-manager TDD pipeline
- Test environment catalog (ADR-039) + qa.yml services rendering
- Persona names: Mary, John, Murat, architect, developer, reviewer — BMAD-aligned per ADR-030
- Component names: `qa-reviewer`, `qa-runner`, `structural-validator`, `harness-profile-discipline` check identifier
- JSON wire field names: `architecture.harness_profiles[]`, `harness_profile_ids`, etc.
- Workflow status enum values: `reviewing_qa`, `ready_for_qa`
- Existing BMAD + OpenSpec output via `output/workflow-documents/`
- Autonomous QA-recovery cascade (ADR-037 + PRs #29-#34) — though issue #36's combinatorial-budget bug is a separate fix

### Persona prompt updates

Three personas get tier-aware language. Operator-tunable via `configs/presets/bmad.json`:

**Mary (analyst sub-phase)** — surface classification appended to her existing exploration role:

> When you list capabilities, also classify each one's surface(s): "ui" (user-visible interface), "api" (programmatic surface for other systems), or "background" (scheduled or event-driven, no human surface). Most capabilities have one surface. UI work has "ui". A background scheduler has "background". A library that other code imports has "api". When in doubt, prefer "api". The architect will use surfaces to select test environments; the scenario-generator will use surfaces to know whether to emit @e2e scenarios.

**John (scenario-generator)** — tier-aware emission:

> For each Requirement: produce scenarios at multiple test tiers. Every requirement MUST have at least one @unit scenario. Requirements bound to services-class or testcontainers-class test environments MUST have at least one @integration scenario tagging that environment. Requirements whose owning capability lists "ui" in surfaces MUST have at least one @e2e scenario.
>
> Tag line goes on its own row above each Scenario heading: `@unit` or `@integration` or `@e2e`. For @integration scenarios, add a `harness:` line immediately below listing the bound test environment profile IDs.
>
> @unit scenarios MUST NOT mention real services, SITL containers, databases, or peers requiring a running process. Use fakes, in-process state, or pure-fixture environments.
>
> @integration scenarios MUST assume the test environment endpoint is environment-injected (e.g., "Given the endpoint at env `$SITL_ENDPOINT`...") and MUST NOT instruct test code to start the environment — qa-runner does that.

**Murat (qa-reviewer) — UNCHANGED** for this ADR. The req-level reviewer (not Murat) is what gets the tier-aware contract update. Murat operates at plan completion per ADR-031 and isn't part of the dev-loop reject pattern this ADR fixes.

**Reviewer (req-level reviewer persona)** — tier-aware contract:

> You are looking at evidence of correct authoring at the dev tier. You are NOT looking at evidence of integration behavior — the test environments aren't running. Your verdict is about whether the dev's tests are correctly scaffolded for the tier each scenario claims, not about whether system behavior holds end-to-end.
>
> For each scenario: check the dev's tests match the scenario's claimed tier. @unit scenarios need unit tests exercising the behavior with fakes. @integration scenarios need tagged test files referencing the harness profile ID, reading endpoint from env, asserting on the catalog's required assertions — they do NOT need to pass at this dev-completion review, only to be authored correctly. @smoke and @e2e scenarios need at least a stub test file or a documented release-gating plan — they do not block dev approval.
>
> If a scenario at one tier seems to require evidence only observable at a higher tier (e.g., an @unit scenario that mentions PX4 SITL), flag it as `scenario.unit_mentions_services` — a category error at scenario authoring time. Do not silently reject the dev's tests for failing to meet impossible obligations.

## Validation against the run-shape failure

Replay the OSH/MAVSDK prompt through the revised pipeline:

1. **Mary (analyst sub-phase, ADR-040 + ADR-041)** classifies the prompt's capabilities and adds `surfaces`:
   ```
   capabilities:
     - mavsdk-lifecycle (new, surfaces: [api]): boot mavsdk_server, peer connection
     - mavsdk-telemetry-datastreams (new, surfaces: [api]): CS API DataStreams
     - mavsdk-control-commands (new, surfaces: [api]): CS API ControlStreams
     - raw-mavlink-integration (new, surfaces: [api]): raw MAVLink fallback
   ```
   No "ui" surfaces → no @e2e scenarios will emit.

2. **Architect** selects test environment profiles via the catalog (ADR-039 — unchanged): `mavlink.px4-sitl.mavsdk-smoke` + `mavlink.raw-mavlink-direct`, both `services`-class.

3. **John (scenario-generator with ADR-041 classifier)** sees that `mavsdk-lifecycle` is bound to `mavlink.px4-sitl.mavsdk-smoke` (services-class) and emits:
   - 2-3 `@unit` scenarios: state-machine transitions, command-line builder, config validation — pure logic, no SITL mention
   - 2-3 `@integration` scenarios with `HarnessProfileIDs: ["mavlink.px4-sitl.mavsdk-smoke"]`: MAVSDK Core connection, HEARTBEAT observation, lifecycle termination — endpoint-from-env, MAVSDK behavior observable when the harness is up
   - Zero `@e2e` scenarios (no "ui" surface)
   - Zero `@smoke` scenarios (not explicitly directed)

4. **Plan-reviewer R2** runs the new rules:
   - `scenario.missing_tier_tag`: zero (every scenario has exactly one tier tag)
   - `scenario.missing_unit_coverage`: zero (every requirement has @unit scenarios)
   - `scenario.missing_integration_for_services`: zero (services-class profile has @integration scenarios)
   - `scenario.harness_id_unresolved`: zero (all profile IDs resolve)
   - `scenario.unit_mentions_services` (warn): zero (persona prompt anti-pattern enforcement)

5. **Execution-manager + dev pipeline** (unchanged) implements the requirement. The dev produces unit tests + tagged integration tests with the SITL endpoint read from env.

6. **Structural-validator** (Move 5) checks the integration tests reference `mavlink.px4-sitl.mavsdk-smoke` as a string literal, read endpoint from `$SITL_ENDPOINT`, and assert on the catalog's required assertions (`HEARTBEAT`, `mavsdk_core_connected`, etc.). All present → pass.

7. **Req-reviewer** (Move 6) runs the tier-aware contract:
   - @unit scenarios: unit tests exist exercising each. ✓
   - @integration scenarios: tagged test files reference the profile ID, read endpoint from env, assert on required catalog assertions. **The tests do not need to pass here.** ✓
   - Approve.

8. **qa-reviewer (Murat) at plan completion** triggers qa-runner. ADR-039 renders qa.yml services from `mavlink.px4-sitl.mavsdk-smoke`. qa-runner brings up SITL. The tagged integration tests run. **PASS or FAIL here is qa-runner's gate — and it's gated on a HARNESS THAT'S ACTUALLY RUNNING.** The system-level integration verdict is observable for the first time at the tier where it can be observed.

The infinite-reject pattern from the 2026-05-31 run becomes structurally impossible:

- Mary's surface classification prevents stray @e2e emission for API-only capabilities
- John's tier-emission rule prevents @unit scenarios from mentioning SITL
- The req-reviewer's tier-aware contract doesn't ask the dev to satisfy assertions only the QA tier can observe
- The structural-validator gates @integration tests on correct scaffolding, not running behavior

## Migration

Six PRs, sequenced for review. Each independently shippable, but downstream PRs in the chain assume earlier ones have landed.

### PR 1: Data model + predicates (~2 days)

- Extend `workflow.Scenario` with `Tags []string` + `HarnessProfileIDs []string`
- Extend `workflow.Capability` with `Surfaces []CapabilitySurface`
- Add `ValidateScenarioTags`, `ValidateCapabilitySurfaces`
- Wire the three predicates into `vocabulary/semspec/`
- Unit tests against the validators
- One mock e2e fixture asserting Scenario.Tags is populated after scenario generation

### PR 2: Mary's surface classification + John's tier-emission classifier (~3 days)

- Update Mary's analyst persona (in `configs/presets/bmad.json`) for surface classification
- Update John's scenario-generator persona for tier-emission
- Add the tier-emission classifier algorithm in `processor/scenario-generator/` (deterministic + LLM dispatch)
- Unit tests against the classifier (deterministic boundary: which profiles are relevant per capability)
- Mock e2e fixtures: assert scenarios for a capability bound to a services-class profile have @unit + @integration; assert scenarios for a UI capability have @e2e; assert anti-pattern enforcement (no @unit scenario mentions SITL)

### PR 3: Plan-reviewer scenario-tag rules (~2 days)

- Add five rules in `processor/plan-reviewer/scenario_tag_rules.go`
- Typed finding shapes + targeted-regen wiring
- Tier-1 seam coverage tests against representative plans
- E2E mock fixture: produce a plan with @unit scenarios mentioning SITL, assert plan-reviewer rejects with the right SOP ID

### PR 4: Structural-validator harness-binding + env-consumption checks (~2 days)

- Extend `CheckHarnessProfileDiscipline` with the two new check types
- Unit tests against tagged test file fixtures (Java, Python, Go, TypeScript)
- Verify check name and operator-visible stdout vocabulary lands consistently

### PR 5: Req-reviewer tier-aware contract (~2 days)

- Update the reviewer persona prompt with the tier-aware contract
- Adjust `buildReviewPrompt` in `processor/requirement-executor/` to communicate the contract
- Mock e2e fixture: produce an @integration scenario with correctly scaffolded but non-passing tagged tests, assert req-reviewer approves at dev-completion time
- **This is the load-bearing fix for issue #37.** Real-LLM smoke (gemini @ easy) after this lands.

### PR 6: OpenSpec round-trip syntax + rule 7b update (~2 days)

- OpenSpec emitter renders tag line + harness binding line in spec.md per the architecture section
- Importer parses both back into `Scenario.Tags` and `Scenario.HarnessProfileIDs`
- Rule 7b in `prompt/domain/software_render.go` updated to reference scenarios' `HarnessProfileIDs` as the binding source (the rewrite deferred from PR #41)
- Round-trip test: import a tagged spec, run scenario-generator, emit OpenSpec artifacts, diff inbound vs outbound — only differences should be semspec-side mutations

**Total: ~13 working days.** PRs 1+2 are the data model + classifier. PR 5 is the req-reviewer contract fix that unblocks the run-#3-shape failure. PR 4 + 6 close the structural validation surface.

## Open questions (resolved)

All design tensions from the ADR-041 conversation are resolved with the operator-confirmed leans:

- **(Q1) Tier tag granularity:** Closed at the four BDD-standard buckets `@unit @integration @smoke @e2e`. Sub-tiers like `@integration:db @integration:proto` are deferred to a future ADR if needed; ship boring first.
- **(Q2) Closed vs extensible tag set:** Extensible. Tier tags + harness-related tags are structurally validated; operator-defined facet tags (`@security`, `@flaky`, `@compliance`, etc.) pass through validation as informational metadata.
- **(Q3) `@e2e` detection signal:** Via `Capability.surfaces` populated by Mary (analyst sub-phase). Replaces the fragile "user prompt contains 'user flow'" heuristic.
- **(Q4) Harness binding syntax:** Structured field `Scenario.HarnessProfileIDs []string`, not a tag. Avoids the pytest-bdd / behave / karate compat hazards of colon-bearing or dot-bearing tag names. Profile IDs keep their dotted catalog naming without normalization layer.
- **(Q5) Multi-binding (one scenario, multiple harness profiles):** Allowed via multi-valued `HarnessProfileIDs`. Plan-reviewer rule still requires at least one scenario per services-class profile if the architecture mandates coverage.
- **(Q6) Connection to Ops persona / harness-manager (ADR-042 deferred):** ADR-041 doesn't need Ops to land. `Scenario.HarnessProfileIDs` exists either way; ADR-042's runtime owner just adds `HarnessRun` lifecycle state on top.
- **(Q7) OpenSpec round-trip syntax:** Tag line on its own row above the `#### Scenario:` heading + harness binding line immediately below. Cucumber-friendly, markdown-valid, parseable on import.
- **(Q8) Vocabulary rename scope:** Soft rename (PR #41 — user-facing surfaces) lands first; hard rename (wire fields + Go types) deferred. ADR-041 writes in the soft-renamed vocabulary.

## Consequences

### Positive

- **Issue #37's infinite-reject loop becomes structurally impossible.** The req-reviewer is no longer asked to confirm assertions it can't observe. Each tier's verdict is well-defined and converges.
- **Boring industry vocabulary throughout.** BDD tags every adopter recognizes; tag-selector language for free runtime filtering; OpenSpec round-trip without a custom parser. Sponsor narrative reads like CI/CD docs, not internal jargon.
- **Smaller-model viability improves.** Each tier emits one focused LLM call from John; the persona context per call is smaller and more cohesive than the current single multi-tier-mixing prompt.
- **Real-LLM cost predictability.** With the tier-aware req-reviewer contract, dev-completion verdicts converge based on authoring evidence rather than asking the model to predict future runtime behavior. Burn-rate per requirement becomes bounded by tier-coverage scope, not by the autonomous-recovery cascade trying to satisfy un-satisfiable contracts (which is issue #36's adjacent fix).
- **Decoupled from ADR-042.** Ops persona / harness-manager / HarnessRun lifecycle ship later without rework here.
- **No persona renames, no substrate changes, no JSON wire breakage.** Plan model survives. Components evolve.

### Negative

- **One LLM call per tier instead of one per requirement.** Net: ~50-100% wallclock + token cost increase for the scenario phase. Per-call cost is lower (smaller, more cohesive context); per-requirement total is higher. Acceptable — the scenario phase is a small fraction of total per-run cost, and tier-appropriate scenarios produce dramatically better dev outcomes at execution time.
- **Persona prompt drift management.** Mary, John, and the reviewer persona all get rewrites. Calibration needed against real prompts. Gemini @ easy + @ hard regression after PR 5 lands.
- **New rule surface in plan-reviewer.** Five new rules add validator complexity. Mitigated by ADR-040's existing five-rule precedent and shared finding-shape pattern.
- **The hard rename of wire fields (`architecture.harness_profiles[]` → `test_environments[]` etc.) is deferred.** Soft-rename vocabulary disconnect (user-facing prose says "test environment profile", JSON field still reads `harness_profiles`) persists until a future PR. Documented in `docs/project-setup.md`.

### Neutral

- **OpenSpec compatibility preserved.** Tag lines and harness binding lines are standard markdown. Adopter tools (`openspec validate`) parse them as prose.
- **BMAD persona alignment preserved.** Mary, John, Murat keep their names per ADR-030. The reviewer persona (which gets the tier-aware contract update) doesn't have a BMAD name — it's the requirement-level reviewer role.
- **No e2e fixture demolition.** Existing fixtures continue working; new fixtures added.
- **Issue #36 (combinatorial retry budget) ships as a separate small PR.** It's contained in `processor/requirement-executor/awaiting_recovery.go` and orthogonal to the scenario-tier work. Issue #38 (decomposer wrong-package paths) is similarly contained.

## Decision is

**Accept this ADR and proceed with the six-PR migration. PRs 1+2 build the data model + classifier foundation. PR 5 is the load-bearing fix for issue #37 — the req-reviewer's tier-aware contract that unblocks the mavlink-hard non-convergence pattern. PRs 3 + 4 + 6 close the structural validation surface and OpenSpec round-trip.**

Required confirmation before code lands:

1. Operator (Coby) signs off on the four persona prompt rewrites (Mary surface classification, John tier-emission, reviewer tier-aware contract, reviewer-side anti-pattern flagging).
2. Operator confirms the 13-day migration is acceptable and which PR sequences first if other priorities collide.
3. Operator confirms issue #36 ships as a separate small fix PR (not bundled into ADR-041 implementation).

This ADR explicitly preserves:
- The SKG as the authoritative state substrate
- The Plan / PLAN_STATES model and the test environment catalog (ADR-039)
- The dev pipeline + autonomous QA recovery cascade (with issue #36 fixed separately)
- BMAD-shaped and OpenSpec-shaped output (ADR-030, ADR-040)
- Persona names (Mary, John, Murat) per ADR-030
- JSON wire field names (`harness_profiles[]`, `harness_profile_ids`, etc.) per the soft-rename scope from PR #41

The operator framing — **"public/operator-facing terms should be boring industry words; semspec-specific names can exist internally"** — is the load-bearing constraint this ADR is designed to honor. ADR-040 applied it to capability vocabulary via OpenSpec; ADR-041 applies it to the test surface via BDD/Cucumber.
