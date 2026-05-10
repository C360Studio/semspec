# ADR-037: Wedge Recovery via Manager-Role Supervision

**Status:** Proposed (2026-05-10, revised 2026-05-10 after audit)
**Deciders:** Coby, Claude
**Related:** ADR-029 (plan completeness + retry — local revision loop within plan-phase), ADR-030 (BMAD personas), ADR-033 (lessons pipeline — captures wedge causes post-hoc; the only existing consumer of `agentic.query.trajectory`), ADR-034 (watch CLI — debug tool, not production data plane), ADR-035 (strict-parse — narrows what counts as a wedge vs silent compensation).

## Context

Semspec's quality gauntlet has grown — completeness review (ADR-029), structural validation, code review, QA review, plus persona-side escape hatches (`software.orientation.graph-errors`, `software.orientation.graph-results`). Each gate catches a real failure class. Cumulatively the gauntlet rejects roughly half of attempts even when the underlying goal is reachable, because at every stage *some* check rejects *some* generation.

That trade is intentional — semspec is a reliability tool, not a vibes tool. But the tail behavior is bad for adoption: a multi-cycle TDD wedge ends with `stage=escalated outcome=failed merge_commit=""` and no recovery. Sister project semteams has a different finding — when a manager-role agent is wired to *intervene* on wedges (read the trajectory, diagnose what the wedged agent missed, refine the task or reshape the work), recoverable wedges recover. The leverage is the manager *role* — fresh context, sees the trajectory, isn't anchored on the wedged agent's prior reasoning — not a smarter model. Today semspec has no such layer: when an agent gets stuck, the run dies.

Concrete examples from the 2026-05-10 gemini @hard run:
- **req.3 create-pom** burned 5 TDD cycles fabricating fictional Maven coordinates after `graph_search` returned the correct hint as a `[project]` entity. A manager reading the trajectory could see the unread hint and refine the task.
- **req.5 update-dependencies** hit `iter=50` budget without calling `submit_work` — a manager that recognized the bash-loop pattern could have redirected to "submit with obstacle summary."
- **plan-phase round 2 iteration 3** — already has local revision (ADR-029) but no manager-class recovery if the third iteration also fails; it just escalates.

Two failure modes that look similar but need different handling:
- **Recoverable wedge** — agent confused; would succeed with refined prompt, narrowed scope, or a different decomposition.
- **Unrecoverable failure** — goal not reachable from current state regardless of agent (e.g., fixture's parent pom isn't on Maven Central; req scope contradicts another req).

A recovery layer that doesn't distinguish these will burn tokens trying to fix unrecoverable cases. Recovery must diagnose first, recover second, and "escalate-to-human-with-analysis" must be a valid recovery outcome.

## Audit finding: trajectory access already exists, only one component uses it

Before designing recovery plumbing, we audited what semstreams already provides:

- **`agentic.query.trajectory` NATS subject** — request `{loopId, limit}`, response is a full `agentic.Trajectory` with all `Steps[]`. 5-second timeout. Available to any component with a NATS client. Implemented by semstreams' agentic-loop component.
- **Graph triples via `agent.loop.has_step` predicate** — every trajectory step is indexed and queryable via the existing graph layer. Linked to the parent loop entity.
- **ObjectStore content** — full step bodies stored, referenced by trajectory step entities.

**Semspec consumers today: one.** `processor/lesson-decomposer/trajectory.go:40` calls `agentic.query.trajectory` post-rejection to cite steps as evidence in lessons (ADR-033). plan-manager, execution-manager, qa-reviewer, code-reviewer, structural-validator — none consume the trajectory of the work they're handling, despite all having loop_id in scope and a NATS client available.

This reframes the problem. ADR-037 is **not** a request for new trajectory infrastructure — that infrastructure exists and works. ADR-037 is **a coverage gap fix in our wedge-handling components**, plus a manager-role recovery dispatch that uses the trajectory data we're currently throwing away.

The watch CLI sidecar (ADR-034) is a debug tool. It reads the same data via the same channels and packages it for offline forensics. Production code paths must consume `agentic.query.trajectory` directly, not depend on sidecar bundle output.

## Decision

Adopt a **hybrid recovery architecture**:

1. **Phase-local recovery** — plan-manager and execution-manager each gain a recovery dispatch on their own escalation paths. Cheap recoveries handled where the state lives.
2. **Coordinator (Site-Manager) component** — handles QA failures and any wedge that survives phase-local recovery. New component, listens on a `recovery.escalation.>` JetStream subject family.

Both phase-local and coordinator recovery use **capability-resolved manager-role agents** (capability-resolve-with-override pattern, commit `ec8ba13`):

- `plan_wedge_recovery` capability — for plan-manager's escalation path. Failure class: structural plan errors, scope conflicts, reviewer-rejection cascades.
- `execution_wedge_recovery` capability — for execution-manager's escalation path. Failure class: TDD cycle exhaustion, iter=N bash loops, hallucinated paths/coords.
- `coordinator_recovery` capability — for the new coordinator. Failure class: QA fails, cross-phase wedges where plan-side and execution-side recovery both ran.

Capability defaults are **the same model class as the wedged agent**, not a forced upgrade. The leverage is the manager role + trajectory access + fresh context — not a smarter model. Per-environment override (the existing pattern) lets us A/B model choices for recovery without touching manager code; bumping to a higher tier becomes one of the bounded actions a recovery agent can choose, not a prerequisite for invoking recovery in the first place.

### Bounded recovery action set

A recovery agent's submit_work must select from a closed set, not produce arbitrary mutations:

- `refine_prompt` — rewrite the task prompt with explicit context the wedged agent missed (e.g., "graph_search showed `[project] org.sensorhub`; use that").
- `bump_model` — retry with a higher-tier model on the same task. *This* is where smarter models enter the design — as one option among several, chosen by the manager when warranted.
- `narrow_scope` — reduce the task's scope (split a multi-file change into one file at a time).
- `split_req` — decompose the requirement into smaller requirements.
- `escalate_human` — analysis written, no further automation; surfaces in the UI with the recovery agent's diagnosis.
- `mark_unrecoverable` — recovery agent has determined this cannot succeed from current state (e.g., upstream artifact doesn't exist); plan continues with reduced scope or fails cleanly with diagnostic.

Closed action set keeps the recovery agent inside an approved blast radius. New actions require ADR addendum + payload registration.

### Three guardrails

1. **No nested recovery.** A recovery agent's failure escalates straight to the next layer (phase-local → coordinator → human). No recovery-of-recovery loops. This is the meta-wedge that eats budget.
2. **Hard timeout per recovery attempt.** Default 5 minutes wall, regardless of model. Recovery is not a second long-running cycle.
3. **One recovery attempt per wedge per layer.** Phase-local recovery gets one shot; if it fails, escalate to coordinator. Coordinator gets one shot; if it fails, escalate to human. Two attempts at the same layer is just spending tokens to feel busy.

## Plumbing prerequisites

The audit reduced this list substantially. What's actually new:

1. **`RECOVERY_STATES` KV bucket** — durable record of recovery attempts (which layer ran, what action chosen, outcome). Reconciled on startup like other manager state.
2. **`recovery.>` JetStream subject family** — recovery requests/responses/escalations. Mirrors existing `agent.> / workflow.>` patterns.
3. **`RecoveryAction` payload type** — registered via `component.RegisterPayload`. Carries the closed action-set choice + supporting fields (refined prompt, target model, scope changes, diagnosis text).
4. **Recovery model registry entries** — three new capabilities need defaults in every E2E config that aspires to ship recovery (e2e-gemini.json, e2e-openrouter.json, e2e-claude.json, semspec.json). Defaults same-class as wedged agent.
5. **Coordinator component** — new processor under `processor/coordinator/`. Watches `recovery.escalation.>` from plan-manager + execution-manager + qa-reviewer. Reuses CQRS twofer pattern (cache + KV + triples).

What's **not** new (audit confirmed):

- Trajectory query API. `agentic.query.trajectory` NATS subject already exists, served by semstreams' agentic-loop component. Wiring it into wedge-handling components is implementation work, not infrastructure.
- Trajectory storage. ObjectStore + graph triples already capture full trajectory step content per loop.

## Implementation phasing

**Stage 0 — universal trajectory consumption (ships independently of recovery):**

This is a coverage-gap fix worth doing on its own merits — every wedge-handling component should be able to read the trajectory of the work it's handling, regardless of whether recovery dispatches downstream.

- Wire `agentic.query.trajectory` consumption into execution-manager (on TDD escalation), plan-manager (on revision-loop exhaustion), qa-reviewer, code-reviewer, structural-validator.
- Each component logs the trajectory summary with its escalation/rejection event so operators can diagnose without sidecar bundles.
- No recovery dispatch yet — just observability + future-proofing for stages 1/2.

**Stage 1 — phase-local recovery:**

- Add `execution_wedge_recovery` + `plan_wedge_recovery` capabilities and dispatch into the existing escalation paths.
- RECOVERY_STATES KV + RecoveryAction payload + recovery.> subjects.
- Real-LLM regression: gemini @hard with the same fixture; expect req.3 (Maven coords) to recover-or-escalate-with-analysis instead of escalating with empty `merge_commit`.

**Stage 2 — coordinator:**

- Add `processor/coordinator/`.
- Move QA-fail handling from current ad-hoc path into coordinator.
- Wire cross-phase escalation (phase-local recovery exhausted → coordinator).
- Real-LLM regression: cases where stage 1 escalated should now succeed via coordinator.

Stages may ship as separate ADR addenda + OpenSpec changes. Stage 0 in particular has near-zero risk — it adds reads of an existing data source — and should not block on stages 1/2.

## Consequences

**Positive:**
- Recoverable wedges recover. Adoption gauntlet stops feeling capricious.
- Stage 0 alone gives operators trajectory data on every escalation event without a sidecar — diagnoses go from "spelunk the bundle" to "read the log."
- Recovery diagnosis surfaces in trajectories — every failed run produces actionable analysis instead of `outcome=failed merge_commit=""`.
- Capability-resolve pattern keeps model selection out of code; per-env tuning becomes a config edit. Bumping to a smarter model for recovery is a config knob, not a code change.
- Bounded action set means recovery agents can't go off-leash.

**Negative:**
- New paid LLM calls on every escalation. Same model class by default keeps cost in line with the wedged agent's call, but volume goes up by one call per escalation.
- Three new capabilities × N providers = matrix of model defaults to maintain.
- Recovery agent itself can wedge — guardrails (no nested, hard timeout, one attempt) cap the damage but don't eliminate it.
- Coordinator is another component to design, test, ship. Not free.
- "Escalate to human" path needs UI work to show recovery diagnosis usefully.

**Risks worth tracking:**
- Goodhart on the recovery layer itself — if recovery actions are easy to log as "tried," but no real diagnosis happens, we'll get green metrics with no real recovery. Mitigation: pin specific recovery patterns with real-LLM regression tests (same shape as `TestSoftwareGraphErrorEscapeHatches`).
- Recovery becoming a crutch for poor phase-local checks — if rejection-then-recovery becomes the happy path, the underlying gates rot. Mitigation: track recovery-rate per gate; sustained high recovery on a specific gate is a signal the gate itself needs improvement.
- `bump_model` becoming the default action — if recovery agents reach for the smarter-model action without trying refine/narrow first, we lose the manager-role-leverage thesis. Mitigation: track action distribution per recovery; bump_model dominance is a signal the manager role isn't being used as designed.

## Open questions

These need resolution before Stage 1 ships, not before this ADR is accepted. (Stage 0 has no open questions — it's a straight wiring fix.)

1. **Inline vs async dispatch.** Does plan-manager block on the recovery agent (simple, sequential), or emit `RecoveryRequested` and resume on `RecoveryComplete` (fits the rest of the KV-driven pipeline)? Lean async to match existing patterns, but inline is simpler for Stage 1.
2. **What does the recovery agent see?** Full trajectory + plan + last-failure-feedback + relevant graph entities, or a summarized brief? Full is simpler to implement; summarized is cheaper per call. Lean full for Stage 1, optimize later.
3. **Recovery-fail signal semantics.** When recovery's chosen action (e.g., `refine_prompt`) itself fails on retry, what gets logged where? Need a distinct outcome separate from the original wedge so the lessons pipeline (ADR-033) can learn from recovery patterns.

## References

- `project_capability_resolve_with_override_pattern.md` (memory) — the seam this design uses for model selection.
- `project_dev_wedge_diagnosis_2026_05_03.md` (memory) — pre-reviewer diff gate pattern; precedent for cheap detect-then-react.
- `project_retry_feedback_gap_iter50.md` (memory) — the iter=50 wedge this design directly addresses.
- `processor/lesson-decomposer/trajectory.go` — the only existing semspec consumer of `agentic.query.trajectory`. Stage 0 generalizes its pattern across all wedge-handling components.
- semstreams `vocabulary/agentic/predicates.go:460` — `agent.loop.has_step` predicate; trajectory steps are graph-queryable today.
- semstreams `processor/agentic-loop/graph_writer.go:WriteTrajectorySteps` — proves trajectory storage already exists in ObjectStore + graph triples.
- ADR-029 — local plan revision retry; this ADR generalizes that pattern across phases with manager-role recovery rather than retry budget exhaustion.
- ADR-034 — watch CLI; debug tool only. Production data paths must use `agentic.query.trajectory` directly, not sidecar bundles.
