# ADR-047: Upstream Interface-Contract Completeness (Method-Signature Resolution for source_build Deps)

**Status:** **WITHDRAWN (2026-06-23)** — never accepted past Proposed. The completeness gate
(`upstreamSourceBuildContractFindings`) was the interim Option-A stopgap; the 2026-06-23
`gemini WITH_EPIC mavlink-hard` run is the evidence the ADR was waiting for, and it confirmed the
architect plateaus on `source_build` method resolution *even with the gate forcing it* (read the
source, named the class, emitted zero method entries, 3 rounds → unconvergeable reject). Rather than
build Option B (a dedicated resolver phase — still an LLM doing the same read, no reason to beat the
architect), we **removed the requirement from the architect and moved method-contract resolution to
the developer** (the BMAD/OpenSpec division of labour: the architect names the dependency + verified
coordinate + paste-ready import; the dev reads `/sources/`/the jar for the exact method signatures at
implementation time). `ValidateUpstreamImports` (the symbol-level import gate, #134) is retained — a
verified import is cheap to resolve and prevents the package-guessing wedge, a separate problem. The
gate, its tests, and the architect prompt's method-enumeration demand are deleted; the developer
prompt now directs proactive `/sources/` contract resolution so the move does not re-arm the
3.5M-token compile-error thrash this ADR originally targeted.

**Superseded by:** the developer-side resolution model (this withdrawal). ADR-046's resolver-phase
fork is moot under it — there is no LLM resolution *step* to place.

---

**Original status:** Proposed (2026-06-13)
**Update (ADR-049, 2026-06-14):** the sibling rule
`architecture.component_overloaded_capabilities` referenced below was retired by
[ADR-049](ADR-049-component-ownership-topology.md) in favour of the
evidence-based `architecture.component_stub_risk`. The pattern this ADR mirrors
(a deterministic plan-reviewer rule appended in `mergeArchitectureFindings`)
still stands; only the named sibling changed.
**Deciders:** Coby, Claude
**Related:** ADR-046 (dedicated dependency-resolver phase — this ADR is the *completeness* half of
the same problem and frames where resolution should live), ADR-044 (M:N capability ↔ Story; sibling
`architecture.component_overloaded_capabilities` rule lives in the same reviewer file), ADR-043
(architect emits implementation files), ADR-037 (wedge recovery — the dev thrash this ADR prevents
is the kind of recoverable-but-expensive wedge recovery exists to catch *after* the fact), ADR-035
(strict-parse — the completeness gate is a strict, deterministic check, not a soft prompt nudge).
**Drives:** a plan-time completeness gate (plan-reviewer rule and/or architect self-check), a scoped
`/sources/` grant to whichever loop resolves contracts, and a decision on *where* method-contract
resolution runs (extend the architect vs a dedicated resolution step).

## Context

### The thrash (verified live, 2026-06-13)

In a `gemini` `WITH_EPIC` `mavlink-hard` run, the developer loop burned roughly 3.5M tokens on
`gemini-pro` reverse-engineering OpenSensorHub's `ICommandStatus` interface contract through
*compile errors* — even though the upstream source file was mounted at `/sources/` and the dev was
reading it. The dev discovered, one failed compile at a time, that `getProgress()` returns `int` not
`float`, that `getExecutionTime()` returns `TimeExtent` not `Instant`, and so on for each method a
subclass must implement.

The reactive feedback loop is *good*: the compile error reaches the model and the model acts on it.
The gap is **proactive** — nobody handed the dev the exact method contracts up front, so the dev
rediscovers them by trial-and-error against the compiler. Each rediscovery is a full TDD sub-cycle:
write code → compile → read the error → re-read source → adjust. That is the most expensive possible
way to learn a method signature, and it is learning a fact the upstream source states plainly.

This is the same failure family the codebase already documents at the symbol level: take-23
(2026-05-13) wedged at `iter=80` with 35 external file reads and 0 worktree writes because the
architect named OSH classes without resolving their constructor + lifecycle; the 2026-06-07 mavsdk
run burned 3.4M tokens running `javap` to find `io.mavsdk.System` because only the bare symbol
`System` was resolved. We hardened *imports* for that case (`ValidateUpstreamImports`,
`workflow/plan_story.go:541`). The `ICommandStatus` thrash is the next layer up: the import is
correct, the *method contracts of the resolved type* are missing.

### The maven_central vs source_build split

The split is structural, by `resolution_kind` (`workflow/upstream_resolution.go`):

- **`maven_central`** deps are resolved *completely*. The architect has a published jar; the prompt
  directs it to `jar tf` / `unzip` / `javap` the artifact (`prompt/domain/software.go`), and the
  system re-verifies the coordinate against Maven Central (`numFound>0`). The dev gets paste-ready
  imports and signatures.

- **`source_build`** deps (OSH and friends — no published jar) get only the *class declaration* plus
  a lifecycle string. The architect names the class, satisfies `ValidateUpstreamImports` with one
  valid import, and stops — there is no gate forcing it to enumerate the *methods a subclass must
  implement*. The dev then thrashes on exactly those unresolved methods.

`ValidateUpstreamImports` (`workflow/plan_story.go`) confirms this is the gap: it iterates the
code-symbol import kinds and rejects any *named* surface whose `Import` is missing or bare. It does
not check whether the *set* of named surfaces is complete for a class a subclass must extend. A
`source_build` class with one valid import and zero method surfaces passes today.

## "Mechanism exists" finding (this is population/completeness, not new plumbing)

The schema and the entire injection-to-dev pipeline already carry method-level contracts end to end.
Verified against current code (2026-06-13):

- **Schema.** `workflow/types.go` `UpstreamResolution` with `ResolutionKind` and `APIs []APISurface`;
  `APISurface` carries `Signature`, `Lifecycle`, `Import`, `Notes`, and a required `Citation`. The
  architect can *already* emit per-class constructors, methods, and lifecycle. The schema-wiring bug
  ADR-046 flagged was fixed (#136; `TestArchitectureSchemaStructParity` is the standing drift guard).

- **Injection.** The pipeline that lands these in the dev's prompt is live and tested three ways:
  `processor/execution-manager/component.go` sets `TaskContext.UpstreamResolutions` via
  `prompt.ProjectUpstreams(...)`; `prompt/architecture_project.go` copies `Signature` and `Lifecycle`
  into `UpstreamResolutionInfo`; fragment `software.task-upstream-resolutions`
  (`prompt/domain/software.go`) calls `FormatUpstreamResolutions` → `writeUpstreamsBody`
  (`prompt/architecture.go`), which emits `Import`, `Signature`, `Lifecycle`, and `Notes` *verbatim*;
  covered by `prompt/domain/task_upstream_resolutions_test.go`.

So a `source_build` class whose `APIs[]` *did* enumerate `getProgress() -> int`,
`getExecutionTime() -> TimeExtent`, etc., would already reach the dev verbatim, before the first
compile. **No new dev-prompt plumbing is needed.** The fix is to *populate* those fields and to
*gate* on their completeness. This ADR is about completeness, not pipes.

## The completeness gate (never built)

The plan-reviewer (and/or an architect self-check at parse time) MUST reject a plan where a
`source_build` class named as a component dependency lacks the subclass-required method signatures.
Concretely, for every `UpstreamResolution` with `ResolutionKind == "source_build"` whose `APIs[]`
names a `class` or `interface` the project *extends or implements*, the resolution must also carry,
as paired `APISurface` entries:

1. The **constructor(s)** a subclass must call (`Signature` populated).
2. The **abstract / interface methods** a subclass must implement — each with a full `Signature`
   (parameters + return type), not just a name.
3. The **referenced config / parameter types** those signatures mention (e.g. `SensorConfig`,
   `TimeExtent`) named as their own surfaces, so the dev does not have to chase a second
   undocumented type.

This is the inverse of `ValidateUpstreamImports`: that gate checks each *named* surface is
*qualified*; this gate checks the *set* of named surfaces is *complete* for an extension point. It
mirrors the existing sibling rule `architecture.component_overloaded_capabilities`
(`processor/plan-reviewer/architecture_rules.go`, PR #172): a deterministic structural backstop that
fires at plan review, surfaces one `PlanReviewFinding` per offending dependency with a concrete
remediation, and calls `result.NormalizeVerdict()` so `approved` → `needs_changes`.

Implementation shape (matching the existing rule style in `architecture_rules.go`):

- New rule id `architecture.upstream_source_build_incomplete_contract`, `Severity: "error"`,
  `Status: "violation"`, `Category: "structural"`, `Phase: "architecture"`,
  `TargetID: <UpstreamResolution.Name>`, `Action: "add"`,
  `TargetField: upstream_resolutions.<name>.apis`, with an `Issue` naming the class and the missing
  contract members and a `Suggestion` pointing at the `/sources/` path to read.
- Append it alongside `componentOverloadedCapabilityFindings` in `mergeArchitectureFindings`,
  reusing the same skip-when-nil-architecture guards.

### Goodhart-safe success measure (carry verbatim in spirit)

Measure **structural completeness, not output size.** The metric is: *every `source_build` class or
interface named as an extension point has a paired, signature-bearing `APISurface` for each
constructor and abstract/interface method a subclass must implement, plus every config/parameter
type those signatures reference.* Do **not** measure resolution length, API count, signature string
length, LOC, or file count — models pad to length metrics. A class with three required methods needs
exactly three method surfaces; ten padded ones are not "more complete," and one is incomplete.

**Punish false claims of completeness, not honest incompleteness.** A resolution honestly marked
`ResolutionKind: "unresolved"` is a reviewable gap, not a gate violation (the existing prompt already
treats `unresolved` as a first-class honest flag — `prompt/domain/software.go`, `workflow/types.go`).
The gate fires only when a `source_build` class is *claimed resolved* (it has at least one named
surface and is used as an extension point) but its contract is *partial*. The Goodhart risk — a
fabricated signature — is caught downstream when the dev's code fails to compile, a far cheaper
failure than the 3.5M-token discovery loop. This is the same cost trade `ValidateUpstreamImports`
already makes.

**Fail fast and cheap at the architect / plan-reviewer, not slow at the dev.** The whole point is to
move the discovery from `gemini-pro` compile-error iteration (expensive, per-method, mid-execution)
to a plan-time read of the same source file (one pass, before any dev loop dispatches). The gate is
the forcing function that relocates the work to where it is cheap.

## Giving the resolver `/sources/` access

The architect already routes `bash` through the sandbox: there is a single global bash executor
(`tools/register.go`, `bash.NewExecutor(repoRoot, os.Getenv("SANDBOX_URL"), ...)`), and the
architect's toolset includes `bash` and `http_request`
(`processor/architecture-generator/component.go` `availableToolNames`). Under `WITH_EPIC`, the
sandbox container mounts the pre-cloned upstream trees read-only at `/sources/`
(`docker/compose/e2e-epic.yml:96`, `semsource-clones:/sources:ro`). The architect prompt already
*instructs* reading them: `bash('ls /sources/ 2>/dev/null')`, read `pom.xml`, read raw source, run
`javap`/`unzip`/source-tree reads (`prompt/domain/software.go`).

So the access exists today **when `SANDBOX_URL` is set and `WITH_EPIC` is on** (in a non-epic run
`/sources/` is not mounted at all — but then no actor can resolve a `source_build` dep, which is a
separate fixture/config concern). The gap is not raw access; it is **completeness of use** — the
architect reads `/sources/` enough to name the class and get the import, then runs out of
budget/attention before enumerating every method (the ADR-046 plateau finding: it cannot fit N-deps ×
M-symbols of mechanical extraction alongside its design job in one budget). Two follow-on notes:

- `WITH_EPIC` also runs ast/docs indexing into the graph (~10K entities) that the agents do not query
  (they read `/sources/` directly) — wasteful and a graph-flood confounder. A **stripped mount**
  (clone `/sources/` for bash reads, skip the ast/docs indexing) was proposed separately and should
  land regardless of A-vs-B; it makes the access cheap without the indexing tax.
- Whatever loop owns contract resolution needs `/sources/` in scope as a *first-class, scoped* tool
  grant, not an incidental side effect of the global bash sandbox. Today it is the latter.

## Decision point (OPEN — both framed, one recommended, left to the human)

**Where does detailed API-surface / method-contract resolution happen?**

### Option A — extend the existing architect

One loop does component boundaries *and* resolves every named `source_build` class's full method
contract from `/sources/`.

- **Pro:** simplest wiring — the schema, injection, and `/sources/` access are all already on the
  architect's side; this ADR becomes "tighten the prompt + add the completeness gate," no pipeline
  stage.
- **Con:** overloads one mid-tier-model loop (decide boundaries + read all of `/sources/` + extract
  every method signature) — the exact budget/attention competition ADR-046 documented as a *stable
  plateau* on hard fixtures, not a tuning problem. The completeness gate would then reject the
  architect repeatedly without the budget to satisfy it — recreating the unwinnable-loop shape that
  motivated ADR-046.

### Option B — a dedicated plan-phase API-surface-resolution step

A focused step runs **after** the architect sets component boundaries and names its `source_build`
dependencies, with scoped `/sources/` access, and *populates* the existing
`UpstreamResolution.APIs[].Signature/Lifecycle` fields. This is exactly the resolver phase ADR-046
specifies (architect emits `dependency_requests` / names symbols → resolver produces verified
`UpstreamResolution` with its own budget). This ADR is the *completeness contract* that step must
satisfy for `source_build` deps.

- **Pro:** tractable per loop (one focused job, its own iteration budget), focused tool scope
  (`/sources/` + bash, nothing design-y), and the completeness gate becomes the step's *done-criteria*
  rather than an architect-rejection — the ADR-046 framing where campaign gates become loop-exit
  contracts.
- **Con:** adds a pipeline stage with a durable handoff (ADR-046 "Placement & durable handoff":
  intermediate `architecture_drafted` status, per-step retry keys, restart/replay survival). More
  moving parts than A.
- **Critically, this is plan-time, NOT dev-time.** It does **not** reincur the shelved `research()`
  objection (see Out of Scope), which was specifically about *dev-time* handoff latency blocking a
  blocked dev loop. A plan-phase step runs once, before any dev dispatches, on its own budget — there
  is no dev loop waiting on it.

### Recommendation

**Option B, as the `source_build`-completeness contract on ADR-046's resolver step** — but ship the
completeness *gate* (the plan-reviewer rule) and the *stripped `/sources/` mount* first, because they
are valuable under either option and independently testable offline. Rationale: the `ICommandStatus`
thrash is the *exact* "N-symbols of mechanical source extraction starves the design loop" failure
ADR-046 was written for; folding contract completeness into the architect (Option A) walks back into
that plateau. The gate is the forcing function; the resolver step is the loop with the budget to
satisfy it. Until the resolver step lands, the gate plus the tightened architect prompt is the
interim (Option-A-shaped) stopgap — fail-fast at plan review beats a 3.5M-token dev thrash even if the
architect occasionally bounces on it.

This decision is **left open for the human**: the 2026-06-13 run is the re-test ADR-046 was gated on
(post-#136 schema fix + #172 granularity fix). It showed the architect now decomposes correctly and
*has* `/sources/` access, yet still did not resolve the `source_build` method contracts — but with no
completeness gate in place, we cannot yet distinguish "architect can't (→ Option B)" from "architect
was never forced to (→ Option A may suffice)." Ship the gate, re-run, and let that evidence decide:
if the architect converges on `source_build` contracts inline once the gate forces it, Option A is
enough and ADR-046's resolver stays deferred; if it plateaus on resolution depth, Option B is
justified and this ADR's completeness contract becomes the resolver's `source_build` done-criterion.

## Goodhart guards

Carried explicitly so they survive into implementation and review:

- **Structural completeness, not output size.** The pass condition is "every required contract member
  of a claimed-resolved `source_build` extension point has a signature-bearing surface," counted
  against the source's actual member set — never API count, signature length, LOC, or file count.
- **Punish false claims, not honesty.** `ResolutionKind: "unresolved"` is a reviewable honest gap and
  never trips the gate. The gate fires only on a *claimed-complete* contract that is *partial*.
- **Fail fast and cheap, upstream.** Relocate discovery from per-method dev compile iteration
  (expensive, mid-execution) to one plan-time source read (cheap, pre-dispatch). A fabricated
  signature is caught at the dev's compile, the cheapest possible backstop — strictly better than the
  unbounded discovery loop the gate exists to prevent.

## Consequences

- The architect (or resolver) does the source read once; the dev gets full method contracts before
  the first compile; the `ICommandStatus`-class thrash disappears for any `source_build` dep that
  reaches the gate.
- `maven_central` deps are unaffected — they already resolve completely; the gate is
  `source_build`-scoped.
- One new deterministic plan-reviewer rule, in the same file and style as PR #172's sibling rule, with
  one `PlanReviewFinding` per offending dependency. Offline-testable (no paid run) — predict and
  reproduce the rejection with `go test` before any LLM burn.
- A scoped `/sources/` grant (and the stripped mount) make the resolving loop's access first-class
  rather than an incidental sandbox side effect.
- Interaction with recovery (ADR-037): a plan that trips this gate is a *plan-phase* rejection
  (architect/resolver revise), not an execution wedge — it never reaches the dev, so recovery never
  has to diagnose it. This removes a recoverable-but-expensive wedge class from the execution tail.

## Out of scope (related upstream-strengthening layers, separate ADRs/future work)

- **Reviving the dev-time `research()` sub-agent.** Built and deliberately shelved (2026-05-15): it
  shuffled context between loops without reducing it, and researcher latency (5-12 min) blew past the
  dev's wait window. The infrastructure still exists in code (`tools/research/*`,
  `processor/researcher-manager`, fragments in `prompt/domain/software.go`) but is granted to the dev
  only in `configs/e2e-hybrid*.json`, not the default `configs/semspec.json`. This ADR does **not**
  propose reviving it. The strategy lesson stands: *reduce the work (K) at the source; do not shuffle
  it to a sub-agent.* Option B above is plan-time, not dev-time, and so does not reincur this
  objection — that distinction is load-bearing, not a loophole.
- **Req-gen API-surface partitioning.** Whether requirements should pre-partition external API surface
  across requirements is a related upstream-strengthening layer — separate ADR.
- **Graph-triple cross-cycle memory of resolved contracts** (so a resolved `source_build` contract is
  reused across cycles / runs rather than re-resolved) is the post-MVP graph-native evolution ADR-046
  sketches. Separate ADR; mentioned only as the eventual backend upgrade.
- **The dedicated resolver *phase* itself** is ADR-046's decision. This ADR specifies the
  `source_build`-completeness *contract* that phase (or the extended architect) must satisfy; it does
  not re-decide whether the phase exists.

## Cross-references

- ADR-046 — dedicated dependency-resolver phase; this ADR is its `source_build`-completeness contract
  and the framing of the A-vs-B placement decision. The 2026-06-13 run is the re-test ADR-046 is
  gated on.
- `processor/plan-reviewer/architecture_rules.go` — `architecture.component_overloaded_capabilities`
  (PR #172), the sibling rule whose style the new gate mirrors.
- `workflow/plan_story.go` — `ValidateUpstreamImports`, the symbol-level gate this layers on top of.
- `workflow/types.go`, `prompt/architecture.go`, `prompt/architecture_project.go`,
  `prompt/domain/task_upstream_resolutions_test.go` — the existing schema + injection pipeline this
  ADR reuses unchanged.
- 2026-06-13 `gemini WITH_EPIC mavlink-hard` run — the `ICommandStatus` thrash that motivated this ADR.
