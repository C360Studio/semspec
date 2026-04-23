# Task #11 — Worktree/Git Invariant Audit

Status: audit-only. No code changes in this pass.

## Scope and stance

The concern is **correctness**, not performance: places where semspec can
silently diverge between "what git actually holds" and "what we tell humans."
A passing scenario on a branch that will not be what ships is the worst class
of failure — the user calls it "lying about state."

Three parallel git lifecycles exist today:

1. **requirement-executor** creates a per-requirement branch in the sandbox
   via HTTP (`sandbox.CreateBranch`), owns its lifecycle, never merges it.
2. **execution-manager** creates a per-task worktree in the sandbox
   (`sandbox.CreateWorktree`), merges each task back to the requirement branch
   on approval, and deletes worktrees on cleanup.
3. **spawn** (`tools/spawn/worktree.go`) creates a *detached-HEAD worktree on
   the host filesystem* under `<repoRoot>/.semspec/worktrees/`, bypassing the
   sandbox HTTP boundary entirely.

Lifecycle 3 is a surprise — the "sandbox mandatory" project memory and
commit history suggested the sandbox was the only execution path. It is not.

---

## Legend for each invariant

- **Claim**: one-line statement
- **Where enforced today**: `file:line` citations, or "NOT ENFORCED"
- **Could this simplify?**: is the invariant load-bearing, or an accident?
- **Failure mode**: what the user sees when it breaks
- **Cheap observability hook**: how we'd notice a violation

---

## A. Git-state correctness

### A1. Every commit on any branch carries the {requirement, task, loop} that produced it

**Where enforced today:**
- `processor/execution-manager/component.go:1592-1599` — merge attaches
  `Task-ID`, `Plan-Slug`, and (conditionally) `Trace-ID` trailers
- `cmd/sandbox/server.go:399, 473` — `appendTrailers` merges into commit
  message before commit and before merge commit

**GAP:** No `Requirement-ID`, `Loop-ID`, or `Node-ID` trailers. `git log` on
main cannot answer "which requirement produced this?" without cross-joining
through EXECUTION_STATES KV. Task-level spawns (`tools/spawn/worktree.go:91`)
write commits with message `"agent: <id> task completion"` and **no trailers
at all**, so their provenance is not recoverable from git.

**Could simplify?** No — adding three trailers is cheap and buys permanent
provenance. Do not simplify; add them.

**Failure mode:** Post-mortem on a bad merge can't trace the commit back to
the requirement/loop that produced it. "Why did this change ship?" is
unanswerable from git alone.

**Cheap observability hook:** A CI/task check that scans `git log` for any
merge commit in the plan range missing `Requirement-ID` or `Task-ID`
trailers and fails the check.

---

### A2. A merge that reports success actually merged the expected changes

**Where enforced today:**
- `cmd/sandbox/server.go:475-481` — `mergeIntoMainRepo` returns error on
  `git merge` failure; caller returns HTTP 409
- `processor/execution-manager/component.go:1606-1630` — 3-attempt retry,
  then `markErrorLocked("merge_failed:…")` on exhaustion (commit `3968e1d`)
- `processor/execution-manager/component_test.go:776-895` — test guards this

**PARTIAL GAP:** `mergeIntoMainRepo` restores `origBranch` with a **silent
`_ = runGit(…, "checkout", origBranch)`** at line 478. If the restore fails,
the sandbox's main repo is left on `req.TargetBranch`, the merge returns an
error (409), and the next merge request that doesn't specify a target_branch
will operate against whatever the HEAD now points to. No alarm.

**GAP (spawn):** `tools/spawn/executor.go:402-413` — `cleanupWorktree`
calls `_ = e.worktrees.Merge(ctx, worktreePath)`. Comment claims "a merge
failure doesn't lose work" because the result is in the LoopEntity — but
that's only true at the "I got a response from the child" layer. The *file
changes* the child committed are stuck in `.semspec/worktrees/<id>/` on
disk (the worktree is left in place per `worktree.go:121-122`) and are
never in the parent repo. Disk fills up and the parent thinks everything
merged.

**Could simplify?** Yes — the sandbox restore path should return an error
when restore fails, and the caller should refuse to serve further merges
until the repo is reconciled. For spawn, the "merge failure silent" design
is wrong; the executor must propagate the error and record it on the
parent loop.

**Failure mode:** User sees "task completed / requirement approved"; the
commits are either missing from the target branch or stranded on disk; CI
passes because the test branch still has old expectations; production bug.

**Cheap observability hook:**
- Sandbox self-check: on `mergeIntoMainRepo` restore failure, set a health
  flag and fail subsequent merges with 503 until an operator clears it.
- `/status` endpoint that reports the count of dangling worktrees older than
  N minutes — would catch the spawn leak immediately.

---

### A3. A deleted branch never reappears with different content

**Where enforced today:**
- `cmd/sandbox/server.go:416, 445` — `git branch -D agent/<taskID>` after merge
- `cmd/sandbox/server.go:529-532` — `CreateBranch` treats "already exists"
  as success (`status: "exists"`)

**GAP:** If two requirement inits race on the same `RequirementID`
(shouldn't happen, but is not guarded), the second's `CreateBranch` returns
`status: "exists"` and the caller proceeds assuming the branch was freshly
created from the requested base. The branch may actually point to a
different commit than this requirement expected. Silent base-ref drift.

**Could simplify?** Yes — `CreateBranch` should either (a) require the
existing branch to point at the requested base and return 409 otherwise,
or (b) always fail on "already exists" and force the caller to delete
explicitly. (a) is stricter and safer.

**Failure mode:** A restructure retry collides with a stale branch from a
previous run, silently gets a branch with old work on it, runs tasks
against that stale base, and reports success against wrong code.

**Cheap observability hook:** Log a warning at INFO+1 severity whenever
`status: "exists"` is returned and include the sha of the existing branch;
grep for it in tests.

---

### A4. `main` is only written by sanctioned merge paths

**Where enforced today:**
- `cmd/sandbox/server.go:431-433` — `repoMu` serializes all checkout/merge
  against the main repo
- No `git push`, no `git remote`, no direct `git commit` against main in
  the sandbox codebase (verified via audit grep)

**GAP:** spawn's `WorktreeManager.Merge` at `tools/spawn/worktree.go:118`
runs `git merge hash --no-ff` in `m.repoRoot` — the **host repo**. If the
host repo is the user's checkout of semspec itself (common in dev), spawn
merges agent work directly into the user's current branch without going
through the sandbox. No `repoMu`. No validation.

**Could simplify?** Yes — spawn should route through the sandbox like
everyone else, or be deleted if no longer used. Two paths to "merge to
main" is one too many.

**Failure mode:** Dev running semspec locally with spawn-wired components
sees commits appear in their working branch, created by the wrong identity,
with no provenance. Could silently overwrite uncommitted work if the user
had the file open.

**Cheap observability hook:** grep `tools/spawn/executor.go` usage — is
spawn still wired? If yes, check whether `register.go` also configures the
sandbox client for it. Dump both at startup with a loud warning.

---

### A5. Branch names are deterministic from the ID; no two entities can collide

**Where enforced today:**
- `processor/requirement-executor/req_watcher.go:146` — `semspec/requirement-<requirement_id>`
- `cmd/sandbox/server.go` — task worktrees use `agent/<taskID>` (from the
  sandbox audit; `handleCreateWorktree`)
- `isValidBranchName` / `isValidID` regex validation at the sandbox boundary

**OK:** Names are deterministic. Collisions would only happen on ID reuse,
which is a separate bug class.

**Could simplify?** No. Keep as-is.

**Failure mode:** N/A unless ID reuse is possible.

**Cheap observability hook:** None needed.

---

## B. Cross-requirement correctness

### B1. Requirement branches eventually reach main (or a plan branch)

**Where enforced today: NOT ENFORCED.**

Evidence:
- `processor/requirement-executor/component.go:1537-1561` —
  `markCompletedLocked` publishes a `RequirementComplete` event and marks
  phase `completed`. It does **not** merge the requirement branch.
- `processor/plan-manager/workspace.go` — only reads branch diffs, never
  merges.
- Grep across `processor/` for any `MergeBranch` / `merge.*Branch` /
  plan-level merge shows no calls.
- Task commits land on `RequirementBranch`. `RequirementBranch` accumulates.
  Main never gets the work.

This is the **single scariest finding** of the audit. Every plan today
finishes with its work stranded on per-requirement branches. Whether the
user notices depends entirely on their mental model of "where does the
code go when a plan finishes?"

Cross-reference: `project_worktree_isolation_bug.md` describes the
symptom for integration-scope scenarios (req-6 cannot see reqs 1-5). The
root cause is this invariant: reqs never merge, so no downstream req and
no human sees the aggregated result until they manually merge
`semspec/requirement-*` branches.

**Could simplify?** This is the design question. Three options:
1. Add a plan-level merge step that fast-forwards (or octopus-merges)
   all completed requirement branches into a plan branch, then offers
   merge-to-main as a gated human step. Most consistent with "plan is
   the unit of work."
2. Serialize requirements on a single plan branch, each req rebasing on
   the prior's HEAD. Kills the parallelism.
3. Remove per-requirement branches entirely; all tasks merge into a
   single plan branch. Kills the "discard one req's work" feature but
   the tradeoff may be fine.

Until this is decided, option (1) is the safest because it's additive
and preserves current behavior.

**Failure mode:** Plan marked `complete`, code is invisible on main, user
assumes it shipped.

**Cheap observability hook:** At plan completion, compute
`git rev-list --count main..semspec/requirement-*` for each req branch
and surface the count in the plan summary UI. If >0 on a "complete"
plan, flag red.

---

### B2. Parallel requirements don't step on each other's merges

**Where enforced today:**
- `cmd/sandbox/server.go:431` — `repoMu` serializes main-repo mutations
- `processor/execution-manager/component.go:1606-1620` — 3 retries with
  backoff to absorb transient conflicts

**PARTIAL:** `repoMu` is a coarse lock on the sandbox's main repo; it
works for the sandbox path. But spawn's merges are unlocked (`tools/spawn/
worktree.go:118`). If spawn is wired in parallel with sandbox merges in
the same repo, they race. See A4.

**Could simplify?** Resolved by A4's "delete or route-through-sandbox."

**Failure mode:** Concurrent spawn merge and sandbox merge interleave; one
silently clobbers the other's working-tree state.

**Cheap observability hook:** See A4.

---

### B3. Integration-scope scenarios run against code that includes siblings' work

**Where enforced today: NOT ENFORCED.**

Each requirement branches from `planBranch` (or `HEAD` if that's empty) at
`processor/requirement-executor/req_watcher.go:147-149`. Sibling merges
never land on this branch during execution. Known-bug status per
`project_worktree_isolation_bug.md`.

**Could simplify?** This is a scoping-vs-serialization tradeoff. Options:
1. Decomposer enforces per-req independence (no integration scenarios in
   parallel reqs).
2. `DependsOn` becomes load-bearing: an integration req waits for its
   prereqs to merge, then rebases/merges from prereq branches.
3. Add an explicit "integration phase" that runs after parallel reqs and
   merges them to a shared branch before running integration-scope
   scenarios.

**Failure mode:** Integration scenarios reject correct code (as in the
mortgage-calc run) because the test harness can't see sibling implementations.

**Cheap observability hook:** Decomposer pre-check: for each scenario,
does any acceptance criterion reference behavior from another requirement?
If so, either mark it integration-scope and refuse to run in parallel, or
fail loud.

---

### B4. `CreateBranch` failures are not silently ignored

**Where enforced today: NOT ENFORCED.**

`processor/requirement-executor/req_watcher.go:151-157`:

```go
if err := c.sandbox.CreateBranch(ctx, branchName, baseBranch); err != nil {
    c.logger.Warn("Failed to create requirement branch; worktrees will branch from HEAD", ...)
} else {
    exec.RequirementBranch = branchName
    ...
}
```

If branch creation fails, `RequirementBranch` stays empty. Tasks dispatched
later (`component.go:1029`, `scenario_branch: exec.RequirementBranch`)
then pass `scenario_branch: ""` to the sandbox. The sandbox merges tasks
against the current HEAD of the main repo. Multiple failed-init reqs now
all merge into whatever HEAD happens to be at each merge time. Total
isolation failure, logged only as a WARN.

**Could simplify?** Yes — on `CreateBranch` failure, mark the requirement
`error` and stop. Isolation is a precondition for the whole execution
model; warning and proceeding violates it.

**Failure mode:** Multi-requirement plan where one req hit a transient
sandbox blip during init silently stops being isolated. Any subsequent
merge ordering becomes load-bearing for correctness; retries or reruns
produce different results.

**Cheap observability hook:** Alert on any log line matching "worktrees
will branch from HEAD." Zero should be acceptable.

---

## C. Observability

### C1. A human can answer "what git state is currently checked out for requirement R?"

**Where enforced today:**
- `RequirementExecution.RequirementBranch` is surfaced via the
  execution-manager entity payload
- UI has branch-diff views (`plan-manager/workspace.go`, `branchDiff`)
- Sandbox has `/git/branch-diff` and `/git/branch-file-diff`

**OK-ish** for live queries.

**GAP:** Post-completion, the requirement branch may still exist (never
deleted on success — only on restructure at
`requirement-executor/component.go:1375-1385`), which is good for
auditability but bad for hygiene. No UI to say "which req branches are
still alive."

**Could simplify?** A `/git/branches` sandbox endpoint listing all
`semspec/requirement-*` branches with ahead/behind counts vs main would
collapse a lot of manual git-archaeology.

**Failure mode:** Branches accumulate across runs; eventually the user
can't tell which are live vs stale.

**Cheap observability hook:** The `/git/branches` endpoint above.

---

### C2. Merge failures produce a concrete error the UI can surface

**Where enforced today:**
- `processor/execution-manager/component.go:1623-1630` — failed merge
  writes `ErrorReason` triple and returns the error
- `component_test.go:867+` tests the routing

**OK** for the sandbox/execution-manager path.

**GAP:** Spawn's `cleanupWorktree` swallows errors (see A2). Sandbox's
restore failure at `server.go:478` is silent (see A2).

**Could simplify?** Fixed by A2.

---

### C3. State-divergence detector: git vs KV agree

**Where enforced today: NOT ENFORCED.**

There is no cron, no `/status` check, and no test that compares
"EXECUTION_STATES says task T is approved with commit X" against "git
shows X is an ancestor of RequirementBranch for task T." If they diverge,
nobody notices until someone reads both by hand.

**Could simplify?** N/A — this is additive infrastructure.

**Failure mode:** Any of the silent paths above (A2, A3, A4, B4) can
produce this divergence. Without a detector, the first user to notice is
the one whose production code doesn't work.

**Cheap observability hook:** A periodic plan-level audit:
1. For each task in `EXECUTION_STATES` with phase=approved and a commit
   hash in its entity, assert `git merge-base --is-ancestor <commit>
   <RequirementBranch>` returns true.
2. For each requirement marked completed, assert its branch exists.
3. Report anything red.

Could be an HTTP endpoint (`/plan-manager/plans/<slug>/git-audit`) or
a startup-time check.

---

## D. Lifecycle

### D1. Worktree creation and branch creation are atomic

**Where enforced today:**
- `cmd/sandbox/server.go` — `handleCreateWorktree` uses `git worktree add
  -b <name> <path> <base>` as a single git invocation (atomic at the
  filesystem level)

**OK.**

---

### D2. Cleanup never removes a worktree before its writes are merged

**Where enforced today:**
- Commit `3968e1d` — `cleanupExecutionLocked(exec, success bool)` only
  deletes node worktrees when `success=true` (merges all succeeded)
- `processor/requirement-executor/component.go:1757-1785`
- Failure path leaves worktrees for sandbox's 24h GC
  (`cmd/sandbox/cleanup.go`)

**OK.** This is the 2026-04-20 hardening.

**Cross-dependency:** Relies on sandbox GC actually running with age >=
semspec's longest expected execution. If the GC age is shortened
without updating semspec's assumptions, we're back to the original race.

**Cheap observability hook:** Assert `cleanup-age` configured on the
sandbox is greater than `execution-manager.max_execution_timeout *
max_retries`. Can be a startup check.

---

### D3. Requirement branches are pruned at exactly one event

**Where enforced today:**
- **Restructure retry:** `processor/requirement-executor/component.go:1375-1385`
  deletes and recreates the branch
- **Normal completion:** NOT deleted, intentionally
- **Plan archive/delete:** no cleanup code found

**GAP:** No pruning of completed requirement branches. Over time, the
sandbox repo accumulates `semspec/requirement-*` branches indefinitely.

**Could simplify?** Yes — a plan-level cleanup when `plan.status` transitions
to `archived` or `complete` (after human acceptance) could prune the
branches. Keep them through the active lifecycle for debugging.

**Failure mode:** Soft — branch list grows forever. Eventually slows down
git operations on huge deployments; in dev mode, makes branch pickers unusable.

**Cheap observability hook:** Branch count metric, `semspec_requirement_branches_total`.

---

### D4. Spawn host-filesystem worktrees are cleaned up on parent termination

**Where enforced today:**
- `tools/spawn/executor.go:402-413` — `cleanupWorktree(ctx, path, success)`
  is called by `Execute` after the child loop returns
- `tools/spawn/worktree.go:131-147` — `Discard` has a fallback chain
  (`remove --force` → `remove` → `os.RemoveAll` → `prune`), with silent
  failures at each step (lines 137, 144)

**PARTIAL:** If the parent loop is killed/crashed before calling
`cleanupWorktree`, the host filesystem accumulates `.semspec/worktrees/<id>/`
directories forever. No GC loop on the host path (unlike the sandbox's
`cleanup-age`).

**Could simplify?** Yes — add a host-side cleanup loop, or (better) fix
A4 and route spawn through the sandbox so there's one cleanup path.

**Failure mode:** Dev machine or CI agent slowly fills its disk with stale
detached-HEAD worktrees from old loops.

**Cheap observability hook:** `/status` endpoint reports count + oldest
mtime of `.semspec/worktrees/*` directories.

---

## Summary table — what must change before we can trust this

| # | Invariant | Enforced? | Severity | Fix class |
|---|-----------|-----------|----------|-----------|
| A1 | Commit provenance trailers | Partial | Medium | Add req-id / loop-id trailers |
| A2 | Merge-success truthful | Partial | **HIGH** | Kill silent error paths (spawn + sandbox restore) |
| A3 | Branch re-use detection | Partial | Medium | Tighten CreateBranch semantics |
| A4 | Only sanctioned writes to main | Partial | **HIGH** | Route spawn through sandbox or delete spawn |
| A5 | Deterministic branch names | Yes | — | — |
| B1 | Req branches reach main | **No** | **HIGH** | Design decision required |
| B2 | Parallel merges serialized | Partial | Medium | Fixed by A4 |
| B3 | Integration scenarios | **No** | Medium | Decomposer / DependsOn design |
| B4 | CreateBranch failure is fatal | **No** | **HIGH** | One-line fix: mark error + stop |
| C1 | Live git state queryable | Yes | — | — |
| C2 | Merge errors surfaced | Partial | Medium | Fixed by A2 |
| C3 | Divergence detector | **No** | Medium | Additive: git-audit endpoint |
| D1 | Atomic worktree creation | Yes | — | — |
| D2 | Cleanup respects in-flight merges | Yes | — | Regression test |
| D3 | Branch pruning | Partial | Low | Plan-archive hook |
| D4 | Host worktree GC | Partial | Medium | Fixed by A4 |

---

## Next step (for Task #11 session, after this doc lands)

Split into three tracks:

1. **HIGH severity, one-line-ish fixes** (B4, the silent restore in A2,
   trailers for A1): go-developer can pick these up with regression tests.
2. **Design decision** (B1): needs a conversation with the user. "Where
   does code go when a plan completes?" is not a bug fix; it's a
   product-level question about what `plan.status = complete` means.
3. **Structural simplification** (A4): is spawn still used? If yes,
   routing it through the sandbox (or gating it off) removes an entire
   parallel lifecycle and resolves several invariants at once. Do not
   touch until we've grepped `Executor.Execute` call sites and confirmed.

Regression tests to write before any fix:
- `TestMerge_SilentRestoreFailure_FailsHealthCheck` (sandbox)
- `TestCreateRequirementBranch_FailureMarksError` (requirement-executor)
- `TestSpawn_MergeFailureSurfacedToParent` (tools/spawn)
- `TestPlanComplete_RequirementBranchesAreMerged` (failing test to pin B1's resolution)
- `TestGitAudit_DetectsDivergence` (new endpoint)

Do not start writing code until the B1 decision is made — it may moot
other fixes.
