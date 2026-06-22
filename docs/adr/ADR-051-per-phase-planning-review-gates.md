# ADR-051: Per-Phase Planning Review Gates — structural-early + semantic-where-proven

**Status:** Proposed (2026-06-22)
**Builds on:** ADR-029 (plan completeness review + retry), ADR-040 (OpenSpec-aligned
planning pipeline), ADR-043 (architect emits implementation files), ADR-044
(capability/story coverage), ADR-049 (component ownership topology)
**Amends:** ADR-029 by moving its "structural completeness" layer from a single
late R2 sweep to a per-phase gate, and by extending the LLM semantic gate to the
phases where a judgment-class defect is proven (requirements, architecture).

## Context

The original intent (ADR-029: *"each round validates SOP compliance **and**
structural completeness"*; ADR-040: *"Layer 1: structural pre-check … Layer 2: …
the semantic gate"*) was a two-layer review — deterministic structural rules plus
an LLM semantic gate — at each planning phase. The implementation drifted:

- The per-phase structural functions (`mergeCapabilityFindings`,
  `mergeArchitectureFindings`, `mergeStoryFindings`, `mergeScenarioTagFindings`)
  are called **only** from `mergeDeterministicFindings`, which runs **only** at R1
  (drafted) and R2 (scenarios_generated). At R1 there is no downstream data, so in
  practice **all 29 structural rules fire at one late point — R2**, after every
  artifact (requirements → architecture → stories → scenarios) already exists.
- The LLM semantic review covers only **2 of 5** artifacts: draft (R1) and
  scenarios (R2). Requirements get only an indirect mention in R2's
  scenario-shaped prompt; **architecture and stories get no LLM review at all.**

Consequence, observed on the mavlink-hard gemini run (plan `2689024ccba9`): the
architect orphaned two `scope.include` files (`build.gradle`, `README.md`). The
defect was determinable at the architecture phase but was only caught at R2,
triggering a full architecture + stories + scenarios re-generation to fix a
mechanical defect. The gate worked; its **timing** wasted tokens.

(The same run also exposed a stale-review-metadata observability bug — see
Decision §3.)

## Decision

Adopt **per-phase structural-early + semantic-where-proven**: every LLM-authored
planning artifact is gated immediately after generation, before the next phase
consumes it.

### 1. Structural-early (timing; no new LLM cost)

Run each phase's deterministic rules at that phase's **generation-acceptance**
hook, retrying the generator loop in place on an error finding — the pattern
`architecture-generator/component.go validateGeneratedArchitecture` already uses.
The rules already exist and are phase-tagged; this is a timing change, not new
logic. Data-availability constraints (verified):

| Gate (at) | Rules to run early | Stays at R2 (why) |
|---|---|---|
| requirements_generated | `capability.*` (5) | — |
| architecture_generated | the `architecture.*` set **except** `component_stub_risk`; `scoped_file_unowned` **scope.include subset only** | `component_stub_risk` (needs scenarios); `scoped_file_unowned` for **scope.create** (draft-partial here — reconciled at stories) |
| stories_generated | `story.*` (9) + `scoped_file_unowned` for **scope.create** (final after `ensureScopeCreateCoversStories`) | `component_stub_risk` (still no scenarios) |
| scenarios_generated (R2) | `scenario.*` (6) + `component_stub_risk` + full-sweep **backstop** | — |

R2 keeps the full deterministic sweep as a backstop; with early gates it should
rarely fire.

### 2. Semantic gate (requirements + architecture only)

Add an adversarial LLM review at `requirements_generated` (R-req) and
`architecture_generated` (R-arch), each mirroring the proven
`preflight → LLM → NormalizeVerdict` shape. Reject → revise that phase only
(re-entry is already minimal in `determineR2ReentryPoint`); approve → advance.

**Stories get no LLM review** — see "Proof" below; the decomposition judgment is
upstream at architecture, and story output is a mostly-mechanical sharding of it
(ADR-044: one component per story, `FilesOwned` derived from
`component.ImplementationFiles`, `DependsOn` SYSTEM-derived). Revisit only if a
story-specific judgment defect is demonstrated.

### 3. Refresh stale review metadata on advance

`review_verdict` / `review_summary` / `review_findings` / `reviewed_at` currently
persist the last rejection even after the plan advances (the mavlink run showed
`needs_changes` + `reviewed_at` frozen at R1 on a plan that R2-approved). Advance
mutations must overwrite these with the operative verdict.

### Proof that "both" is right per phase (not assumed by symmetry)

A phase needs both layers only if it has a **mechanical** defect class
(→ structural) *and* a **judgment** defect class (→ semantic), each with evidence:

| Phase | Mechanical (structural) | Judgment (semantic) | Needs |
|---|---|---|---|
| Requirements | `capability.orphan` (ADR-040 run#3 "docs-as-capability") | over-bundling / goal-coverage (ADR-040 §35 "mega-bundled req that never executed"); this run `acceptance_criteria=0` | both |
| Architecture | `scoped_file_unowned` (this run's build.gradle orphan) | facade/clean-room & boundary placement — `component_stub_risk`'s own history (ADR-049: the file-count proxy "was backwards"); #237 control-in-telemetry | both |
| Stories | `story.contract_scope_uncovered` (2026-06-13 README wedge) | **none proven** — judgment is upstream at architecture; stories are mechanical sharding | **structural only** |
| Draft / Scenarios | — | already R1 / R2 | unchanged |

## Consequences

- **Move 1 is free** (deterministic) and converts late detection into early,
  removing downstream re-gen of stories/scenarios for upstream defects.
- **Move 2 adds 2 LLM calls/plan** (R-req, R-arch), each of which *prevents* a
  larger downstream re-gen when it rejects — net token-negative on any plan that
  would otherwise be rejected downstream.
- New in-progress states `reviewing_requirements`, `reviewing_architecture` must be
  added to every transition-validity table and mock fixture (or KV-watch
  self-triggers reject the claim). This completes the "co-fire interim" the code
  flags at `plan-reviewer/component.go` (the deferred P4-C5 work).

## Implementation slices (smallest blast radius first)

1. **Arch structural-early — scope.include ownership** (proven by this run; bundled
   with this ADR). Extract `IsConcreteScopedFile` to `workflow` (single
   concreteness definition), add `workflow.UnownedScopedIncludeFiles`, wire into
   `validateGeneratedArchitecture`.
2. Requirements + stories structural-early (move `capability.*` / `story.*` +
   scope.create ownership to their generators).
3. R-arch semantic round.
4. R-req semantic round.
5. Cross-cutting: stale-metadata refresh.

## Validation (mock ladder — no paid runs)

Per-slice deterministic scenarios, e.g.: `arch-orphan-include` (orphaned
scope.include caught at arch-gen, no stories/scenarios generated before fix);
`directory-include-exempt` (negative control — `scope.include: ["src/"]` must not
trip); `arch-semantic-reject`; `stories-no-semantic` (structurally-valid partition
advances with no LLM story call); `advance-clears-stale-verdict`.

## Risks / footguns (verified during investigation)

- Do **not** enforce scope.create ownership at arch-gen (draft-partial → false
  positives); include at arch-gen, create at stories-gen.
- Do **not** assert "every scope file → exactly one component" — companion test
  files are owned implicitly via `ExpandFileScopeWithCompanionTests`; directories
  and globs are exempt (`IsConcreteScopedFile`); `do_not_touch` is exempt; shared
  ownership across components is legal and load-bearing for the scheduler.
- `component_stub_risk` must stay at R2 (needs scenarios as TDD forcing functions).
- `NormalizeVerdict` stays the single verdict↔findings reconcile point.
- Stories get structural-early only; an LLM story review is out of scope until a
  story-specific judgment defect is demonstrated.
