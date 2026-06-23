# ADR-051: Per-Phase Planning Review Gates — structural-early + semantic-where-proven

**Status:** Accepted — all slices implemented (2026-06-22)
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

- The plan-reviewer's deterministic rule functions (`mergeCapabilityFindings`,
  `mergeArchitectureFindings`, `mergeStoryFindings`, `mergeScenarioTagFindings`)
  run only at R1/R2. **Correction (verified during implementation — the original
  draft overstated this as "all 29 rules fire late at R2"):** the single-writer
  **mutation handlers** and the **architecture-generator** already enforce most of
  these rules early, so R2 is largely a backstop, not the primary gate. Verified
  per-phase coverage:
  - **Requirements** — `handleRequirementsMutation` enforces capability-orphan,
    requirement-orphan, requirement DAG, and file-ownership early (rejection
    bounces to req-gen for retry). `capability.orphan.docs_only` is intentionally
    dead — `FindDocsOnlyCapabilities` is a no-op since ADR-043 Move 4 removed
    `Requirement.FilesOwned`; the concern moved to the architecture
    (`component_implementation_files_doc_only`) and story
    (`docs_only_files_owned`) layers. **No genuine requirements gap.**
  - **Architecture** — `validateGeneratedArchitecture` enforces impl-files,
    capability coverage, upstream, and harness early; **`scope.include` ownership
    was the gap, closed by Slice 1.** `component_stub_risk` is intrinsically late
    (needs scenarios as TDD forcing functions).
  - **Stories** — `ValidateStories` (at `handleStoriesMutation`) enforces
    per-story shape, docs-only `files_owned`, tasks, cross-story DAG, and
    cross-story file-ownership early. **The 5 cross-entity rules that need plan
    context — component resolution, requirement-id resolution,
    files-owned-outside-component, contract-scope coverage, topology — are the
    genuine remaining gap → Slice 2b.**
  - **Scenarios** — R2 is their own phase; correct.
- The LLM semantic review covers only **2 of 5** artifacts: draft (R1) and
  scenarios (R2). **Architecture and requirements get no LLM review at all** — the
  larger remaining value (Slices 3–4).

Net: the structural-early work was never "relocate 29 late rules" — ~22 already
fire early at the mutation handlers / arch-gen validator. The real structural gaps
were **`scope.include` (Slice 1, done)** and **story cross-entity rules (Slice
2b)**. Requirements (Slice 2a) is a no-op. Slices 3–4 (semantic gates) are the
larger remaining value.

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
   `validateGeneratedArchitecture`. **DONE.**
2. Requirements + stories structural-early. **2a (requirements) = NO-OP** (already
   early-enforced at `handleRequirementsMutation`). **2b (stories cross-entity) =
   DONE** (`workflow.ValidateStoriesAgainstPlan` wired into story-preparer
   pre-publish).
3. **R-arch semantic round. DONE** (states + round wiring; plan-reviewer is the
   sole claimant of `architecture_generated`, story-preparer claims
   `architecture_reviewed`).
4. **R-req semantic round. DONE** (states + round wiring; plan-reviewer is the
   sole claimant of `requirements_generated`, architecture-generator claims
   `requirements_reviewed`).
5. **Cross-cutting: stale-metadata refresh. DONE** (`refreshApprovedReviewMetadata`
   on every advance-past-review mutation).
6. **Made the per-phase reviews mandatory. DONE** (removed the config flags /
   env vars / dual-watch — R-req and R-arch are unconditional pipeline stages).

**Implementation note — the per-phase rounds are MANDATORY pipeline stages, like
R1 and R2 (no flag, no toggle).** Each `*_generated` state has exactly one
claimant — the plan-reviewer — which claims it into the `reviewing_*` state; the
downstream generator (architecture-generator for R-req, story-preparer for
R-arch) claims the post-review `*_reviewed` state instead of the `*_generated`
state. Single claimant per state means there is no race and no dual-watch — the
earlier flag-gated/dual-watch design was removed. Both rounds reuse the proven
`claimAndDispatchReview` → `preflight → LLM → NormalizeVerdict` shape; the
deterministic preflight at a generated-but-pre-stories phase must NOT enforce
scope.create ownership (gated on `len(plan.Stories) > 0`) — scope.create is
draft-partial until `ensureScopeCreateCoversStories`.

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
