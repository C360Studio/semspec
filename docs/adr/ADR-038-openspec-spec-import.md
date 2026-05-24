# ADR-038: OpenSpec Spec Import — Brownfield Plan Bootstrap

**Status:** Proposed (2026-05-24)
**Deciders:** Coby, Claude
**Related:** ADR-029 (plan completeness + retry — the review loop this
ADR reuses for imported plans), ADR-030 (BMAD alignment — the framing
this ADR extends to explain why we don't import BMAD prose), ADR-037
(wedge recovery — layer 3 of the three-layer defense), workflow's
existing PlanDecision cascade (delta-spec target).

## Context

A brownfield-adoption ask surfaced from an early-feedback team:
> "We use both OpenSpec and BMAD. Can semspec READ those artifacts
> (not just write them) to update graph + runtime state?"

This is a legitimate brownfield concern. Teams arrive with existing
authoring conventions; if semspec can absorb their artifacts as
inputs, the adoption ramp shortens substantially. The question forces
a clean architectural answer because OpenSpec and BMAD differ
structurally — they need different decisions.

### Verified prior art (no rumor, grepped 2026-05-24)

- **OpenSpec ingest to graph: EXISTS.** `vocabulary/spec/` defines
  `spec.specification`, `spec.requirement`, `spec.scenario`,
  `spec.delta`, `spec.delta-operation` predicates. semsource indexes
  OpenSpec markdown end-to-end including normatives, Given/When/Then,
  and ADDED/MODIFIED/REMOVED operations. Validated by
  `test/e2e/scenarios/openspec_ingest.go` (7 stages green).
- **Consumers of `spec.*` entities: zero.** Grepped processor/,
  tools/, every generator — nothing reads these predicates.
  OpenSpec entities are dark data in the graph today.
- **BMAD ingest: does not exist.** No `vocabulary/bmad/`. Only Go
  reference is a misleading comment in
  `processor/plan-manager/http_artifacts.go`.
- **Artifact writes (`plan.md`, `architecture.md`, `requirements.md`,
  `scenarios.md`, `qa-summary.md`, `run-summary.md`): BMAD-shaped,
  output via `output/workflow-documents/`.**

So the conversation is two-sided:
1. **OpenSpec READ**: 80% paid for already; what's missing is the
   bridge from graph entities to runtime state.
2. **BMAD READ**: structurally different (methodology not format);
   addressed in Section "BMAD scope" below.

### Why semsource's ingest doesn't already solve the runtime-state
problem

semsource publishes entities to `ENTITY_STATES` for graph-side query.
Nothing in semspec consumes those entities to update `PLAN_STATES` or
`EXECUTION_STATES` (the runtime KV the planner/execution pipeline owns).
Three sub-gaps stack:

1. **Dark-data gap.** Entities exist but no production consumer.
2. **Bridge gap.** `spec.requirement` (graph entity, OpenSpec's
   identity scheme) ≠ `Requirement` in `PLAN_STATES` (our domain
   type with 6-part entity IDs). Identity reconciliation is
   load-bearing.
3. **Lifecycle gap.** No rule fires when an external doc changes
   ("requirement X removed from auth.md → cascade to in-flight plan
   referencing it"). This is the conflict-resolution quagmire that
   makes "live bidirectional sync" a multi-quarter commitment.

The dark-data and bridge gaps are tractable. The lifecycle gap is
deferred (see Out-of-scope below).

### Why "just put OpenSpec in the prompt" isn't enough

The cheapest path is letting the planner agent read imported specs
as text and generate a plan. Zero new code. But:

- No identity preservation. Planner can rewrite, merge, or drop
  spec content silently; nothing maps the resulting Requirements
  back to source spec entities.
- No guaranteed 1:1 requirement count. Whatever the planner decides.
- Operator can't tell post-hoc whether a Requirement came from
  their spec or from the planner's invention.

For brownfield adoption, **provenance matters**. The team imported
their auth.md because they wanted *that* spec executed, not "a plan
inspired by that spec."

### Why a separate `spec-importer` adapter component is wrong

Early sketch proposed a new component that translated `spec.*` →
`PLAN_STATES` and landed the plan at `status=scenarios_reviewed`,
skipping our entire review chain. That shape is wrong because:

- **Bypasses standards gates.** Imported requirements could violate
  `standards.json`'s `must`-severity rules; skipping plan-reviewer
  means the violation surfaces only at execution time, after the
  reviewer has rejected agent-produced work that was operating on
  bad input.
- **Bypasses our value-add for brownfield.** The honest sales pitch
  for import is "we'll execute and review what you've authored."
  Skipping the reviewer chain abandons "review," leaving only
  "execute." That's a worse product than even doing nothing.
- **Duplicates infrastructure.** plan-reviewer already exists and
  already runs standards-aware semantic checks. A bypass path is
  net negative on system surface area.

## Decision

Adopt a **three-layer defense** for OpenSpec spec import,
implemented by extending **project-manager** to be bounded-graph-aware
and reusing the **existing review chain**.

### Architectural commitments

1. **project-manager becomes bounded-graph-aware.** `/detect` is
   extended to scan for OpenSpec conventional paths
   (`sources/openspec/`, `specs/`, `.openspec/`). After detection,
   project-manager polls `ENTITY_STATES` for `source.type=openspec`
   entities matching the detected file inventory, presenting them in
   the brownfield setup UI alongside detected stack/standards.
2. **Three-layer defense** for imported plans:

   | Layer | Cost | Catches | Component |
   |---|---|---|---|
   | 1. Structural pre-check | None (no LLM) | Missing scenarios for requirements; malformed frontmatter; broken delta refs | New `workflow/specimport/structural_check.go` helper, ~50 LOC, schema-only |
   | 2. Semantic review | One plan-reviewer call (~10–20s) | Standards violations; scenarios that don't cover their requirement's acceptance criteria; scope/context contradictions | **`plan-reviewer` already exists** — verdict pipes into existing `reviewed`/`revision_needed` plan status |
   | 3. Wedge recovery | LLM call only on actual execution wedge | Specs technically valid but operationally undecomposable | **`recovery-agent` already exists (ADR-037)** — can split requirements, escalate to human |

3. **Strict pass-through for generators.** Imported plans bypass
   planner + architecture-generator + requirement-generator +
   scenario-generator for the imported content. The reviewers
   (plan-reviewer, scenario-reviewer) DO run.
4. **Delta specs map to PlanDecisions.** An OpenSpec delta spec
   (`session-timeout.md` modifying `auth.md`) is structurally a
   PlanDecision in semspec vocabulary. Importing a delta against an
   already-imported source-of-truth = translate delta → PlanDecision
   payload, POST against the existing plan, let the existing
   PlanDecision review + cascade pipeline handle the rest. Zero new
   delta-handling code.
5. **Identity bridge via triples, no shared ID space.** Our
   Requirement entity ID is minted fresh in our 6-part scheme; the
   source spec entity ID is preserved verbatim as a triple predicate
   `external_ref.spec_requirement` on our Requirement. Reverse
   lookup ("what plan came from auth.md?") = graph query for
   Requirements whose `external_ref.spec_requirement` matches. Both
   ID schemes evolve independently.

### Pipeline shape (imported plan)

```
project-manager /detect → OpenSpec files found → wait for semsource
                                                  to finish indexing
                                                  (poll graph)
                              │
                              ▼
              UI: "Seed plan from auth.md?" (operator chooses)
                              │
                              ▼
              POST /plan-manager/plans/from-spec
                              │
                              ▼
              Layer 1: structural pre-check (instant)
                pass → proceed
                fail → operator chooses: auto-fill / cancel / import-as-is
                              │
                              ▼
              Translation: graph entities → PLAN_STATES domain
                Plan + Requirements + Scenarios + external_ref triples
                              │
                              ▼
              Plan enters at status=drafted (NOT scenarios_reviewed)
                              │
                              ▼
              Layer 2: plan-reviewer (existing LLM gate)
                approved        → ready_for_execution
                revision_needed → block; show findings; operator picks:
                                  (a) edit spec + re-import
                                  (b) accept verdict + mutate spec
                                  (c) override with skip_review=true
                              │
                              ▼
              Standard pipeline: implementing → reviewing_qa → complete
                                                       ▲
              Layer 3: recovery-agent catches operational wedges ─┘
              (ADR-037, unchanged)
```

### What's deliberately out of scope

- **Live sync.** Editing `auth.md` after plan creation does not
  mutate plan state. Re-import = new plan. The conflict-resolution
  semantics that make live sync work are a multi-quarter commitment;
  not justified by operator demand observed today.
- **Reverse rendering.** Plan mutations do NOT regenerate
  `auth.md`. semspec writes its own `requirements.md` to
  `.semspec/plans/<slug>/` (existing behavior); the source spec is
  not touched.
- **Drift detection.** No reconciliation between current source spec
  and prior import state.
- **BMAD ingest.** See BMAD scope section below.
- **New LLM gate.** No "spec-import-reviewer" component. plan-reviewer
  is the gate. Reusing it dodges calibration drift between "reviews
  imports" and "reviews cold-start plans," and avoids growing the
  reviewer surface area.

### BMAD scope

BMAD ingest is **not** included in this ADR. Per ADR-030, semspec
adopted BMAD as a *methodology*: BMAD personas (Mary, Winston, Bob)
attach as configurable display labels on our roles; BMAD's phase
model (Analysis → Planning → Solutioning → Implementation) maps to
our planner → architecture-generator → requirement-generator →
execution pipeline; BMAD's artifact vocabulary (plan, architecture,
requirements, scenarios) is what `output/workflow-documents/`
already writes.

The user-facing framing:

> Semspec is BMAD-aligned methodologically. Your BMAD users will
> recognize our outputs because they ARE BMAD artifacts (generated
> by our agents instead of authored by humans). One deliberate
> divergence: our agents have role-scoped lessons-learned that
> BMAD's static persona files can't express. Treat our personas as
> opinionated and methodology-aware, not as readers of upstream BMAD
> prose.

The structural reason: OpenSpec is a wire format with deterministic
shape — semsource has a parser, semspec has a vocabulary, round-trip
is concrete. BMAD is a methodology with multiple file shapes (PRD,
story files, architecture sections, brief, etc.) each with their own
conventions. Even a partial BMAD ingest ("read PRD only") covers
~20% of "BMAD support" but takes ~80% of the work — and is redundant
with what our agents already do.

**Escape hatch:** if a customer specifically demands PRD ingest, the
PRD parser slots into the same `workflow/specimport/` translator
shape. Defer until that conversation actually happens.

## Consequences

### Wins

- **Brownfield UX matches existing pattern.** OpenSpec detection
  rides the existing detect/init flow operators already understand
  from standards/checklist.
- **No new LLM components.** plan-reviewer is the gate; recovery-agent
  is the safety net. Both proved out on cold-start plans.
- **Standards enforcement free.** Imported specs that violate
  `standards.json` get caught by plan-reviewer before any execution
  tokens burn. Operator gets actionable findings ("Requirement R3
  violates eng-test-coverage") at import time, not 90 minutes into
  a wedged execution.
- **Delta specs free.** Map 1:1 to PlanDecision; reuses entire
  PlanDecision review + cascade pipeline.
- **semsource stays the only parser.** No duplicate parsing logic;
  no risk of two parsers disagreeing.
- **Identity bridge is forward-compatible.** `external_ref` triples
  make any future live-sync work tractable (reverse lookup already
  exists in the graph).
- **Easy to remove if users don't adopt.** ~700 LOC across 8 files,
  all in clearly bounded paths.

### Costs

- **Calibration unknown for plan-reviewer on external prose.**
  plan-reviewer was implicitly tuned on our planner's output style.
  External authors' prose may produce systematically different
  verdicts. Will know after N≥5 real imports; remediation is prompt
  calibration, not architecture.
- **"Waiting for graph" UX state.** project-manager has to poll
  ENTITY_STATES for semsource to finish indexing detected specs.
  Small spinner pattern ("3 of 7 specs indexed") is acceptable but
  is a new UX state to design and test.
- **No live sync sets adoption expectations.** Teams that want
  bidirectional sync will be disappointed. Mitigation: doc this
  explicitly in import-from-spec.md so the trade is up front.
- **Reviewer override path adds operator load.** When plan-reviewer
  says `revision_needed`, the operator has three choices to make.
  Some teams may default to `skip_review: true` to avoid the
  friction, leaking violations into execution. Mitigation: telemetry
  on `skip_review` usage feeds back into calibration work.

### Risks

- **External-prose verdict drift (likely).** plan-reviewer's signal
  may be noisier on imports than on cold-start plans. Acceptable
  during Phase 1; mitigated by calibration in Phase 2.
- **Operator confusion about what runs vs what's skipped (medium).**
  Imported plans skip planner/architecture-generator/req-gen/sce-gen.
  UI must clearly indicate which phases ran vs which were sourced
  from spec. Trajectory view already shows per-loop activity, so
  imported plans simply show fewer planning loops.
- **Delta-spec fidelity loss (low).** OpenSpec deltas have
  per-requirement ADDED/MODIFIED/REMOVED granularity; PlanDecision
  is coarser. Some complex deltas may need multiple PlanDecisions or
  may lose nuance. Known limitation; track via operator feedback.

## Phased rollout

**Phase 1** (this ADR — implement on demand):

- project-manager `/detect` extended to find OpenSpec files +
  graph-readiness poll
- structural pre-check helper (`workflow/specimport/structural_check.go`)
- translator (`workflow/specimport/translator.go`) with delta-spec →
  PlanDecision mapping
- `POST /plan-manager/plans/from-spec` HTTP handler
- UI: spec picker + import preview + verdict-handling flow
- `docs/import-from-spec.md` user-facing doc
- E2E test extending the existing `openspec-ingest` scenario to drive
  through import and reach `ready_for_execution`

**Phase 2** (gated on Phase 1 operator validation):

- plan-reviewer calibration based on observed verdict drift on
  external prose
- Tuning of structural-check completeness thresholds (what counts
  as "too incomplete to import")
- Telemetry on `skip_review` override usage

**Phase 3** (gated on actual operator demand, not assumed):

- Optional: re-import-as-delta workflow (re-run a spec import and
  produce PlanDecisions reflecting changes vs prior import)
- Optional: BMAD PRD parser if a specific customer requests it
- Optional: live sync / reverse rendering / drift detection if Phase
  1+2 telemetry shows operators want it

## Alternatives considered

### A. Separate `spec-importer` adapter component

A dedicated component reads `spec.*` from graph, writes
`PLAN_STATES`, lands plan at `status=scenarios_reviewed`, skipping
the review chain.

**Rejected.** Bypasses standards gates (Section "Why a separate
adapter is wrong"); the import value proposition collapses to
"execute" without "review."

### B. Put OpenSpec content in the planner's prompt

Planner reads imported specs as text and generates a plan.

**Rejected.** No identity preservation, no guaranteed 1:1
requirement count, planner can silently rewrite spec content.
Provenance matters for brownfield adoption.

### C. Plumb spec entities through requirement-generator

Add spec-aware mode to requirement-generator so it knows to skip
LLM generation when spec data is present.

**Rejected.** Couples a generator to a specific source format and
complicates its prompt logic. Cleaner to translate upstream and
skip the generator for imported content.

### D. Standards-as-imports model

Treat OpenSpec specs like standards.json — operator-authored,
project-manager owns the read, no graph involvement.

**Rejected.** Loses semsource's parsing investment and removes the
spec from the graph where future agents could query it. Also
duplicates the parsing work semsource already does. Standards are
operator *config*; OpenSpec is operator *content* — different
material.

### E. Tier C live-sync from the start

Build bidirectional sync as the primary offering.

**Rejected.** Conflict-resolution semantics (file says X, KV says
Y, who wins?) are a multi-quarter design problem we'd ship
half-built. Tier B (this ADR) is the experiment that tells us
whether Tier C is even necessary based on observed operator
behavior.

### F. BMAD ingest as primary feature

Build BMAD parsers (PRD, story files, architecture sections)
alongside OpenSpec.

**Rejected.** BMAD is methodology, not format — we've already
adopted the methodology per ADR-030. Ingesting BMAD prose is
reverse-engineering our own agents' opinions. Different problem
shape; not worth the parser-per-artifact lift.

## Open questions

1. **Conventional path enumeration.** Which OpenSpec paths does
   `/detect` scan? `sources/openspec/specs/`, `specs/`,
   `.openspec/specs/`, root-level `spec.md`? Tracked for Phase 1
   implementation; default to OpenSpec's documented conventions.
2. **Structural-check strictness.** What counts as "too incomplete
   to import"? Probably: req without scenarios = warn (operator can
   import-as-is); req with no normatives = error (auto-fill via
   scenario-gen, cancel, or override). Calibrate from real specs.
3. **Re-import semantics.** If user re-imports the same `auth.md`
   after edits, what happens? Default: new plan. Open question:
   surface a "create as delta against existing plan" option for
   operators who want it.
4. **plan-reviewer prompt awareness.** Should plan-reviewer's prompt
   be made explicitly aware that the content it's reviewing was
   imported (to set expectations about style/prose)? Default: no
   change; let the calibration data tell us.
5. **`skip_review` audit logging.** When operator overrides
   plan-reviewer verdict, telemetry should capture (a) the verdict
   that was overridden, (b) the spec identity, (c) the override
   reason. Phase 1 logs to structured event; Phase 2 may surface
   in UI as a "override history" view.

## References

- ADR-029 (plan completeness + retry) — the review loop reused here
- ADR-030 (BMAD alignment) — the framing this ADR extends for
  declining BMAD ingest
- ADR-037 (wedge recovery) — layer 3 of the three-layer defense
- `vocabulary/spec/` — OpenSpec predicate definitions
- `test/e2e/scenarios/openspec_ingest.go` — proves end-to-end
  OpenSpec → graph ingest works today
- `output/workflow-documents/` — the existing BMAD-shaped artifact
  writer
- Memory: `project_openspec_bmad_import_research_2026_05_24.md` —
  full research log + alternative-evaluation history
