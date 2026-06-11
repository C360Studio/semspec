# Mutation-API / Graphable-Bypass Audit

Status: audit-only. No code changes in this pass. Pinned to
`github.com/c360studio/semstreams v1.0.0-beta.103`.

## Scope and stance

This audit examines where semspec writes graph entity state through the
`graph.mutation.*` subjects (via `workflow/graphutil.TripleWriter`) instead of
declaring entities through the canonical `graph.Graphable` + `graph.ingest.entity`
path. It runs in parallel with a similar audit on the semstreams side.

The headline result corrects the framing of the initial pass: **for mutable,
repeatedly-republished entities the mutation subject is not abuse — it is the only
correct primitive at beta.103.** The genuine problems are narrower (truly conjured
entities, raw per-triple writes used as a write path) plus one internal dual-writer
hazard the first pass missed entirely.

## The load-bearing contract (verified against beta.103)

| Primitive | beta.103 behavior | Source |
|-----------|-------------------|--------|
| `graph.Graphable` | Pure declaration contract: `EntityID()` + `Triples()`. No replace/merge semantics. | `graph/graphable.go:54` |
| `graph.ingest.entity` → `MergeEntity` | Append-only: `existing.Triples = append(existing.Triples, entity.Triples...)` on every re-publish; bumps `Version`; never replaces scalars. | `graph-ingest/component.go:946` |
| `graph.mutation.entity.update_with_triples` | Only replace-own-predicates primitive: CAS that drops `RemoveTriples` predicates then appends `AddTriples`. | `graphutil/triple.go:524`, `:635` |

Consequence: for an entity re-published on every status/phase change,
`graph.ingest.entity` accumulates unbounded duplicate triples and lets stale
scalars (e.g. an old `status`) coexist with new ones — the rule engine reads
first-match while lifecycle reads last-match, so the divergence is a silent
correctness bug. This is the gh#177 class, and the write amplification that
Phase 3a eliminated. `TripleWriter.UpsertEntityIfChanged` over
`update_with_triples` is the deliberate, correct choice for these entities; the
reasoning is recorded inline at `graphutil/triple.go:568-603`.

**Implication for any fix:** "migrate entity persistence to `graph.ingest.entity`"
is safe only for write-once entities. Applying it to mutable entities would
reintroduce gh#177 corruption and undo the merged Phase-3a leak fix.

## The refined "abuse" definition: `triple.add` implicit entity-create

The abuse is not "use of `graph.mutation.*`" and not "failure to be Graphable." It
is narrower and sharper: **`graph.mutation.triple.add` silently materializes a
missing entity from just `triple.Subject`, with no entity-declaration metadata.**

`AddTriple` (`graph-ingest/component.go:1408-1438`) does an upsert: on a missing
KV record it creates `EntityState{ID: triple.Subject, Version: 0}` — no
`MessageType`, no producer contract, no Graphable-equivalent declaration — then
appends the triple. Yet the request contract advertises the opposite:
`AddTripleRequest` is documented as "adds a triple **to an existing entity**"
(`graph/mutation_requests.go:77`). That contract-vs-behavior gap is the footgun:
any `TripleWriter.WriteTriple` (and the add-half of `UpdateTriple` /
`ReplaceTripleList`) can conjure a metadata-less entity shell.

By contrast, the create-bearing path already carries metadata:
`CreateEntityWithTriplesRequest.Entity` is a full `EntityState`
(`mutation_requests.go:39`), so `UpsertEntity`'s create fallback stamps
`MessageType` on creation. And `update_with_triples` already supports optimistic
concurrency: `UpdateEntityWithTriplesRequest.ExpectedRevision` (ADR-049,
`mutation_requests.go:61-71`) rejects on revision mismatch when non-zero — the
primitive `lifecycle.Manager.Transition` uses for state-machine races. Every
semspec caller passes zero today.

### Write-class taxonomy

Classify every graph write into one of three classes and bind each to a primitive:

| Class | Definition | Correct primitive | Metadata requirement |
|-------|------------|-------------------|----------------------|
| **state** | mutable scalar lifecycle on an entity this writer owns | `update_with_triples` (replace-own); thread `ExpectedRevision` for contended entities | none beyond create-time |
| **evidence** | append-only fact / relation stamped onto an entity that ALREADY exists | `triple.add` is acceptable **only** when the subject is guaranteed to exist | none (annotates an existing node) |
| **entity-create** | materializing a new node | `create_with_triples` carrying `EntityState` with `MessageType` (Graphable-equivalent metadata) | **required** |

The abuse is any **entity-create performed through `triple.add`'s implicit
upsert** — i.e. an evidence/state write whose subject did not yet exist, so it
births a metadata-less node instead of going through `create_with_triples`.

### The abuse set in semspec (precise)

Entity-create via `triple.add` (metadata-less conjuring) — these are the genuine
"abuse" sites:

- `workflow/lessons/writer.go` `RecordLesson` — conjures the lesson node via `WriteTriple`.
- `workflow/parseincident/emit.go` — conjures the standalone incident node via `WriteTriple`.
- `workflow/recoveryhint/emit.go` — conjures the recovery-incident node via `WriteTriple`.
- `processor/project-manager/project_store.go` — the first `UpdateTriple` on a
  fresh `project`/`checklist`/`standards` entity conjures it (no `MessageType`).
- `agentgraph.Helper` (dead) — a worse variant: raw `kv.Put` whole-entity, no metadata.

State-via-`triple.add` (wrong primitive for mutable lifecycle, no CAS) — the
[#153](https://github.com/C360Studio/semspec/issues/153) dual-writer set in
`execution-manager` `component.go` / `task_watcher.go`. Note `task_watcher.go:164-176`
initializes the task-execution entity via `UpdateTriple`; if that is the first
write, the entity is **born without `MessageType`** and the later
`update_with_triples` path will not backfill it (update only adds/removes triples)
— a concrete instance of the conjure-then-orphan hazard (ordering not yet traced).

NOT abuse: every `UpsertEntity` / `UpsertEntityIfChanged` caller — create goes via
`create_with_triples` (metadata-bearing), update via `update_with_triples`.

## Per-finding verdicts

| ID | Claim | Verdict | Correction |
|----|-------|---------|------------|
| F1 | `TripleWriter` is a mutation facade; large blast radius | Confirmed | Split it: `UpsertEntityIfChanged` (batched + sha256 dirty-track) is the Phase-3a-blessed form; raw `WriteTriple`/`UpdateTriple`/`ReplaceTripleList` used as a write path is the worst form. |
| F2 | plan-manager persists plan + children via mutation | Confirmed, framing wrong | Entities are mutable-republished; mutation is forced, not abuse. Not ingest-migratable. |
| F3 | execution-manager bypassed | Confirmed, framing wrong | Same as F2. `payload_registry.go:25` cite is the comment; methods at `:36/:39`. `publishEntity` writes only foreign rel-edges (legit-derived-stamp). |
| F4 | requirement-executor mutates Graphable-ish entities | Confirmed, sharpened | `RequirementExecutionEntity` mutable → mutation correct. `DAGNodeEntity` is Flavor-A conjured (no registered dag-node payload); its doc comment ("for `graph.ingest.entity`") contradicts the code. |
| F5 | project config conjured, no Graphable payload | Confirmed | Verified: zero `RegisterPayload` for project-config. Lower severity — `UpdateTriple` is scalar-safe (no append leak), writes are rare. Real gaps: no Graphable contract, per-triple amplification, and a full marshaled JSON blob stored as a triple object (`project_store.go:244`, `ProjectConfigJSON`). |
| F6 | lessons + incidents create sidecar nodes by mutation | Confirmed, split | Incidents (`parseincident/emit.go`, `recoveryhint/emit.go`) are write-once → the clean migration target. Lessons are NOT write-once — `RetireLesson` (writer.go:311) + `RotateLessonsForRole` (writer.go:276) mutate them post-creation. |
| O1 | exec-manager + requirement-executor co-write same req-exec entity | Confirmed — real single-writer violation | Predicate sets overlap on six predicates (`Type, Slug, Phase, TraceID, NodeCount, ErrorReason`); each writer's `RemoveTriples` strips the other's values → last-writer-wins. `execution_store.go:493-502` admits a "deferred ownership decision." |
| O2 | DAGNode published under wrong message type | Confirmed | `publishEntity` hardcodes `RequirementExecutionPayloadType` for every entity (`payload_registry.go:74`); dag-nodes are stamped `requirement-execution/v1` though a correct `DAGNodeEntityType` is registered and unused (`workflow/entity.go:267`). Mislabeled `MessageType`, not a misrouted subject. |
| O3 | agentgraph.Helper direct ENTITY_STATES KV writes | Confirmed but DEAD | `graph.go:114/310` are raw `kv.Put` whole-entity overwrites bypassing both canonical paths — but test-only dead code (only `graph_test.go` constructs `Helper`). Downgrade to: delete dead code. |

## What the first pass missed

### 1. TaskExecution has two live writers using two different primitives

Highest-priority finding, independent of the ingest-vs-mutation axis. Tracked as
[C360Studio/semspec#153](https://github.com/C360Studio/semspec/issues/153).

- `execution_store.writeTaskTriples` — atomic `UpsertEntityIfChanged` with a
  sha256 dirty-track cache and `OwnedPredicates` = the full scalar lifecycle set
  (`execution_store.go:463-485`).
- `component.go` + `task_watcher.go` — ~20 out-of-band `UpdateTriple` /
  `ReplaceTripleList` calls writing the same predicates (`task_watcher.go:164-176`,
  `component.go:920-921,1075-1080,1148,1275,1318-1321,1421-1425,1507-1508,1876-1879,1901`).

The out-of-band writes mutate the graph without updating the dirty-track hash, so
a later `saveTask` whose `exec` fields hash-match the cache is silently skipped,
leaving whatever the fan-out last wrote. A few predicates (`ErrorClass`,
`CurrentStage`, `Prompt`) are written only by the fan-out and are absent from
`OwnedPredicates` → mixed ownership. The divergence is latent (provable from the
code; runtime interleaving not yet traced) and needs single-writer consolidation
regardless.

### 2. Plan entity also has a second live writer

`graph_marshal.go`'s fan-out `writePlanTriples` (UpdateTriple + ReplaceTripleList)
is not dead — `workflow/project.go:112` calls it live, alongside
`plan_store.writePlanTriples` (which uses `UpsertEntityIfChanged`). Same
dual-primitive shape as #1 for the Plan entity; verify both fire on the same plan
before asserting impact.

### 3. Other missed call sites (all forced-mutation or clean)

`question-manager` Question via `UpsertEntity` (mutable; has an unused
`EntityPayload`); plan-manager `external_spec` + deletion-tombstone stamps
(legit-derived); `plan-decision-handler` Cascade (forced-mutation). One Flavor-A
candidate: `processor/ast/entities.go` `CodeEntity` has `Triples()` but no
`EntityID()` (not Graphable) and appears unwired to any graph writer — likely a
semsource-routed extraction lib or vestigial; verify before acting.

### 4. A refuted hypothesis (recorded for completeness)

`execution-manager/config.go:168` and `requirement-executor/config.go:138`
(`graph.mutation.triple.add`) are output port declarations, not consumer
subscriptions. There is no mutation-stream feedback loop.

## Fix shape (corrected) — five-point program

CAS mutation is first-class. The target is not "make everything Graphable"; it is
to bind each write class to the right primitive and to close the `triple.add`
entity-create footgun.

**1. Keep CAS mutation as first-class.** `graph.mutation.entity.update_with_triples`
(replace-own-predicates) stays the canonical write path for mutable entities. The
Phase-3a `UpsertEntityIfChanged` path is correct and is retained.

**2. State writers use `update_with_triples` with `ExpectedRevision`.** Thread the
entity's last-known KV revision (ADR-049) so two writers transitioning the same
entity from the same revision can't both silently commit — the loser gets a
revision-mismatch and re-reads. This is the canonical fix for the
[#153](https://github.com/C360Studio/semspec/issues/153) TaskExecution dual-writer
incoherence, and applies equally to the Plan second-writer (#2 above) and the
RequirementExecution six-predicate overlap (O1). Replaces the ad-hoc `UpdateTriple`
fan-outs in `execution-manager` `component.go` / `task_watcher.go` with a single
CAS state writer.

**3. Stop or constrain `triple.add` upsert-create.** The semstreams-side ask:
`AddTriple` should not silently materialize a missing entity. Either reject on
missing subject (forcing callers to create first) or require the create to carry
metadata. This structurally prevents metadata-less conjuring. Until that lands,
semspec entity-creators must stop using `triple.add` to create (point 4).

**4. Require entity-declaration metadata only on create.** When a write
**creates** an entity (or declares a new entity-producer contract), route it
through `create_with_triples` with a `MessageType` (Graphable-equivalent
metadata). Mutating an existing entity's predicates needs no re-declaration.
Concretely: the abuse-set creators — lessons (`writer.go`), parse incidents
(`parseincident/emit.go`), recovery incidents (`recoveryhint/emit.go`), project
config (`project_store.go`) — switch their node-create from `WriteTriple` /
`UpdateTriple` to a metadata-bearing create, and gain a registered payload. The
write-once incidents can use `graph.ingest.entity` (a registered Graphable
payload; append-merge harmless). Lessons and project config are mutable, so they
create via `create_with_triples` + update via `update_with_triples`, not ingest.
Also: DAGNode (O2) is a create that stamps the wrong `MessageType` — plumb each
Graphable's own `message.Type` into `publishEntity` and use `DAGNodeEntityType`.

**5. Make the write-API taxonomy explicit.** Add a "state vs evidence vs
entity-create" distinction to `TripleWriter` (or its callers) so the choice of
primitive is enforced, not incidental: a `StampEvidence` that asserts the subject
exists, a `CreateEntity` that demands metadata, and the existing
`UpsertEntityIfChanged` for state. Delete dead `agentgraph.Helper` (O3) and the
project-config JSON-blob-in-triple (F5) as part of this pass.

**Still blocked on semstreams:** the mutable entities (Plan, Requirement, Scenario,
Capability, PlanDecision, Question, Task/RequirementExecution, DAGNode, Cascade,
lesson body) cannot move to `graph.ingest.entity` until that path gains
replace-own semantics — they stay on `update_with_triples`. That is consistent
with point 1, not a migration target.

## Cross-audit coupling (the ask for semstreams)

The semspec and semstreams audits are coupled at two points:

1. **Constrain `triple.add` (point 3).** Make `AddTriple` reject-on-missing or
   require create metadata, so the implicit metadata-less upsert-create can't
   happen. This is the structural fix that turns "abuse" from a discipline problem
   into an impossible one.
2. **A replace-capable Graphable path.** So that mutable entities could one day be
   declared canonically: either a Graphable-aware ingest variant that does
   CAS-replace-own (like `update_with_triples`) instead of `MergeEntity`'s raw
   append, or an explicit blessing of `update_with_triples` as the canonical write
   path for mutable Graphable entities (in which case semspec is already correct).

Until those land, treating the mutation **subject** as abuse for mutable entities
is the wrong target. The real abuse is `triple.add` entity-create; the actionable
semspec work (points 2, 4, 5) stands on its own.

## Impact: the incoming `triple.add` must-exist change

semstreams is changing `graph.mutation.triple.add` to **must-exist** semantics
(ADR pending): `AddTriple` will stop conjuring a missing entity. This is point 3 of
the program landing upstream. Per-entity ordering was traced to classify exposure
(create-via-`triple.add` breaks; stamp-onto-existing survives; CAS paths are
untouched).

**Failure mode is silent, not loud.** Almost nothing crashes — the exposed sites
are best-effort observability writes whose errors are swallowed. The realistic
outcome is that affected subsystems **quietly stop populating the graph**, noticed
only when something reads them empty.

### Safe — no change (the whole CAS mirror)

Everything created via `UpsertEntity` / `UpsertEntityIfChanged` already creates
through `create_with_triples`, never `triple.add`:

| Entity class | Why safe |
|--------------|----------|
| **TaskExecution** (~11 `UpdateTriple` sites, `component.go` / `task_watcher.go`) | `saveTask → writeTaskTriples → UpsertEntityIfChanged` runs synchronously inside the create mutation, **before the KV put is visible** to the watcher — the create wins the race every time. |
| **RequirementExecution + DAGNode** | All writes via `UpsertEntity`; no raw triple writes exist. |
| **Plan + children (production)** | `handleCreatePlan → planStore.writeTriples → UpsertEntityIfChanged`. |
| **Question, Cascade** | `UpsertEntity`. |
| `external_spec` + deletion-tombstone stamps | Stamp entities created earlier via the CAS path. |

### Breaks / silent-drop — the migration set

| Subsystem | Site | Verdict | Consequence |
|-----------|------|---------|-------------|
| **Agent lessons** | `workflow/lessons/writer.go:134` (create), `:276` / `:311` (stamps) | BREAKS / silent-drop | `RecordLesson`'s first `WriteTriple` is rejected; all 4 producers swallow the error → the **ADR-033 lessons graph subsystem silently empties**; injection reads nothing. |
| **Parse + recovery incidents** | `parseincident/emit.go:124`, `recoveryhint/emit.go:90` (node create); `:114` / `:80` (relation) | BREAKS / at-risk | Incident nodes vanish → **ADR-035 CP-1/CP-2 telemetry goes dark**. The relation stamp is also at-risk: the call/loop entity lives only in `AGENT_LOOPS` KV and may have no graph node to attach to. |
| **Project config** | `project_store.go:229-290` (6× `UpdateTriple` create) | silent-drop | project/checklist/standards graph mirror disappears. Low functional impact — `.semspec/*.json` is durable, HTTP still 200 — but graph reads of config return nothing. |
| **Plan seeding (tests only)** | `graph_marshal.go:28-85` via `workflow.CreatePlan` | BREAKS | 7 `scenario-orchestrator/integration_test.go` seeds fail. Confirmed test-only — zero production callers; production uses `planStore`. |

### Fix — convert each creator to `UpsertEntity`

Uniform, small, and independently correct (also kills the `triple.add`
write-amplification). Must land **before** the semstreams bump that ships
must-exist. Tracked in [#154](https://github.com/C360Studio/semspec/issues/154).

1. `workflow/lessons/writer.go` `RecordLesson` → `UpsertEntity` — highest value.
2. `parseincident/emit.go` + `recoveryhint/emit.go` incident node → `UpsertEntity`;
   attach the relation only when the parent exists.
3. `project_store.go` `writeConfig/Checklist/StandardsTriples` → `UpsertEntity` +
   a registered project-config payload — lower urgency.
4. `workflow/graph_marshal.go` `writePlanTriples` → `UpsertEntity` (or route
   `CreateProjectPlan` through `planStore`) — unblocks the integration tests.

## Clean / excluded (confirmed)

- `semsource/payload.go:22` — compile-time `graph.Graphable` assertion;
  publishes via `graph.ingest.entity`.
- `tools/httptool/ingest.go:123` — builds Graphable web payloads → `graph.ingest.entity`.
- `processor/plan-manager/graph.go:39` — approvals are write-once (fresh UUID per
  call) → `graph.ingest.entity`; append-merge is harmless.
