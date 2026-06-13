# Recovery-Path Dependent Invalidation (P3)

Status: PROPOSED — design for human review before coding
Author: architect
Date: 2026-06-12
Branch: fix/shared-file-assembly-conflict (follows slices 1-4a, commit `5c9e2024`)
Depends on: docs/design/dependson-branch-derivation.md (the branch-derivation fix)

## 1. Problem statement

The branch-derivation fix (slices 1-4a) makes a dependent requirement's branch
*derive* from its prerequisite's branch, and gates dispatch so a dependent only
dispatches after its prerequisite completes (`filterByBranchPrereqCompletion`).
That closes the **forward** race. It does NOT handle the **backward** case: a
prerequisite that re-runs *after* it already completed leaves its dependents
forked from the prerequisite's stale state.

### Verified trace (QA-recovery on a prerequisite)

1. Owner `a1` completes → `semspec/requirement-a1` carries its commits.
   Dependent `b1` dispatches (gate satisfied), forks from `semspec/requirement-a1`,
   completes. `b1`'s branch now contains `a1`'s shared-file edits + its own.
2. Plan reaches QA; QA returns `needs_changes`. An accepted recovery
   `PlanDecision` fires the QA-recovery resume
   (`requirement-executor/awaiting_recovery.go:463` →
   `recover_completed.go:resumeTerminalForRecoveryLocked`).
3. The resume reopens `a1`'s owned Stories `complete→ready`
   (`recover_completed.go:155`) and **deletes + recreates `a1`'s branch from
   `"HEAD"`** (`awaiting_recovery.go:239-247`), then re-runs the dev loop. `a1`'s
   branch ref now holds DIFFERENT commits.
4. `a1` re-completes → plan-manager re-fires the orchestrator
   (`execution_events.go:143`). But `b1` is still at stage `completed`, so
   `filterReadyRequirements` **skips it** (`dag.go:59-61`,
   `if complete[req.ID] { continue }`). `b1` is never re-derived.
5. `b1`'s branch is now **stale** — forked from `a1`'s OLD commits, missing
   `a1`'s new shared-file edits. Re-assembly merges `a1` (new) and `b1` (old):
   if both touched the shared file → conflict; recovery cannot fix it (nothing
   re-derives `b1`) → bounded recovery loops → budget exhaustion → `rejected`.

### Two root causes (file:line)

- **R1 — no backward invalidation.** Reopening `a1` resets `a1`'s own stories +
  branch but nothing resets `a1`'s dependents. The dispatch gate prevents
  premature *forward* dispatch; it cannot un-complete an already-complete
  dependent.
- **R2 — the orchestrator's completed-set is additive-only.** Both
  `reconcileCompletedRequirements` (`component.go:589`) and the live consumer
  (`req_completion_watcher.go:95`) only `Set` into `completedReqs`; neither ever
  evicts. So even if `b1`'s KV entry were reset, the orchestrator would still
  believe `b1` is complete and skip it. `reconcileCompletedRequirements` runs on
  every dispatch sweep, so it is the natural place to make the set authoritative.

### Related bug found alongside (R3)

- **R3 — reopened owner loses its own derivation.** `awaiting_recovery.go:243`
  recreates the reopened branch from `"HEAD"`. With branch derivation, an owner
  that itself has prerequisites must recreate from its *resolved base*, not
  `"HEAD"` — otherwise a reopened mid-chain owner drops the prereq edits it was
  derived from. Pre-existing recovery behavior, now inconsistent with the model.

## 2. Fix overview (3 coordinated pieces)

1. **Reopen cascade (R1):** when a requirement is reopened for recovery,
   transitively reset every requirement whose branch derives from it.
2. **Authoritative completed-set (R2):** evict a requirement from
   `completedReqs` when its KV stage is no longer `completed`, so a reset
   dependent is actually re-dispatched.
3. **Owner re-derivation on recovery (R3):** recreate the reopened branch from
   its resolved base, not `"HEAD"`.

With all three: reopening `a1` resets the `{b1, …}` subtree → orchestrator's
completed-set drops them on the next sweep → `filterReadyRequirements` re-admits
them → `filterByBranchPrereqCompletion` holds each until its (re-running)
prerequisite re-completes → each re-derives from the rebuilt branch. Convergence
is conflict-free again, by the same construction as the initial run.

## 3. Mechanism — the dependent subtree (Q1)

A requirement `r` is a **direct branch-dependent** of `o` iff
`o ∈ ResolveRequirementBranchPrereqs(r, stories)`. The set to invalidate when
`o` is reopened is the **transitive closure** over that reverse relation across
`plan.Requirements` (a dependent's dependents are also stale). Pure function:

```
func DependentBranchSubtree(reopened string, reqs []Requirement, stories []Story) []string
  // BFS/DFS over reverse edges r -> {p : p ∈ ResolveRequirementBranchPrereqs(r, stories)}
  // returns every r reachable from `reopened` (excluding `reopened`), deterministic (sorted)
```

Lives in `workflow/` next to `ResolveRequirementBranchPrereqs` (same inputs,
pure, offline-testable). Cycle-safe via a visited set (the derived DAG is
acyclic by construction, but guard anyway).

## 4. Reset of each dependent (Q2)

For each dependent `d` in the subtree:

- **Reset execution state:** `execution.mutation.req.reset` (deletes the KV
  entry — `mutations.go:137`). The orchestrator's next sweep re-creates it as
  `pending` via `req.create` (it is no longer in EXECUTION_STATES). Reusable
  sender already exists: `plan-manager/http.go:1170 sendReqReset`.
- **Delete its branches:** `semspec/requirement-<d>` AND `semspec/reqbase-<d>`
  (best-effort, 404-benign) so re-dispatch recreates from the rebuilt base
  (`initReqExecution`'s `ErrBranchExistsAtDifferentBase` path also covers this
  if a branch lingers).

**Live-dependent case:** if `d` is mid-execution (not yet `completed`) when `o`
is reopened, `d` is building on a now-stale base and must also be reset. This
requires terminating any live `d` exec. Two options:
- (a) Reset the KV entry regardless; the live exec's next mutation/finish loses
  the claim (its key was deleted) and is abandoned. Simplest; relies on
  reset-detection in the executor's mutation path.
- (b) Emit an explicit cancel for `d` before reset.
RECOMMENDATION: (a) for the first cut, with a test pinning that a reset-while-
live entry does not resurrect; escalate to (b) only if (a) proves racy.

## 5. Placement (Q3)

The reopen is triggered by an accepted recovery `PlanDecision`, consumed by the
requirement-executor (`awaiting_recovery.go:463`). Two placements for the
dependent cascade:

- **(A) plan-manager drives it [RECOMMENDED].** plan-manager owns plan-level
  coordination, PlanDecisions, `sendReqReset`, and the orchestrator re-fire. On
  the same recovery-accept it computes `DependentBranchSubtree(owner, …)` and
  resets each dependent. Keeps "a component owns its own entity lifecycle":
  resetting *other* requirements is a plan-level action, not something one
  requirement's executor handler should reach across to do.
- **(B) executor inline.** `resumeTerminalForRecoveryLocked` already loads the
  plan (for the story reopen) and could reset dependents there. Fewer moving
  parts, but a requirement's recovery handler mutating sibling requirements'
  state is a layering smell.

DECISION (revised during implementation — chose B): **(B) executor-inline**, for
a RACE-CORRECTNESS reason the original recommendation missed.

The race in (A): plan-manager resets a dependent `b1` at accept time, but the
*owner* `a1` is reopened only later (the executor consumes the async
`.accepted` event). In the window between `b1`'s reset and `a1`'s reopen, `a1`
still shows `completed`, so if any orchestrator sweep fires (another req
completes, a plan KV watch), `filterByBranchPrereqCompletion` sees `a1` complete
and re-dispatches `b1` against `a1`'s pre-rebuild branch — stale again.

(B) closes the window: the cascade runs in `resumeTerminalForRecoveryLocked`
AFTER `resumeFromRecoveryLocked` has moved `a1` off `completed`
(`sendReqPhase → decomposing`, a confirmed KV write). Because the orchestrator
reconciles its completed-set BEFORE gating on every sweep (R2), once `a1` shows
non-completed the gate HOLDS `b1` until `a1` re-completes. So `b1` can never
re-dispatch against the stale base. The executor is the requirement-execution
domain owner and already loads the plan for the story reopen, so the "layering
smell" is mild and outweighed by the race-safety. `sendReqReset` was added to the
executor's mutation senders (it already drives all req-exec state via mutations).

## 6. Authoritative completed-set (Q4 / R2)

`reconcileCompletedRequirements` runs every sweep and is the single rebuild
point. Make it authoritative: build the completed set from the current KV scan
and EVICT any cached `completedReqs` entry whose KV stage is no longer
`completed` (or whose entry is absent). The live consumer
(`req_completion_watcher.go`) stays Set-only (it only ever sees completions);
the per-sweep reconcile is what corrects drift.

IMPLEMENTATION NOTES (revised):
- Eviction is **global** (the scan covers every `req.*` key across all plans, and
  `completedReqs` is keyed by `RequirementID`), NOT plan-scoped. This is safe
  because `RequirementExecutionKey` is `req.<slug>.<reqID>` and reqIDs are
  slug-qualified, so there is no cross-plan ID collision. Per-trigger scans are
  cheap relative to a dispatch sweep; if reqID generation ever drops the slug
  qualifier, scope the scan to the trigger's slug.
- A partial scan must NOT evict. A single per-key `Get`/unmarshal failure (or a
  context-deadline mid-scan) makes the completed set an undercount; evicting on
  it would drop still-completed reqs from the shared cache and wrongly BLOCK
  their dependents in the same sweep. A `scanClean` flag gates eviction — a
  partial scan is treated like the empty-keys early return (skip eviction, the
  next clean sweep corrects).

## 7. Owner re-derivation on recovery (Q5 / R3)

`awaiting_recovery.go:243` `CreateBranch(exec.RequirementBranch, "HEAD")` →
recreate from the owner's resolved base. The executor does not currently carry
the resolved base into the recovery path; options:
- carry `BaseBranch` onto the in-memory `requirementExecution` (it is already on
  `workflow.RequirementExecution` from slice 1) and use it here; OR
- recompute it at reopen from the plan (needs Requirements+Stories, already
  loaded for story reopen).
RECOMMENDATION: carry `exec.BaseBranch` (set it in `initReqExecution` from the
KV entry) and use `selectReqBranchBase(planBranch, exec.BaseBranch)` here, so the
recovery recreate mirrors the initial create. Scope check: confirm this does not
regress the root-requirement case (empty base → HEAD, unchanged).

## 8. Idempotency / concurrency (Q6)

- `DependentBranchSubtree` is pure + deterministic (sorted).
- Reset is idempotent: ReqReset on an absent key is a no-op; branch deletes are
  404-benign.
- Re-entrancy: a recovery cycle that reopens the same owner twice recomputes the
  same subtree and re-resets (harmless).
- Budget: dependent re-runs consume recovery budget. The existing
  `countAcceptedRecoveryCyclesForReq` gate (`awaiting_recovery.go:449`) bounds
  the OWNER; confirm the dependent re-runs are bounded too (they re-run as fresh
  dispatches, not recovery cycles — acceptable, but note it).

## 9. Interaction with slices 1-4a

- Reuses `filterByBranchPrereqCompletion` (the forward gate) for sequencing the
  re-dispatched subtree — no new gate.
- Reuses slice-3 owner-only assembly + slice-4a conflict surfacing unchanged. If
  a residual conflict survives a recovery (e.g. truly-undeclared shared file),
  slice 4a still names the path; this fix removes the *staleness-induced* class.
- Complements 4b (detect-and-repair) but is independent: this fixes
  recovery-driven staleness; 4b fixes initial-dispatch undeclared overlap.

## 10. Rejected alternative — base-SHA drift detection (design §6 as written)

The original §6 sketch: record each dependent's base SHA; on re-fire, recompute
and re-dispatch when it drifted. Rejected because: (1) it still needs the R2
authoritative-completed-set fix to re-admit the dependent; (2) it is reactive
(detect-after-the-fact) and adds SHA storage + comparison + a drift-race window;
(3) cascade-invalidation is deterministic and reuses the existing reset + gate
machinery. SHA-diff offers no advantage here.

## 11. Offline test plan (LLM-free)

- **A. `DependentBranchSubtree`** (`workflow`): a1←b1←c1 chain → reopening a1
  returns {b1, c1}; diamond (b1,b2 both on a1; d1 on b1,b2) → {b1,b2,d1};
  no-Stories / no-dependents → empty; cycle-guard.
- **B. completed-set eviction** (`scenario-orchestrator`): seed `completedReqs`
  with b1; KV scan shows b1 stage=`decomposing` → after reconcile, b1 NOT in the
  completed set (re-admitted by `filterReadyRequirements`).
- **C. plan-manager reopen cascade** (`plan-manager`): on recovery-accept for
  owner a1 with dependents {b1,c1}, assert ReqReset sent for b1+c1 and their
  branches deleted (stub captures the mutations); a1-only plan → no resets.
- **D. owner re-derivation (R3)** (`requirement-executor`): `resumeFromRecovery`
  with `exec.BaseBranch="semspec/requirement-x"` recreates from that base, not
  `"HEAD"` (extend the stale-branch stub to record the recreate base).
- **E. end-to-end (offline)** (`plan-manager` or `cmd/sandbox` real-git):
  a1→b1 share a file; complete both; reopen a1 (new a1 commits); assert b1 is
  reset, re-derives from the moved a1, and re-assembly is clean (no conflict).
- **F. live-dependent reset** (`requirement-executor`): a reset-while-live
  dependent does not resurrect / re-mark complete on a stale base.

## 12. Open questions / product calls

- **Q-live:** option (a) abandon-on-key-deleted vs (b) explicit cancel for live
  dependents (§4). Recommended (a) first.
- **Q-budget:** do dependent re-runs count against the plan's recovery budget,
  or are they "free" fresh dispatches (§8)? Recommended: free, but logged.
- **Q-placement:** plan-manager (A) vs executor-inline (B) (§5). Recommended (A).
