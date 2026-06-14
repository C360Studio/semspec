# ADR-048: File-Ownership Discipline (Every Written File Must Be Owned)

**Status:** Proposed (2026-06-13)
**Deciders:** Coby, Claude
**Related:** ADR-043 (architect emits `ComponentDef.ImplementationFiles` — the ownership source),
ADR-044 (M:N capability ↔ Story; `Story.FilesOwned` derives from the component's
`ImplementationFiles`, and `workflow.DeriveStoryScheduling` serializes stories that share an owned
file), ADR-047 (sibling deterministic completeness gate in the same plan-reviewer file — Gate 1 here
mirrors its shape), ADR-037 (wedge recovery — the assembly wedge this prevents is the kind of
expensive-but-recoverable failure recovery exists to catch *after* the fact).
**Drives:** a plan-time ownership-completeness gate (plan-reviewer), a dev-loop containment gate
(structural-validator), a distinct terminal `assembly_conflict` PlanDecision kind, and scratch
hygiene (.gitignore + developer prompt).

## Context

### The wedge (verified live, 2026-06-13)

In a `gemini` `WITH_EPIC` `mavlink-hard` run (slug `9a5eca2f2fac`, the furthest a paid run has
reached) all 13 execution nodes passed review and the Java **source** merged with ZERO conflicts
across all four requirement branches — decomposition (ADR-044) was genuinely clean. Yet
`plan-manager.assembleRequirementBranches` failed on a plan-level merge conflict over **`README.md`**
and the run wedged before QA.

Root cause, traced end to end:

- Source merged clean because every source file was owned by exactly **one** component
  (`ComponentDef.ImplementationFiles` → `Story.FilesOwned`, disjoint partition).
- `README.md` was the **one file owned by no component at all**. The user prompt's acceptance
  criteria mandated "README documents … the coverage matrix", so all four parallel stories' devs
  each wrote their section of it — but it appeared in no `ImplementationFiles`.
- `workflow.DeriveStoryScheduling.applyResourceEdges` only serializes stories whose `FilesOwned`
  overlap. Blind to `README.md` (in zero `FilesOwned`), it ran the four stories in parallel.
- The sandbox commits each worktree with `git add -A`, so each branch carried its own divergent
  `README.md` (plus scratch like `patch.diff` and `*.orig`). `assembleRequirementBranches`'
  sequential `git merge` then hit an unmergeable content conflict.
- Recovery fired `escalate_human`, which has no unattended fallback (the auto-accept watcher only
  handles `requirement_change`), so the plan idled at `implementing` until the Playwright timeout
  (~65 min), failing late with no actionable signal — and the decision was misclassified as
  `execution_exhausted` even though execution *succeeded*.

### Reframing: this is ownership, not docs

The first instinct was to special-case documentation. It is not a docs problem. A doc was merely the
file type most likely to be cross-cutting and slip out of the component model. The invariant is
general: **every file a story writes must be owned**, so the existing scheduler serializes shared
owned files automatically. Fuzzy free-text doc-detection is the wrong tool.

A complicating constraint: ADR-043's `architecture.component_implementation_files_doc_only` rule
*rejects* a component whose `ImplementationFiles` are docs-only. So a pure-documentation deliverable
has no clean home as its own component — it must ride as a **companion file on a source component**
(which that rule's own text blesses: "Documentation companion files may remain alongside the source
but never alone").

## Decision

Defense in depth across the three phases where the invariant can be enforced.

### Gate 1 — Prevention (plan-reviewer, deterministic, pure)

New rule `architecture.scoped_file_unowned` (`processor/plan-reviewer/architecture_rules.go`,
mirroring the ADR-047 sibling). Let

```
S = NormalizeFilePaths(Scope.Create)
    ∪ { f ∈ NormalizeFilePaths(Scope.Include) : isConcreteScopedFile(f) ∧ f ∉ Scope.DoNotTouch }
O = ⋃ over components of NormalizeFilePaths(ImplementationFiles)
```

Every `f ∈ S \ O` emits one error finding naming the orphan and instructing the architect to declare
it on the single source component that produces it (companion-on-source for docs), or move it to
`scope.do_not_touch` if it is a read-only reference. `isConcreteScopedFile` requires a file
extension *or* a well-known extensionless deliverable basename (`Dockerfile`, `Makefile`, …) and
excludes directories and globs (a component owns concrete files, not dirs/patterns).

Once a doc is owned by one component → single writer; if the architect declares it on several source
components → `DeriveStoryScheduling` serializes those stories. Either outcome closes the wedge. The
rule is NOT docs-specific — a doc and a source file are treated identically.

### Gate 2 — Containment (structural-validator, deterministic, post-dev)

New always-on check `file-ownership-containment` (`processor/structural-validator/ownership_check.go`).
It computes the **authoritative** changed set from `git status --porcelain` in the worktree (the
agent's self-reported `files_modified` cannot be trusted for a gate about the agent overstepping) and
classifies each change against `Story.FilesOwned` (threaded as `TaskExecution.FileScope` →
`ValidationRequest.FilesOwned`):

- **Hard fail (Required):** a committed scratch/merge artefact (`*.orig`, `patch*.diff`, …) OR a
  pre-existing **documentation** file modified by a non-owner — the unmergeable co-write that wedged
  the README assembly.
- **Advisory (surfaced, not blocking):** a pre-existing non-doc file (build/manifest/source) modified
  by a non-owner — these usually merge cleanly and a dep bump is a routine TDD move; #176 fails
  honestly if one does conflict. New files outside ownership (legitimate class splits) are likewise
  advisory.
- Skipped entirely when there is no ownership context (manual validation / E2E).

The hard-fail vs advisory line is drawn at **unmergeable vs usually-mergeable**, not new-vs-modified,
so a dev legitimately editing `build.gradle`/`go.mod`/a pre-existing test is not blocked.

### Gate 3 — Honest failure (plan-manager, #176)

A plan-level merge conflict is recorded as a distinct terminal `PlanDecisionKindAssemblyConflict`
(naming the conflicting branch + files) and the plan transitions to `rejected` — fail-fast — instead
of firing recovery's `escalate_human` (which wedges unattended runs) or mislabelling it
`execution_exhausted`. This is the backstop when an undeclared shared file slips both gates.

### Scratch hygiene (#177)

- Fixture `.gitignore`s ignore `*.orig`/`patch*.diff`/etc. `git add -A` honours `.gitignore` for
  untracked files, so ignored scratch never reaches the commit (necessary-but-not-sufficient: only
  helps repos that *have* such a gitignore — Gate 2's junk arm is the portable fallback).
- The developer prompt (`prompt/domain/software.go`) tells the dev that the worktree is committed
  wholesale, so scratch belongs in `/tmp`, and to stay inside its file scope.

## Consequences

- A user-requested deliverable doc must be declared on a source component (single writer) or several
  (scheduler serializes). The wedge cannot recur from an undeclared shared file.
- A dev that oversteps ownership on a doc, or commits scratch, fails one TDD cycle with actionable
  feedback rather than wedging the run at assembly. Non-doc overreach is surfaced, not blocked.
- An assembly conflict that still slips through fails the plan immediately with a named, correctly
  classified signal, retryable via the existing `/plans/{slug}/retry` endpoints.
- All three gates are deterministic and offline-testable — validated by `go test` without a paid run.

## Alternatives rejected

- **Fuzzy free-text doc-detection** — this is ownership, not docs; a path classifier (used only to
  tune Gate 2 severity) is deterministic, free-text scanning is not.
- **Union/auto-merge of README at assembly** — no such primitive exists; treats the symptom, not the
  unowned-file cause.
- **Hard-failing every modified-unowned file** — false-fails legitimate build/test edits and would
  itself wedge legitimate dev submissions; the advisory/required split avoids this.
- **Keep firing recovery on assembly conflict** — `escalate_human` has no unattended fallback and is
  the exact wedge being removed.
