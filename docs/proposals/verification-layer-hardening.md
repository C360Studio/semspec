# Proposal: Verification Layer Hardening

**Status:** Proposed (post-MVP)
**Date:** 2026-05-27
**Related:** ADR-029 (Plan Completeness Review), ADR-031 (QA Test Execution),
ADR-033 (Lesson Decomposition), `eng-test-coverage` standard, `qa-reviewer` component

## Summary

Harden the verification layer between developer submission and plan completion
so that BDD scenarios cannot be satisfied tautologically. Three additive
changes downstream of the existing spec layer:

1. **Typed coverage claims** extending the reviewer's existing structured-findings
   payload, verified by a deterministic post-review gate.
2. **A `scenario-expander` component** that synthesizes adversarial scenarios
   from the requirement and function signatures (never from the original
   scenarios) and runs them against the frozen implementation before review.
3. **Mutation challenge as a qa-reviewer extension** at higher `qa.level`
   tiers — fold the independent-oracle check into the role that already owns
   release-readiness, rather than introducing a new agent persona.

The spec layer is unchanged. Elicitation, planner, requirement-generator,
scenario-generator, plan-reviewer R1/R2, structural-validator, and the
existing reviewer payload all continue to function as today. Every change
lives in the post-implementation verification path where the implementing
agent's optimization pressure actually concentrates.

## Motivation

SemSpec is positioned downstream of elicitation. The input prompt is assumed
to encode sufficient business intent — either because a human has done the
upstream clarification work, or because an upstream system has produced a
structured request. Multi-perspective Three-Amigos-style discovery is a
separate product concern handled before the issue reaches SemSpec.

Within SemSpec's scope, BDD scenarios are the optimization surface the
implementing LLM minimizes against. Two structural failure modes follow:

1. **Tautological code** — implementation hardcodes the scenario's example
   values, passes the original tests, fails on inputs the scenario did not
   enumerate.
2. **Tautological tests** — implementation and test are co-derived from the
   same spec, so the test reflects the implementation rather than
   independently encoding the requirement.

The reviewer's current payload (`verdict`, `feedback`, `summary`,
`rejection_type`, `findings[]`, `scenario_verdicts[]` — see
`tools/terminal/schemas.go`) is already structured findings rather than the
prose rubrics that earlier versions of SemSpec carried. That structure is
the right substrate to build on, but it does not yet *constrain* what the
reviewer must claim about coverage, and nothing checks the claim
independently of the agent that made it.

Goodhart's law applies: the reviewer agent inhabits the same optimization
basin as the developer agent. Defense requires an oracle the implementing
agent did not author and a deterministic gate the reviewer cannot
talk past.

## Design

### 1. Typed coverage claims (extension)

Extend the existing `reviewSchema` in `tools/terminal/schemas.go` with two
optional arrays. The reviewer continues to emit verdict, findings, and
scenario_verdicts as today; the new fields make coverage assertions
machine-checkable.

```json
{
  "verdict": "approved",
  "feedback": "...",
  "summary": null,
  "rejection_type": null,
  "findings": [],
  "scenario_verdicts": [...],
  "coverage_claims": [
    {
      "function": "withdraw",
      "branches_tested": ["sufficient", "insufficient", "zero_balance"],
      "boundary_cases": ["min_amount", "max_amount"]
    }
  ],
  "unhandled_branches": []
}
```

A new deterministic component `coverage-gate` (no LLM) reads the modified
files' AST, derives the canonical function and branch inventory, and
verifies every claimed branch has a test referencing it. Discrepancies
block the approved verdict — the reviewer must either revise the claim,
reject the submission, or have the developer add the missing coverage.
This makes the existing `eng-test-coverage` standard structurally
enforced rather than enforced by reviewer judgment alone.

New predicates under `review.coverage.*`:

| Predicate | Type | Description |
|-----------|------|-------------|
| `review.coverage.function` | string | Function name covered by claim |
| `review.coverage.branch` | string | Branch identifier (multi-valued) |
| `review.coverage.boundary` | string | Boundary case identifier (multi-valued) |
| `review.coverage.unhandled` | string | Branch identified by AST but not claimed |
| `review.coverage.verified` | bool | coverage-gate verification result |

The reviewer payload extension is backward compatible (`coverage_claims`
and `unhandled_branches` default to empty arrays). The gate only enforces
on projects that opt in via a standard or project-config flag, allowing
incremental rollout.

### 2. Frozen-implementation scenario-expander

A new component `scenario-expander` runs after `structural-validator`
succeeds and before the reviewer is dispatched. It synthesizes additional
Given/When/Then instances the developer never saw and exercises them
against the frozen implementation.

**Critical derivation constraint:** expansion scenarios are derived from
the requirement text and function signatures only, never from the
original scenarios or the implementation itself. Paraphrasing the spec
the developer optimized against provides no adversarial value; reading the
implementation would inherit the same tautology problem.

Expansion sources:

- **Boundary values** from typed parameters — numeric ranges, string
  lengths, collection sizes, nullable fields, encoding edges
- **Equivalence-class neighbors** of values mentioned in the requirement
  — if the requirement says `$100`, generate `$0`, `$0.01`, `MAX_INT`,
  negative, currency mismatch
- **Cross-requirement interactions** — scenarios where this requirement's
  preconditions intersect with adjacent requirements in the DAG (sourced
  from the SKG)

Expansion scenarios become first-class graph entities linked to the
original requirement:

| Predicate | Type | Description |
|-----------|------|-------------|
| `scenario.derived_from` | entity_id | Original scenario this expansion derives from |
| `scenario.expansion_kind` | string | `boundary`, `equivalence`, or `interaction` |
| `scenario.frozen_impl` | bool | Generated against an implementation, not for planning |

Typing them distinctly lets the reviewer's coverage claims and downstream
analytics distinguish "passed original scenarios" from "passed adversarial
expansion." Failures feed back as concrete review findings with failing
inputs attached — the reviewer rejects on evidence, not feel.

**Semstreams shape:** standard Listen → Process → Persist → Publish.
Listens for `developer_submitted` + `structural_validation_passed`,
persists expansion scenarios as graph entities, publishes
`expansion_ready_for_review`. No new abstractions required.

### 3. Mutation challenge inside qa-reviewer

The cleanest home for mutation testing is the role that already owns
release-readiness: `qa-reviewer`. Adding a new "Adversarial Challenger"
persona would duplicate responsibilities already split across architect,
reviewer, and qa-reviewer — and would compete with qa-reviewer for the
same post-execution slot.

Instead, gate mutation challenge on `qa.level`. The current ladder
(`none` / `synthesis` / `unit` / `integration` / `full` — see
`workflow/types.go`) extends naturally:

| qa.level | qa-reviewer behavior today | + mutation extension |
|----------|----------------------------|----------------------|
| `none` | skipped | skipped |
| `synthesis` | LLM verdict, no execution | unchanged |
| `unit` | runs project test suite in sandbox | unchanged |
| `integration` | runs `.github/workflows/qa.yml` via qa-runner | unchanged |
| `full` | adds Playwright e2e | **+ N mutants per modified function** |

At `qa.level=full`, qa-reviewer generates 3–5 targeted mutants per
modified function — flip a comparison, return a constant, skip a null
check, invert a conditional — then re-runs the existing test suite
against each mutant. If any mutant survives (all tests pass against the
broken implementation), qa-reviewer emits a finding identifying the
surviving mutant and the tests that should have caught it.

MVP-scope mutation: high-signal patterns only, no full mutation testing
framework. This catches the most common tautological patterns at modest
cost without inheriting the equivalent-mutant problem in its full
generality.

Optionally, introduce `qa.level=full+mutation` as a distinct tier rather
than overloading `full`, depending on cost tolerance once measured.

New predicates under `mutation.*`:

| Predicate | Type | Description |
|-----------|------|-------------|
| `mutation.mutant_kind` | string | `boundary_flip`, `constant_return`, `null_skip`, `condition_invert` |
| `mutation.target_function` | string | Function the mutant modified |
| `mutation.survived` | bool | True if all tests passed against the mutant |
| `mutation.surviving_tests` | string | Test identifiers that should have caught it |
| `mutation.finding_id` | entity_id | Review finding generated from this mutation |

Surviving mutants become graph entities linking the mutation description
to the surviving tests, giving qa-reviewer concrete evidence to reject
on rather than vague "tests feel weak."

### 4. Standards flywheel from new failure classes (requires new mechanism)

Note honestly: there is no `origin: review-pattern` mechanism in
`standards.json` today. The only `origin` value currently emitted is
`init`. Phase 4 is the build of that mechanism, not the reuse of one.

Once 1–3 produce data, recurring failures in adversarial expansion and
mutation survival become candidate `standards.json` entries via a new
promoter component that watches review-finding and mutation-finding
streams:

- "Functions accepting numeric inputs must have tests for zero, negative,
  and maximum-value boundaries" — promoted from repeated boundary
  expansion failures
- "Tests must assert at least one observable per branch of the function
  under test" — promoted from repeated coverage-claim/AST mismatches
- "Mutating a comparison operator in implementation must cause at least
  one test to fail" — promoted from repeated mutation survival findings

Format follows existing standards schema; the new infrastructure is the
promoter logic and the `origin: review-pattern` taxonomy entry. This
phase compounds over time once the upstream data exists.

## Pipeline Shape

Current (shipping) — pre-QA execution path:

```
developer_submitted
  ↓
structural-validator
  ↓
reviewer
  ↓
reviewed → (next requirement, or eventually plan-level)
```

Then at plan completion:

```
implementing_convergence
  ↓
ready_for_qa (when qa.level ≠ none)
  ↓
qa-reviewer (scoped by qa.level: synthesis/unit/integration/full)
  ↓
complete
```

Proposed additions (★ = new, ✎ = extended):

```
developer_submitted
  ↓
structural-validator
  ↓
scenario-expander           ★ (boundary/equivalence/interaction, frozen impl)
  ↓
reviewer                    ✎ (emits coverage_claims + unhandled_branches)
  ↓
coverage-gate               ★ (deterministic AST check against claims)
  ↓
reviewed → ... → ready_for_qa
                    ↓
                  qa-reviewer  ✎ (mutation challenge at qa.level=full)
                    ↓
                  complete
```

Two new components, one extended payload, one extended persona, zero
changes to the spec layer or upstream gates.

## Consequences

### Positive

- **Tautological code detection** — frozen-implementation scenario
  expansion catches implementations that hardcoded the original scenario
  values.
- **Tautological test detection** — mutation challenge inside qa-reviewer
  catches tests that mirror the implementation rather than independently
  encoding the requirement.
- **Structural reviewer accountability** — typed coverage claims verified
  by deterministic gate; the reviewer cannot produce prose to bypass
  coverage requirements.
- **No new persona** — mutation lives in the role that already owns
  release-readiness, avoiding overlap with architect/reviewer/qa-reviewer.
- **Backward compatible** — coverage_claims optional, mutation gated on
  `qa.level=full`, expansion-scenario subjects are additive. Existing
  projects continue to work unchanged.
- **Enumerable failure classes** — new `review.coverage.*` and `mutation.*`
  predicates feed graph queries and (eventually) the standards flywheel.
- **Spec layer untouched** — issue-queue-to-PR workflow, upstream
  elicitation, Gherkin authoring, plan-reviewer R1/R2 all continue working
  exactly as today.

### Negative

- **Per-language AST tooling** — scenario expansion needs type-aware
  boundary derivation, coverage-gate needs branch inventory, mutation
  needs semantic mutation generation. Each language under SemSpec
  requires its own AST adapter.
- **Latency** — two new pre-review pipeline steps add wall-clock time
  between developer submission and approval. Mutation at `qa.level=full`
  multiplies qa-runner duration by mutant count.
- **Equivalent mutants** — some mutants are semantically equivalent
  (`<` vs `<=` on a value never at the boundary). Mitigated by limiting
  mutant kinds to high-signal patterns, not eliminated.

### Risks

- **Coverage-gate brittleness** — AST-derived branch inventory may
  disagree with reviewer's claim format. Mitigated by deriving the claim
  schema directly from the AST inspector's output format and treating
  unhandled_branches as an info-level finding before promoting to
  blocking.
- **Expansion-scenario tautology** — if the expansion generator is
  LLM-based and reads the implementation, it inherits the same problem.
  Mitigated by the strict derivation constraint (requirement + signature
  only, never implementation), enforced at the prompt level and ideally
  by isolating the expander from the implementation files in its tool
  palette.
- **Mutation cost at `qa.level=full`** — every mutant re-runs the test
  suite. Mitigated by starting with 3–5 high-signal mutants per modified
  function and only at the top tier; revisit with measured data before
  expanding.
- **Standards flywheel infrastructure lag** — Phase 4 needs the
  promoter mechanism built. Until then Phases 1–3 produce data that
  accumulates in the graph but isn't auto-promoted.

## Alternatives Considered

### A. Replace BDD with property-based testing

PBT has its own Goodhart problem: if the LLM writes the properties, it
picks properties its implementation already satisfies. Most business
logic also lacks the algebraic invariants PBT excels at, and the spec
layer's human-readability is a genuine asset for upstream elicitation
and human approval gates. PBT's core idea — independent oracle the
implementing agent did not author — is preserved in the mutation
challenge and frozen-implementation expansion.

### B. Replace BDD with formal verification

Spec-writing cost is enormous for typical business logic, and formal
specs still require an oracle the implementing agent did not author.
Formal methods remain valuable for specific high-assurance subsystems
(auth, financial calculation, state machines) but are not a drop-in
replacement for the spec layer.

### C. Stricter reviewer rubric prompts

Tightening prose rubrics is exactly what's being gamed. No prompt
engineering solves a structural verification gap. The existing
structured findings schema is already a meaningful step beyond rubric
prose; this proposal continues that trajectory.

### D. Introduce a separate "Adversarial Challenger" persona

Considered and rejected. SemSpec already runs three review-shaped
personas (architect for design review, reviewer for code review,
qa-reviewer for release readiness). A fourth post-implementation
persona would duplicate the slot qa-reviewer already occupies and add a
coordination boundary without paying for itself. Folding mutation into
qa-reviewer at higher qa.level tiers gets the structural benefit
without the persona proliferation.

### E. Mandatory tester/developer split

The historical SemSpec position rejected this: testers wrote minimal
tests, developers couldn't modify them, system looped. The current
developer + reviewer + qa-reviewer architecture is the working baseline;
the hardening proposed here strengthens its verification side rather
than reverting the split.

## Implementation Phasing

| Phase | Change | Cost | Impact |
|-------|--------|------|--------|
| 1 | Typed coverage claims (payload extension) + coverage-gate component | Low | Immediate review-quality improvement; eliminates prose-bypass on coverage |
| 2 | scenario-expander component (boundary/equivalence/interaction, frozen impl) | Medium | Biggest impact on tautological-code detection |
| 3 | qa-reviewer mutation extension at `qa.level=full` | High (per-language AST + test-suite multiplier) | Specifically targets tautological-test detection |
| 4 | Standards flywheel promoter (`origin: review-pattern` mechanism) | Medium (new infrastructure) | Compounds over time once Phases 1–3 produce data |

Phase 1 is the smallest delta to shipping code and gives the most
immediate value. Phase 2 introduces a new component but keeps the
reactive event model intact. Phase 3 is gated behind a qa.level tier
that few projects use today, so cost is opt-in. Phase 4 requires the
flywheel infrastructure that doesn't exist yet and is best deferred
until 1–3 have produced enough data to be worth promoting.

## References

- ADR-029 — Plan Completeness Review and Retry Loop (upstream gate this
  proposal complements)
- ADR-031 — QA Test Execution (the qa-reviewer + qa-runner architecture
  Phase 3 extends)
- ADR-033 — Lesson Decomposition (failure-trajectory infrastructure
  Phase 4 will build on)
- `eng-test-coverage` baseline standard (the intent this proposal makes
  structural)
- `tools/terminal/schemas.go` `reviewSchema` (the payload this proposal
  extends)
- `processor/qa-reviewer/` (the component Phase 3 extends)
- `workflow/types.go` `QALevel` (the tier gating Phase 3)
