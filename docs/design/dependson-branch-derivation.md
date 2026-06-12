# DependsOn-Driven Branch Derivation

Status: PROPOSED — design for human review before coding
Author: architect
Date: 2026-06-12
Branch: fix/shared-file-assembly-conflict

## 1. Problem statement

`DependsOn` today serializes *timing* and feeds *LLM context*, but it does **not**
drive *git branch derivation*. Every requirement branch forks from the same
plan-level base, so two requirements that edit a shared file (README coverage
matrix, a shared integration test, a build file) each produce an independent
branch containing one half of the change. Plan-level assembly
(`assembleRequirementBranches` → sandbox `git merge --no-ff`) then collides → 409
conflict, which the system mishandles (silent `implementing` stall).

### Verified root cause (file:line)

1. Dispatch passes the plan branch as base: `scenario-orchestrator/component.go:373`
   `triggerRequirementExecution(ctx, slug, traceID, trigger.PlanBranch, r, sc, deps)`.
   `trigger.PlanBranch` is identical for every requirement.
2. The dependent branch is created from that base, never from a prerequisite:
   `requirement-executor/req_watcher.go:152-157` — `baseBranch := "HEAD"; if
   planBranch != "" { baseBranch = planBranch }; CreateBranch(branchName, baseBranch)`.
3. `deps` is LLM context only: `[]payloads.PrereqContext` (RequirementID, Title,
   Description, FilesModified, Summary) from `buildPrereqContext`
   (`component.go:396-428`). It *tells the agent in prose* the prereq touched
   README; the dependent's branch does not contain that change.
4. `req.PlanBranch` is effectively empty for the failing plans → base falls back to
   `"HEAD"` (workspace-left-on-last-branch fluke). Any inheritance today is
   order-dependent, not the DependsOn DAG.
5. **THE LOAD-BEARING GAP: file-overlap serialization edges live ONLY on
   `Story.DependsOn`, never on `Requirement.DependsOn`.** `DeriveStoryScheduling`
   Pass 2 (`applyResourceEdges`, `derive_story_scheduling.go:108-156`) writes the
   cross-component file-overlap edge to `higher.DependsOn` where `higher` is a
   `*Story` (:152). Nothing copies it to `Requirement.DependsOn`.
6. Both the dispatch gate (`filterReadyRequirements`, `dag.go:39-71`) and the
   assembly order (`topoSortRequirementsByDependsOn`, `plan_merge.go:235`) read
   `Requirement.DependsOn` — so both are **blind** to the file-overlap edge. The
   only thing honoring it is the M:N gate `filterByM2NStoryReservations`
   (`dag.go:99-130`), which serializes *timing* but never changes the branch base.
7. Scope is unenforced for undeclared shared files. `FilesOwned` derives from
   `component.ImplementationFiles`; agents write ad-hoc files via bash
   (`cat > … << EOF`). `tools/governance/FileScopeFilter` is dead code (zero
   production callers) and bash-blind (only `file_write/create/delete`).

### Conceptual fix

A `DependsOn` edge drives **branch derivation**: a dependent requirement's branch
base = the assembled merge of its prerequisites' branches, not the plan base. The
DependsOn DAG becomes a coherent branch-derivation tree — roots fork from the
baseline, each dependent forks from its prerequisites' merged state (already
containing their shared-file edits) — so plan-level assembly merges only the DAG
leaves and every merge is a fast-forward. Conflict-free by construction. A
residual conflict then means two truly-parallel requirements edited the same file
with no DependsOn edge — a provable declaration/scheduling defect (part #1).

## 2. Mechanism (Q1): per-dependent base-merge branch, computed at dispatch

When requirement `R` is dispatched (prereqs already complete):

```
prereqBranches := { ownerBranch(P) : P ∈ ResolveRequirementBranchPrereqs(R, stories) }
if len == 0:   base := plan baseline (resolvePlanBase)          # DAG root
elif len == 1: base := the single prereq branch                 # linear chain, pure fork
else:          MergeBranches(Target: "semspec/reqbase-<R>", Base: planBase,
                             Branches: sortDeterministic(prereqBranches))
               base := "semspec/reqbase-<R>"
CreateBranch("semspec/requirement-<R>", base)
```

Hook: compute the base in the **scenario-orchestrator** dispatch (it holds the full
Requirements + Stories + completion state), pass a new `BaseBranch` through the
ReqCreate mutation (execution-manager `mutations.go:400` → `RequirementExecution`)
→ `req_watcher.go:initReqExecution` uses `exec.BaseBranch` instead of
`planBranch`/`HEAD`. The `>1 prereq` merge is done by the orchestrator before the
mutation, so the executor only sees one ready base ref.

Rejected: (b) a native N-parent sandbox create (git has none — it'd just be
MergeBranches renamed); (c) always merge even when linear (adopted only as the
fast path — single prereq → direct fork, no throwaway branch); merging prereqs
*into* R's branch as the dev loop's first step (agent might "undo" prereq work;
basing keeps prereq commits as immutable ancestors → fast-forward at assembly).

## 3. Requirement- vs Story-level DependsOn (Q2)

Add a pure helper `workflow.ResolveRequirementBranchPrereqs(req, stories) []string`
returning the set of **owner requirement IDs** R's branch must derive from = union
of: `req.DependsOn` AND, for every Story covering req, the `DeterministicStoryOwner`
of each `Story.DependsOn` entry (excluding req itself).

Keep `Requirement.DependsOn` authoritative for *dispatch readiness* (and LLM
context); use the Story-derived union only for *branch base*. Do NOT back-propagate
onto `Requirement.DependsOn` (would double-gate dispatch and change John's contract
semantics for plan-reviewer/UI). `topoSortRequirementsByDependsOn` (assembly order)
must use the SAME resolved union so merge order matches derivation order.

## 4. M:N (Q3)

A Story's work lives on the **owner** requirement's branch (`DeterministicStoryOwner`
= smallest req ID). Prereq resolution follows owner branches:
`ownerBranchFor(P) = "semspec/requirement-" + DeterministicStoryOwner(storyCovering(P))`
(fallback to P's own branch only when P is covered by no Story). Non-owner
requirements produce no commits — recommend assembly enumerate **owner branches
only** (one per Story) + Story-less reqs, deduped, rather than every
`semspec/requirement-*` branch.

## 5. Residual conflict = real signal, not silent stall (Q4)

With correct derivation, a surviving assembly conflict = two truly-parallel reqs
editing the same file with no DependsOn (part #1's undeclared-shared-file hole).
Reuse `routeAssemblyConflict` (`execution_events.go:409-443`) but make it
terminate-or-remediate:
1. Surface the conflicting *path* (extend `handleMergeBranchesConflict` with
   `git diff --name-only --diff-filter=U`); classify as a planning-partition defect
   → `architecture_revise`/re-partition to add the missing serialization edge, then
   re-derive.
2. Bound by recovery budget; on exhaustion → terminal **`rejected`** with
   `LastError = "unresolvable plan-level merge conflict on <path> between <reqA>,<reqB>"`,
   NOT a silent `implementing` stall.
3. Requires the M:N-complete-story re-open fix (the QA-recovery no-op) so a
   remediation actually re-runs the owner dev loop.

## 6. Idempotency / recovery (Q5)

Deterministic: `ResolveRequirementBranchPrereqs` is pure; `reqbase-<R>` merge sorts
prereqs lexicographically; `MergeBranches` uses `checkout -B` (no accumulation).
Stale base: `initReqExecution`'s `ErrBranchExistsAtDifferentBase` delete-and-recreate
(`req_watcher.go:165-173`) now also covers a *changed base* across attempts. A prereq
re-run must invalidate + re-dispatch its dependent subtree (orchestrator treats a
dependent whose recorded base SHA ≠ recomputed base as needs-re-dispatch). Extend
`pruneRequirementBranches` to delete `semspec/reqbase-*`.

## 7. Interaction with the QA data-plane fix (Q6)

`assembleRequirementBranches`/`assembleAndStageQAWorktree` still work and get
simpler (merges become fast-forwards). Required: enumerate owner branches only
under M:N, order by the resolved union. No change to worktree staging or the
`routeAssemblyConflict` wiring beyond surfacing the conflicting path.

## 8. Closing the undeclared-shared-file hole (Q7 / part #1)

- (B) **Detect-and-repair (ship first):** after dev loops, `git diff --name-only`
  each req branch vs base; find cross-req overlaps with no DependsOn edge; inject the
  missing serialization edge (lower req ID first) and re-derive + re-run the dependent.
  Reuses the conflict-routing + re-dispatch paths, no per-loop latency.
- (A) **Bash-aware scope enforcement (durable follow-up):** post-hoc `git diff` vs
  the Story's `FilesOwned`, fixable-reject out-of-scope writes (the dead bash-blind
  `FileScopeFilter` is NOT it — needs a new gate + "declare file" remediation).

## 9. Offline test plan (the validation gate — LLM-free)

- **A. Serialized chain A→B, same file → clean assembly** (the core regression;
  fails on main today). Derivation returns {A}; B's branch bases on A; assembly
  merges clean, README has both lines stacked. (`workflow` + `cmd/sandbox`.)
- **B. Parallel A,B, disjoint files → clean** (no over-serialization).
- **C. Parallel A,B, SAME file, NO DependsOn → conflict routes to terminal/recovery**
  (not silent stall). Mock sandbox conflict; assert recovery payload + final plan
  status + terminal `LastError` names the path + req IDs. (`plan-manager`.)
- **D. M:N: story covers 2 reqs, depends on a prior story → derivation follows owner.**
  `ResolveRequirementBranchPrereqs(b1) == {a1}`; orchestrator bases b1 on
  `semspec/requirement-a1`. (`workflow` + `scenario-orchestrator`.)
- **E. >1 prereq → deterministic `reqbase-<R>`** (same tree SHA twice = idempotent).
- **F. Stale base on retry** → `ErrBranchExistsAtDifferentBase` → delete-and-recreate
  from the moved prereq. (`requirement-executor` + `cmd/sandbox`.)
- **G. Keep green + extend:** `derive_story_scheduling_test.go` (unchanged — Pass 2
  still writes Story.DependsOn); new `workflow/resolve_branch_prereqs_test.go`;
  `plan_merge_test.go:123` topo-sort updated to the resolved union + owner-only
  enumeration; new `cmd/sandbox` `TestCreateBranch_FromPrereqBranch`,
  `TestMergeBranches_StackedChainFastForwards`.

## 10. Product calls

- **P1 (§3):** `Requirement.DependsOn` stays authoritative for dispatch readiness;
  Story-derived edges authoritative only for branch base (translate, don't
  back-propagate). [Recommended: yes]
- **P2 (§4):** Assembly enumerates owner branches (one per Story) + Story-less reqs,
  deduped — not every `semspec/requirement-*` branch. Review vs UI/prune expectations.
- **P3 (§6):** A prereq re-run invalidates + re-runs its dependent subtree (correct,
  costs budget) vs dependents pinned to the prereq SHA (cheap, risks divergent
  assembly). [Recommended: invalidate-and-re-run, budget-bounded]
- **§5 terminal:** residual conflict surviving bounded re-partition → `rejected` with
  a precise diagnostic (confirm `rejected`, not a new `blocked` status).
- **§8:** detect-and-repair (B) ships first; bash-aware scope enforcement (A) follows.

## Incidental findings

- `tools/governance/FileScopeFilter` is dead code AND bash-blind — wire+extend it or
  delete it; today it's a misleading "we have scope enforcement" signal that does nothing.
- `Requirement.DependsOn` and `Story.DependsOn` silently diverge (the Pass-2 edge
  lives only on stories) — the root mechanism, and a latent footgun.
- `handleMergeBranches` `checkout -B` silently destroys human conflict fixes on the
  target (acknowledged "Phase 5" at `server.go:1264-1271`) — stakes rise as residual
  conflicts become rare-and-real.
