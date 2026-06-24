# Planning-Gate Convergence Audit

**Date:** 2026-06-23 · **Main:** `0b47353e` (pre-#276) · **Trigger:** four consecutive
`gemini mavlink-hard` runs that each rejected in the planning phase, every time on a
*different* gate, never reaching execution.

## Why this audit

Across one session, four `mavlink-hard` runs hit four distinct unconvergeable planning
gates. Each looked at first like "the agent can't do it," but every one turned out to be
a plumbing, prompt, or contract defect — not a model-capability ceiling:

1. **method signatures** (architect, ADR-047) — gate demanded upstream method contracts no
   agent resolves → removed the gate (#273), moved resolution to the dev.
2. **companion-test ownership** (R-arch) — reviewer checked `scope.create` ownership the
   deterministic gate defers, and was companion-test-blind → fixed the prompt (#274).
3. **(R-arch's LLM reviewer finally ran for the first time)**
4. **complete plugin list** (R-req/R2) — reviewer demanded a requirement enumerate "all
   mandated MAVSDK plugins," but `plan.Constraints` was never delivered to the
   requirement-generator → plumbed the constraint (#276).

That pattern motivated a systematic audit of every planning gate, classified by failure
mode, to stop the whack-a-mole.

## Method

Three parallel surface audits + a tool-access audit, all read-only with file:line evidence:
- **Deterministic gates** — every error-severity `PlanReviewFinding` in `processor/plan-reviewer/`.
- **LLM reviewer criteria** — `planReviewerCompleteness{R1,Req,Arch,R2}` in `prompt/domain/software_render.go`.
- **Producer-side validators + upstream-fact sweep** — `workflow/` validators, structural-validator, `tools/terminal/validators.go`, in-component preflights, and every demand for an upstream fact.
- **Per-agent tool access** — which LLM loops have external-lookup tools vs. are guessing.

## Classification

| Class | Meaning |
|---|---|
| **U** | Demands an upstream/external fact (method signatures, complete plugin/symbol/endpoint lists, exact coordinate/version) that **no agent in the pipeline resolves deterministically** — LLMs guess from memory and churn. |
| **M** | prompt↔system mismatch / wrong-phase / stricter-than-deterministic — false-rejects. |
| **S** | demands a field absent from the producer's submit_work schema. |
| **L** | legitimate & satisfiable by the producing agent. |

## Findings — the unconvergeable gates cluster into three root causes

Most gates (≈40 of the surveyed deterministic + LLM gates) are **Class L**. The
unconvergeable ones cluster:

### Root cause 1 — constraint coverage demanded from producers who never receive the constraints  ✅ FIXED (#276)

`planReviewerCompletenessReq` criterion 1 and `planReviewerCompletenessR2` criterion 1a
(#204) demand requirements/scenarios cover *every coverage mandate in `plan.Constraints`*,
but `Constraints` was never on `RequirementGeneratorRequest`/`RequirementGeneratorContext`
or the renderer. The reviewer saw the mandate; the producer didn't → guaranteed churn. This
is the #267 pattern (a gate demands data the producer can't access). **Fixed in #276** by
plumbing `plan.Constraints` to the requirement-generator with a complete-coverage-by-reference
directive (the requirement description then carries the mandate to scenarios). The
**U-half** — whether R2 crit 1a still demands literal per-plugin scenario enumeration — is
Root cause 3.

### Root cause 2 — scope/ownership is planner-authored, but only downstream phases can satisfy ownership, and they can't reshape scope  ⛒ FILE

Four deterministic `Class-M` gates — `architecture.scoped_file_unowned`,
`contract.scope_missing`, `story.contract_scope_uncovered`,
`architecture.topology_unapproved_build_root` — plus the structural
`file-ownership-planning-gap` (`processor/structural-validator/ownership_check.go:504`). The
planner authors `scope.create`/`scope.include`/contract-scope; the architect and Sarah can
only reshape *their own* artifacts. When the planner scopes a deliverable that no component
can sensibly own — canonically a `build.gradle` the dev must touch to add a dependency (the
`io.reactivex.Flowable not found` / RxJava wedge) — the regeneration loop has **no move that
satisfies the gate**; only a recovery contract-amendment (a non-regeneration action) escapes.
Mitigated on the wrong-phase axis by `determineR2ReentryPoint` (`mutations.go:2928`, findings
route back to the phase named on them) but **not** on the can't-reshape-scope axis.

### Root cause 3 — upstream facts have exactly ONE deterministic resolver  ⛒ FILE

The pipeline deterministically resolves **only coordinate existence** (the architect's
Maven-Central HEAD probe, `architecture-generator/component.go:739`). Every other upstream
fact — imports, method signatures, symbol/plugin/endpoint enumerations, version pins,
runtime-behavior/role claims — is **LLM-guessed from model memory, verified only at the dev's
compile (or never)**. The Class-U inventory (U1–U16) is dominated by:
- **U7** exact method contracts (now on the dev, #273) — no resolver, only compile catches a wrong signature.
- **U14/U15** complete enumeration of an upstream library's API surface (R-req crit 1, R2 crit 1a) — *"the demand most likely to create an unconvergeable review loop, because no deterministic source can adjudicate."*
- **U4** import correctness — `ValidateUpstreamImports` only checks the string is dotted, not real.

**Tool-access compounds it.** The agents *asked* to produce upstream facts (architect,
developer, researcher) do have lookup tools (`web_search`/`http_request`/bash on `/sources/`).
But:
- **The reviewers** (plan-reviewer R-arch & R2, the per-node code reviewer `RoleReviewer`) are
  asked to *validate* coordinate/import/citation **correctness** with a **bash-only** palette —
  no web/http/graph. They can check presence/shape but cannot confirm a coordinate is real or
  an import exists unless `/sources/`+`~/.m2` are mounted, so on correctness they **rubber-stamp**.
- **The researcher** — the one persona purpose-built to resolve upstream surfaces on demand — is
  **dead on the live path**: `research()` is shelved out of `RoleDeveloper` (`prompt/tool_filter.go:135`),
  its only caller. All upstream resolution therefore rests on the architect (one-shot, up front)
  with no mid-execution fallback.
- **Graph query tools are granted to ZERO agents** (removed 2026-05-12; `prompt/tool_filter.go`
  has no `graph_*` in any role's `AllowExact`), though six components still list them in dead
  `availableToolNames()` candidate lists.

**The leverage:** Root cause 3 is the real argument for a **deterministic upstream-resolver**
(or reviving the researcher) — the thing the method-signature and plugin-list failures both
circled. Until one exists, every Class-U gate is structurally a guess-and-churn.

## Incidental bugs found (filed separately)

- **Maven verifier ignores the version** (`upstream_verifier.go:78`) — strips the version, so a fabricated *version* of a real artifact passes the "specific version" gate the prompt insists on.
- **R2 and R-arch upstream-resolution criteria are duplicated nearly verbatim** (`software_render.go:1361` vs `:1381`) — two hand-maintained copies, drift risk.
- **Const-name ↔ round-number inversion** (`software_render.go:1076`): `…Arch` binds `case 3`, `…Req` binds `case 4`; the R2 const header is stale; `mutations.go:184` struct comment still says "1 or 2".
- **`ValidateFileOwnershipPartition` is a live no-op** (`plan_requirement.go:118`) still invoked from `plan-manager/mutations.go:269`.
- **Deterministic mergers are round-blind** (`deterministic_preflight.go:14`) — safe today via nil/empty guards; a future merger without an early-phase guard would false-reject at R1/R-req.
- **qa-reviewer prompt references filtered-out graph tools** (`software.go:~2892` says "Start with `graph_summary`") — the role cannot call them.

## Bottom line

The pipeline is **not fundamentally broken** — most gates are sound, and three of the four
session blockers were genuine, fixable defects, not capability ceilings. The discipline of
"don't blame the agent until plumbing is ruled out" was correct every time. The remaining
structural risk is concentrated in **Root cause 2** (planner-scope vs. downstream ownership)
and **Root cause 3** (no deterministic upstream resolver), both of which want design changes
rather than prompt tweaks.
