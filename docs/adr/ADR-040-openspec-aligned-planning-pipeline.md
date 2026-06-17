# ADR-040: OpenSpec Vocabulary Alignment + Bidirectional Compat (SKG-Authoritative)

**Status:** Proposed (2026-05-30, revision 5)
**Deciders:** Coby, Claude
**Replaces:** ADR-040 rev 1 (greenfield-replacement framing — rejected 2026-05-30), ADR-040 rev 2 (added implementation/derived capability classification — superseded by vocab-alignment direction), ADR-040 rev 3 (wrong predicate shape — 5+ parts with embedded slugs; corrected in rev 4 to semstreams 6-part EntityID / 3-part predicate convention. `depends_on` upgraded from advisory to hard constraint per operator review.), ADR-040 rev 4 (predicate surface-syntax over-tightening — corrected in rev 5 to match existing repo convention: underscores allowed in property segment, use full word `requirement` not `req`, all predicates strictly 3-part `domain.category.property`; the load-bearing rule remains "no slugs or instance IDs in predicates"; the only structural violation found in the existing codebase — the `source.git.decision.*` 4-part family — was flattened in PR 1b as part of this rev).
**Folds in (supersedes):** ADR-038 (OpenSpec Spec Import — Brownfield Plan Bootstrap). ADR-038's inbound design is incorporated as Move 4 of this ADR so the full OpenSpec story (vocab + outbound + inbound) is unified.
**Builds on:** ADR-029 (plan completeness + retry), ADR-030 (BMAD persona alignment), ADR-037 (wedge recovery), ADR-039 (harness catalog → qa.yml rendering)
**Amended by:** ADR-050 (contract authority over BMAD/OpenSpec handoffs)
**Does not change:** Plan / PlanState / PLAN_STATES, ENTITY_STATES, the semstreams substrate, the harness catalog, the SKG, the dev → validator → reviewer → QA pipeline, the autonomous recovery cascade, the existing BMAD-shaped outputs in `output/workflow-documents/`.

## Context

### What rev 1 and rev 2 got wrong

Rev 1 proposed replacing semspec's Plan model with OpenSpec's Spec/Change/Delta entities and operating the dev pipeline as OpenSpec's "apply phase." Operator rejected as "throwing out semspec to become an OpenSpec prose machine."

Rev 2 corrected the framing but introduced a custom `CapabilityKind = Implementation | Derived` classification to prevent run #3's failure mode in the type system. Operator preference: align capability vocabulary fully with OpenSpec (which uses `New | Modified` lifecycle distinction, not implementation/derived). The prevention of run #3's failure mode shifts from type-system enforcement to persona-prompt and plan-reviewer-rule enforcement. Vocabulary alignment is the higher-order goal: closer to OpenSpec is easier for adopters to grok and for us to maintain bidirectional compat.

This revision preserves the architectural shape rev 2 got right (no model replacement, SKG-authoritative, prose-as-side-effect) and aligns the capability vocabulary exactly with OpenSpec.

### The triggering failure

Three real-LLM runs of the OSH/MAVSDK epic (`docs/sponsor-packages/mavlink-osh-hybrid-gpt5-2026-05-29/`) produced essentially no MAVSDK driver code after ~3h of paid LLM work in run #3:

- `build.gradle` (dependency configuration)
- `BuildGradleConfigurationTest.java` (asserts strings exist in `build.gradle`)
- A README documenting a non-existent coverage matrix

Zero `mavsdk_server` lifecycle code. Zero CS API DataStream/ControlStream implementations. Zero MAVLink fallback. 9 approved reviewer verdicts — all on meta-work.

Forensic finding (plan-reviewer iteration 1):

> "The plan misses requirements for the core implementation. The goal specifies 'Implement MAVLink/MAVSDK support...' but the requirements only cover `build.gradle` and `README.md`. The files in `scope.create` are completely orphaned. Suggestion: Add **a** requirement to implement the MAVSDK driver, generic MAVLink fallback, and tests."

Plan-reviewer's catch was right. Its suggestion was too coarse ("add A requirement" singular → mega-bundled req that never executed). Upstream cause: the planner persona is asked to do too much in one LLM call and lands on "scope = files I see named in the prompt text" instead of "scope = files needed to realize the goal."

### The architectural reality this revision respects

| Part | What it does | Why it's authoritative |
|---|---|---|
| SKG (`ENTITY_STATES` triples) | Cross-component facts | The harness catalog selection (ADR-039) consults graph entities to map capabilities → catalog profiles. Without the SKG, harness selection is structurally impossible. |
| PLAN_STATES + EXECUTION_STATES | Runtime state for plans + their executions | Single-writer pattern (plan-manager / execution-manager) gives consistency without distributed locking. Replacing this substrate is a multi-quarter effort. |
| `vocabulary/spec/` (already present) | OpenSpec predicate definitions | Per (folded) ADR-038: semsource indexes OpenSpec markdown end-to-end. The graph already speaks OpenSpec. No new vocabulary infra needed. |
| `output/workflow-documents/` | Emits BMAD-shaped artifacts | Per ADR-030: our BMAD vocabulary alignment is concrete via this output layer. Adding an OpenSpec emission sibling is incremental. |
| ADR-038 design (folded in) | Three-layer defense for OpenSpec → PLAN_STATES translation | Designed, ready to implement; folded here so it stays in lock-step with the outbound emitter and the vocab alignment. |

The framing, operator-named:

> **We hydrate OpenSpec from our flow. Prose is a side effect.**

The SKG and Plan model are the authoritative state. OpenSpec files are emitted projections — written from our state for adopter compatibility. ADR-038 (folded) covers the inbound projection. This ADR's Move 3 covers the outbound projection. Move 1+2 fix the planning quality bug that run #3 surfaced. Move 4 is the inbound import.

## Decision

**Four additive moves. No model replacement, no substrate change. ADR-038 folded in as Move 4 to ensure inbound + outbound + vocabulary land coherently.**

### Move 1: Split Mary's job, not Mary herself

Add an explicit **exploration sub-phase** inside the existing `planner` component:

- **Sub-phase A — Analyst**: Mary reads the user prompt + project context and produces a structured **exploration document**: a list of capabilities (with OpenSpec `New | Modified` lifecycle distinction) + open questions. No scope. No files. ≤2 KB output.
- **Sub-phase B — Planner**: Mary reads the exploration document + project context and produces the existing `goal + context + scope` Plan fields. Scope is derived from the capability list.

Both sub-phases live inside the existing `planner` component, dispatched serially. The Plan entity gains one new field — `exploration` — written to PLAN_STATES (per Q2: KV write for audit trail + deterministic reference by plan-reviewer + content source for OpenSpec emitter). PLAN_STATES schema bumps a version; no new KV bucket.

### Move 2: Capability-first requirement-generator + plan-reviewer rule

The requirement-generator's persona prompt consumes `Plan.exploration.capabilities` directly:

- One Requirement per capability
- Each Requirement uses SHALL or MUST normative language
- Each Requirement has ≥1 scenario in GIVEN/WHEN/THEN format
- Each Requirement owns a focused set of files (`applies_to` glob ≤5 files); if a capability would require more, the analyst step gets a `capability_too_broad` finding and re-runs

Plan-reviewer gains **three** new structural rules:

1. **`capability_orphan`**: "Every capability in `Plan.exploration` MUST have at least one Requirement whose `applies_to` glob includes implementation files (not just `*.md` documentation)." This is the deterministic encoding of what plan-reviewer's prose catch in run #3 was trying to enforce. The prose rejection becomes a typed finding that triggers targeted regen of just the requirement step — not the whole cascade.

2. **`capability_dependency_cycle`**: Hard reject any plan whose `semspec.capability.dependency` triples form a cycle. No warn-fallback. Operator note: capabilities with cyclic dependencies cannot be serialized; parallel execution of cyclic capabilities will not converge. Rejecting at plan-review time is much cheaper than discovering it during execution.

3. **`capability_dependency_orphan`**: Hard reject any `Capability.depends_on` reference to a capability not present in the same `Plan.exploration`. Either the analyst sub-phase missed a capability or the depends_on is wrong.

Prevention of the "docs-as-capability" failure mode lives in **persona prompts + the `capability_orphan` rule**, not in a type-system enum. This matches OpenSpec — they enforce normative discipline through prose guidance in proposal/spec instructions and through structural validation, not through capability subtyping.

requirement-executor uses the `semspec.capability.dependency` DAG (already-validated by plan-reviewer above) to serialize Requirement execution along capability dependency edges. Requirements owning the same `applies_to` files cannot execute in parallel — the depends_on DAG converts to an execution-time dependency edge automatically.

### Move 3: OpenSpec emission as output sibling

Extend `output/workflow-documents/` with an OpenSpec emitter that projects the existing Plan model into OpenSpec file layout. **All artifacts emit live (per Q3: per-mutation, 1s debounced) including `tasks.md` checkbox state (per Q4: live flip as execution-manager completes nodes).**

`.openspec.yaml` metadata declares schema = `spec-driven` (per Q5: align with OpenSpec default; no custom semspec schema).

### Move 4 (folded from ADR-038): OpenSpec inbound import

Adopt ADR-038's three-layer defense for OpenSpec spec import, with one update: imported plans now produce `Plan.exploration` populated from the source spec's capability identity (kebab-case spec name → `Capability.name` directly). This seals the round-trip:

```
OpenSpec spec.md → semsource → graph (ENTITY_STATES) → translator → Plan + Capabilities + Requirements (PLAN_STATES, with external_ref triples)
                                                                              │
                                                                              ▼
                                                                    Plan-reviewer + execution pipeline
                                                                              │
                                                                              ▼
                                                              OpenSpec emitter (Move 3) → OpenSpec spec.md
```

The external_ref triple bridge from ADR-038 §"Architectural commitment 5" preserves source-spec identity through the round-trip. When ADR-038's emitter runs on a Plan that was imported, the regenerated `specs/<cap>/spec.md` carries the original `applies_to` and overview verbatim (via external_ref); only Plan-mutated content (scenarios refined by reviewer, scope evolved by execution) reflects semspec-side changes.

Phase 1 implementation surface from ADR-038 is unchanged:
- project-manager `/detect` extended to find OpenSpec paths
- Layer 1: structural pre-check (`workflow/specimport/structural_check.go`, ~50 LOC)
- Layer 2: existing plan-reviewer is the semantic gate
- Layer 3: existing recovery-agent (ADR-037) is the wedge-recovery net
- `workflow/specimport/translator.go` (~300 LOC) — graph entities → PLAN_STATES with capability hydration into `Plan.exploration`
- `POST /plan-manager/plans/from-spec` HTTP handler
- UI: spec picker + import preview + verdict handling

Delta specs map to PlanDecisions per ADR-038 unchanged. Identity bridge via `external_ref.spec_requirement` triples per ADR-038 unchanged. Out-of-scope items from ADR-038 (live sync, reverse rendering, drift detection, BMAD ingest) remain out of scope here.

## Architecture

### Capability vocabulary (aligned with OpenSpec) + semstreams 6/3 convention

Semstreams enforces two foundational conventions this ADR must honor:

- **6-part dotted EntityID**: `{org}.{platform}.wf.{domain}.{type}.{hash}` — last segment is `HashInstanceID(logical-id)`. Logical IDs surface as triples on the entity. Semspec already ships `Plan`, `Spec`, `Req`, `Scenario`, `Tasks`, `Task`, `Phase`, `Phases`, `Project`, `Config` entity types in `workflow/entity.go`.
- **3-part dotted predicate**: `{domain}.{category}.{property}` — lowercase, no camelCase, no embedded slugs or instance IDs. Underscores in the property segment are accepted per existing repo convention (`semspec.requirement.depends_on`, `semspec.plan.scope_include`, `semspec.plan.created_at`); hyphens too (`semspec.plan.github-epic`). The load-bearing rule is the no-slugs-no-instance-IDs constraint — surface syntax in the property segment follows what the rest of the codebase already uses. Example: `semspec.plan.title`. Example: `semspec.capability.depends_on`.

In `workflow/types.go`:

```go
type Plan struct {
    // ... existing fields ...
    Exploration *Exploration `json:"exploration,omitempty"`
}

type Exploration struct {
    Capabilities  []Capability `json:"capabilities"`
    OpenQuestions []string     `json:"open_questions,omitempty"`
}

type Capability struct {
    Name        string              `json:"name"`        // kebab-case identifier (matches OpenSpec spec dir name)
    Lifecycle   CapabilityLifecycle `json:"lifecycle"`   // New | Modified (matches OpenSpec proposal sections)
    Description string              `json:"description"` // 1-3 sentences
    DependsOn   []string            `json:"depends_on,omitempty"` // other capability names — HARD CONSTRAINT (see below)
}

type CapabilityLifecycle string
const (
    CapabilityNew      CapabilityLifecycle = "new"      // OpenSpec: "New Capabilities" in proposal.md
    CapabilityModified CapabilityLifecycle = "modified" // OpenSpec: "Modified Capabilities" in proposal.md
)
```

No `Kind` enum (implementation vs derived). Capability vocabulary is OpenSpec-aligned. The prevention of doc-as-capability failure modes lives in persona prompts + plan-reviewer rules (Move 2).

**`Capability.DependsOn` is a HARD CONSTRAINT, not advisory.** Per operator review: if A depends on B and both run in parallel, A may never converge because B's outputs don't exist yet. plan-reviewer rejects cycles (no warn-fallback); requirement-executor serializes requirements that own conflicting `applies_to` globs along the depends_on DAG.

### New entity type — `Capability`

Add to `workflow/entity.go`:

```go
// CapabilityEntityID returns the 6-part federated entity ID for a Capability.
// Format: {org}.{platform}.wf.plan.capability.{hash}
func CapabilityEntityID(slug, capabilityName string) string {
    return fmt.Sprintf("%s.wf.plan.capability.%s", EntityPrefix(), HashInstanceID(slug, capabilityName))
}
```

The logical ID composed from `slug` + `capabilityName` ensures uniqueness across plans (two different plans can both declare a `mavsdk-bootstrap` capability without collision). The logical pair surfaces on the entity as triples (predicates below).

Capability is a first-class entity. It owns triples. It can be referenced by Requirement entities via predicate triples. It can be queried via graph-query for the harness catalog (ADR-039) without going through the Plan owner.

### Predicate additions (3-part dotted, no slugs or instance IDs in the predicate)

All predicates live in `vocabulary/spec/` and follow `domain.category.property`. Slugs and capability names appear in the **subject and object** of the triple (as 6-part EntityIDs), never in the predicate itself.

| Predicate | Subject (6-part EntityID) | Object | Cardinality | Notes |
|---|---|---|---|---|
| `semspec.capability.name` | `…wf.plan.capability.{hash}` | string (kebab-case) | 1 | Logical capability name |
| `semspec.capability.lifecycle` | `…wf.plan.capability.{hash}` | `"new"` or `"modified"` | 1 | OpenSpec lifecycle |
| `semspec.capability.description` | `…wf.plan.capability.{hash}` | string | 1 | 1-3 sentences |
| `semspec.capability.plan` | `…wf.plan.capability.{hash}` | `…wf.plan.plan.{hash}` | 1 | Capability → owning Plan |
| `semspec.capability.depends_on` | `…wf.plan.capability.{hash}` | `…wf.plan.capability.{hash}` | N | Capability → capability it depends on (hard constraint). Underscore in property segment matches existing `semspec.requirement.depends_on` convention. |
| `semspec.plan.exploration` | `…wf.plan.plan.{hash}` | string (JSON blob) | 0 or 1 | Plan's exploration document |
| `semspec.plan.open_questions` | `…wf.plan.plan.{hash}` | string | N | One predicate per question (multi-valued; matches `semspec.plan.execution_trace_id` repo pattern) |
| `semspec.requirement.capability` | `…wf.plan.req.{hash}` | `…wf.plan.capability.{hash}` | 1 | Requirement → its owning Capability. Full word `requirement` matches the existing `semspec.requirement.*` family (rev 4 said `semspec.req.*`; rev 5 corrects to match repo). |
| `semspec.requirement.external_spec` | `…wf.plan.req.{hash}` | external entity ID | 0 or 1 | (from folded ADR-038) imported requirement provenance |
| `semspec.capability.external_spec` | `…wf.plan.capability.{hash}` | external entity ID | 0 or 1 | imported capability provenance for round-trip |

Why this shape works:
- Every predicate is exactly three dotted segments (`domain.category.property`)
- Subjects and objects are 6-part federated entity IDs (or primitive values for terminal properties)
- NATS wildcard queries like `semspec.capability.*` work — find every fact about capabilities
- The harness catalog (ADR-039) can query `semspec.capability.name = "mavsdk-bootstrap"` to find the capability entity, then traverse `semspec.requirement.capability` reverse-edges to find owning requirements, then map to catalog profiles. All via standard graph traversal.

Written through plan-manager (single writer) per the CQRS twofer pattern. No new KV bucket. The `semspec.*.external.spec` predicates enable round-trip identity for the inbound/outbound projections.

### Component changes

| Component | Change | LOC est. |
|---|---|---|
| `planner` | Add Analyst sub-phase before existing Planner phase. Both reuse `planning` capability (same gemini-pro endpoint). Plan-manager emits Analyst output as `Plan.exploration` to PLAN_STATES before dispatching the Planner LLM call. | ~150 |
| `requirement-generator` | Update persona prompt to consume `Plan.exploration.capabilities`. Rule: one Requirement per capability. | ~50 |
| `plan-reviewer` | Add `capability_orphan` structural rule + typed finding shape + targeted regen wiring. | ~80 |
| `output/workflow-documents/openspec/` | NEW: OpenSpec emitter (proposal/specs/design/tasks) + `.openspec.yaml`. Read-only against PLAN_STATES + EXECUTION_STATES. Per-mutation 1s-debounced; live checkbox flipping on tasks.md. | ~300 |
| `project-manager` | (Move 4) Extend `/detect` for OpenSpec conventional paths + graph-readiness poll. | ~80 |
| `workflow/specimport/` | NEW (Move 4): structural_check.go + translator.go (graph → PLAN_STATES + Plan.exploration hydration). | ~350 |
| `plan-manager` HTTP | NEW (Move 4): `POST /plan-manager/plans/from-spec` handler. | ~80 |
| `vocabulary/spec/` | Add the predicates table above. | ~40 |

Total: ~1130 LOC additive. Zero deletions of existing functionality.

### Outputs table (Q6 — comprehensive)

**Today** (BMAD-shaped, via existing `output/workflow-documents/`):

| File | Source state | Adopter use | Status post-ADR |
|---|---|---|---|
| `.semspec/plans/<slug>/plan.md` | Plan (goal/context/scope + summary refs) | BMAD users, audit, sponsor packs | **Keep.** BMAD compat (ADR-030). |
| `.semspec/plans/<slug>/requirements.md` | Plan.Requirements | BMAD users, code reviewers | **Keep.** BMAD compat. |
| `.semspec/plans/<slug>/architecture.md` | Plan.Architecture (incl. harness profiles per ADR-039) | Architects, ops | **Keep.** BMAD compat. |
| `.semspec/plans/<slug>/scenarios.md` | Requirements[].Scenarios | QA, testers | **Keep.** BMAD compat. |
| `.semspec/plans/<slug>/qa-summary.md` | qa-reviewer verdict | Stakeholders | **Keep.** BMAD compat. |
| `.semspec/plans/<slug>/run-summary.md` | execution-manager run rollup | Ops, sponsor packs | **Keep.** BMAD compat. |

**New** (OpenSpec-shaped, this ADR Move 3):

| File | Source state | Adopter use | Cadence |
|---|---|---|---|
| `.semspec/plans/<slug>/openspec/proposal.md` | Plan.goal + Plan.exploration.capabilities + Plan.exploration.open_questions | OpenSpec adopters, `openspec status`, sponsor packs | Per-mutation, 1s debounce |
| `.semspec/plans/<slug>/openspec/specs/<cap>/spec.md` | Requirements owned by capability + their scenarios + applies_to glob | `openspec validate` per capability | Per-mutation |
| `.semspec/plans/<slug>/openspec/design.md` | Plan.Architecture (decisions, harness_profiles, tradeoffs) | OpenSpec adopters | Per-mutation |
| `.semspec/plans/<slug>/openspec/tasks.md` | EXECUTION_STATES task DAG with live `- [ ]` ↔ `- [x]` checkbox state | `openspec apply` adopters, live progress surface | Live flip on every node verdict |
| `.semspec/plans/<slug>/openspec/.openspec.yaml` | static (schema: spec-driven) | OpenSpec CLI schema detection | Once at plan create |

**Coexistence rationale**: BMAD-shaped + OpenSpec-shaped both live as projections of the same Plan state. Cost is ~300 LOC of emitter code + negligible disk space. Different adopters use different surfaces. ADR-030 (BMAD methodology alignment) and this ADR (OpenSpec vocab alignment) are both load-bearing for different audiences. Neither is deprecating the other.

**Disambiguation tracking**: a future ADR can deprecate the BMAD output if telemetry shows zero adopter use. Today's signal: ADR-030 explicitly named adopter teams asking for BMAD artifacts. Keep them.

### What stays exactly the same

- Plan / PlanState enum / PLAN_STATES KV bucket
- EXECUTION_STATES KV bucket + execution-manager TDD pipeline
- Harness catalog (ADR-039) + qa.yml rendering
- Dev → structural-validator → reviewer → QA → autonomous recovery cascade
- Lessons learned system (ADR-033)
- Watch CLI (ADR-034), strict-parse discipline (ADR-035), wedge recovery (ADR-037)
- Existing BMAD-shaped output via `output/workflow-documents/`

### Persona prompt updates

Three changes in `configs/presets/bmad.json` — operator-tunable:

**Mary (analyst sub-phase)** — NEW persona block:

> You are Mary in analyst mode. You have just received the user's request. Your ONLY job is to identify the CAPABILITIES this change will introduce or modify.
>
> A capability is a NAMED UNIT of system behavior. Capability names are kebab-case (e.g., `user-auth`, `mavsdk-bootstrap`, `telemetry-stream`). Each capability you list will become its own specification file in the next sub-phase (`specs/<name>/spec.md`).
>
> Classify each capability as **new** (does not exist in the project's `openspec/specs/` directory) or **modified** (extends an existing spec). When in doubt, prefer **new**.
>
> Anti-pattern: listing documentation, READMEs, or coverage matrices as capabilities. If the user's request says "produce a coverage matrix" or "README documents tradeoffs," the documentation is a DERIVED ARTIFACT of whichever implementation capability it describes — its content attaches as additional scenarios or detail under that capability's spec, not as a standalone capability. A coverage matrix derived from a `mavsdk-plugins` capability belongs IN that capability's deliverables.
>
> Output ONLY the capability list (kebab-case name, new|modified lifecycle, 1-3 sentence description) + brief open questions about ambiguity. Do not propose files, scope, requirements, or implementation steps.

**Mary (planner sub-phase)** — UPDATED:

> You are Mary in planner mode. You have an exploration document with classified capabilities. Your job is to produce the plan's goal, context, and scope.
>
> Scope is DERIVED from capabilities. Every capability contributes file ownership to `scope.include`. If a capability's content is purely documentation (e.g., README updates), it contributes documentation files only.
>
> Anti-pattern: listing files in scope that are not owned by any capability. If the user's prompt names a file but no capability owns it, either flag a missing capability back to the analyst step or accept that the file is out of scope.

**John (requirement-generator)** — UPDATED:

> You are John, a product manager. Given the plan's exploration document, produce ONE Requirement per capability.
>
> Each requirement owns a focused set of files (`applies_to` glob ≤5 files). Each requirement uses SHALL or MUST normative language. Each requirement has 1-3 scenarios in GIVEN/WHEN/THEN format.
>
> If a single capability would require more than 5 files of ownership, flag `capability_too_broad` back to the analyst step rather than producing an overloaded requirement.
>
> Documentation content (READMEs, coverage matrices, tradeoff write-ups) attaches as additional scenarios under the implementation requirement it describes, NOT as its own separate requirement.

## Validation against the run #3 failure

Replay the OSH/MAVSDK prompt through the revised pipeline:

1. **Mary (analyst sub-phase)** reads the prompt. Per persona, she lists capabilities only:
   ```
   capabilities:
     - mavsdk-bootstrap (new): boot mavsdk_server, peer connection
     - telemetry-stream (new): CS API DataStream from MAVSDK telemetry
     - control-stream (new): CS API ControlStream for MAVSDK actions
     - raw-mavlink-fallback (new): generic MAVLink wire for plugin gaps
     - coverage-matrix-tooling (new): machine-checkable inventory (incl. README docs)
   open_questions:
     - Runtime introspection vs static analysis for coverage matrix?
   ```
   Persona forbids "documentation" as its own capability. README content folds into `coverage-matrix-tooling`. 5 capabilities. ≤2 KB. One LLM call.

2. **Mary (planner sub-phase)** consumes the exploration. Goal: same as today. Scope: derived from 5 capabilities — every Java file under `src/main/java/org/sensorhub/mavsdk/`, `build.gradle`, and the README owned by `coverage-matrix-tooling`.

3. **John (requirement-generator)** produces 5 Requirements (one per capability). README content attaches as scenarios under `coverage-matrix-tooling`.

4. **Plan-reviewer** validates: every capability has a Requirement ✓; no orphan files ✓; standards.json compliance ✓. The `capability_orphan` rule fires zero findings.

5. **Architecture-generator** runs. Harness catalog (ADR-039 — unchanged) consults graph entities and selects `mavlink.px4-sitl.mavsdk-smoke` + `mavlink.raw-mavlink-direct` based on the SKG's capability-to-profile mapping.

6. **OpenSpec emitter** (Move 3) writes `proposal.md` (capability list, new+modified), `specs/mavsdk-bootstrap/spec.md`, `specs/telemetry-stream/spec.md`, etc., `design.md`, `tasks.md` (live checkboxes). All from existing Plan state.

7. **Execution-manager + dev pipeline** (unchanged) works through 5 Requirements. Each is right-sized (≤5 files). Dev produces actual Java implementation.

The failure mode — "9 approved verdicts, zero MAVSDK driver code" — is structurally impossible:
- Analyst persona forbids documentation as standalone capability
- Plan-reviewer `capability_orphan` rule rejects orphan-capability plans
- requirement-generator "1 requirement per capability" rule prevents mega-bundling
- Each Requirement is bounded to ≤5 files so it fits within TDD cycle budget

## Migration

Four PRs sequenced. Each independently shippable. Move 4 (folded ADR-038) can sequence first, last, or interleaved.

### PR 1: Analyst sub-phase + capability data model (~3 days)

- Extend `Plan` with `Exploration` + `Capability` types (OpenSpec-aligned: `new` | `modified` lifecycle)
- Wire predicates into `vocabulary/spec/`
- Update `planner` component to dispatch analyst → planner serially
- New persona prompt block for Mary's analyst mode in `bmad.json`
- Plan-manager writes `Plan.exploration` to PLAN_STATES before requirement-generator dispatches
- Unit tests against OSH/MAVSDK prompt: expect ≥5 capabilities, no doc-as-capability
- One mock e2e fixture asserting `Plan.exploration` is populated before requirement-generator dispatches

### PR 2: Capability-first requirement-generator + plan-reviewer rule (~2 days)

- Update requirement-generator persona prompt
- Add `capability_orphan` rule helper in `processor/plan-reviewer/`
- Typed finding shape + targeted-regen wiring
- E2E mock fixture: produce a "doc-as-capability" plan, assert plan-reviewer rejects with `capability_orphan` finding
- Real-LLM smoke test (gemini @ easy tier) — assert capability_orphan never fires

### PR 3: OpenSpec emitter (~2 days)

- New `output/workflow-documents/openspec/` subpackage with 4 emitter files (proposal, spec, design, tasks)
- `.openspec.yaml` metadata writer (schema = spec-driven)
- Hook into PLAN_STATES / EXECUTION_STATES mutation events (1s debounce)
- Live tasks.md checkbox flipping on every node verdict
- Unit tests: synthetic Plan produces markdown that passes `openspec validate`
- E2E assertion: after any e2e completion, the change directory passes `openspec validate`

### PR 4 (folded ADR-038): OpenSpec inbound import (~5 days)

- project-manager `/detect` extension for OpenSpec conventional paths
- Graph-readiness poll for semsource indexing
- `workflow/specimport/structural_check.go` (~50 LOC)
- `workflow/specimport/translator.go` (~300 LOC) — graph entities → PLAN_STATES + Plan.exploration hydration
- `POST /plan-manager/plans/from-spec` HTTP handler
- UI: spec picker + import preview + verdict-handling flow
- `docs/import-from-spec.md` user-facing doc
- E2E extending existing `openspec-ingest` scenario through import → `ready_for_execution`
- Round-trip e2e: import a spec, run pipeline, emit OpenSpec artifacts, diff inbound vs outbound (only differences should be semspec-side mutations like reviewed scenarios)

**Total: ~12 working days.** PRs 1+2 are the run #3 fix. PR 3 is OpenSpec emission. PR 4 is OpenSpec import. Independently valuable.

## Open questions (remaining)

Resolved across rev 2 → rev 3 → rev 4:
- (1) Align fully with OpenSpec capability vocab — done in rev 3.
- (2) KV write for `Plan.exploration` — confirmed.
- (3) Per-mutation 1s debounce emitter — confirmed.
- (4) Live tasks.md checkbox flipping — confirmed.
- (5) spec-driven schema as default — confirmed.
- (6) Keep both BMAD + OpenSpec outputs — confirmed via outputs table.
- (7) ADR-038 folded in as Move 4 — done.
- (rev 3 Q1) Round-trip fidelity tolerance — confirmed: scenarios refined by reviewer are expected to differ; capability names + `applies_to` globs must match the source via `external.spec` triples; design.md may grow harness_profiles that the source didn't have (since ADR-039 selects them).
- (rev 3 Q2) `Plan.exploration.open_questions` resolution path — confirmed: planner sub-phase makes a reasonable assumption and flags it in context for autonomous mode; UI surfaces the open questions as low-severity findings when operator runs in human-review mode.
- (rev 3 Q3) `Capability.depends_on` enforcement — confirmed in rev 4 as a HARD CONSTRAINT (Move 2 — three new plan-reviewer rules). No warn-fallback.

No open questions remain at the architecture level. Implementation-level details surface during PR review.

## Consequences

### Positive

- **Run #3's failure mode becomes structurally impossible** via persona + plan-reviewer rules.
- **Smaller-model viability improves**: analyst sub-phase has narrow input/output; planner sub-phase gets cleaner input; both individually smaller than today's single planner call.
- **OpenSpec adopters get artifacts they recognize without internal model change.** Emitter is read-only against our state. Adopter tooling (`openspec status`, `openspec validate`) runs against the projection.
- **Full bidirectional OpenSpec compat in one ADR.** Move 4 (folded ADR-038) ships the inbound side; Move 3 ships outbound. Adopters can bring their own specs, or have semspec emit them, or both.
- **Capability vocabulary matches OpenSpec exactly** (kebab-case names, new/modified lifecycle). Adopter mental model is portable.
- **SKG-authoritative harness selection (ADR-039) unchanged.** The substrate that makes our differentiator possible is untouched.
- **No new entity types, no new KV bucket, no new state machine, no demolitions.** Plan model survives. Components evolve.
- **BMAD-shaped outputs continue.** ADR-030 commitments preserved.

### Negative

- **Two LLM calls in planner phase instead of one.** Roughly doubles wallclock and token cost. Mitigated by the second call being cheaper (lighter input). Net: ~60-90s + ~5-10K tokens per plan. Acceptable.
- **Persona prompt drift management.** Three personas got rewrites (Mary-analyst, Mary-planner, John). Calibration needed against real prompts. Run gemini @ easy + @ hard regression after PR 2 lands.
- **OpenSpec emitter + importer = new maintenance surface.** OpenSpec format may evolve; we track upstream. Mitigated by semver discipline upstream and small emitter size (~300 LOC outbound + ~350 LOC inbound).
- **Duplicate output surfaces (BMAD + OpenSpec).** Some content (plan.md vs proposal.md, requirements.md vs specs/*/spec.md) appears in both shapes. Cost: emitter code + disk space (both small). Track adopter feedback for future deprecation candidates.

### Neutral

- **No internal vocabulary breakage.** Plan/Requirement/Architecture/Scenario stay. Capability is a new additive concept.
- **No API breakage.** plan-manager HTTP surface unchanged (Plan response shape gains optional `exploration` field).
- **No e2e fixture demolition.** Existing fixtures continue working; new fixtures added.
- **ADR-038 lifecycle.** Marked superseded by ADR-040 (folded in). Memory note + index updates accordingly.

## Decision is

**Accept this ADR and proceed with the four-PR migration. PR 1 + 2 unblock the run #3 failure mode. PR 3 ships OpenSpec emission. PR 4 ships OpenSpec import (folded from ADR-038). Sequence is flexible; PRs 1+2 are highest leverage for upcoming runs.**

Required confirmation before code lands:

1. Operator (Coby) signs off on the three persona prompt rewrites (Mary-analyst, Mary-planner, John).
2. Operator confirms the 12-day migration is acceptable and which PR sequences first if other priorities collide.

This ADR explicitly preserves:
- The SKG as the authoritative state substrate
- The Plan / PLAN_STATES model and the harness catalog (ADR-039)
- The dev pipeline + autonomous QA recovery cascade
- BMAD-shaped output via `output/workflow-documents/` (ADR-030)
- ADR-038's design intent (now folded in as Move 4 to ensure inbound + outbound + vocabulary land coherently)

The operator framing — **"we hydrate OpenSpec from our flow; prose is a side effect"** — is the load-bearing constraint this ADR is designed to honor.
