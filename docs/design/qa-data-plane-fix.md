# QA Gate Data-Plane Fix — Implementation Design

Status: Phase A + Phase B IMPLEMENTED (branch fix/qa-data-plane-assemble-before-qa, commits 5416bfba + 3183a3be). Remaining: e2e regression scenario (depends on #151 mock-coder fixture) + stale qa-runner doc cleanup.
Author: architect
Scope: `processor/plan-manager`, `processor/qa-reviewer`, `processor/requirement-executor`, `cmd/sandbox`, `workflow`
Related: ADR-045 (BMAD story gate + operator-executed tiers), `docs/audit/task-11-worktree-invariants.md` (invariant B1), CLAUDE.md (3-layer manager pattern, plan-manager single-writer)

## Problem statement

The release-gate QA stage evaluates a workspace that contains **none** of the
per-requirement implementation, and the QA-rejection recovery path is a **no-op**
for M:N-complete Stories. Both are confirmed by prior audit and reproduced on the
2026-06-12 mavlink-hard run #2 (reqs 3-4 stranded in unmerged branches → Murat
correctly `needs_changes` → recovery skips because Stories are `complete`).

### Problem 1 — QA inspects pre-implementation `main` HEAD

- Per-requirement work lands on `semspec/requirement-<id>` branches, created at
  `processor/requirement-executor/req_watcher.go:152` (base = `planBranch` or
  `HEAD`, never off `main`).
- `assembleRequirementBranches` (`processor/plan-manager/plan_merge.go:29`) merges
  those into `semspec/plan-<slug>` — but is called ONLY in the `QAVerdictApproved`
  arm at `processor/plan-manager/mutations.go:1368`, i.e. **after** the verdict.
- Release-gate Murat dispatches with `Metadata["task_id"]="main"`
  (`processor/qa-reviewer/component.go:549`). Sandbox resolves `task_id=main` →
  `repoPath` (main repo root) via `worktreeFor` (`cmd/sandbox/server.go:68-72`).
  So Murat inspects `main` HEAD — zero per-requirement commits.
- Unit-QA runner runs `execCommand(ctx, h.srv.repoPath, evt.TestCommand, …)`
  (`cmd/sandbox/qa_subscriber.go:186-187`) — also `main` HEAD. `go test` runs
  against the pre-implementation baseline. **`qa_level=unit` is meaningless today.**

The per-Story Murat gate during execution is correct because it reviews on a
worktree off `exec.RequirementBranch`; that asymmetry is what exposed the bug.

### Problem 2 — QA-rejection recovery is a no-op for M:N-complete Stories

- On release-gate `needs_changes`/`rejected`, plan-manager fires
  `RecoveryRequested` (`mutations.go:1424`) with `AffectedRequirementIDs` =
  active reqs (`collectActiveRequirementIDsForRecovery`, `mutations.go:1056`).
- The resume path `resumeTerminalForRecoveryLocked`
  (`recover_completed.go:139`) → `resumeFromRecoveryLocked`
  (`awaiting_recovery.go:166`) → `dispatchSynthesizerLocked`
  (`component.go:1004`) → `dispatchCurrentStoryLocked` (`component.go:1067`).
- `dispatchCurrentStoryLocked` short-circuits at `component.go:1080` when
  `story.Status == StoryStatusComplete` ("Story already complete (M:N coverage
  by prior requirement); skipping"). **Nothing resets `Story.Status` out of
  `complete`**, so recovery re-marks the requirement complete without doing
  work and bounces back to QA with identical state.
- Contrast: per-Story FIXABLE rejection (`startFixableRetryLocked`,
  `component.go:1763`) works because the Story is still `executing`/`failed`
  at that altitude.

## Verified mechanism facts (read before reviewing the design)

| Fact | Evidence |
| --- | --- |
| `MergeBranches` target uses `checkout -B <target> <base>` | `cmd/sandbox/server.go:1272` — creates-or-resets target from base; idempotent for plan-manager's own re-calls, NOT against human edits (comment 1264-1271) |
| `MergeBranchesRequest` carries `Base` (empty = HEAD) | `tools/sandbox/client.go:323-328`; server reads it `server.go:1243-1246` |
| Merge endpoint serializes on `repoMu`, restores HEAD after | `server.go:1249-1250,1262,1299` |
| `MergeBranches` returns descriptive `ErrMergeBranchesConflict` naming the conflicting branch | `plan_merge.go:75-78`, sentinel `tools/sandbox/client.go:348` |
| Story DAG already legalizes `complete → ready` | `workflow/story_task.go:177-178` |
| Story-status mutation enforces `CanTransitionTo` and re-fires orchestrator on terminal | `plan-manager/mutations.go:841-871` |
| `ClaimStoryStatus(ctx, nc, slug, storyID, target, logger)` is the cross-component story-status setter | `workflow/kv_helpers.go:90`, used at `requirement-executor/component.go:1093,2049,2394` |
| `dispatchSynthesizerLocked` loads the plan **fresh from KV** every call (`loadPlanFromKV`, line 1005) and reads `story.Status` from it | `component.go:1005,1019,1080` — so a KV story-status reset before resume is visible to the skip check |
| `CurrentStoryIdx`/`SortedStoryIDs` are NOT reset on recovery resume (`resumeFromRecoveryLocked` resets DAG/node state only) | `awaiting_recovery.go:208-229`; `dispatchSynthesizerLocked` only repopulates when `len(SortedStoryIDs)==0` (line 1018) |
| `Requirement.Status` stays `active` through execution (lifecycle field, not execution status) | `workflow/types.go:1430`, only set `deprecated`/`superseded` in http handlers; execution state lives in EXECUTION_STATES |
| `worktreeFor(taskID)`: `"main"`→`repoPath`; else `worktreeRoot/<taskID>` (empty if absent) | `cmd/sandbox/server.go:68-77` |
| qa-reviewer cleanup site is `handleLoopCompletion` (`component.go:586`) | confirmed; stale-loop + stale-plan guards already live there |
| `CreateWorktree(ctx, taskID, WithBaseBranch(branch))` → `git worktree add -b agent/<taskID> <path> <base>` | `tools/sandbox/client.go:143-152` |

---

## Design overview

Two phases, each independently shippable and testable.

- **Phase A — Assemble-before-QA + single workspace-selection mechanism.**
  Move branch assembly to the `implementing → ready_for_qa` / review transition,
  and route BOTH the unit-QA runner and the release-gate Murat loop to the
  assembled plan branch via ONE mechanism (a dedicated QA worktree).
- **Phase B — Recovery resets covered Story status.** On QA-rejection recovery,
  reset the covering Stories from `complete → ready` for the M:N owner so
  `dispatchCurrentStoryLocked` re-does work instead of skipping.

The phases are decoupled: Phase A makes QA evaluate real code (so its verdict is
trustworthy); Phase B makes a `needs_changes` verdict actually drive rework.
Shipping A alone already converts "QA always sees green baseline" into "QA sees
real code and correctly rejects" — which surfaces Problem 2 deterministically,
so A should land first.

---

## Phase A — Assemble before QA, one workspace-selection mechanism

### A1. WHEN to assemble

**Decision: assemble at the implementing-convergence transition, inside
`checkPlanConvergence`, in the `failedCount == 0` arm, BEFORE the status flips
to the QA target.** Concretely a new step between `targetForQALevel`
(`execution_events.go:301`) and `setPlanStatusCached` (`execution_events.go:309`).

Rationale:
- It is the single funnel where a plan leaves `implementing` with all
  requirements terminal-success. Both QA sub-paths (unit `QARequestedEvent` and
  synthesis qa-reviewer dispatch) originate downstream of it
  (`publishQARequestIfNeeded` at line 323; qa-reviewer claims `ready_for_qa`).
- Assembling here guarantees the assembled branch exists BEFORE the
  `QARequestedEvent` fires (unit) and BEFORE qa-reviewer transitions
  `ready_for_qa → reviewing_qa` (synthesis).
- It keeps plan-manager the single writer of plan state and of the assembled
  branch (matches CLAUDE.md single-writer invariant).

**Hook point (exact):** `processor/plan-manager/execution_events.go`, inside
`checkPlanConvergence`, the `if failedCount == 0 {` block (currently lines
298-324). Insert assembly after `target := c.targetForQALevel(...)` (line 301)
and before `setPlanStatusCached(ctx, plan, target)` (line 309), gated on
`target == StatusReadyForQA` (i.e. QA is actually going to run — `none`/gated
paths that go straight to `complete`/`awaiting_review` do NOT need a pre-QA
assemble; they keep using the approve-time assemble — see A2/A4).

Pseudo-shape (NOT code — for review):
```
if failedCount == 0:
    level  := plan.EffectiveQALevel()
    target := c.targetForQALevel(level, plan, slug)
    if target == StatusReadyForQA:
        if err := c.assembleRequirementBranches(ctx, plan); err != nil:
            routeAssemblyConflict(ctx, plan, err)   // A4 — does NOT proceed to QA
            return
        // plan.AssembledBranch / AssembledMergeCommit now populated
    setPlanStatusCached(ctx, plan, target)
    publishQARequestIfNeeded(ctx, plan)             // unit event now carries the branch (A3)
    return
```

`assembleRequirementBranches` already populates `plan.AssembledBranch` /
`plan.AssembledMergeCommit` on the in-memory plan; `setPlanStatusCached` persists
it (write-through to KV, same as today's approve-time path).

> PRODUCT CALL #1 (low stakes): the assemble-before-QA call writes the assembled
> branch + commit onto the plan record at `ready_for_qa`. That is strictly more
> information earlier and is backward-compatible. No new status needed for the
> happy path. Flagged only so the reviewer is aware plan records now carry an
> assembled branch from `ready_for_qa` onward, not just from `complete`.

### A2. What the QA-approve-time call becomes

**Decision: keep the `mutations.go:1368` call as an idempotent safety-net
reconcile; do NOT remove it.**

Rationale:
- `MergeBranches` is idempotent via `checkout -B <target> <base>`
  (`server.go:1272`): a second call discards the first attempt's partial merges
  and re-merges from base. So calling assemble again at approve-time recomputes
  the same branch (assuming no new req branches landed — which is the case at
  approve-time because execution is already terminal).
- Keeping it covers the cold-path where a plan reached `complete` via a route
  that skipped A1 (e.g. force-complete, level transitions, or a future code path
  that lands at approve without a `ready_for_qa` hop). Removing it would silently
  regress B1 on those routes.
- Cost is one extra merge round-trip on the already-assembled branch — cheap and
  serialized by `repoMu`.

**Idempotency caveat to document in-code:** the comment at `mutations.go:1355-1367`
about an operator cherry-picking a conflict fix onto the assembled branch between
a failed save and a retry still applies, and now also applies between the
ready_for_qa assemble and the approve-time reassemble. That is an existing Phase-5
reconciliation gap, NOT introduced here — call it out in the comment but do not
attempt to fix it in this PR.

### A3 + A2(workspace). ONE workspace-selection mechanism for release-gate AND unit QA

Three candidate mechanisms were considered for getting QA to inspect the
assembled branch:

- **(A) Checkout the assembled branch into `repoPath` (main HEAD) before
  dispatch.** REJECTED. It mutates the shared main working directory that every
  other `task_id=main` consumer (planner, plan-reviewer, future read-only agents)
  resolves to. Concurrency: a plan-reviewer or a second plan's QA running
  concurrently would observe a drifted HEAD; `repoMu` only serializes individual
  sandbox endpoints, not the *logical* "main is on plan-X's branch" window.
  Restart risk: if semspec restarts mid-QA, main HEAD is left on the assembled
  branch with no owner to restore it. This is exactly the failure class
  `needsReconciliation` exists to prevent. Reject.

- **(C) Add a `branch` field the bash tool checks out per call.** REJECTED.
  Bash tool reads only `task_id` from call metadata (`tools/bash/executor.go`).
  Teaching it to `git checkout <branch>` per call mutates whatever working
  directory `task_id` resolved to (for `task_id=main`, that is `repoPath` again
  → same shared-HEAD hazard as A). It also makes "which branch am I on" stateful
  per-bash-call rather than per-workspace, which races against the unit runner
  and any parallel tool calls in the same loop. Reject.

- **(B) Create a dedicated QA worktree on the assembled branch and address it by
  its own `task_id`.** CHOSEN. A worktree is an isolated checked-out tree at
  `worktreeRoot/<taskID>`; it never touches main HEAD, survives restart as a
  plain directory, and is the exact mechanism the per-Story Murat gate already
  uses successfully. Both the unit runner and the release-gate loop point at the
  SAME worktree → one mechanism, not two.

**Chosen mechanism (B) — single QA worktree per plan, owned by plan-manager:**

1. **plan-manager creates the QA worktree at assemble time (A1).** Immediately
   after a successful `assembleRequirementBranches`, plan-manager calls
   `sandbox.CreateWorktree(ctx, qaTaskID(plan), WithBaseBranch(plan.AssembledBranch))`
   where `qaTaskID(plan)` is a deterministic id, e.g. `qa-<slug>`. Determinism
   matters for idempotency (A6) and for the unit runner and release loop to
   converge on the same path without extra plumbing.
   - `CreateWorktree` → `git worktree add -b agent/qa-<slug> <path> <assembledBranch>`
     (`client.go:143-152`). The `agent/` prefix is added server-side; the
     worktree directory key is `qa-<slug>`, so `worktreeFor("qa-<slug>")`
     resolves it.
   - PRODUCT CALL #2 (mechanical): confirm the worktree branch (`agent/qa-<slug>`)
     vs the assembled branch (`semspec/plan-<slug>`) relationship is acceptable.
     The worktree is a throwaway checkout *of* the assembled branch; QA reads it,
     QA does not write durable artifacts to it. The durable reviewable artifact
     remains `semspec/plan-<slug>` (B1). If a future requirement wants Murat to be
     able to *commit* fixes, that is a separate decision — out of scope here.

2. **Unit-QA runner uses the worktree (A3).** Add `Workspace` (worktree task_id)
   to `QARequestedEvent` (`workflow/subjects.go:116`):
   ```
   type QARequestedEvent struct {
       Slug           string
       PlanID         string
       Mode           QALevel
       Workspace      string   // NEW: sandbox task_id of the QA worktree; "" → legacy repoPath
       TestCommand    string
       TimeoutSeconds int
       TraceID        string
   }
   ```
   - Populate it in `publishQARequestIfNeeded`
     (`execution_events.go:404-412`): `Workspace: qaTaskID(plan)`.
   - In `cmd/sandbox/qa_subscriber.go:185-187`, replace the hardcoded
     `h.srv.repoPath` with `h.srv.worktreeFor(evt.Workspace)`, falling back to
     `repoPath` when `Workspace == ""` (legacy / empty) OR when `worktreeFor`
     returns `""` (worktree missing — degrade to today's behavior but log a WARN
     so the gap is visible rather than silent). This is the SAME
     workspace-selection primitive the bash tool uses, so there is exactly one
     resolution function (`worktreeFor`) in play.
   - `QARequestedEvent.Validate()` must accept the new optional field (validate
     it as a safe task_id token when present — no path traversal; reuse the
     existing slug-validation discipline).

3. **Release-gate Murat loop uses the worktree (A2-workspace).** In qa-reviewer
   `dispatchReviewer` change the metadata at
   `processor/qa-reviewer/component.go:549` from `"task_id": "main"` to
   `"task_id": qaTaskID(plan)` (derived from `plan.Slug`; qa-reviewer already
   has the plan). Now Murat's bash inspection resolves to the assembled-branch
   worktree instead of main HEAD. No sandbox endpoint change is needed — the
   worktree was created by plan-manager at A1, and `worktreeFor` already resolves
   non-"main" ids.
   - **Fallback:** if `worktreeFor(qa-<slug>)` returns `""` (worktree missing —
     e.g. created before this code shipped, or pruned), qa-reviewer should detect
     it is absent. Two options:
     (i) qa-reviewer best-effort creates the worktree itself from
     `plan.AssembledBranch` before dispatch (resilient, but introduces a second
     creator → mild single-writer smell); or
     (ii) qa-reviewer falls back to `task_id=main` with a WARN (safe, but
     re-exposes Problem 1 on the cold path).
     RECOMMEND (i) guarded so plan-manager remains the primary creator and
     qa-reviewer only heals a missing worktree (idempotent: `CreateWorktree` on an
     existing path is a benign error the client can treat as success — verify the
     server's existing-worktree response shape before relying on it).
     > PRODUCT CALL #3: pick (i) self-heal vs (ii) main-fallback-with-WARN. (i)
     > is more correct; (ii) is simpler and keeps a single creator. I lean (i).

4. **Cleanup hook.** The QA worktree must be deleted once the plan leaves the QA
   gate, or worktrees accumulate (one per plan, plus stale ones across recovery
   re-runs). Cleanup sites:
   - **Primary:** plan-manager, when the plan transitions OUT of the QA states
     (`reviewing_qa`/`ready_for_qa`) to a terminal-of-QA state (`complete`,
     `awaiting_review`, `rejected`, `archived`). Add a
     `sandbox.DeleteWorktree(ctx, qaTaskID(plan))` best-effort call in:
     - the `QAVerdictApproved` arm (`mutations.go:1380` region, after status set),
     - the `QAVerdictNeedsChanges/Rejected` arm (`mutations.go:1392` region),
     - the archive path (alongside `pruneRequirementBranches`,
       `plan_merge.go:103`).
     Best-effort + 404-tolerant exactly like `pruneRequirementBranches`
     (`plan_merge.go:113-130`).
   - **Secondary (defense-in-depth):** qa-reviewer `handleLoopCompletion`
     (`component.go:586`) already runs on every QA loop terminal and already has
     stale-plan guards. It is a natural place to delete the worktree once the
     verdict is in — BUT the worktree may still be needed for a recovery re-run
     (Phase B re-runs execution, then QA re-dispatches → needs a fresh worktree
     anyway). Because Phase B re-assembles and re-creates the worktree on the next
     convergence (A6), it is SAFE for `handleLoopCompletion` to delete the
     worktree after producing a verdict. RECOMMEND putting cleanup in
     plan-manager's verdict arms (primary) and NOT in qa-reviewer, to keep a
     single owner; list `handleLoopCompletion` only as the fallback if we observe
     leaks.
   - Requires a `DeleteWorktree` client method + sandbox endpoint. **VERIFY**
     `cmd/sandbox/server.go:86` already binds `DELETE /worktree/{taskID}`
     (`handleDeleteWorktree`) — it does (confirmed in RegisterRoutes). Confirm the
     `tools/sandbox` client exposes a `DeleteWorktree`; if not, add the thin
     wrapper (the endpoint exists, only the client method may be missing).

**Why one mechanism, not two:** both QA sub-paths now resolve their working
directory through `Server.worktreeFor(<qa-task-id>)`. The unit runner gets the
id via the new `QARequestedEvent.Workspace` field; the release loop gets it via
qa-reviewer metadata. Same worktree, same branch, same resolution function. A
future heavier-tier executor would use the same id.

### A4. Conflict handling PRE-QA

Today, an assembly conflict at approve-time leaves the plan in its current state
with `LastError` and the mutation fails (`mutations.go:1368-1379`). Pre-QA we are
inside `checkPlanConvergence`, not a mutation handler, and we must NOT silently
proceed to QA on a failed merge (QA would re-inspect the unmerged baseline → the
exact bug we are fixing).

**Decision: on pre-QA assembly conflict, route to RECOVERY via the existing
`RecoveryRequested` mechanism, classified as `plan_conflict`. Do NOT invent a new
plan status.** Keep the plan in `implementing` (it never advanced to
`ready_for_qa`), set `LastError`/`LastErrorAt` for the UI stall signal, and emit
`RecoveryRequested` so an architecture_revise / plan-reconcile decision can be
generated.

Routing detail:
- `assembleRequirementBranches` already wraps the sentinel
  (`errors.Is(err, sandbox.ErrMergeBranchesConflict)`, `plan_merge.go:75`). The
  new `routeAssemblyConflict` helper branches on that sentinel:
  - `ErrMergeBranchesConflict` → `RecoveryRequested` with an EscalationReason /
    classification that Phase 5's `infra_health` vs `plan_conflict` keying maps to
    **plan_conflict** (two requirements edited the same file — a planning-scope
    problem, fixable by architecture_revise / re-partition, NOT an infra retry).
  - any other error (sandbox unreachable, 503 needs_reconciliation) →
    `infra_health` classification (transient infra), set LastError, leave plan in
    `implementing`, bump SSE; do NOT emit a plan_conflict recovery for an infra
    blip. (Mirror the savePlanCached stall-notify path at
    `execution_events.go:368`.)
- Plan stays in `implementing` in both cases (it never advanced). This reuses the
  existing "requirements terminal but plan can't advance" stall semantics
  operators already understand.

> PRODUCT CALL #4 (the one real product decision): pre-QA conflict route.
> Option 4a (RECOMMENDED): reuse `RecoveryRequested` + `plan_conflict`
> classification, plan stays `implementing` + LastError. No schema change, lands
> in the existing recovery UX. Option 4b: introduce a dedicated
> `StatusConflictReconcile` plan status with its own UI affordance. 4b is cleaner
> for humans but is a status-machine change (new transitions, new UI, new
> reconcile mutation) — heavier and out of proportion to this fix. I recommend
> 4a; flag 4b as a follow-up if conflict frequency justifies a first-class state.

### A5. (covered in A1/A2) — synthesis vs unit ordering

- unit: assemble (A1) → status `ready_for_qa` → `publishQARequestIfNeeded` emits
  `QARequestedEvent{Workspace: qa-<slug>}` → sandbox runs tests on the worktree →
  qa-reviewer claims `ready_for_qa → reviewing_qa` and inspects the same worktree.
- synthesis: assemble (A1) → status `ready_for_qa` → qa-reviewer claims directly
  (no test event) and inspects the worktree.
Both are downstream of A1, so the branch + worktree exist before either runs.

### A6. Idempotency & re-runs (Phase A)

- **Assembly:** `MergeBranches` is `checkout -B` → re-calling reproduces the
  branch from the same source branches. Safe to repeat on recovery re-convergence
  and on the approve-time safety-net (A2). One caveat (already documented): a
  human edit on the assembled branch between two assembles is destroyed — existing
  Phase-5 gap, not introduced here.
- **Worktree:** `qaTaskID` is deterministic (`qa-<slug>`). On a recovery re-run,
  execution re-runs, requirements re-converge, A1 fires again: it MUST
  delete-and-recreate the worktree (stale checkout of the previous assembled
  commit) — mirror the stale-branch handling in
  `req_watcher.go:165-173`/`resumeFromRecoveryLocked` (delete then create). So A1's
  create step should be: best-effort `DeleteWorktree(qa-<slug>)` then
  `CreateWorktree(qa-<slug>, WithBaseBranch(assembledBranch))`. This guarantees the
  worktree reflects the freshly re-assembled branch after recovery rework.
- **architecture_revise:** this path resets req execs (`abandonExecsForSlug`,
  `awaiting_recovery.go:299`) and re-runs from the architect. The previously
  assembled branch + QA worktree are now STALE (they point at the old
  implementation). Because A1 re-runs at the next convergence (delete+recreate),
  the stale worktree is replaced. Additionally, the stale `semspec/plan-<slug>`
  assembled branch is force-reset by the next `checkout -B`, so no stale-commit
  leakage into QA. ACTION: confirm `abandonExecsForSlug` / the architecture_revise
  reset does not need to also delete the QA worktree eagerly — it does not
  (A1 delete+recreate covers it), but add a belt-and-suspenders
  `DeleteWorktree(qa-<slug>)` in the plan-reset path if we want the stale tree gone
  immediately rather than at next convergence. RECOMMEND deferring that; A1
  delete+recreate is sufficient.

---

## Phase B — Recovery resets covered Story status

### B1. The minimal change

`dispatchSynthesizerLocked` loads the plan fresh from KV (`component.go:1005`) and
`dispatchCurrentStoryLocked` reads `story.Status` from that fresh plan
(`component.go:1080`). Therefore: **if recovery resets the covering Stories from
`complete → ready` in KV (via the existing story-status mutation) BEFORE the
resume re-enters synthesis, the skip will not fire and the dev loop re-runs.**

`complete → ready` is already a legal transition (`story_task.go:177-178`) and the
story-status mutation already enforces it and re-fires the orchestrator
(`mutations.go:841-871`). So the change reuses existing primitives — no new
transition, no new mutation.

**Where to reset:** in the QA-recovery resume entry, `resumeTerminalForRecoveryLocked`
(`processor/requirement-executor/recover_completed.go:139`), BEFORE it calls
`resumeFromRecoveryLocked`. Add a step that, for the requirement being resumed,
loads the plan, finds the Stories covering it (`plan.StoriesForRequirement(reqID)`,
`workflow/types.go:1169`), and for each Story currently `complete`, issues
`workflow.ClaimStoryStatus(ctx, c.natsClient, slug, storyID, StoryStatusReady, logger)`.

```
// resumeTerminalForRecoveryLocked, before resumeFromRecoveryLocked:
plan := c.loadPlanFromKV(ctx, exec.Slug)
for _, s := range plan.StoriesForRequirement(exec.RequirementID):
    if s.Status == StoryStatusComplete && c.ownsStoryForReset(s, exec.RequirementID):
        workflow.ClaimStoryStatus(ctx, c.natsClient, exec.Slug, s.ID, StoryStatusReady, c.logger)
```

### B2. WHICH stories to reset — only the M:N owner, only covering stories

- **Only Stories covering the affected requirement(s)**, not all plan Stories. A
  release-gate QA rejection's `AffectedRequirementIDs` is "all active reqs"
  (`collectActiveRequirementIDsForRecovery`), and each affected req resumes
  independently via the consumer loop (`awaiting_recovery.go:357-459`). Resetting
  only the resuming req's covering Stories keeps the blast radius minimal and
  composes correctly when multiple reqs are affected (each resets its own
  coverers; shared coverers are reset by whichever req gets there first, and the
  mutation is idempotent — `ready → ready` is a no-op-ish CanTransitionTo=false
  that we treat as already-reset, see B4).

- **Only the deterministic M:N owner resets.** `dispatchCurrentStoryLocked`'s
  reservation makes the lexicographically-smallest req ID in
  `Story.RequirementIDs` the owner that actually runs the dev loop; non-owners are
  gated behind `Story.Status == complete`
  (`scenario-orchestrator/dag.go:101 filterByM2NStoryReservations`). If a NON-owner
  req resets the Story to `ready`, the non-owner would then try to claim
  `ready → executing` and run the dev loop itself — duplicating work the owner
  should do, and inverting the M:N reservation. So the reset must be gated:
  `ownsStoryForReset(story, reqID)` returns true only when `reqID` is the
  deterministic owner (smallest in `story.RequirementIDs`). A non-owner resume
  does NOT reset; it will re-skip (Story still `complete`) and fast-complete via
  the executor's Tier-1 dedup — exactly the current happy-path M:N behavior, now
  correctly gated behind the owner having actually re-run.
  - VERIFY the owner-selection predicate matches `filterByM2NStoryReservations`'s
    definition exactly (lexicographically smallest req ID in
    `Story.RequirementIDs`) — extract a shared helper
    (`workflow.DeterministicStoryOwner(story) string`) so the executor reset and
    the orchestrator filter cannot drift. This is a small refactor worth doing in
    Phase B to prevent a latent owner/non-owner mismatch bug.

- **CurrentStoryIdx reset (latent bug to fix here).** On recovery resume,
  `SortedStoryIDs` is preserved and `CurrentStoryIdx` is NOT reset
  (`resumeFromRecoveryLocked` resets DAG/node state only,
  `awaiting_recovery.go:208-229`; `dispatchSynthesizerLocked` only repopulates the
  story list when `len(SortedStoryIDs)==0`, line 1018). For the QA-recovery path
  the exec is loaded fresh from KV (`loadTerminalReqExecFromKV`,
  `awaiting_recovery.go:422`), where `SortedStoryIDs`/`CurrentStoryIdx` may be
  empty/zero or stale depending on what was persisted. To re-run ALL covering
  Stories from the start, `resumeTerminalForRecoveryLocked` should reset
  `exec.CurrentStoryIdx = 0` (and clear `exec.SortedStoryIDs` so
  `dispatchSynthesizerLocked` re-derives the topo order from the now-reset
  Stories). Confirm with a test that a 2-Story requirement re-runs both Stories
  on QA-recovery, not just the cursor's current one.

### B3. M:N owner/non-owner reservation interaction

After reset, the owner's resume runs `dispatchCurrentStoryLocked`:
- Story is now `ready` (not `complete`) → skip at 1080 does NOT fire.
- `ClaimStoryStatus(... executing)` (line 1093) succeeds (ready→executing legal).
- dev loop re-runs, Story re-reaches `complete` via the normal completion path
  (`component.go:2049`), which re-fires the orchestrator (`mutations.go:864`),
  releasing non-owners to fast-complete via Tier-1 dedup.

This is identical to the original execution flow — Phase B simply re-opens the
window. No change to `filterByM2NStoryReservations` itself is required; only the
shared owner-selection helper extraction (B2).

### B4. Idempotency & happy-path safety (Phase B)

- **Idempotent reset:** if a Story is already `ready` (a prior affected-req reset
  got there first, or it never completed), `ClaimStoryStatus(... ready)` fails the
  `CanTransitionTo` check (`ready → ready` is false) and returns false — treat
  that as "already in a re-runnable state," log Debug, continue. Do NOT fail the
  resume on a no-op reset.
- **Happy path untouched:** the reset is only reached from
  `resumeTerminalForRecoveryLocked`, which is only invoked on the QA-recovery
  branch (`awaiting_recovery.go:456`) after a `needs_changes`/`rejected` verdict
  produced an accepted recovery PlanDecision. Normal first-pass execution never
  enters this path, so Stories are never spuriously reset to `ready`.
- **Budget gate already present:** the QA-recovery consumer already gates on
  `countAcceptedRecoveryCyclesForReq > maxRecoveryRestarts`
  (`awaiting_recovery.go:442-452`), so the reset cannot loop unboundedly.

---

## Risks

1. **Worktree leakage.** If cleanup (A3.4) is missed on some path, QA worktrees
   accumulate one-per-plan. Mitigation: deterministic id + delete-and-recreate at
   A1 means at most one live `qa-<slug>` per plan; archive-path prune is the
   final sweep. LOW.
2. **Stale assembled branch after architecture_revise.** Addressed by A6
   delete+recreate; residual risk if A1 is somehow not re-run before QA on a
   recovery path. Test B-e2e covers this. MEDIUM → LOW with the test.
3. **Owner/non-owner predicate drift (B2).** If the reset's owner predicate and
   `filterByM2NStoryReservations` diverge, a non-owner could reset+run and
   duplicate work. Mitigation: shared `DeterministicStoryOwner` helper. MEDIUM —
   the reason the helper extraction is in-scope.
4. **`CurrentStoryIdx` mis-reset (B2).** If we reset the cursor but not the story
   list (or vice-versa), recovery could re-run the wrong subset. Mitigation:
   reset BOTH (`CurrentStoryIdx=0`, `SortedStoryIDs=nil`) and cover with a
   2-Story test. MEDIUM.
5. **Unit runner fallback hides regression.** If `worktreeFor(Workspace)` returns
   `""` and we silently fall back to `repoPath`, Problem 1 silently returns.
   Mitigation: WARN-log the fallback and assert in the e2e that the worktree
   resolves. MEDIUM.
6. **Concurrent plans sharing the sandbox.** Each plan has its own `qa-<slug>`
   worktree; merges serialize on `repoMu`. No shared-HEAD window (the whole reason
   mechanism B beat A/C). LOW.

---

## Test plan

### Phase A unit tests (plan-manager / sandbox)

- `checkPlanConvergence` assembles before QA: given a plan with all reqs
  terminal-success and `qa_level=unit`, assert `assembleRequirementBranches` is
  invoked and `plan.AssembledBranch`/`AssembledMergeCommit` are populated BEFORE
  the `QARequestedEvent` is published, and that the published event carries
  `Workspace == "qa-<slug>"`. (fake sandbox client capturing MergeBranches +
  CreateWorktree calls; fake NATS capturing the event.)
- `qa_level=synthesis` assembles before status flips to `ready_for_qa`; no
  `QARequestedEvent` published.
- `qa_level=none`/gated path does NOT assemble at convergence (keeps approve-time
  assemble); assert no pre-QA MergeBranches call.
- Conflict routing (A4): fake sandbox returns `ErrMergeBranchesConflict`; assert
  plan stays `implementing`, `LastError` set, a `RecoveryRequested` is emitted
  with `plan_conflict` classification, and NO status advance to `ready_for_qa`,
  NO `QARequestedEvent`.
- Infra error (non-conflict): fake sandbox returns a generic/unreachable error;
  assert plan stays `implementing`, LastError set, SSE bump, and NO plan_conflict
  recovery emitted (infra_health route).
- Idempotent re-assemble (A2/A6): calling the approve-time assemble after a
  ready_for_qa assemble produces the same branch (checkout -B); assert no error
  and stable `AssembledMergeCommit`.
- `QARequestedEvent.Validate()` accepts a valid `Workspace` token and rejects a
  path-traversal token.
- qa_subscriber workspace selection: given `Workspace="qa-x"` and a present
  worktree, `runUnitQA` runs the test command in `worktreeFor("qa-x")`; given
  empty/missing worktree, it falls back to `repoPath` and WARN-logs.
- qa-reviewer dispatch metadata: assert `Metadata["task_id"] == "qa-<slug>"`
  (not `"main"`).
- Cleanup: on `QAVerdictApproved` and on `QAVerdictRejected`,
  `DeleteWorktree("qa-<slug>")` is called best-effort (404-tolerant).

### Phase B unit tests (requirement-executor / workflow)

- Story-reset on QA-recovery: a requirement covered by a Story currently
  `complete`, resumed via `resumeTerminalForRecoveryLocked`, issues
  `ClaimStoryStatus(... ready)` for that Story (owner case) BEFORE
  `resumeFromRecoveryLocked`, and `dispatchCurrentStoryLocked` then does NOT skip
  (re-runs the dev loop).
- Non-owner does NOT reset: a non-owner req's resume leaves the Story `complete`
  and re-skips (no duplicate dev loop).
- Owner predicate parity: table test asserting `DeterministicStoryOwner(story)`
  equals `filterByM2NStoryReservations`'s owner for representative
  `RequirementIDs` orderings (the extracted shared helper).
- Cursor reset: a 2-Story requirement on QA-recovery re-runs BOTH Stories
  (`CurrentStoryIdx` reset to 0, `SortedStoryIDs` re-derived).
- Idempotent reset: a Story already `ready` → `ClaimStoryStatus(... ready)`
  returns false, resume continues without error.
- Happy path untouched: first-pass execution never calls the reset (Stories not
  spuriously moved to `ready`).

### ONE new mock e2e scenario (workstream #3 — shape only)

Name: `qa-data-plane` (mock LLM, Tier-2).

Shape that ACTUALLY catches both bugs (must NOT use a pre-seeded green baseline
like the current `qa-cycle`):
- **Multi-requirement** plan (>=2 reqs) so assembly merges multiple
  `semspec/requirement-<id>` branches, exercising B1 ordering and conflict-free
  merge.
- The project's unit test (`qa_level=unit`) must FAIL on the pristine `main`
  baseline and PASS only after the agent's implementation commits land — e.g. a
  test asserting a function/file that does not exist on `main` and is created by
  the mock-coder fixture on the requirement branch. This is the load-bearing
  property: if QA were (wrongly) run against `main`, the test FAILS; it can only
  pass if QA runs against the assembled branch. Contrast with today's `qa-cycle`
  whose test is green on the baseline, masking Problem 1 entirely.
- Assert: plan reaches `ready_for_qa` with a populated `AssembledBranch`; the
  `QACompletedEvent.Passed == true` ONLY because the worktree carried the
  implementation; plan reaches `complete`/`awaiting_review`.
- **Recovery sub-shape (Phase B):** a second variant where the mock fixture's
  first pass leaves the unit test FAILING (or the mock Murat returns
  `needs_changes`), and the M:N coverage is such that the failing requirement's
  Story is `complete` (covered via M:N). Assert that recovery actually re-runs
  the dev loop (Story reset `complete → ready`, a second dev dispatch observed)
  rather than bouncing back to QA with identical state. This is the scenario that
  reproduces the 2026-06-12 mavlink-hard-run-2 no-op-recovery failure
  deterministically with a mock LLM.

> Note: building this scenario depends on a mock-coder fixture that writes real
> files on the requirement branch (the open #151 fixture limitation — "2nd DAG
> node leaves worktree clean" — is adjacent; the new scenario needs the fixture
> to actually produce a file the test keys off). Flag #151's fixture work as a
> dependency of workstream #3.

---

## Sequencing & flags for the user

1. **Land Phase A first.** It makes QA evaluate real code; it will START
   producing correct `needs_changes` verdicts where today everything passed on the
   baseline. Expect previously-green mock scenarios with weak tests to flip — audit
   them (this is desirable signal, not a regression).
2. **Land Phase B second**, once A is producing real rejections, so the recovery
   reset has something real to act on.
3. **PRODUCT CALLS to confirm before coding:**
   - #1 — plan record carries `AssembledBranch` from `ready_for_qa` (vs only at
     `complete`). Low stakes; recommend accept.
   - #2 — QA worktree branch `agent/qa-<slug>` is a throwaway checkout of
     `semspec/plan-<slug>`; QA reads, does not write durable artifacts. Confirm
     Murat is read-only at the gate.
   - #3 — qa-reviewer missing-worktree behavior: (i) self-heal create vs (ii)
     main-fallback+WARN. Recommend (i).
   - #4 — pre-QA conflict route: 4a reuse `RecoveryRequested`+`plan_conflict`
     (recommended, no schema change) vs 4b a dedicated `StatusConflictReconcile`
     status (cleaner UX, heavier). Recommend 4a, 4b as follow-up.

## Other issues found (incidental, while auditing)

- **Latent recovery cursor bug (pre-existing).** `resumeFromRecoveryLocked`
  (`awaiting_recovery.go:208-229`) resets DAG/node state but NOT
  `exec.CurrentStoryIdx`/`exec.SortedStoryIDs`. On a multi-Story requirement, a
  recovery resume can re-enter `dispatchSynthesizerLocked` with a stale cursor and
  re-run only a subset of Stories. Phase B fixes it for the QA-recovery path;
  the iteration-exhaustion recovery path (`handlePlanDecisionAccepted` →
  `resumeFromRecoveryLocked`, `awaiting_recovery.go:406`) has the same latent gap
  and should get the same cursor reset. File: `awaiting_recovery.go:166-261`.
- **`collectActiveRequirementIDsForRecovery` can return empty.** Because
  `Requirement.Status` is a lifecycle field that stays `active` through execution
  (`workflow/types.go:1430`), the function returns all reqs — correct today. But if
  a plan ever marks reqs `deprecated`/`superseded` mid-flight, a QA rejection could
  produce an empty `AffectedRequirementIDs` and the WARN-only path
  (`mutations.go:1063`) leaves recovery requiring manual acceptance. Not in scope
  here; noting the coupling. File: `processor/plan-manager/mutations.go:1056`.
