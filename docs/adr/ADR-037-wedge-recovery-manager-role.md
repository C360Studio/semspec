# ADR-037: Wedge Recovery via Frontier-Model Supervision

**Status:** Proposed (2026-05-10)
**Deciders:** Coby, Claude
**Related:** ADR-029 (plan completeness + retry — local revision loop within plan-phase), ADR-030 (BMAD personas), ADR-033 (lessons pipeline — captures wedge causes post-hoc), ADR-034 (watch CLI — surfaces wedge signals at runtime), ADR-035 (strict-parse — narrows what counts as a wedge vs silent compensation).

## Context

Semspec's quality gauntlet has grown — completeness review (ADR-029), structural validation, code review, QA review, plus persona-side escape hatches (`software.orientation.graph-errors`, `software.orientation.graph-results`). Each gate catches a real failure class. Cumulatively the gauntlet rejects roughly half of frontier-model attempts even when the underlying goal is reachable, because at every stage *some* check rejects *some* generation.

That trade is intentional — semspec is a reliability tool, not a vibes tool. But the tail behavior is bad for adoption: a multi-cycle TDD wedge ends with `stage=escalated outcome=failed merge_commit=""` and no recovery. Sister project semteams has a different finding — when a sufficiently capable model is wired to *intervene* on wedges (read the trajectory, diagnose what the wedged agent missed, refine the task or swap models), recoverable wedges recover. Today semspec has no such layer: when an agent gets stuck, the run dies.

Concrete examples from the 2026-05-10 gemini @hard run:
- **req.3 create-pom** burned 5 TDD cycles fabricating fictional Maven coordinates after `graph_search` returned the correct hint as a `[project]` entity. A recovery agent reading the trajectory could see the unread hint and refine the task.
- **req.5 update-dependencies** hit `iter=50` budget without calling `submit_work` — an agent that recognized the bash-loop pattern could have prompted "submit with obstacle summary."
- **plan-phase round 2 iteration 3** — already has local revision (ADR-029) but no smart-recovery if the third iteration also fails; it just escalates.

Two failure modes that look similar but need different handling:
- **Recoverable wedge** — agent confused; would succeed with refined prompt, different model, or narrowed scope.
- **Unrecoverable failure** — goal not reachable from current state regardless of agent (e.g., fixture's parent pom isn't on Maven Central; req scope contradicts another req).

A recovery layer that doesn't distinguish these will burn tokens trying to fix unrecoverable cases. Recovery must diagnose first, recover second, and "escalate-to-human-with-analysis" must be a valid recovery outcome.

## Decision

Adopt a **hybrid recovery architecture**:

1. **Phase-local recovery** — plan-manager and execution-manager each gain a recovery dispatch on their own escalation paths. Cheap recoveries handled where the state lives.
2. **Coordinator (Site-Manager) component** — handles QA failures and any wedge that survives phase-local recovery. New component, listens on a `recovery.escalation.>` JetStream subject family.

Both phase-local and coordinator recovery use **capability-resolved frontier models** (capability-resolve-with-override pattern, commit `ec8ba13`):

- `plan_wedge_recovery` capability — for plan-manager's escalation path. Failure class: structural plan errors, scope conflicts, reviewer-rejection cascades. Model trait needed: strong instruction-following + multi-round feedback synthesis.
- `execution_wedge_recovery` capability — for execution-manager's escalation path. Failure class: TDD cycle exhaustion, iter=N bash loops, hallucinated paths/coords. Model trait needed: code reasoning + ability to ingest a trajectory and identify *what the agent missed*.
- `coordinator_recovery` capability — for the new coordinator. Failure class: QA fails, cross-phase wedges where plan-side recovery and execution-side recovery both ran. Model trait needed: synthesis across phases, judgment on whether to retry with reshaping vs escalate to human.

Each capability defaults to frontier-tier per provider config (gemini-pro, claude-opus, the strongest available on openrouter, etc.). Per-environment override allows A/B testing recovery models without touching manager code.

### Bounded recovery action set

A recovery agent's submit_work must select from a closed set, not produce arbitrary mutations:

- `refine_prompt` — rewrite the task prompt with explicit context the wedged agent missed (e.g., "graph_search showed `[project] org.sensorhub`; use that").
- `bump_model` — retry with a higher-tier model on the same task.
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

These are real new infrastructure, not refactors:

1. **Trajectory query API** — recovery agents need the wedged agent's tool-call sequence + responses in-process. Today this exists in watch CLI bundles (post-hoc) but not via a query endpoint. Decision pending: graph-gateway, semspec HTTP, or message-logger. Lean toward message-logger since it already indexes by trace_id.
2. **`RECOVERY_STATES` KV bucket** — durable record of recovery attempts (which layer ran, what action chosen, outcome). Reconciled on startup like other manager state.
3. **`recovery.>` JetStream subject family** — recovery requests/responses/escalations. Mirrors existing `agent.> / workflow.>` patterns.
4. **`RecoveryAction` payload type** — registered via `component.RegisterPayload`. Carries the closed action-set choice + supporting fields (refined prompt, target model, scope changes, diagnosis text).
5. **Recovery model registry entries** — the three new capabilities need defaults in every E2E config that aspires to ship recovery (e2e-gemini.json, e2e-openrouter.json, e2e-claude.json, semspec.json).
6. **Coordinator component** — new processor under `processor/coordinator/`. Watches `recovery.escalation.>` from plan-manager + execution-manager + qa-reviewer. Reuses CQRS twofer pattern (cache + KV + triples).

## Implementation phasing

Stage 1 — phase-local recovery only:
- Wire `execution_wedge_recovery` capability + dispatch into execution-manager's escalation path.
- Wire `plan_wedge_recovery` capability + dispatch into plan-manager's `Plan revision loop — retrying` exhaustion path (currently caps at iter=3).
- Trajectory query API + RECOVERY_STATES KV.
- Real-LLM regression: gemini @hard with the same fixture; expect req.3 (Maven coords) to recover or escalate-with-analysis instead of escalating with empty `merge_commit`.

Stage 2 — coordinator:
- Add `processor/coordinator/`.
- Move QA-fail handling from current ad-hoc path into coordinator.
- Wire cross-phase escalation (phase-local recovery exhausted → coordinator).
- Real-LLM regression: cases where stage 1 escalated should now succeed via coordinator.

Stages may ship as separate ADR addenda + OpenSpec changes.

## Consequences

**Positive:**
- Recoverable wedges recover. Adoption gauntlet stops feeling capricious.
- Recovery diagnosis surfaces in trajectories — every failed run produces actionable analysis instead of `outcome=failed merge_commit=""`.
- Capability-resolve pattern keeps model selection out of code; per-env tuning becomes a config edit.
- Bounded action set means recovery agents can't go off-leash.

**Negative:**
- New paid LLM calls on every escalation (frontier tier — expensive per call). Token budget per failed cycle goes up.
- Three new capabilities × N providers = matrix of model defaults to maintain.
- Recovery agent itself can wedge — guardrails (no nested, hard timeout, one attempt) cap the damage but don't eliminate it.
- Coordinator is another component to design, test, ship. Not free.
- "Escalate to human" path needs UI work to show recovery diagnosis usefully.

**Risks worth tracking:**
- Goodhart on the recovery layer itself — if recovery actions are easy to log as "tried," but no real diagnosis happens, we'll get green metrics with no real recovery. Mitigation: pin specific recovery patterns with real-LLM regression tests (same shape as `TestSoftwareGraphErrorEscapeHatches`).
- Recovery becoming a crutch for poor phase-local checks — if rejection-then-recovery becomes the happy path, the underlying gates rot. Mitigation: track recovery-rate per gate; sustained high recovery on a specific gate is a signal the gate itself needs improvement.

## Open questions

These need resolution before Stage 1 ships, not before this ADR is accepted:

1. **Inline vs async dispatch.** Does plan-manager block on the recovery agent (simple, sequential), or emit `RecoveryRequested` and resume on `RecoveryComplete` (fits the rest of the KV-driven pipeline)? Lean async to match existing patterns, but inline is simpler for Stage 1.
2. **Trajectory query endpoint placement.** message-logger, graph-gateway, or new semspec HTTP route? Lean message-logger.
3. **What does the recovery agent see?** Full trajectory + plan + last-failure-feedback + relevant graph entities, or a summarized brief? Full is simpler to implement; summarized is cheaper per call. Lean full for Stage 1, optimize later.
4. **Recovery-fail signal semantics.** When recovery's chosen action (e.g., `bump_model`) itself fails on retry, what gets logged where? Need a distinct outcome separate from the original wedge so the lessons pipeline (ADR-033) can learn from recovery patterns.

## References

- `project_capability_resolve_with_override_pattern.md` (memory) — the seam this design uses for model selection.
- `project_dev_wedge_diagnosis_2026_05_03.md` (memory) — pre-reviewer diff gate pattern; precedent for cheap detect-then-react.
- `project_retry_feedback_gap_iter50.md` (memory) — the iter=50 wedge this design directly addresses.
- ADR-029 — local plan revision retry; this ADR generalizes that pattern across phases with smarter model choice.
- ADR-034 — watch CLI; the trajectory data the recovery agent will consume is already captured at runtime.
