# ADR-049: Component Ownership Topology (Mergeable Surfaces, Not One-File-Per-Capability)

**Status:** Accepted (2026-06-14). Decisions resolved; implementation tracked
under the four moves below.
**Gate:** No paid MAVLink/`mavlink-hard` run until **both**: (a) the three
contracts below (architect prompt, reviewer rule + prose, dev-review gate)
agree, **and** (b) the mock ladder is restored and green. The mock ladder is the
*free* pre-paid regression gate; it has been left to rot (#162/#163 stale
fixtures + plan-phase/execution-phase non-conformant to #172/#175), so it cannot
currently catch a structural regression before a paid burn — which is exactly
how this defect cost ~$50 instead of a `go test`. Restore the fixtures (and add
a multi-requirement parallel-assembly fixture, which today has *no* offline
coverage) as part of the re-run gate. Otherwise the pipeline keeps bouncing
between yesterday's stub trap and today's assembly trap.
**Amends:** ADR-044's `component_overloaded_capabilities` rule and the #172
"component = unit of execution" prompt change (`2d8c8e95`).
**Builds on:** [ADR-048](ADR-048-file-ownership-discipline.md) (every written
file must be owned) — extends ownership from files to *component topology*.
**Motivated by:** the 2026-06-14 paid `mavlink-hard` run (slug `8beacfaa5856`,
~34.5M gemini-pro tokens), which reached assembly for the first time and then
failed terminally on a `assembly_conflict` planning-partition defect.

## The core reframe: this is not under- vs over-decomposition

It is **missing ownership topology**. The system currently conflates three
distinct axes into one "component split" knob:

```text
Capability split      what behavior must be proven        (analyst / scenarios)
Component split       what code surface a Story owns       (architect)
File ownership        what can merge safely                (DeriveStoryScheduling + assembly)
```

A capability can be independently *testable* and still share a single
*mergeable code surface*. An OSH `AbstractSensorModule` subclass (the driver
entry class) is the canonical counterexample: one file is legitimately the
integration surface for several outputs/controls. The three axes are not 1:1,
and the architect's job is to express the **component** axis as *mergeable
ownership surfaces*, not as one-file-per-capability arithmetic.

### The invariant this ADR establishes

> **Every capability needs evidence, and every written file needs declared
> ownership. Component boundaries express mergeable ownership surfaces, not
> arbitrary one-file-per-capability math.**

## Context: how #172 overcorrected

#172 (`2d8c8e95 feat(architecture): component = unit of execution; reviewer
flags overloaded components`) was a *correct* fix for the opposite failure —
the 2026-06-13 under-decomposition where one component mapped 3 capabilities to
2 files and the dev stubbed the rest. But it overcorrected on two surfaces that
now use **file-count vs capability-count** as the proxy for healthy split:

1. **Architect prompt — two coupled strings, `software_render.go:797` *and*
   `:814`.** The over-split pressure lives in *both*; editing only `:814`
   leaves the landmine. `:797` (the Guidelines `UNIT OF EXECUTION` bullet, #172's
   replacement for the deleted *"component boundaries should reflect natural
   module/service divisions"* line):
   > "Component boundaries are the planning system's UNIT OF EXECUTION … Default
   > to ONE independently-testable capability per component. … When capabilities
   > have independent behavior (separate plugins, streams, command handlers,
   > protocols), give each its own component with its own implementation_files."

   `:814` (the `component_boundaries` field doc):
   > "GRANULARITY: prefer one capability per component. A component that maps N
   > capabilities MUST declare at least one distinct, substantive
   > implementation_file per capability … if you cannot name a distinct
   > implementation surface for each capability, split the component."

   Precision note: #172 removed the explicit *"natural module/service
   divisions"* cohesion framing, but `:797` still carries a *narrow* cohesion
   clause (*"map multiple capabilities onto one component ONLY when the same
   classes genuinely implement them"*) — `:814` then layers the file-count
   proxy on top. So the architect has a sliver of cohesion permission but no
   counter-weight against the splitting math. The driver-facet triggers
   (*"separate plugins, streams, command handlers, protocols"*) live at `:797`.

2. **Reviewer rule** `componentOverloadedCapabilityFindings`
   (`processor/plan-reviewer/architecture_rules.go:152`, heuristic comment
   ~`:142`): "a component mapping N capabilities needs at least N source files;
   fewer = overloaded."

That proxy is Goodhartable and backwards for framework code: a cohesive driver
**legitimately has fewer files than capabilities**. The rule conflates
*cohesive single-class implementation* (fine) with *facade stubbing* (bad), and
because the only escape from the penalty is "more files / more components," it
**rewards inventing files and splitting** — there is no counter-weight for
cohesion.

### What the 2026-06-14 run actually did (the architect followed the prompt perfectly)

The plan goal ("MAVLink/MAVSDK support for OSH via the Connected Systems API")
has capabilities that are *literally the prompt's split-triggers* — telemetry
datastreams, control streams, raw-mavlink fallback, server lifecycle. So the
architect:

- **Over-split** into 4 components, one per capability → 4 requirements run in
  parallel (reqs 2/3/4 reqbase'd off req 1 via the #169 derivation).
- **Fabricated distinct per-capability files** to satisfy "distinct
  implementation_file per capability" — `MavsdkDataStreams.java`,
  `MavsdkControlStreams.java`, `MavlinkRawFallback.java`, all under a
  non-canonical `org/sensorhub/driver/mavsdk/` package — and **never named the
  real entry class** (`org/sensorhub/impl/sensor/mavsdk/MavsdkDriver.java`) that
  all four capabilities must register into.

All four developers, building real OSH code, independently created the canonical
`impl/sensor/mavsdk/MavsdkDriver.java` + its shared test — a file in **no**
component's declared ownership. The declared partition was disjoint *on paper*
(so `DeriveStoryScheduling` parallelized), the real edit-sets collided, and the
conflict only surfaced at the terminal assembly merge — after 16 TDD nodes and
~34.5M tokens.

**Intermittency makes it worse.** The 2026-06-13 run ran the *same* 4-way
over-split; its Java source happened to merge clean and only docs (README,
scratch) collided. 06-14's source converged on the driver and collided. The
over-split is a landmine #172 plants every run; whether you step on it (source)
or near it (docs) is non-deterministic. A paid run "sometimes works."

### A contradiction that fights the middle path

Reviewer rule 6 (`prompt/domain/software_render.go:1341`) still says
*"component boundaries must not overlap."* But `DeriveStoryScheduling`'s
`applyResourceEdges` (`workflow/derive_story_scheduling.go:108-156`) *already*
serializes two cross-component stories whose `FilesOwned` overlap (tested at
`derive_story_scheduling_test.go:366`). As long as rule 6's prose stands, it
penalizes the legitimate "separate components that explicitly share an entry
file" choice this ADR wants to enable.

> **Caveat on the branch name.** This ADR was drafted on a branch named
> `declare-shared-docs-serialize` (PR #184), which *implies* a declared-shared-
> ownership field already exists. It does not — there is no `shared_files` /
> `SharedOwnership` schema field anywhere. #184 was the ADR-048 merge; the only
> way to *declare* a shared file today is to list the same path in two
> components' `implementation_files[]`, which the scheduler then serializes.
> Move 1's third option rides on that existing mechanism; **no new schema field
> is needed** — only the prompt to instruct it and move 4 to stop rule 6
> rejecting it.

## Decision: three coordinated contract changes + one prose fix

All four must land together; partial application reopens one of the two traps.

### 1. Rebalance the architect (Winston) — `software_render.go:797` **and** `:814`

Edit **both** strings (`:797` carries the split-triggers and the cohesion
counter-weight removal; `:814` carries the file-count math). Replace
one-file-per-capability with **split by independently-mergeable code surface**.
A capability may be independently testable and still share one framework entry
class. The architect chooses **one** of:

- **Cohesive component** — one real single-entry-point implementation (a driver
  / plugin host / `AbstractSensorModule` subclass) owns several capabilities
  *and* their shared entry file; one Story builds it.
- **Separate components with genuinely separate files** — distinct
  handler/plugin/stream classes that build and merge independently.
- **Separate components that explicitly share an entry file** — declare the
  shared file by **listing the same path in each sharing component's
  `implementation_files[]`**. `DeriveStoryScheduling` already serializes the
  sharing stories (no new schema field, no new scheduler code). This is the
  option rule 6 must stop rejecting (move 4).

Name the over-split failure (parallel branches independently creating the same
canonical entry file → unmergeable at assembly) the way #172 named the stub
failure, so the model has **both** guardrails — not just the splitting one.

### 2. Retire the raw overload rule — `architecture_rules.go:152`

`N capabilities need N source files` (`componentOverloadedCapabilityFindings`,
wired at `:72`) is too Goodhartable; **delete it and its wiring, and migrate its
tests.** Replace with two semantic checks that land in the **same change** (no
coverage gap). Both fire at R2 (the architecture rules already no-op at R1
because `plan.Architecture` is nil there — `mergeArchitectureFindings`,
`architecture_rules.go:55`), where `component_boundaries`, `Scenarios`, and the
`Capability → Requirement.CapabilityName → Scenario.RequirementID → Tags` chain
are all in-process:

- **stub-risk:** a multi-capability component where one or more of its
  capabilities has **no per-capability evidence** — no scenario reachable via
  `Capability → Requirement.CapabilityName → Scenario.RequirementID`, or whose
  only implementation surface for that capability is a doc/config/facade file.
  Guard on `len(plan.Scenarios) > 0` so it never fires evidence-blind. (Catches
  the 06-13 stub trap by *evidence*, not file count.)
- **cohesion-violation (the inverse, new — does not exist today):** two or more
  components that imply the **same framework entry point** where that shared
  entry file is **not** declared in each component's `implementation_files[]`.
  **v1 signal: a shared `used_by` / `upstream_ref` integration target** (two
  components naming the same upstream resolution as their integration point).
  Secondary heuristic (lower priority, more Goodhartable): `implementation_files`
  whose paths/names resolve to one entry class. (Catches the 06-14 over-split at
  plan review — best-effort, since the reviewer can only reason over *declared*
  data; move 3 is the real backstop for fabricated-disjoint paths.)

Severity: default `cohesion-violation` to a level that does **not** false-reject
a legitimate cohesive driver (start as `warning` if it proves noisy — the
upstream-rule comment already documents that dial).

### 3. Fast-fail the dev-review gate to a recovery path — two components

This move spans **two** files; the routing decision **cannot** live in the
structural-validator (it emits only a `ValidationResult`, no decision/recovery
vocabulary).

**(a) `processor/structural-validator/ownership_check.go` — promote with a
declared-territory rule.** The red test
(`ownership_check_test.go`, `TestDecideOwnership_NewUnownedSourceFile_FailsContainment`)
is correct that a newly-created unowned **source/test** file must not sail
through as advisory `NewUnowned`. But a blanket hard-fail breaks the legitimate
in-package class split (two existing GREEN tests:
`ownership_check_test.go:123-128`, `:343-358`). Discriminate by **declared
territory**: compute the set of directory prefixes from the story's
`FilesOwned`; a new source/test file **under** one of those prefixes stays
advisory (legitimate split), a new source/test file **outside** all of them is
a hard-fail **ownership gap** (a new verdict bucket that `clean()` checks). This
is computable from the single story's `FilesOwned` — no sibling/architecture
data needed — and catches the 06-14 case exactly (declared
`…/driver/mavsdk/`, written `…/impl/sensor/mavsdk/MavsdkDriver.java` — different
prefix). It also catches the shared **test** file (`MavsdkDriverTest.java`),
which the planning partition can never see. Emit a distinguishable `CheckResult`
(stable check ID) so the router can recognize it.

**(b) `processor/execution-manager/component.go:1873-1893`
(`dispatchValidatorLocked`) — route it, don't dev-retry.** Today *any*
validation hard-fail → `startDeveloperRetryLocked` → (budget exhausted) →
generic escalate. That thrashes the dev on a planning defect it cannot fix.
Instead: when the failing `CheckResult` is the ownership-gap, **skip the
dev-retry budget** and fire an **immediate `RecoveryRequested`** with a crisp
`EscalationReason` naming the offending path(s). The data-rich recovery-agent —
whose prompt already discriminates the two cases — picks
`architecture_revise` (a new entry class the partition should own) or
`story_reprepare` (the story's `FilesOwned` was wrong). This **reuses** the
existing recovery → `architecture_revise`/`story_reprepare` machinery and its
`MaxAutoArchitectureRevises` guard (#119/#166); **no new `PlanDecisionKind`**.
The deterministic part (this is a planning gap, fail in seconds) is at the node;
the judgment part (which layer fixes it) is at recovery, which has the context
the node lacks.

Align the developer prompt's "surface a planning gap, don't grab the file"
(**`prompt/domain/software.go:200`** — note the ADR draft mis-cited
`software_render.go:200`; the string lives in `software.go`, added by ADR-048)
with this now-real mechanism.

### 4. Soften the "must not overlap" prose — `software_render.go:1341`

Reviewer rule 6 must permit **declared** shared ownership — i.e. the same path
listed in two or more components' `implementation_files[]`, which
`DeriveStoryScheduling` serializes. Only *undeclared* overlap (an accidental
duplicate, or a file in a sibling's scope that is not mutually declared) is a
defect. Otherwise it fights move 1's third option. Pure prose edit — the
reviewer LLM sees the full architecture JSON and can check whether a shared file
is declared in both components vs accidentally duplicated.

## Consequences

- The architect stops being rewarded for inventing files and splitting; it
  expresses mergeable surfaces. Cohesive framework drivers become one component
  again; genuinely separable handlers stay separate.
- Stub-detection moves from architecture-time file-counting to evidence
  (scenarios/tests) — where the real signal lives.
- The over-split landmine is caught at plan review (cohesion-violation) or, as a
  backstop, at the first node (dev gate → planning_gap) — never deferred to a
  multi-hour paid assembly merge.
- The three axes (capability / component / file-ownership) become explicit and
  non-conflated; ADR-044's overload rule and the #172 prompt are superseded by
  the cohesion-aware contract.

## Resolved decisions (were open questions)

- **Cohesion-violation detection signal → shared `used_by` / `upstream_ref`
  target (v1).** Two or more components naming the same upstream resolution as
  their integration point, where the shared entry file is not mutually declared.
  Secondary heuristic (path/name resolves to one entry class) is lower priority
  and more Goodhartable. Accepted as a **partial net**: the reviewer can only
  reason over *declared* data, so it cannot catch fabricated-disjoint paths —
  **move 3 is the real backstop** for those.
- **stub-risk fires at R2 with scenario evidence.** The architecture rules
  already only effectively fire at R2 (architecture is nil at R1), and scenarios
  are generated before R2, so the `Capability → Requirement → Scenario → Tags`
  chain is reachable. Guard explicitly on `len(plan.Scenarios) > 0` so a future
  ordering change can't make it fire evidence-blind.
- **Ownership-gap routing → fast-fail at the node, recovery-agent picks the
  layer.** The node deterministically classifies "this is a planning gap" (and
  skips the dev-retry budget); the recovery-agent — which has the full plan
  context the node lacks — chooses `architecture_revise` vs `story_reprepare`.
  Reuses existing machinery; no new `PlanDecisionKind`. The
  declared-territory (path-prefix) rule keeps the node from false-failing
  legitimate in-package splits without needing sibling visibility.

## Already delivered by ADR-048 (do not re-build)

ADR-048 (commit `257edce2`, merged as PR #184) already shipped the scaffolding
this ADR extends — confirmed present in code:

- the file-ownership containment gate (`ownership_check.go`) and the
  `FilesOwned → Execute` wiring (re-tested by #186);
- the `architecture.scoped_file_unowned` reviewer rule
  (`scopedFileOwnershipFindings`, #175) — note it reads `plan.Scope`, so it
  does **not** catch an emergent canonical entry class no one listed in scope
  (the exact 06-14 gap → why move 3 is necessary, not redundant);
- the terminal `PlanDecisionKindAssemblyConflict` honest-failure path
  (`plan-manager/execution_events.go`) — the backstop that fired correctly on
  06-14;
- the developer "stay inside your file scope / planning gap" prompt fragment
  (`software.go:200`).

ADR-049's **net-new deltas** are exactly the four moves above.

## Deferred follow-up (out of scope here)

- **Test files in the planning partition.** `FilesOwned` never includes test
  paths unless the architect types them, so the planning-time partition is
  structurally blind to shared test files. This ADR covers the test-file half at
  **execution time** (move 3's declared-territory rule sees the real worktree
  `git status`, including new test files). Bringing test-path ownership into the
  partition itself (changing `implementation_files` semantics or deriving test
  paths) is a larger, separable change tracked as a follow-up.

## First red (already written)

`TestDecideOwnership_NewUnownedSourceFile_FailsContainment`
(`processor/structural-validator/ownership_check_test.go`) reproduces the 06-14
wedge against the pure `decideOwnership` seam and is RED today (new unowned
source → advisory `NewUnowned` → `clean()==true`). It is the executable anchor
for move 3.
