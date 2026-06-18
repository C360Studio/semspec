# ADR-050: Contract Authority Over BMAD/OpenSpec Handoffs

**Status:** Proposed (2026-06-17)
**Builds on:** ADR-040 (OpenSpec-aligned planning pipeline), ADR-044
(capability/story coverage), ADR-049 (component ownership topology)
**Amends:** ADR-040 by naming the authoritative contract layer that governs
BMAD/OpenSpec projections and recovery decisions.

## Context

SemSpec's BMAD/OpenSpec alignment is valuable, but recent hard runs showed a
gap: the original sponsor brief can survive as prose while losing authority as
the plan moves through analyst, planner, architect, product/story, scenario,
developer, reviewer, recovery, and QA handoffs.

The MAVLink/OSH failure made the shape concrete:

- The prompt required extending an existing brownfield baseline.
- Planning and recovery could narrow the current plan without preserving why
  the earlier contract changed.
- Developer output could present a clean-room standalone project shape.
- QA caught the topology/build-root defect only after expensive execution.
- The UI exposed stale or orphaned phase rows rather than the authoritative
  current state.

ADR-040 remains correct: SemSpec hydrates BMAD/OpenSpec artifacts from
PLAN_STATES/SKG state, and prose is a projection. The missing piece is a
machine-readable contract packet that every projection, prompt, validator,
recovery decision, and UI surface can cite.

## Decision

Add Plan-owned contract authority as the governing layer over BMAD/OpenSpec
handoffs.

Each new plan records a root contract packet before downstream work begins. The
packet has stable identity, schema version, source references, root brief,
non-negotiable constraints, acceptance obligations, forbidden moves, the initial
scope snapshot, brownfield topology facts, accepted amendments, and validation
findings.

The root packet is immutable. Later accepted PlanDecisions append amendments
that declare whether they preserve, refine, or change contract obligations.
Downstream agents may propose contract changes, but they do not silently mutate
the root packet or shrink accepted scope.

BMAD/OpenSpec roles receive role-scoped projections of the same contract packet:

- Mary/planner see sponsor intent, source references, constraints, acceptance
  obligations, forbidden moves, and root scope.
- Winston/architect sees constraints, acceptance obligations, forbidden moves,
  root scope, brownfield topology, and accepted amendments.
- John/requirement-generator and Bob/scenario-generator see contract identity,
  constraints, acceptance obligations, forbidden moves, and amendment context.
- Sarah/story-preparer and Amelia/developer see topology and file/scope
  obligations because they shape or write ownership surfaces.
- Review, recovery, and QA see topology facts, accepted amendments, and
  validation findings so they can classify failures against the root contract.

Generated BMAD/OpenSpec artifacts remain projections. They must show the
governing contract packet ID/version and accepted amendment references, but they
do not become the source of truth.

Accepted `scope.create` entries are deliverable obligations, not only file-edit
permissions. Execution must prove those obligations progressively at Story
approval, Requirement completion, and final plan convergence before QA can run.

## Consequences

Validators compare downstream artifacts to the root contract plus accepted
amendments, not only to the latest mutable plan shape. Silent baseline
replacement, clean-room project roots, missing acceptance obligations, and
unexplained scope shrinkage become contract-fidelity failures.

Missing declared deliverables become deterministic closure failures. A
Story-level gap retries the current Story with missing-file feedback while
budget remains. A Requirement-level or assembled-branch gap produces a
`scope_incomplete` PlanDecision that preserves the original contract, resets the
affected executable closure, and redispatches only those affected requirements.

Recovery actions must carry contract impact. Targeted dirty marking is the
default; whole-phase reset requires evidence that the entire phase is invalid.
Full-auto mode may auto-accept only policy-safe changes with explicit impact
and affected nodes.

UI/API summaries should derive from authoritative phase/execution state and
contract references rather than stale feed rows. Operators should be able to
see current phase, waits, recovery decisions, QA evidence, lesson activity, and
staleness without asking.

## Non-Goals

This ADR does not replace BMAD roles, OpenSpec artifact layout, PLAN_STATES,
EXECUTION_STATES, or the SKG.

It does not introduce Java- or MAVLink-specific validation into the core model.
The first regression fixture may use the OSH composite Gradle failure, but the
contract/topology model is language- and framework-agnostic.

It does not solve cross-run lesson hydration. Lesson activity should be surfaced
and costed, but persisting lessons for future runs is a separate decision.
