# ADR-044: M:N Capability ↔ Story Coverage

**Status:** Proposed (2026-06-03)
**Drives:** Sarah's contract revision, Story schema change, R3 plan-reviewer rules sweep
**Closes:** smoke-9 file-overlap shape (PR #97 / #88 validator surfaces the bug; this ADR fixes the contract that produced it)

## Context

The smoke-9 (2026-06-02) and smoke-10-hard (2026-06-03) paid runs both surfaced a file-overlap shape from Sarah: multiple sibling Stories under different Requirements declaring identical `files_owned`. PR #97 (#88) added `ValidateStoryFileOwnership` which catches this as "would race-write at parallel dispatch — add depends_on edge." Sarah's retry recovers by adding depends_on edges, which is a runtime workaround (sequenced execution), not a fix.

The deeper issue: **we collapsed two orthogonal axes — capability-as-acceptance-contract and story-as-execution-unit — into one chain.**

### Evidence from smoke-10-hard run 980f64ab3143

Captured artifacts in `/tmp/sarah-evidence/` (also pinned in the smoke memory note). Salient inputs Sarah saw:

**Mary's capabilities (4):**
- `mavsdk-lifecycle` — server process + peer connections
- `mavsdk-cs-telemetry` — telemetry → CS API DataStreams
- `mavsdk-cs-control` — control → CS API ControlStreams
- `raw-mavlink-fallback` — generic raw MAVLink streams

**John's requirements (4, 1:1 with capabilities):**
- `requirement.980f64ab3143.1-4`, each titled to its capability

**Winston's architecture (1 component, 7 files, all 4 capabilities):**

```json
{
  "name": "mavsdk-driver",
  "implementation_files": [7 Java files + 1 JSON resource],
  "capabilities": [
    "mavsdk-lifecycle",
    "mavsdk-cs-telemetry",
    "mavsdk-cs-control",
    "raw-mavlink-fallback"
  ]
}
```

**This is correct.** An OSH MAVSDK driver is structurally one cohesive Java module. Forcing artificial decomposition into per-capability components would be LLM gymnastics over real code shape.

**Sarah's prompt (verbatim, `configs/presets/bmad.json`):**

> "files_owned: union of the selected components' implementation_files. Assemble this explicitly so the dev sees the exact file set."

**Sarah's output (consistent across attempts):** 4 stories (one per requirement / capability), each with `components: ["mavsdk-driver"]`, each with `files_owned` = the same 7-file union. PR #88's validator rejected; Sarah's retry added depends_on edges between sibling stories; validator passed; execution serialized.

### The two axes we conflated

- **OpenSpec's `spec.md` per capability** is an **acceptance contract**. It says "this capability is satisfied when its `required_assertions` have evidence." Multiple specs can be verified by the same shipped code; one spec may require evidence from multiple shipped units. OpenSpec is silent on scheduling.

- **BMAD's PO-shaped stories** are **execution units**. They say "this story is the unit of work the dev gets dispatched, owns these files, ships this code." BMAD canonical assumes the PO uses judgment to shape stories around component cohesion, with dependencies expressing prereq ordering.

We hardcoded both axes into one chain (`Capability → Requirement → Story` collapsed 1:1:1), which works when the architect partitions naturally (small per-feature components) but breaks for cohesive modules like driver SDKs.

### Codex's framing (verbatim)

> "BMAD handles this with PO-level story shaping and dependencies. OpenSpec handles it by treating capability specs as acceptance contracts, not scheduling units. The gap is in SemSpec's adapter layer: we made per-capability specs drive per-story file ownership. For cohesive modules, we need many-to-many coverage: one execution story can cover multiple capability specs, and one capability spec can derive evidence from one or more execution stories."

This ADR adopts that framing.

## Decision

**Stories partition by execution-unit (component anchor); capabilities and requirements bind M:N via coverage joins. Capability specs become acceptance contracts that accumulate evidence from any shipped Story.**

### New model

```
Capabilities (Mary)            Requirements (John)
       │                              │
       │ M:N coverage                 │ M:N coverage
       │ (capability_names)           │ (requirement_ids)
       ▼                              ▼
              Stories (Sarah)
                    │
                    │ 1:1 anchor
                    ▼
              Components (Winston)
                    │
                    │ owns
                    ▼
              Implementation files
```

### Story schema

```go
type Story struct {
    ID              string
    ComponentName   string    // 1:1 — anchor; the component this Story implements
    RequirementIDs  []string  // M:N — Requirements this Story carries evidence for
    CapabilityNames []string  // M:N — Capabilities this Story provides evidence for
    FilesOwned      []string  // = component.ImplementationFiles (NO union)
    Title           string
    Intent          string
    Tasks           []Task
    DependsOn       []string  // story-level execution prereqs (cross-component sequencing)
    Status          StoryStatus
    // ... timestamps, etc.
}
```

**Greenfield:** the singular `RequirementID` field is **removed** outright (no deprecation cycle, no back-compat read). Existing tests and fixtures rewrite in the same PR.

### Sarah's algorithm (constraint-satisfying, not algorithmic)

Sarah's new prompt shifts from algorithmic (`files_owned = union of...`) to constraint-satisfying:

> "Here are the components, capabilities, and requirements. Produce Stories such that:
>
> 1. **Coverage closure** — every capability appears in ≥1 Story's `capability_names`; every requirement appears in ≥1 Story's `requirement_ids`.
> 2. **One component per Story** — `component_name` is a single declared component.
> 3. **Files derived, not chosen** — `files_owned = component.implementation_files`. You don't pick files; the component pick determines them.
> 4. **Story-level DependsOn is NOT yours to author** — the system derives `Story.DependsOn` from `Requirement.DependsOn` closure post-emission. Focus on the coverage joins; the dispatch graph follows.
>
> For each architectural component, emit ONE Story covering every requirement whose capabilities map into this component. For requirements spanning multiple components, emit multiple Stories (one per component) with depends_on edges expressing prereq ordering.
>
> Apply your readiness gate before signing off each Story. If a constraint cannot be satisfied, flag back rather than emit unready output."

This restores Sarah's PO role (judgment-driven, BMAD canonical) while honoring OpenSpec's spec-as-contract premise.

### Story DAG derived from Requirement DAG (not Sarah-authored)

**Critical:** Sarah does NOT author `Story.DependsOn` under M:N. The Story-level execution DAG is derived deterministically from `Requirement.DependsOn` closure post-emission. This is a non-trivial change from today's single-author-graph model.

**Why:** Under M:N, a Story covers multiple Requirements. A correct cross-Story dependency edge requires transitive closure across the M:N join:

```
Reqs:      R1 (deps=[]), R2 (deps=[R1]), R3 (deps=[R1])
Components: A covers R1; B covers R2 + R3
Stories:   Story-A {covers: [R1]}; Story-B {covers: [R2, R3]}

Required Story.DependsOn:
   Story-B.DependsOn must contain Story-A  (because both R2 and R3 transitively need R1)
```

Asking Sarah to author this directly is fragile — she'd need to compute the transitive closure correctly across both the Req graph AND her own component → requirement coverage. LLM judgment is the wrong tool for graph closure.

**The right shape:**

- **Sarah picks:** `component_name`, `requirement_ids[]`, `capability_names[]`, `tasks[]`, intra-story task deps
- **System derives** (post-emission, deterministic):
  - `Story.DependsOn` = set of other Story IDs whose `requirement_ids` include any Requirement in the closure of `this.requirement_ids[*].DependsOn`
  - Computed by `workflow.DeriveStoryDAG(stories, requirements) error` — closure algorithm
  - Validation: post-derivation, the Story DAG must be acyclic. A cycle here indicates an inconsistent Sarah emission (a Story whose requirements transitively depend on themselves through other Stories), surfaced as a hard validator error.

**Today's single-author graph** (Sarah emits both Story shape AND cross-Story DependsOn labels) becomes **two-step:** Sarah emits Story shape; system derives DependsOn.

### Capability spec as acceptance contract

A Capability spec is **satisfied** when ALL of these hold:
- Its `required_assertions` have evidence from at least one shipped Story's scenarios (test passing OR reviewer-approved assertion)
- At least one Story's `capability_names` includes this capability AND that Story reached `approved` state

A Story is **complete** when:
- All its `requirement_ids` are satisfied (the standard requirement-execution rollup)
- All its `tasks` reached terminal-approved state
- Its component's tests pass

QA-reviewer's rollup contract changes from "all requirements complete" to "all capabilities satisfied" — strictly more rigorous because it requires the capability evidence to be present, not just the requirement to be marked done.

### Examples

**The mavlink-hard case (1 cohesive component):**

| Today (broken) | Under ADR-044 |
|---|---|
| 1 component → 4 stories × 7-file union | 1 component → **1 Story** |
| Stories: `[lifecycle, telemetry, control, fallback]` × duplicate files | 1 Story `mavsdk-driver` covers 4 requirements + 4 capabilities |
| #88 catches; Sarah serializes with depends_on | No race possible by construction |
| Dev gets 4 TDD nodes × identical scope | Dev gets 1 TDD node × cohesive scope |
| Capability acceptance | Same — 4 spec.md, but evidence comes from 1 Story's scenarios |

**E-commerce (3 components covering 5 capabilities):**

| Today | Under ADR-044 |
|---|---|
| 5 stories (one per requirement/capability) | **3 stories** (one per component) |
| Each story touches 1-2 components | Each story is anchored to exactly 1 component |
| Cross-component file overlaps need depends_on | Disjoint by construction |
| | Story `auth` covers `[login, signup]` requirements + `[user-auth]` capability |
| | Story `payments` covers `[card, bank]` requirements + `[payment-processing]` capability |
| | Story `checkout` covers `[checkout]` requirement + `[checkout-flow]` capability |

**Multi-component requirement (`login with audit trail` → auth + audit):**

| Approach | Shape |
|---|---|
| Block at plan-reviewer | "Requirement spans multiple components — split into per-component requirements" — strict, easy |
| Allow Story.ComponentName as set | Story carries multiple components, union of files. Re-introduces the overlap risk. |

**This ADR's decision: block at plan-reviewer.** A requirement spanning multiple components is itself ill-shaped; either John should have produced 2 requirements (one per component), or Winston should have produced 1 component covering both. Forcing this resolution upstream keeps the Story schema clean (one component per Story).

## Migration (greenfield — break and fix)

No back-compat path. Single PR train rewrites the contract end-to-end:

1. **Schema PR.** `Story.RequirementID` → `RequirementIDs []`, add `CapabilityNames []` + `ComponentName`. Update all referenced types in `workflow/`, all consumers in `processor/`.
2. **Sarah prompt PR.** New constraint-satisfying prompt in `configs/presets/bmad.json` + `prompt/domain/software_render.go`. Old algorithmic prompt deleted.
3. **R3 plan-reviewer PR.** Rule sweep: `story.missing_files_owned` (unchanged), `story.docs_only_files_owned` (unchanged), new `requirement.spans_multiple_components` rule, retired single-requirement-per-story assumptions.
4. **Req-reviewer / QA-reviewer PR.** Rollup contract: "story's covered requirements all complete" + "every capability has evidence from ≥1 shipped story."
5. **Requirement-executor PR.** DAG synthesis change — Stories iterate by component, not by requirement; per-Story node DAG aggregates tasks across the covered requirements.
6. **Test fixture sweep.** Mock fixtures, plumbing integration test, smoke fixtures regenerated against new schema. CLAUDE.md note: "Story is the per-component execution unit; one Story covers N capabilities/requirements via M:N joins."

## What stays

- `OpenSpec/specs/<capability>/spec.md` emission — **unchanged.** Still per capability, still the acceptance contract.
- `Winston's components + implementation_files + capabilities[]` — **unchanged.** Still the code shape.
- `Mary's capabilities + John's requirements` — **unchanged.** Capabilities are user-observable units; requirements are scoping intent.
- `CheckCapabilityCoverage` validator — **unchanged.** Every capability still must be claimed by ≥1 component.
- `ValidateStoryFileOwnership` (PR #88) — **still load-bearing.** Catches the rare case where Winston produces overlapping `implementation_files` across components. Under ADR-044 this is the only race path left.
- Active-poll + watch sidecars — **unchanged.**
- Issue #113's structural-validator simplification — **unchanged.**

## Risks / open questions

1. **Story.ID generation scheme.** Today: `story.<slug>.<reqseq>.<storyseq>` — anchored by parent requirement. Under ADR-044, Stories no longer have a parent requirement (the relationship is M:N). The schema PR must pick a new scheme — likely `story.<slug>.<componentName>` (with kebab-case-clean componentName) or `story.<slug>.<componentSeq>`. The component-anchored form is preferred for human readability + stability across architecture revisions where component count stays but capability mapping shifts.

2. **Open bug #68 (cross-req `Story.DependsOn` topo-sort) likely closes under ADR-044.** Under M:N with derived Story DAG (`DeriveStoryDAG`), the Story.DependsOn graph is computed deterministically from the Requirement DAG via transitive closure. The classes of failure #68 was tracking — Sarah getting cross-requirement DependsOn wrong — are eliminated because Sarah no longer authors that field. The schema PR should evaluate whether #68 closes outright or restates to a narrower scope.

3. **What if a capability genuinely spans multiple components?** Example: "GDPR-compliant user data export" might require auth + storage + audit components. ADR-044 says: capability appears in the `capability_names` of multiple Stories (one per component); spec.md accumulates evidence from all. This works — the spec is the acceptance contract; evidence rolls up.

4. **Reviewer load per Story increases.** A Story now has multiple acceptance contracts (capability specs). The reviewer must verify all of them. In practice: code review verifies the SHAPE, capability-spec evidence comes from the scenarios passing. Per-Story TDD cycle counts may increase modestly in exchange for fewer Stories overall. Net token cost is empirically unknown; smoke-11 measurement.

5. **Mismatch shapes.** Cases where Winston's component partition doesn't align with the natural capability/requirement boundaries — e.g., 1 component covers 3 capabilities but only 1 requirement uses 2 of them. ADR-044 still produces 1 Story (anchored to the component), covering 1 requirement + 3 capabilities. The other 2 capabilities have evidence shipped but no requirement currently exercises them. Plan-reviewer should flag "capability declared but not exercised by any requirement" — separate concern, possibly future work.

6. **Architect retry shape under capability_coverage rejection.** Today if Winston produces a bad architecture, retry shifts capabilities between components. Under ADR-044 the same retry shape works — Stories regenerate after architecture changes. No new instability.

7. **Smoke fixture coverage.** ADR-044 needs both cases tested:
   - **Cohesive component** (mavlink-hard's natural shape) — 1 component, N capabilities → 1 Story.
   - **Multi-component, disjoint capabilities** (e-commerce-shape) — 3 components, 5 capabilities → 3 Stories.
   - Plumbing integration test (`test/plumbing/`) needs both fixtures.

## Decision summary

- Adopt M:N capability ↔ Story coverage.
- Stories anchor to ONE component (`component_name`) and inherit its files directly (no union).
- Capabilities and requirements bind via `capability_names []` + `requirement_ids []` on Story.
- OpenSpec spec.md remains per-capability (acceptance contract); BMAD component remains per-cohesive-code-unit.
- Sarah shifts from algorithmic (compute files = union) to constraint-satisfying (pick component, derive files, cover capabilities).
- No back-compat for `Story.RequirementID` singular — greenfield rewrite.

## Cross-references

- Issue #88 (PR #97) — `ValidateStoryFileOwnership`. Stays load-bearing under ADR-044.
- Issue #113 (PR #114) — structural-validator goodhart-cleanup. Orthogonal; both compose.
- ADR-043 — per-Story execution. ADR-044 refines what "Story" means but doesn't undo the per-Story DAG synthesis.
- Smoke-9 forensics (`project_smoke9_mavlink_hard_hybrid_gpt5_forensics_2026_06_02` memory) — the original "file overlap" finding.
- Smoke-10-hard run 980f64ab3143 — the run that surfaced the contract gap explicitly (Sarah retrying twice on the same shape).
