# ADR-045: BMAD-Aligned Story Gate + Risk-Based, Capability-Gated, Operator-Executed Test Tiers

**Status:** Accepted (2026-06-05)
**Supersedes:** the *executor* pieces of ADR-039 (qa.yml → qa-runner act execution) and the
classifier/gate framing of ADR-041 (services-class profiles forcing a gating `@integration`).
Retains: the harness catalog, tier tags, and the qa.yml *emitter* (now an operator-CI contract).
**Drives:** role→prompt projection, Murat persona at the Story tier, classifier capability gate,
qa-runner removal, `qa_level` collapse.

## Context

A role-context audit (2026-06-05) plus the paid mavlink-hard runs (#4 M:N wipe, #5/#6 SITL
infinite-reject + dep hallucination) surfaced that the refactor from the old
`plan → requirements → scenarios` model (where the execution layer decomposed requirement DAGs
and reviewed at the requirement level) to the BMAD/OpenSpec model
(`capabilities → architecture → Sarah Stories+Tasks → per-task dev+Cline → QA`) left three
classes of debt:

1. **A lossy, ad-hoc role→prompt projection.** The graph entity model is correct and carries the
   right structured facts (`UpstreamResolution{Coordinate,APIs,Role}`,
   `ComponentDef{UpstreamRefs,ImplementationFiles,Capabilities}`, `Story` M:N joins). But each
   role's prompt context was built by a hand-rolled projection that silently dropped facts the
   role's BMAD responsibility required as input. Winston wrote `upstream_resolutions` into the
   graph; the dev, Sarah, Bob, and recovery each re-projected architecture through their own lens
   and dropped it. "`upstream_resolutions` unwired" (run #6 root cause — devs re-hallucinated
   coordinates the architect had already resolved) was one instance.

2. **Murat (the quality gate) was mis-homed at the plan/release level**, and an orphaned
   `scenario-reviewer` (no `bmad.json` persona, thinnest context of any role) sat at the
   requirement level as old-model residue. Its scenario→node "dirty retry" was structurally
   broken: every DAG node carries the *identical* full Story scenario list, so any one failed
   scenario dirtied all nodes → a full-DAG restart (the run #4 M:N wipe).

3. **MVP over-scoped by making semspec the e2e executor.** `qa-runner` mirroring GitHub CI via
   nektos/act was semspec taking the project's CI role. ADR-041 shipped the tier tags but
   deferred the availability declaration to ADR-042 — so the pipeline mandated environments
   (live PX4 SITL) the sandbox structurally cannot run, producing the ADR-041 infinite-reject
   wedge.

### What BMAD/OpenSpec actually prescribe

BMAD's Test Architect (TEA / Murat) does **risk-based** test design (probability × impact, P0–P3)
and renders **gate decisions at the Story** (and epic/release); the **project's own CI executes**
— TEA has explicit framework/CI-setup workflows to build that capability first, then gates on
real results. OpenSpec scenarios are testable acceptance criteria; OpenSpec is a spec layer, not
a runner. Neither mandates blanket e2e, and neither executes it. So e2e is opt-in by risk and
presupposes the capability to run it.

## Decision

> **Gate at the Story, with Murat, on scenarios. Test levels are risk-based and
> capability-gated. semspec designs + gates + emits `qa.yml`; the operator's CI executes
> e2e/SITL. Un-runnable-in-sandbox tiers are deferred-and-noted, never rejected.**

Implemented across four phases on `fix/role-context-audit-train`:

1. **Faithful graph→role projection.** One shared `prompt.ProjectArchitecture` /
   `ProjectUpstreams` converter that every role draws from (`FormatArchitectureContext`,
   `FormatUpstreamResolutions`). Wired into developer + per-task reviewer (`TaskContext.UpstreamResolutions`),
   Sarah (`UpstreamRefs` + integrations + upstreams), Bob (full projection), and recovery
   (architecture surface). Plus `Plan.PreviousArchitectureJSON` revision memory for the architect.

2. **Murat at the Story tier.** The per-Story acceptance gate gets Murat's persona (a
   `scenario-reviewer` persona in `bmad.json`) and faithful context (standards, rotated lessons,
   plan/requirement titles, architecture). The scenario→node dirty-retry is deleted: a fixable
   rejection re-runs the whole Story on its existing branch with feedback (no scenario partition);
   a single-node mid-DAG *error* still targets that node. The persona treats a tier the sandbox
   can't execute (live SITL, `@smoke`, `@e2e`) as documented-deferred, not a failure.

3. **Risk-based, capability-gated tiers; operator-executed e2e.** The classifier requires
   `@integration` only for **testcontainers-class** profiles (sandbox-runnable). Services-class
   profiles (live SITL) are operator-tier: the architect still selects them by risk and they are
   emitted into the operator's `qa.yml`, but they never gate the dev/Story. The `qa-runner` act
   executor is removed; `qa_level` collapses to `none / synthesis / unit` (sandbox runs unit);
   `EffectiveQALevel` coerces any stale `integration`/`full` snapshot to `synthesis`. The harness
   catalog and the qa.yml emitter stay — qa.yml is now an operator-CI contract.

## `architecture_revise` recovery action (implemented — follow-up to this ADR)

When recovery diagnoses an *architecture-root* wedge (a missing/mis-resolved upstream dependency,
a wrong component boundary, an un-runnable integration target), the recovery-agent now picks
`architecture_revise` instead of `escalate_human`. On accept, plan-manager captures the prior
architecture into `PreviousArchitectureJSON` (the revision base the architect already reads),
routes the diagnosis into `ReviewFormattedFindings`, clears Architecture, resets the affected
requirement/story closure, and drives the new back-transition `implementing →
requirements_generated`. The architect re-fires and *revises* the prior architecture against the
diagnosis. Whole-phase decisions may regenerate the full downstream pipeline when explicitly
justified; scoped decisions preserve unrelated Stories/Scenarios and merge only the dirty closure
before execution resumes.

Because this fires from `implementing` — a LIVE phase with in-flight executor state, unlike its
sibling `story_reprepare` (which fires from the quiescent `stories_generated`) — three guards
make it safe:

1. **Auto-accept cap** (`plan-decision-handler.max_auto_architecture_revises`, default 1). Each
   accepted `architecture_revise` is a full, expensive re-run; the cap bounds the
   `implement → wedge → revise → …` loop. Past the cap the decision stays proposed for human
   review. The count is monotonic — `PlanDecisions` survive the entity wipe.
2. **Abandon-not-resume.** `PlanDecisionAcceptedEvent` now carries `Kind`; the requirement-executor
   abandons (terminates + drops from `activeExecs`) all in-flight execs for the slug instead of
   resuming the wedged one, so the stale DAG can't race the architect re-run on the shared
   `req.<slug>.<reqID>` key.
3. **Atomic-ish apply.** The back-transition is checked before any mutation or I/O (an out-of-window
   accept is a clean no-op); the execution reset runs on a detached context before the in-memory
   wipe, so a reset failure aborts the accept with the plan untouched.

**Known limitation (LOW, cost not correctness):** the reset clears `req.<slug>.*` EXECUTION_STATES
rows but not `task.<slug>.*` rows, and the requirement-executor has no channel to force-cancel
in-flight execution-manager TDD loops. On a genuinely-parallel hard-tier plan, a sibling node's
loop can run out its token budget before noticing the slug restarted (its terminal write is
absorbed by the `activeExecs` cache-miss no-op — no corruption, just wasted spend). The M:N
cohesive shape (one dev loop per Story) does not hit this. A follow-up could thread loop
cancellation through execution-manager.

## Deferred (designed, not yet implemented)

- **Sarah-authored per-task scenario partitions.** Would let a fixable Story rejection re-run a
  *subset* of nodes instead of the whole Story. Requires a `Task.ScenarioIDs` schema + Sarah
  prompt change. The current coarse Story-altitude retry is correct (BMAD gates are coarse) and
  bounded by the retry budget.

- **A structured `Scenario`-level deferral record.** Today the deferral is expressed structurally
  (classifier doesn't require services-class) + behaviorally (Murat persona defers) + via the
  emitted qa.yml. A per-scenario "deferred — pending operator CI" field would make it queryable
  for UI/reporting.

## Consequences

- The five audit findings collapse into instances of one principle. The run #6 dep-hallucination
  and the run #4 M:N wipe are fixed by construction; the ADR-041 SITL infinite-reject is dissolved
  (the gate no longer demands evidence the sandbox can't produce).
- `qa_level=integration`/`full` are gone; projects that set them coerce to `synthesis`. Heavier
  tiers move to the operator's CI against the emitted qa.yml.
- semspec's QA executor is the sandbox (unit) only; the qa-runner trust-boundary container and its
  Docker-socket mount are removed.
