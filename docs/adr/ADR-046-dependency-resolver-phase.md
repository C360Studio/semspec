# ADR-046: Dedicated Dependency-Resolver Phase (Split Resolution from Architecture Design)

**Status:** Proposed — **GATED on a prerequisite fix + re-test** (2026-06-08). See
"Prerequisite finding" below: a live schema-wiring bug means the architect was *structurally
unable* to emit imports in the runs that motivated this ADR, so the "architect can't resolve
deps" evidence is not yet trustworthy. Fix the schema and re-run before approving this build.
**Supersedes (if approved):** the *inline-resolution* expectation of the "upstream-strengthening"
strategy (2026-05-15) — the architect producing fully-verified `upstream_resolutions` itself.
Retains the verification gates from the 2026-06-07/08 campaign (#126 coordinate reality-check,
#133 capability-index, #134 import qualifier, #135 revise-on-retry) and **repurposes them as the
resolver's done-criteria** rather than architect-rejection gates.
**Drives:** architect output contract change (`dependency_requests` instead of resolved
`upstream_resolutions`), a resolution loop with its own budget, `resolution_kind`-aware
resolution strategy.

## Prerequisite finding (2026-06-08 review): the motivating evidence is contaminated by a schema bug

The architecture `submit_work` tool schema in `tools/terminal/schemas.go` is **missing the very
fields the architect is required to produce**:

- `upstream_resolutions[].apis[]` lists only `{symbol, kind, signature, lifecycle, notes,
  citation}` with `additionalProperties: false` — there is **no `import` and no `artifact`**.
- `upstream_resolutions[]` has **no `resolution_kind`** (required keys are `name, coordinate,
  source_ref, apis, used_by, role`).

But #125 added `import`/`artifact` and #126 added `resolution_kind` to the Go struct
(`workflow/types.go`), the architect prompt (`prompt/domain/software.go`), the deterministic
validator (`ValidateUpstreamImports`), and the openapi spec — **never to the submit_work schema
the model actually calls.** The function signature has stronger pull than prompt prose (the
codebase documents this exact failure mode in `explorationSchema`), so the architect emitted
`apis[]` *without* `import` because `import` is not a parameter of the function it is calling;
`ValidateUpstreamImports` (#134) then hard-rejected the missing field — an **unwinnable loop**.

Consequences for this ADR:

1. The "import plateau" and "architect won't switch OSH to `source_build`" findings are most
   likely **this bug**, not a model-capability ceiling. (`resolution_kind` absent from the schema
   is why the architect never declared `source_build`; it only functioned at all because
   `EffectiveResolutionKind` infers from coordinate shape.)
2. **Therefore the immediate next step is NOT to build this ADR.** It is to (a) fix the schema so
   `apis[].import`, `apis[].artifact`, and `upstream_resolutions[].resolution_kind` are
   first-class, required-where-appropriate fields, then (b) re-run `mavlink-hard` and observe
   whether the architect now converges with inline resolution.
3. ADR-046 is justified **only if the architect still fails to resolve deps after the schema is
   correct.** The rest of this document specifies that build properly (per the 2026-06-08 review)
   so it is ready if needed — but it is explicitly conditional on the re-test.

## Context

The "upstream-strengthening" bet (2026-05-15) replaced a shelved research sub-agent with a
simpler idea: the architect resolves every external dependency inline into
`upstream_resolutions` (coordinate + `resolution_kind` + per-symbol fully-qualified imports),
so the developer never re-discovers them mid-cycle. The 2026-06-07/08 paid `mavlink-hard`
campaign disproved that bet for hard fixtures.

We added a deterministic gate per defect as each surfaced — coordinate must resolve on Maven
Central (#126), capabilities referenced by index not retyped name (#133), imports must be
present and qualified (#134), retries must revise rather than rewrite (#135). Each gate is
individually correct, and #135 eliminated the rewrite-regression (missing imports went from
oscillating 5→4→6 to a stable plateau). But the cumulative result was that the architect
**plateaus at architecture** and never reaches execution: it cannot fit N-dependencies ×
M-symbols of mechanical resolution work (`curl search.maven.org`, `jar tf | grep Class`, clone +
grep github source for source-build deps) **alongside its actual design job** (component
boundaries, decisions, data flow, capability mapping) inside one iteration budget.

The evidence is specific: the architect *can* do each resolution step — we observed it
`curl raw.githubusercontent.com .../AbstractSensorModule.java` and
`jar tf mavsdk.jar | grep System.class` — but it runs out of budget/attention mid-design and
submits with 2-3 imports unresolved. The plateau (stable, not converging) means it is at its
capability edge for this task **shape**, not a gate-tuning problem. No additional gate fixes a
budget/attention exhaustion.

Conclusion: dependency resolution is **specialized, tool-heavy, iterative, and verifiable** work
that deserves its own loop and budget. This is the research sub-agent problem returning — but
narrowly scoped to dependency resolution, with our campaign's gates as its objective contract.

## The ordering problem (the key design question)

> If the resolver runs *before* the architect, how does it know which dependencies to gather?

It can't, and it must not run first. **Dependency identity is itself an architecture decision.**
Only the architect decides "use MAVSDK Java" vs "use Dronefleet MAVLink" vs "use MAVSDK Server"
— we literally watched it waffle between these across attempts. The plan goal and requirements
name a *domain* ("MAVLink/MAVSDK support for OpenSensorHub via the Connected Systems API") but
not the resolvable artifacts. So a resolver that runs ahead of design has nothing concrete to
resolve.

The resolution is to **split the architect's current monolithic job at the right seam**, not to
reorder phases:

- **Architect (design — keep):** chooses the libraries and names the symbols it intends to use.
  It is *good* at "I need MAVSDK's `System` class and OSH's `AbstractSensorModule`." This is
  design intent, fully within its capability.
- **Resolver (mechanical — new):** takes that **named** list and produces the verified
  `{coordinate, resolution_kind, source_ref, apis:[{symbol, import, signature, citation}]}`. It
  is *good* at the iterative lookup because it has its own budget and a single focus.

So the resolver runs **after the architect names its dependencies, fed by the architect** — not
before it. The architect stops doing the work it's bad at (verified resolution) and keeps the
work it's good at (deciding what to depend on). The seam is "what do I need" (architect) vs "what
is the verified coordinate/import for it" (resolver).

## Decision

Introduce a **dependency-resolution loop** between architecture design and persistence. Concretely:

### 1. Architect output contract shrinks to `dependency_requests`

The architect no longer authors resolved `upstream_resolutions`. It emits, per external
dependency it chose:

```jsonc
{
  "dependency_requests": [
    {
      "name": "MAVSDK Java",                 // CANONICAL LINK KEY (see below) — stable across revisions
      "ecosystem": "maven",                  // MVP: maven | kmp | source. (npm/pypi/go = post-MVP, see Ecosystem boundary)
      "role": "runtime_dep",                 // runtime_dep | build_dep | integration_target  (architect-owned)
      "used_by": ["mavsdk-driver"],          // component_boundaries[].name that depend on this  (architect-owned)
      "symbols": [                           // the code symbols the design uses
        {"symbol": "System", "kind": "class"},
        {"symbol": "MavsdkServer", "kind": "class"}
      ],
      "hint": "io.mavsdk; github.com/mavlink/MAVSDK-Java"  // optional coordinate/repo hint
    }
  ]
}
```

This is design intent — the architect already knows it. It is *not* asked to verify coordinates
or resolve imports.

**Linkage & ownership (Medium-4).** Downstream invariants depend on `used_by`,
`component_boundaries[].upstream_refs`, integration-name pairing, `role`, and harness-profile
coverage — none of which the resolver can invent. The split is by *who knows the fact*:

| Field | Owner | Why |
|---|---|---|
| `name` (canonical link key) | architect | it's the join key both `component_boundaries[].upstream_refs` and the resolved `UpstreamResolution.name` must match verbatim |
| `role`, `used_by` | architect | component/test linkage is a design decision |
| `harness_profiles[]` selection | architect | profile choice for `integration_target` deps is design; gated on `role` |
| `symbols[]` (what to resolve) | architect | the design dictates which API it uses |
| `coordinate`, `resolution_kind`, `source_ref`, `apis[].import/artifact/signature/citation` | **resolver** | mechanical, verifiable resolution |

The resolver emits `UpstreamResolution` carrying the architect's `name`/`role`/`used_by`
**verbatim** (so the existing name-pairing + `upstream_refs` invariants hold) plus the resolved
machine fields. The integration-name pairing rule (every `integrations[].name` ↔
`upstream_resolutions[].name`) is preserved because `name` flows through unchanged.

**Ecosystem boundary (Medium-5).** The verifier built this campaign is Maven/source-shaped, so
**MVP supports exactly `maven_central | kmp_multiplatform | source_build | unresolved`.** `npm`,
`pypi`, and `go` are out of MVP scope — they require ecosystem adapters with their own
authoritative probes and evidence shapes (registry existence check + import/package derivation),
which are a follow-up. The architect's `ecosystem` field is validated against the MVP set; an
out-of-scope ecosystem is an explicit `unresolved` with a note, not a silent pass.

### 2. The resolver loop produces verified `upstream_resolutions`

A specialized agent loop with its **own iteration budget** takes `dependency_requests` and
produces the existing `UpstreamResolution` shape, `resolution_kind`-aware:

- `maven_central` / `kmp_multiplatform`: verify coordinate via Maven Central
  (`maven-metadata.xml` HEAD per #132's verifier), `jar tf` the artifact to derive each symbol's
  fully-qualified import.
- `source_build`: clone/fetch the git source (the resolver has `http_request` + bash and its own
  budget to do this *thoroughly*, unlike the architect), grep for `class <Symbol>` to derive the
  package → import; verify the `source_ref` resolves.
- `unresolved`: honest outcome when no consumable artifact exists, with a note.

### 3. The campaign gates become the resolver's done-criteria

`ValidateUpstreamImports` (#134), the coordinate reality-check (#126), and capability-index
resolution (#133) stop being "reject the architect" and become the resolver's **objective
loop-exit contract**: the resolver iterates until every requested symbol has a verified
qualified import and every coordinate resolves (or is honestly marked `unresolved`). The
deterministic checks are the resolver's self-test, not a downstream punishment.

### 4. Failure classes & retry ownership (High-3)

Today every arch-gen validation failure bounces through one path (`retryOrFail` → architect)
with one budget. With a resolver loop, failures split into two classes with **separate retry
budgets and owners**:

| Failure class | Example | Retries against | Budget |
|---|---|---|---|
| **resolver-retryable** | a requested symbol's import wasn't derived; a coordinate the resolver *picked* didn't verify; source clone/grep incomplete | the **resolver loop** (same `dependency_requests`, more iterations) | `MaxResolverRetries` (new, e.g. 2) |
| **design-bounce** | a requested library genuinely has no artifact in its declared ecosystem ("no Maven artifact for 'Dronefleet MAVLink'; resolvable candidates: io.mavsdk:mavsdk, io.dronefleet:mavlink — pick one"); a request with `role=integration_target` but no harness profile | the **architect** (revise `dependency_requests`) | the existing `MaxGenerationRetries` |
| **terminal-honest** | resolver exhausts and marks a dep `unresolved` with a note | neither — flows downstream as an explicit, reviewable gap (plan-reviewer / human) | — |

Key property: resolver-retryable failures **must not** consume the architect's budget (the bug
this whole ADR exists to fix). Only a genuine design defect — a non-existent or mis-ecosystemed
library, or an unmet harness obligation — bounces to the architect, and the resolver makes that
bounce *actionable* by reporting the resolvable candidates it found. "Missing import" is **never**
an architect-bounce; it is resolver-retryable or, ultimately, `unresolved`.

## Schema migration (High-1)

The architect can't emit `dependency_requests` while the live architecture `submit_work` schema
(`tools/terminal/schemas.go`) still *requires* `upstream_resolutions` and forbids unknown fields
(`additionalProperties: false`). Two schema jobs, sequenced:

**Job A — fix the existing schema first (the Prerequisite bug, ships independently).** Add
`apis[].import` (string), `apis[].artifact` (string, omitempty/nullable), and
`upstream_resolutions[].resolution_kind` (enum: `maven_central | source_build | kmp_multiplatform
| unresolved`) to the *current* architecture schema, and add `import` to the `apis[]` required
set for code-symbol kinds. This makes #125/#126 actually wired and lets us re-test inline
resolution. **This is correct regardless of whether ADR-046 proceeds** and is the gating
experiment.

**Job B — only if ADR-046 proceeds: a dedicated design deliverable schema.** Rather than overload
the architecture schema with a dual mode, add a distinct deliverable type
`schemaForDeliverable("architecture-design")` whose `component_boundaries`/`actors`/`decisions`/
`integrations`/`test_surface`/`harness_profiles` match today's architecture schema **minus**
`upstream_resolutions`, **plus** a required `dependency_requests[]` (shape in §1). The resolver's
deliverable stays the existing `UpstreamResolution` shape (now with Job-A's fields). The
two-deliverable approach keeps each schema strict (`additionalProperties: false`,
strict-mode-clean per ADR-035) instead of a permissive union, and matches the existing
one-schema-per-role pattern in `schemaForDeliverable`.

Back-compat: a plan whose architecture already carries resolved `upstream_resolutions` and no
`dependency_requests` (old shape, mock fixtures) skips the resolver and is used as-is — the
resolver only runs when `dependency_requests` is present.

## Placement & durable handoff (High-2)

Two sequential agent loops inside the existing `architecture-generator` component — (1) architect
design loop, (2) resolver loop — **still need a durable handoff**, because the current completion
watcher accepts only `WorkflowStep == "architecture-generation"`, parses a *final*
`ArchitectureDocument`, validates, and publishes (`component.go` loop-completion handler). A naive
"second loop before persistence" has no answer for: where does the architect's design live while
the resolver runs, and what survives a process restart / KV replay between the two loops?

The review is right that "no new status" was hand-waving. The handoff requires:

1. **A distinct resolver workflow step.** The architect loop dispatches with
   `WorkflowStep = "architecture-design"`; the resolver loop with
   `WorkflowStep = "dependency-resolution"`. The completion watcher routes on step so it doesn't
   mis-parse a design-only deliverable as a final architecture.
2. **The architect's partial design must be persisted before the resolver runs.** The design
   deliverable (everything *except* resolved `upstream_resolutions`, plus `dependency_requests`)
   is written to `PLAN_STATES` — either as a new persisted field (`plan.ArchitectureDraft`) or by
   reusing `PreviousArchitectureJSON` as the carrier — under a new intermediate status
   **`architecture_drafted`**. This is the concession: durable two-phase handoff *does* warrant
   an intermediate status; "no new status" was wrong.
3. **Per-step retry keys + stale-loop handling.** Each step gets its own `dispatchretry` key
   (`design:<slug>` vs `resolve:<slug>`) and its own `IsStaleLoop` guard keyed on the step's task
   ID, so a late completion from a superseded design loop can't clobber a resolver in flight (and
   vice-versa). This mirrors the existing stale-loop guard, one per step.
4. **Restart/replay survival.** On restart mid-flight: if status is `architecture_drafted` with a
   persisted draft, reconcile re-dispatches the resolver from the draft (no re-design). If the
   draft is absent (crash before persist), reconcile re-dispatches the architect from
   `requirements_generated`. The KV write of the draft is the commit point that makes the handoff
   replay-safe — same discipline as every other phase transition.

So the honest MVP placement is: **one component (`architecture-generator`), two workflow steps,
one new intermediate status (`architecture_drafted`), per-step retry keys + stale guards.** It
still needs **no new component and no `spawn_child_loop`** — but it is *not* "no new status."

**Rejected for MVP:** a separate `dependency-resolver` *component* with its own watcher/reconcile.
The intermediate status above gives durable handoff without a second component's full lifecycle
surface; promote to a separate component only if resolution must run independently of arch-gen.

**Rejected for MVP:** a `resolve_dependency` *tool* the architect calls mid-loop. Attractive
(resolution happens exactly when a dep becomes known) but a judgment-bearing resolver tool needs
a sub-loop (`spawn_child_loop`, a draft semstreams ask) or it degrades to deterministic-only and
can't handle the judgment cases (which artifact, which of several matching classes). Keep for the
post-MVP graph era.

## semstreams coupling (deliberately minimal)

This design depends on **nothing new in semstreams** — no `research_graph`, no `semsource`
revival, no graph-ingest changes, no `spawn_child_loop`. Given semstreams is currently a moving/
breaking dependency for us, that is a primary selection criterion. It reuses the agentic-loop +
component patterns already in production.

## Post-MVP evolution (Option A: graph-native)

The resolver's *contract* (named requests → verified resolutions) is backend-agnostic. Once
semstreams stabilizes, the resolver's lookups (`curl`/`jar tf`/git-grep) can be replaced by
`research_graph` queries against a graph that `semsource` pre-indexed with the relevant upstream
repos. Same input/output contract, faster and offline. The per-fixture pre-indexing pipeline
(clone → ingest → wait) and its complexity stay deferred until then — and crucially, the
pre-index step is only an optimization of the resolver's backend, not a prerequisite for the
phase to exist.

## What this simplifies / removes

- The architect is **no longer rejected for missing/bare imports** — it never authors imports.
  `ValidateUpstreamImports` moves to the resolver's done-check. This directly removes the
  arch-gen plateau this campaign hit.
- The architect's prompt sheds the entire "resolve every API surface against the artifact"
  burden (the largest, least-design part of its current spec).
- Recovery's `architecture_revise` still works; on a re-architect, dependency_requests are
  re-resolved by the resolver (cheap, its own budget) rather than re-burdening the architect.

## Risks / open questions

- **Two loops per arch-gen = more tokens per attempt.** Mitigated: the resolver's budget is
  spent on the work the architect was *already* failing to do; net token cost is comparable or
  lower because we stop the architect's retry-churn (3 full re-designs to chase imports).
- **The architect could under-specify `symbols`.** The resolver can only resolve what it's asked
  for; if the architect omits a symbol the dev needs, it surfaces downstream. Mitigation: the
  resolver may surface "you depend on X but requested no symbols from it" as a soft note.
- **Source-build resolution is still genuinely hard** (clone + grep a large repo). The resolver
  with its own budget is the right home for it, but gemini-pro may still struggle on the hardest
  source-build deps. This ADR makes the problem *tractable and isolated*, not trivially solved;
  it may still expose a model-capability ceiling for the hardest fixtures, which is then a
  model-selection decision, not an architecture one.
- **Contract migration:** existing mock fixtures and tests author `upstream_resolutions`
  directly. Back-compat: when `dependency_requests` is empty but `upstream_resolutions` is
  present (old shape), skip the resolver and use them as-is.

## Consequences

- The architect does design; the resolver does resolution; the developer does implementation —
  a clean three-way separation that matches each agent's demonstrated strengths.
- The campaign's verification work is preserved and *repurposed* (gates → done-criteria), not
  discarded.
- We stop patching arch-gen gates reactively; the next failure mode (if any) is isolated to the
  resolver, with a clear contract to test against.
- A tractable MVP path that does not wait on the breaking semstreams `research_graph`/`semsource`
  work, while leaving that as a clean backend upgrade.

## Next steps (sequenced; this ADR is gated on step 2)

1. **Fix the schema-wiring bug (Job A). — DONE (2026-06-08).** Added `apis[].import`,
   `apis[].artifact` (nullable, required), and `upstream_resolutions[].resolution_kind` (nullable
   enum, required) to the live architecture `submit_work` schema; updated the architect prompt
   example to model a `source_build` dep (OSH) and a real `maven_central` dep (MAVSDK) instead of
   the previous coordinate that mis-taught OSH as Maven. **Added `TestArchitectureSchemaStructParity`
   — a reflection-based schema↔struct parity guard that fails CI whenever a struct JSON field is
   absent from the submit_work schema (or vice-versa), modulo a documented `systemOwned`
   allowlist. This is the class-of-bug prevention: schema-vs-struct drift can no longer ship
   silently. The pre-existing strict-mode tests only checked the schema against itself, which is
   why they never caught it.**
2. **Re-run `mavlink-hard` and observe.** Does the architect now converge with inline resolution
   once it can actually emit imports + `resolution_kind`?
   - **If yes** → the plateau was the bug; ADR-046 is deferred (the inline bet may hold for now),
     and we revisit only if a genuinely harder fixture re-exposes the budget-competition problem.
   - **If still plateauing on resolution depth** (not the schema) → ADR-046 is justified;
     proceed to Job B + the resolver phase as specified above.
3. **Only then build the resolver phase.** Do not start the ADR-046 build before step 2 settles
   the question — that was the reactive-patching trap this whole exercise is trying to escape.
