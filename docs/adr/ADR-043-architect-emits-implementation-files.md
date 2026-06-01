# ADR-043: Sarah-prepared stories + Winston tech-spec alignment (BMAD-shaped planning + simplified execution)

**Status:** Proposed (2026-05-31, revision 2)
**Deciders:** Coby, Claude
**Builds on:** ADR-030 (BMAD persona alignment), ADR-040 (OpenSpec vocab + capability vocabulary alignment), ADR-038 (OpenSpec spec import), ADR-041 (Test Evidence Ladder + Scenario Tags)
**Does not change:** Plan / PlanState / PLAN_STATES KV substrate, the harness catalog (ADR-039), the SKG-as-source-of-truth pattern, JSON wire shape of existing fields that this ADR doesn't explicitly add or remove, the BMAD persona names (Mary, John, Winston, Bob, Amelia, Murat — and now Sarah, also BMAD canonical), the prose-from-graph projection direction.
**Decoupled from ADR-042 (forthcoming):** Ops persona / harness-manager / HarnessRun runtime state. Independent migration; ADR-042 remains the next slot.

## Context

### The recurring failure

Two mavlink-hard real-LLM runs on 2026-05-31 (`hybrid-gpt5` + `hybrid` gemini+claude — see [[mavlink-prompt-drives-docs-only-requirements]] and [[adr041-partial-validation-mavlink-hard-2026-05-31]]) both stalled at the scenarios-review phase. The plan-reviewer's `capability.orphan.docs_only` rule (ADR-040's "run-#3 fingerprint") fired on 3 of 4 capabilities. Regen cycles did not converge.

John (our requirement-generator, gemini-pro across both configs) reads the user's mavlink prompt — which literally says *"produce a coverage matrix"* and *"README documents tradeoffs"* — and emits requirements with `files_owned = ["CoverageMatrix.md"]` for capabilities like `cs-api-telemetry`, `cs-api-control`, and `raw-mavlink-fallback`. John has no upstream signal saying *"the implementation lives in these files."* The persona's anti-pattern prose loses to the user's literal acceptance criteria.

This is the third real-LLM run that exhibits this shape, despite ADR-040 explicitly catching it.

### Why prompt-only patches fail

The first instinct is to strengthen John's persona prompt with anti-pattern examples (don't put .md-only, prefer source extensions, etc.). But this is gaming the symptom — each new prompt shape produces a new failure mode. The deeper issue is structural: **John is being asked to do file-mapping work that he doesn't have the inputs to do correctly.** Mary classified surfaces. Winston picked harness profiles + drew architectural boundaries. But nobody told John *which files implement which capability* — he has to guess from the user prompt's literal text.

### What OpenSpec and BMAD do — the structural fix

Both proven methodologies put file-path determination **upstream of the requirement-generator**:

| Phase | OpenSpec | BMAD v6.2.2 | Semspec today |
|---|---|---|---|
| Capability identification | spec.md name + intent | John (PRD/epic) | Mary (analyst sub-phase) |
| Component design | implicit per spec | Winston (tech-spec) | Winston (ArchitectureDocument — fileless) |
| **File-path determination** | **`## Applies To` in spec.md** | **Winston (tech-spec) declares files per component** | **John (guesses from prompt + scope.create)** |
| **Story preparation + tasks** | inside spec.md | **Sarah (PO) shards epics into stories with task checklists** | **— missing layer —** |
| Story-shaped acceptance criteria | inside spec.md | Bob (Scrum Master) crafts stories from Sarah's prep | Bob (scenario-generator — links to requirement directly) |
| Dev unit | spec-as-implementation contract | Story (with tasks) | Requirement (decomposed into nodes at execution time) |

Two structural gaps in semspec today:

1. **Winston doesn't declare implementation files per component.** ComponentDef has Name + Responsibility + Dependencies + UpstreamRefs — no files. So John has to bridge "abstract component" → "concrete files" with insufficient inputs.

2. **The story-preparation layer doesn't exist.** Semspec collapses BMAD's `Epic → Stories → Tasks` into a single Requirement, then defers task-level decomposition to execution time via the decomposer LLM. The result: the dev-execution unit is "discovered" at execution time, not "authored" at plan time. Re-runs produce different decomposition shapes. Sarah's PO judgment (story sharding, prereq ordering, readiness gate) has no home.

### ADR-030 left these gaps open

ADR-030 mapped semspec's existing components to BMAD personas (Mary, John, Winston, Bob, Murat). It chose to skip:
- The story-preparation phase (Sarah)
- The tech-spec scope expansion for Winston (implementation files per component)

This ADR closes both gaps. Two new artifacts (Story, Task), one new persona (Sarah), one extended persona (Winston grows tech-spec scope, John narrows to PRD scope), one retired execution-time agent (the decomposer).

### The graph-first observation (corrected from rev 1)

Rev 1 of this ADR proposed a "graph-first storage" move. That was a misread on my part. Semspec is ALREADY graph-first:

```
PLAN_STATES KV (authoritative)
  ↓ mirrored to
ENTITY_STATES graph triples (cross-component cache)
  ↓ projected to
.semspec/plans/<slug>/plan.json (git-friendly snapshot, derived view)
  ↓ projected to
plan.md / spec.md / design.md / proposal.md / tasks.md (markdown views, derived)
```

The yaml emitter literally says: *"Source of truth for capability identity + scenarios is in `.semspec/plans/<slug>/plan.json` — this directory is a derived projection."* All five OpenSpec markdown renderers take `*workflow.Plan` (the struct loaded from KV → mirrored from graph). The bidirectional OpenSpec compat goal is already wired in `workflow/specimport/translator.go`.

This ADR doesn't change the projection direction. It adds new entities (Story, Task) and new fields (ComponentDef.ImplementationFiles, ComponentDef.Capabilities, Scenario.StoryID) that flow through the existing pipeline. Everything stays graph-first.

## Decision

**Six additive moves, no substrate changes. Greenfield wire churn (we control both sides).**

### Move 1: Winston declares the tech-spec — `ComponentDef` extensions

```go
type ComponentDef struct {
    Name           string
    Responsibility string
    Dependencies   []string
    UpstreamRefs   []string

    // NEW: BMAD tech-spec scope
    ImplementationFiles []string `json:"implementation_files,omitempty"`
    Capabilities        []string `json:"capabilities,omitempty"`
}
```

`ImplementationFiles` are workspace-relative paths (from `scope.create` for new components, or the existing project tree for modified). `Capabilities` are kebab-case capability names this component implements — Winston's bidirectional bridge between Mary's capability list and the file space he just declared.

Architecture-generator's schema gets these two required fields per component. Winston's persona prompt scope grows from "design components" to BMAD's tech-spec scope: *"design components AND declare implementation files AND map components to capabilities."*

### Move 2: Story + Task as first-class artifacts

```go
type Story struct {
    ID            string      // story.<slug>.<reqseq>.<storyseq>
    RequirementID string      // parent requirement (Sarah's sharding decision lives here)
    Title         string      // human-readable, Sarah-authored
    Intent        string      // 1-2 sentences — what implementing this proves
    Components    []string    // ComponentDef.Name entries Sarah selected
    FilesOwned    []string    // union of Components[].ImplementationFiles
    DependsOn     []string    // other Story.ID entries that must complete first
    Tasks         []Task      // Sarah-authored ordered checklist (replaces decomposer)
    Status        StoryStatus
    PreparedBy    string      // "sarah" when Sarah's readiness gate signs off
    PreparedAt    *time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
}

type StoryStatus string
const (
    StoryStatusPending    StoryStatus = "pending"     // Sarah hasn't finished
    StoryStatusReady      StoryStatus = "ready"       // Sarah's readiness gate passed
    StoryStatusExecuting  StoryStatus = "executing"   // dev TDD pipeline in flight
    StoryStatusComplete   StoryStatus = "complete"    // dev + reviewer approved
    StoryStatusFailed     StoryStatus = "failed"      // unrecoverable; PlanDecision needed
)

type Task struct {
    ID          string      // task.<slug>.<reqseq>.<storyseq>.<taskseq>
    StoryID     string      // parent story
    Description string      // 1-line of what this task accomplishes
    DependsOn   []string    // intra-story Task.IDs that must complete first (TDD ordering)
    Status      TaskStatus
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type TaskStatus string
const (
    TaskStatusPending    TaskStatus = "pending"
    TaskStatusDispatched TaskStatus = "dispatched"
    TaskStatusComplete   TaskStatus = "complete"
    TaskStatusFailed     TaskStatus = "failed"
)
```

`Plan` gains `Stories []Story`. `Requirement` loses `FilesOwned` (clean greenfield break — files now live on Story).

### Move 3: Add Sarah — new LLM persona + new processor

New persona in `configs/presets/bmad.json`:

```json
"story-preparer": {
  "display_name": "Sarah",
  "system_prompt": "You are Sarah, a product owner who takes Mary's capability identification, John's requirement intents, and Winston's tech-spec (components with implementation files and capability mappings) and shards each requirement into ready-for-dev Stories with task checklists.\n\nFor each Requirement, decide whether it implements as ONE story or N stories. A single-component requirement is usually one story. A requirement that spans multiple components — or has prereq ordering, or splits cleanly along feature lines — gets multiple stories with DependsOn edges.\n\nFor each Story, populate:\n- Title + Intent (1-2 sentences)\n- Components (kebab-case ComponentDef.Name entries from architecture)\n- FilesOwned (derived: union of selected components' implementation_files; you assemble it explicitly so the dev knows the exact file set)\n- DependsOn (other Story.ID entries — explicit prereqs only, not implicit chronology)\n- Tasks: an ordered TDD checklist. Typical shape is 3-5 tasks per story: write failing tests, implement to pass, integration smoke test, verify scenarios. Each task description is 1 line of intent — the dev decomposes further as needed inside the TDD pipeline.\n\nReadiness gate: every Story you sign off MUST have non-empty FilesOwned with at least one source-code file (.java/.go/.ts/.py/.rs/...), at least one Task, and either no DependsOn or DependsOn entries that resolve to other Story.IDs in this same plan. If you cannot meet the gate for a Story (e.g., architect's components have no ImplementationFiles), flag story.readiness_failure back to the architect rather than emitting an unready story.",
  "traits": ["disciplined", "structural", "judgment-driven"],
  "style": "precise"
}
```

New component `processor/story-preparer/`. Dispatch shape mirrors the architecture-generator: one LLM call per plan (Sarah sees the full architecture context), emits `StoriesGeneratedEvent` payload, plan-manager (single writer) persists.

### Move 4: Phase ordering — narrow John, slot Sarah

Phase order before and after:

```
BEFORE (today):
  Mary analyst → capabilities + surfaces
  Mary planner → goal/context/scope
  John         → requirements WITH files_owned        ← BMAD-incompatible
  Winston      → architecture (fileless)
  Bob          → scenarios per requirement
  execute      → per-requirement dispatch
                 → decomposer LLM → nodes (DAG)
                   → per-node dispatch
                   → per-requirement reviewer

AFTER:
  Mary analyst → capabilities + surfaces                            (unchanged)
  Mary planner → goal/context/scope                                 (unchanged)
  John         → requirements — PRD SCOPE: intent + AC,
                 NO files_owned, capability link only               (narrowed)
  Winston      → architecture INCLUDING components,
                 implementation_files, capability mapping            (extended — tech-spec)
  Sarah        → stories with tasks; readiness gate                  (NEW)
  Bob          → scenarios per story (tier-tagged per ADR-041)      (link target shifts)
  execute      → per-Story dispatch
                 → tasks run in topo order (Sarah-authored, no LLM)
                 → per-task structural validator + code reviewer
                 → per-Story reviewer (tier-aware contract from ADR-041)
```

New workflow status:

```go
const (
    ... (existing) ...
    StatusArchitectureGenerated Status = "architecture_generated"
    StatusPreparingStories      Status = "preparing_stories"      // NEW — Sarah running
    StatusReadyForExecution     Status = "ready_for_execution"
    ...
)
```

State transitions: `architecture_generated → preparing_stories → ready_for_execution`. Plan-reviewer R3 round runs on `preparing_stories → ready_for_execution` to validate Sarah's output.

### Move 5: Execution stage simplifies — decomposer retired

The current execution path looks like:

```
Requirement → decomposer LLM dispatch → node DAG → per-node TDD → per-Requirement reviewer
```

The decomposer was an LLM call at execution time that split a Requirement into N task-nodes with TDD-shaped ordering. Its work moves to Sarah at plan time, authored as `Story.Tasks`.

New execution path:

```
Story → tasks (Sarah-authored, in topo order)
  → for each task:
       dispatch dev → structural validator → code reviewer → approved/retry
  → per-Story reviewer (ADR-041 PR 5 tier-aware contract, scope tightened from req to story)
  → story complete
```

What gets DELETED:
- `tools/decompose/` (the `decompose_task` tool implementation)
- The decomposer persona + prompt fragment (`prompt/context_decomposer.go`)
- Decomposer dispatch path in `processor/requirement-executor/`
- The decompose-task NATS subject + payload registration

What gets PRESERVED:
- `topo_sort.go` (same algorithm, applied to `Story.Tasks` instead of decomposer nodes)
- Per-task dispatch shape (dev → structural-validator → reviewer)
- ADR-037 wedge recovery (re-triggers Sarah for whole-story re-prep on terminal task failure; no `split_task` action needed)
- Worktree-per-task pattern
- Per-Story reviewer (renamed from per-Requirement; tier-aware contract from ADR-041 PR 5 still applies)

Net code: ~−280 LOC from deletions, ~+350 LOC from new per-Story dispatch + Sarah's wiring.

### Move 6: Plan-reviewer rules + artifact projections

**Plan-reviewer new rules** (architecture round — R1, after `architecture_generated`):

| Rule ID | Catches |
|---|---|
| `architecture.component_missing_implementation_files` | A `ComponentDef.ImplementationFiles` is empty. One finding per offending component. |
| `architecture.component_implementation_files_doc_only` | A `ComponentDef.ImplementationFiles` contains only doc-extension files. Architect-side equivalent of `capability.orphan.docs_only` — caught upstream of Sarah. |
| `capability.unresolved_in_architecture` | A `Capability.Name` (from Mary's exploration) has no `ComponentDef` whose `Capabilities` list contains it. Winston didn't map this capability to any component. |

**Plan-reviewer new rules** (story round — R3, after `preparing_stories`):

| Rule ID | Catches |
|---|---|
| `story.missing_files_owned` | A `Story.FilesOwned` is empty (Sarah didn't complete the join). |
| `story.docs_only_files_owned` | A `Story.FilesOwned` contains only doc-extension files. Defensive backstop — Sarah's gate should fail-fast first. |
| `story.unresolved_components` | A `Story.Components` entry doesn't match any `ComponentDef.Name` in the architecture. |
| `story.depends_on_orphan` | A `Story.DependsOn` entry doesn't match any `Story.ID` in the plan. |
| `story.depends_on_cycle` | DAG cycle in story-level depends_on. |
| `task.missing_within_story` | A `Story.Tasks` is empty (Sarah signed off without authoring tasks). |
| `task.depends_on_cycle` | DAG cycle in task-level depends_on (within a single story). |

**ADR-040's `capability.orphan.docs_only` rule** becomes a defensive backstop — the new architect-side and Sarah-side rules normally fire first, but the existing rule stays as a safety net.

**Artifact projections — all five renderers updated**:

`spec.md` (per capability, `output/workflow-documents/openspec/spec.go::RenderSpec`):

```markdown
# Spec: cs-api-telemetry

## Overview
Surface MAVSDK telemetry plugins as Connected Systems API DataStreams.

## Applies To
- `src/main/java/io/sensorhub/mavsdk/cs/CSTelemetry.java`
- `src/main/java/io/sensorhub/mavsdk/cs/CSTelemetryConfig.java`
- `CoverageMatrix.md`

## Requirements
### CS API DataStream for telemetry
SHALL surface MAVSDK telemetry plugins as Connected Systems API DataStreams.

#### Stories
- **`story.840b.1.1`** — Telemetry Bootstrap *(ready, depends on: —)*
  - Implements components: `cs-telemetry-stream`
  - Files: `CSTelemetry.java`, `CSTelemetryConfig.java`
  - Tasks:
    1. Write failing test for boot lifecycle
    2. Implement CSTelemetryConfig loader
    3. Implement CSTelemetry.start() lifecycle wiring
    4. Wire integration smoke test against PX4 SITL

  ##### Scenario: HEARTBEAT observed after driver start
  `@integration` · harness: `mavlink.px4-sitl.mavsdk-smoke`
  **GIVEN** the SITL endpoint at env $PX4_SIM_MODEL
  **WHEN** the driver starts
  **THEN** a HEARTBEAT is received within 10 seconds
```

`tasks.md` becomes story-driven (each story is a top-level task entry, with sub-tasks nested):

```markdown
# Tasks

## story.840b.1.1 — Telemetry Bootstrap
- [ ] Write failing test for boot lifecycle
- [ ] Implement CSTelemetryConfig loader
- [ ] Implement CSTelemetry.start() lifecycle wiring
- [ ] Wire integration smoke test against PX4 SITL

## story.840b.1.2 — Telemetry Plugin Mapping  (depends on: story.840b.1.1)
- [ ] ...
```

`design.md` gets a `## Components → Files` section listing each component's ImplementationFiles + which capabilities it implements.

`proposal.md` gets story counts per capability.

`plan.md` gets a story breakdown per requirement.

`plan.json` gets `stories: [...]` as a first-class field, requirements lose their `files_owned`, scenarios gain `story_id`.

**OpenSpec round-trip**: `specimport/translator.go` reads `spec.story.*` triples (when present in the source graph) and reconstructs `Plan.Stories`. When importing a legacy OpenSpec change without stories, the translator falls back to deriving a single Story per Requirement (1:1) so legacy plans don't break.

## Architecture

### Predicate additions

All follow the rev-5 ADR-040 three-part `domain.category.property` convention.

| Predicate | Subject | Object | Cardinality |
|---|---|---|---|
| `semspec.component.implementation_file` | component entity | string (workspace-relative path) | N |
| `semspec.component.capability` | component entity | string (kebab-case cap name) | N |
| `semspec.story.title` | story entity | string | 1 |
| `semspec.story.intent` | story entity | string | 1 |
| `semspec.story.requirement` | story entity | entity_id (requirement) | 1 |
| `semspec.story.component` | story entity | string (component name) | N |
| `semspec.story.files_owned` | story entity | string (path) | N |
| `semspec.story.depends_on` | story entity | entity_id (story) | N |
| `semspec.story.status` | story entity | string | 1 |
| `semspec.story.prepared_by` | story entity | string | 1 |
| `semspec.story.prepared_at` | story entity | datetime | 1 |
| `semspec.story.created_at` | story entity | datetime | 1 |
| `semspec.story.updated_at` | story entity | datetime | 1 |
| `semspec.task.story` | task entity | entity_id (story) | 1 |
| `semspec.task.description` | task entity | string | 1 |
| `semspec.task.depends_on` | task entity | entity_id (task) | N |
| `semspec.task.status` | task entity | string | 1 |
| `semspec.task.created_at` | task entity | datetime | 1 |
| `semspec.task.updated_at` | task entity | datetime | 1 |
| `semspec.scenario.story` | scenario entity | entity_id (story) | 1 |

The existing `semspec.scenario.requirement` stays — Scenario keeps both links for query convenience. The new `semspec.scenario.story` is the semantic primary.

The existing `semspec.requirement.files_owned` predicate gets **deprecated** (legacy plans still have it; new plans don't emit it). Greenfield clean break.

### Graph population — who writes what

Single-writer pattern preserved (per CLAUDE.md). Each component owns its predicate space; plan-manager remains the sole persister.

| Phase | Component | Writes |
|---|---|---|
| Mary analyst | `processor/planner` (analyst sub-phase) | `semspec.capability.*` (existing) |
| Mary planner | `processor/planner` (planner sub-phase) | `semspec.plan.goal/context/scope` (existing) |
| John | `processor/requirement-generator` | `semspec.requirement.*` (NARROWED — no files_owned predicate anymore) |
| Winston | `processor/architecture-generator` | `semspec.component.*` (EXTENDED — adds implementation_file + capability) |
| **Sarah** | **`processor/story-preparer` (NEW)** | **`semspec.story.*` + `semspec.task.*` (NEW)** |
| Bob | `processor/scenario-generator` | `semspec.scenario.*` (EXTENDED — adds story link) |

### Component changes

| Component | Change | LOC est. |
|---|---|---|
| `workflow` | New `Story`, `Task` types + `StoryStatus`, `TaskStatus` enums + `Plan.Stories` field. `ComponentDef` gains `ImplementationFiles`, `Capabilities`. `Requirement.FilesOwned` REMOVED (greenfield break). `Scenario.StoryID` added. Validators for all. | ~250 + tests |
| `vocabulary/semspec/` | 20 new predicates. | ~70 |
| `tools/terminal/schemas.go` | `architectureSchema`: add `implementation_files` + `capabilities` (required when component_boundaries non-empty). `requirementsSchema`: REMOVE `files_owned` field (greenfield). `scenariosSchema`: add `story_id`. New `storiesSchema()` for Sarah. | ~80 |
| `processor/architecture-generator` | Winston persona prompt update. Schema validation. | ~40 + persona |
| **`processor/story-preparer` (NEW)** | New component. Watches PLAN_STATES for `architecture_generated` → dispatches Sarah → consumes `StoriesGeneratedEvent` via plan-manager. Mirrors architecture-generator shape. | ~300 + persona + tests |
| `processor/requirement-generator` | John persona narrowed to PRD scope. Schema update (no files_owned). | ~40 + persona |
| `processor/plan-manager` | Add `StoriesGeneratedEvent` handler. Single-writer persists Story + Task entities. New state transition `architecture_generated → preparing_stories → ready_for_execution`. | ~100 + tests |
| `processor/plan-reviewer` | 7 new rules (3 architecture-side, 4+ story-side). New R3 round on `preparing_stories`. | ~250 + tests |
| `processor/scenario-generator` | Bob's classifier accepts a Story instead of a Requirement. Tier-emission logic unchanged. `Scenario.StoryID` populated. | ~50 + persona |
| `processor/requirement-executor` | Major simplification: drops decomposer dispatch, dispatches per-Story, tasks come from `Story.Tasks` (Sarah-authored). topo_sort still runs the DAG. Per-Story reviewer (was per-Requirement). | ~−200 + tests |
| `tools/decompose/` | **DELETED**. | ~−400 |
| `prompt/context_decomposer.go` | **DELETED**. | ~−100 |
| `output/workflow-documents/openspec/*.go` | All five renderers (spec.go, design.go, proposal.go, tasks.go, plan.md) updated to project Stories + Tasks. | ~120 |
| `workflow/specimport/translator.go` | Read `spec.story.*` triples on import; fall back to 1:1 derivation for legacy specs. | ~50 |

**Net LOC**: roughly +1,200 added, −700 deleted = **net ~+500 LOC**.

### Phase flow + state machine

```
StatusCreated
  ↓ (analyst sub-phase)
StatusExplored — Mary's capabilities + surfaces persisted
  ↓ (planner sub-phase)
StatusDrafting — Mary's goal/context/scope being authored
  ↓
StatusDrafted — drafted plan; plan-reviewer R1 fires
  ↓
StatusReviewed — R1 approved
  ↓
StatusGeneratingRequirements — John running
  ↓
StatusRequirementsGenerated — requirements persisted (no files_owned)
  ↓
StatusGeneratingArchitecture — Winston running
  ↓
StatusArchitectureGenerated — components + implementation_files + capability mapping;
                              plan-reviewer R2 fires (existing rules + 3 new architecture rules)
  ↓
StatusPreparingStories — Sarah running (NEW STATE)
  ↓
StatusReadyForExecution — Sarah complete; plan-reviewer R3 fires (4+ new story rules)
  ↓
StatusImplementing — execution-manager dispatches per-Story
  ↓
StatusReviewingQA — Murat at plan completion (ADR-031 unchanged)
  ↓
StatusComplete | StatusRejected | StatusArchived
```

### Recovery cascade interaction (ADR-037)

When a Story's terminal task fails:
1. `requirement-executor` deferred-terminal-fail mechanism fires (existing pattern)
2. Recovery-agent (ADR-037 stage 1) emits a PlanDecision with `action=story_reprepare` (new action class) and `affected_stories=[story.X.Y]`
3. plan-decision-handler accepts the PlanDecision
4. plan-manager transitions the affected Story back to `pending`, marks the requirement's status as `preparing_stories` (cascade target)
5. story-preparer (Sarah) re-runs for that specific Story with the failure context as Sarah's RecoveryHint (mirrors ADR-037's existing RecoveryHint pattern on Requirement)
6. New Story + Tasks emitted; execution resumes

No `split_task` runtime action needed. The cascade re-triggers the plan-time author (Sarah), who has full context to produce a different shape.

### Persona prompt updates

Operator-tunable via `configs/presets/bmad.json`. Five personas affected:

**Mary (analyst)** — UNCHANGED from ADR-041 PR 2. Capabilities + surfaces are her job; she doesn't touch components or files.

**John (requirement-generator)** — NARROWED to BMAD PRD scope:

> You are John, a product manager. Given the plan's exploration document (capabilities + surfaces), produce ONE Requirement per capability. Each requirement has a title, a 1-3 sentence description, a CapabilityName link, and 1-3 GIVEN/WHEN/THEN scenario outlines.
>
> Your job is INTENT + acceptance criteria — what the system MUST do. Implementation file paths are NOT your concern; the architect declares files per component, and Sarah the product owner assigns files to stories downstream. Do NOT emit files_owned; the field has been removed from your schema.

**Winston (architect)** — EXTENDED to BMAD tech-spec scope:

> ...existing role description...
>
> For every component_boundary you declare, populate `implementation_files` with the workspace-relative paths that house this component's code. Source these from `plan.scope.create` (for new components) or the existing project file tree (for modified components). Every component MUST own at least one source-code file (.java/.go/.ts/.py/.rs/...); documentation companion files (.md/.txt) MAY appear alongside source but never alone.
>
> Also populate `capabilities` per component — list the kebab-case capability names from `plan.exploration.capabilities[]` that this component implements. Every capability should appear in at least one component's Capabilities list; if a capability has no component, flag it rather than declaring an implementation-less abstract.
>
> You are producing the BMAD tech-spec — components, their files, and their capability bindings. Sarah will use this to shard requirements into ready-for-dev stories.

**Sarah (story-preparer)** — NEW persona, see Move 3 above for full prompt.

**Bob (scenario-generator)** — UNCHANGED contract, link target shifts from Requirement to Story. The classifier algorithm and tier-emission rules from ADR-041 PR 2 still apply; the input shape is `Story` instead of `Requirement` but contains the same fields the classifier needs (capability link, harness profiles via architecture).

**Reviewer (req-level reviewer per ADR-041 PR 5)** — Tier-aware contract UNCHANGED. The reviewed entity narrows from Requirement to Story. The per-Story reviewer scope is tighter (fewer scenarios, fewer files) which makes the review more focused.

### What stays the same

- Plan / PlanState / PLAN_STATES KV — substrate unchanged
- ENTITY_STATES graph triples — additive only
- The CQRS twofer pattern (cache + KV + graph)
- The single-writer pattern (plan-manager persists; generators emit events)
- Harness catalog (ADR-039)
- Tier-tagged scenarios (ADR-041)
- BMAD persona name canonicality (Mary, John, Winston, Bob, Amelia, Murat, and now Sarah)
- Bidirectional OpenSpec compat
- Autonomous QA-recovery cascade (ADR-037) — extended with `story_reprepare` action; existing `requirement_change` action continues to work
- Per-Story reviewer's tier-aware contract from ADR-041 PR 5

## Validation against the recurring failure shape

Replay the mavlink-hard prompt through the revised pipeline:

1. **Mary (analyst)** — capabilities + surfaces (unchanged):
   ```
   capabilities:
     - mavsdk-bootstrap (new, surfaces: [background])
     - cs-api-telemetry (new, surfaces: [api])
     - cs-api-control (new, surfaces: [api])
     - raw-mavlink-fallback (new, surfaces: [api])
   ```

2. **John (req-gen)** — requirement intents, NO files:
   ```
   requirements:
     - req.840b.1: "MAVSDK lifecycle"
       capability: mavsdk-bootstrap
       description: Boot mavsdk_server and observe MAVLink HEARTBEAT lifecycle
       scenarios: [GIVEN/WHEN/THEN outlines × 2]
     - req.840b.2: "CS API DataStreams for telemetry"
       capability: cs-api-telemetry
       description: Surface MAVSDK telemetry plugins as CS API DataStreams + Observations
       scenarios: [...]
     - req.840b.3: ...
     - req.840b.4: ...
   ```

3. **Winston (architect)** — components + files + capability mapping:
   ```
   component_boundaries:
     - name: mavsdk-server-lifecycle
       responsibility: Boot mavsdk_server, manage MAVLink peer connection
       implementation_files:
         - src/main/java/io/sensorhub/mavsdk/MavsdkServerLifecycle.java
         - src/main/java/io/sensorhub/mavsdk/MavsdkConfig.java
       capabilities: [mavsdk-bootstrap]
     - name: cs-telemetry-stream
       responsibility: Surface telemetry as CS API DataStreams
       implementation_files:
         - src/main/java/io/sensorhub/mavsdk/cs/CSTelemetry.java
         - src/main/java/io/sensorhub/mavsdk/cs/CSTelemetryConfig.java
         - CoverageMatrix.md (companion doc)
       capabilities: [cs-api-telemetry]
     - name: cs-control-stream
       responsibility: Map MAVSDK actions/missions/camera to CS API ControlStreams
       implementation_files: [...]
       capabilities: [cs-api-control]
     - name: raw-mavlink-bridge
       responsibility: Generic raw-MAVLink fallback for messages outside MAVSDK
       implementation_files: [...]
       capabilities: [raw-mavlink-fallback]
   ```

4. **Plan-reviewer R2** — architecture round:
   - `architecture.component_missing_implementation_files`: zero ✓
   - `architecture.component_implementation_files_doc_only`: zero ✓
   - `capability.unresolved_in_architecture`: zero ✓

5. **Sarah (story-preparer)** — shards requirements into stories with task lists:
   ```
   stories:
     - story.840b.1.1: "MAVSDK Lifecycle Bootstrap" → requirement req.840b.1
       components: [mavsdk-server-lifecycle]
       files_owned: [MavsdkServerLifecycle.java, MavsdkConfig.java]
       depends_on: []
       tasks:
         - Write failing test for lifecycle boot
         - Implement MavsdkConfig defaults
         - Implement MavsdkServerLifecycle.start()
         - Integration smoke test against PX4 SITL
     - story.840b.2.1: "CS Telemetry DataStreams" → requirement req.840b.2
       components: [cs-telemetry-stream]
       files_owned: [CSTelemetry.java, CSTelemetryConfig.java, CoverageMatrix.md]
       depends_on: [story.840b.1.1]  # need lifecycle first
       tasks: [...]
     - story.840b.3.1: ...
     - story.840b.4.1: ...
   ```

6. **Plan-reviewer R3** — story round:
   - `story.missing_files_owned`: zero ✓
   - `story.docs_only_files_owned`: zero (every story has source code) ✓
   - `story.unresolved_components`: zero ✓
   - `story.depends_on_orphan/cycle`: zero ✓
   - `task.missing_within_story`: zero ✓

7. **Bob (scenario-generator)** — scenarios per Story with tier tags + harness binding (ADR-041 surfaces preserved):
   ```
   scenario.840b.1.1.1: @unit + @integration scenarios for the lifecycle story
   scenario.840b.2.1.1: @unit + @integration scenarios for telemetry story
   ...
   ```

8. **Execution-manager** dispatches per Story:
   - story.840b.1.1 runs first (no depends_on)
   - Sarah's tasks run in topo order (write tests → implement → integration test)
   - Per-task structural validator + code reviewer
   - Per-Story reviewer with ADR-041 PR 5 tier-aware contract: "@unit tests must pass, @integration tests must be authored correctly (not pass)"
   - story.840b.1.1 → complete
   - story.840b.2.1 unblocks; dispatches next

9. **Murat (qa-reviewer)** at plan completion (ADR-031 unchanged) — triggers qa-runner, which runs @integration tests in the catalog harness.

The run-#3 docs-only fingerprint is **structurally impossible**: Winston's tech-spec round catches it first (every component must own source code), Sarah's readiness gate catches it second, plan-reviewer R3 catches it third. Three layers of defense; the prompt-anchoring failure mode has no path through.

ADR-041 PR 5 (issue #37 fix) continues to apply — the per-Story reviewer is just the per-Requirement reviewer with tighter scope. Tier-aware contract unchanged.

## Migration

Five PRs, sequenced for review. Each independently shippable.

### PR 1: Data model + predicates (~2 days)

- `workflow.Story`, `workflow.Task`, `StoryStatus`, `TaskStatus`, `Plan.Stories []Story` field
- `ComponentDef.ImplementationFiles []string` + `ComponentDef.Capabilities []string`
- `Requirement.FilesOwned` REMOVED (greenfield break)
- `Scenario.StoryID` added
- 20 new predicates wired into `vocabulary/semspec/`
- Validators (`ValidateStory`, `ValidateTask`, `ValidateComponentImplementationFiles`, etc.)
- Workflow Status enum gains `StatusPreparingStories`
- Unit tests covering JSON round-trip + validators

### PR 2: Winston extension (~2 days)

- Winston persona prompt update
- `architectureSchema` adds `implementation_files` (required, ≥1 entry per component) + `capabilities` (required, ≥1 entry per component)
- Architecture-generator validates output before persistence
- 3 new plan-reviewer rules (architecture round)
- Mock e2e fixtures: assert architecture output has implementation_files + capabilities populated; assert validator rejects docs-only and unresolved-capability shapes

### PR 3: Sarah — new component (~4 days)

- New `processor/story-preparer/` component (mirrors architecture-generator shape):
  - `component.go`, `config.go`, `factory.go`, `payload_registry.go`, `plan_watcher.go`
  - LLM dispatch via agentic-dispatch
  - Sarah persona in `configs/presets/bmad.json`
  - `storiesSchema()` in `tools/terminal/schemas.go`
  - `StoriesGeneratedEvent` payload (single-writer pattern: Sarah emits, plan-manager persists)
- plan-manager handles `StoriesGeneratedEvent`, persists Story + Task entities, triple-writer mirrors to graph
- New workflow status `StatusPreparingStories` + transition
- 4+ new plan-reviewer rules (story round, R3)
- Component registered in `cmd/semspec/main.go`

### PR 4: John narrowed + Bob updated + execution simplified (~4 days)

- John persona narrowed to PRD scope (no files_owned in output)
- `requirementsSchema` removes `files_owned` field
- Bob persona: `Scenario.StoryID` populated (existing tier-aware emission logic unchanged)
- `scenariosSchema`: add `story_id` field, primary key over `requirement_id`
- **Execution-manager rewrites**:
  - Dispatch per Story (was per Requirement)
  - `Story.Tasks` provide the DAG (no decomposer LLM call)
  - Per-task dispatch unchanged shape
  - Per-Story reviewer (renames per-Requirement reviewer; ADR-041 PR 5 tier-aware contract scope shrinks from req to story)
- **Decomposer retired**:
  - DELETE `tools/decompose/`
  - DELETE `prompt/context_decomposer.go`
  - DELETE decomposer dispatch path in `processor/requirement-executor/`
- ADR-037 recovery cascade: new `story_reprepare` PlanDecision action class; existing `requirement_change` continues
- Plan-reviewer R3 rules wired in `processor/plan-reviewer/`

### PR 5: Artifact projections + OpenSpec round-trip (~2 days)

- All five renderers updated (`spec.go`, `design.go`, `proposal.go`, `tasks.go`, plan.md)
- `spec.md` per-capability gets Story + Task sub-sections
- `tasks.md` becomes story-driven (each story is a top-level task entry)
- `design.md` gets Components → Files section
- `proposal.md` gets story counts per capability
- `plan.md` adds story breakdown
- `plan.json` shape changes (greenfield, no migration concerns; documented in PR description)
- `specimport/translator.go` reads new spec.story.* and spec.task.* triples; falls back to 1:1 derivation for legacy OpenSpec imports
- Round-trip integration tests: emit a tagged plan → spec.md → re-import → plan.Stories preserved
- Real-LLM smoke (gemini @ easy + mavlink-hard rerun) after this lands. **Mavlink-hard is the proof point: this ADR's structural fix lets the run complete past the scenarios stage.**

**Total: ~14 working days. ~+1,200 LOC additive + ~−700 LOC deleted = net ~+500 LOC.** PR 4 is the load-bearing one — execution simplification + greenfield wire changes.

## Open questions

- **(Q1) Should Sarah dispatch per-Plan or per-Requirement?** Per-Plan keeps Sarah's context whole and lets her make global story-ordering decisions (one LLM call per plan). Per-Requirement would parallelize but risks Sarah missing cross-requirement story dependencies. **Lean: per-Plan** for the load-bearing decision (story DAG across all requirements). Single LLM call ~30-60s adds little to wallclock.

- **(Q2) Should Sarah produce scenarios too, or stay with Bob doing them?** BMAD has Sarah authoring acceptance criteria as part of story prep; Bob equivalent doesn't really exist in BMAD (the SM-as-coach role doesn't generate ACs). Our Bob has the tier-emission expertise from ADR-041 PR 2 (classifier walks architecture for services-class profiles, etc.). **Lean: Bob stays.** Sarah produces stories + tasks; Bob produces tier-tagged scenarios per story. Keeps tier expertise concentrated.

- **(Q3) Task IDs vs Node IDs.** Today's decomposer produces "nodes" with their own ID space. Tasks have a new ID format (`task.<slug>.<reqseq>.<storyseq>.<taskseq>`). Cleaner; no rename needed since decomposer goes away.

- **(Q4) Recovery action vocabulary.** ADR-037 stage 2 has `split_req` as a recovery action. With this ADR, we add `story_reprepare`. Should `split_req` be renamed `story_reprepare` since requirement-level splits no longer make sense (requirements are PRD-shaped now)? **Lean: keep both for the migration window, deprecate `split_req` after the smoke proves the new path.**

- **(Q5) Greenfield break: should `Requirement.FilesOwned` be hard-deleted or marked deprecated?** Hard delete is the user's stated preference ("wire churn is fine"). Hard delete keeps the wire shape clean; legacy plans that re-serialize from KV without the field just lose it (which is correct, since files now live on Story). Documented as a non-back-compat shape change in PR 1's commit message.

- **(Q6) Should the decomposer hard-delete or transition to dormant?** Greenfield break — hard delete. The `decompose_task` tool is invoked only by the decomposer agent; with Sarah authoring tasks at plan time, no caller remains. Delete in PR 4. Documented as a non-back-compat shape change.

- **(Q7) Should Story emit OpenSpec round-trippable per-story spec files (`specs/<cap>/<story>.md`)?** Tempting for fine-grained provenance but explodes the file count. **Lean: stay with one `specs/<cap>/spec.md` per capability**, with stories nested inside (Move 6 design). Less file churn; matches BMAD's "story exists in the PRD" pattern.

- **(Q8) Persona naming for the new `processor/story-preparer/` directory.** BMAD canonical is "Sarah" (PO). Industry-vocab role key options: `story-preparer`, `requirement-resolver`, `story-shaper`. **Lean: `story-preparer`** — clearest about what the component DOES. Display name "Sarah" per [[bmad-persona-canonical-names]].

## Consequences

### Positive

- **The run-#3 docs-only fingerprint becomes structurally impossible.** Three layers of defense (Winston's tech-spec round, Sarah's readiness gate, plan-reviewer R3) close the prompt-anchoring failure mode that has reproduced on three mavlink-hard runs.
- **ADR-030's BMAD alignment gap closes.** The missing tech-spec scope + missing PO persona were the structural holes. Both filled.
- **Execution stage simplifies meaningfully.** Decomposer LLM call retired (~280 LOC + 1-2 LLM calls saved per Story dispatch). Per-Story dispatch is tighter than per-Requirement.
- **Story DAG is authored at plan time, not discovered at execution time.** Reproducibility improves — re-runs produce the same task graph. Plan-reviewer can validate the DAG before dev burns tokens on a bad shape.
- **BMAD-faithful execution pipeline.** Sarah's PO role, Story-shaped dispatch units, Task-checklists per story — all match v6.2.2 vocabulary and phase ordering.
- **OpenSpec round-trip strengthens.** `specimport/translator.go` reads story triples on import; emitters write the symmetric markdown. Bidirectional compat improves; no provenance lost.
- **Plan-reviewer gets richer.** Three new structural-rule classes (component round, story round, task round) layered on the existing capability rules. More structural violations caught at plan time vs runtime.
- **Mavlink-hard validation finally unblocks.** Two consecutive 2026-05-31 runs stalled on docs-only fingerprint. With this ADR, scenarios + execution stages can run end-to-end and finally exercise ADR-041 PR 5 (#37 fix) in production.
- **Recovery cascade gets a cleaner restart vector.** Re-triggering Sarah for story re-prep is simpler than the current ADR-037 `split_req` action surface.

### Negative

- **Bigger refactor than the original ADR-043 v1 envisioned.** ~14 working days vs ~10. Acceptable given the structural payoff, but worth naming.
- **One new processor + one new persona + one new workflow status.** Each adds operational surface. Mitigated by mirroring the architecture-generator's existing patterns (single-writer + KV watch + dispatch).
- **Sarah's LLM call cost.** One additional dispatch per plan (~30-60s on gemini-pro). Modest given the existing planning pipeline already does 4-6 LLM calls (Mary analyst, Mary planner, John, Winston, Bob, plan-reviewer R1/R2/R3).
- **Greenfield wire shape changes break legacy plans.** `Requirement.FilesOwned` removed; `Scenario.StoryID` added (was optional, now primary); `Plan.Stories` first-class. Operator-confirmed acceptable; documented in PR commit messages.
- **`Plan.Requirements[].DependsOn` becomes ambiguous in role.** Today it's both "scheduling order at execution" and "logical prereq". With Story.DependsOn, the scheduling role moves to stories. Requirement.DependsOn becomes capability-level intent ordering (still useful for plan-reviewer rules + reasoning), but executors look at Story.DependsOn. Worth a short doc-comment update in `workflow/types.go`.

### Neutral

- **No impact on the harness catalog, the tier evidence ladder, or Murat (qa-reviewer).** ADR-039/041/031 all compose orthogonally.
- **BMAD persona canonical names preserved.** Mary, John, Winston, Bob, Amelia, Murat keep their roles; Sarah joins as the BMAD-canonical PO persona.
- **Industry-vocabulary on user-facing surfaces preserved.** Stories use industry-standard naming. Field name `FilesOwned` stays.
- **Worktree-per-task pattern unchanged.** Execution-manager still creates per-task worktrees; per-story branch grouping continues.

## Decision is

**Accept this ADR and proceed with the five-PR migration. PR 1+2 build the data model + Winston extension. PR 3 lands Sarah. PR 4 is the load-bearing simplification (decomposer retired, per-Story dispatch). PR 5 closes the round-trip surface.**

Required confirmation before code lands:

1. Operator (Coby) signs off on the five persona prompt updates (Mary unchanged, John narrowed, Winston extended, Sarah new, Bob link-target shifted, reviewer scope tightened).
2. Operator confirms the ~14-day migration is acceptable.
3. Operator confirms greenfield wire-shape breaks (`Requirement.FilesOwned` removed, `decompose_task` tool deleted) are acceptable.
4. Operator confirms ADR-042 (Ops persona / harness-manager) remains the next slot after this one ships.

This ADR explicitly preserves:
- The SKG as the authoritative state substrate (graph-first projection direction)
- The Plan / PLAN_STATES model + KV twofer + single-writer pattern
- The tier evidence ladder (ADR-041) — composes orthogonally with story-shaped dispatch
- The harness catalog (ADR-039)
- BMAD persona canonical names (ADR-030, [[bmad-persona-canonical-names]])
- Bidirectional OpenSpec compatibility
- The autonomous QA-recovery cascade (ADR-037) — extended with `story_reprepare` action

The operator framing — *"how does OpenSpec and BMAD handle this?"* and *"we're greenfield so wire churn is under our control"* — is the load-bearing intuition behind this ADR. Both reference methodologies put file-path determination upstream of John and have a story-preparation layer between PRD and dev. Semspec adopts both moves, retires the execution-time decomposer in the process, and lands with a tighter pipeline that's BMAD-faithful + graph-first + bidirectional OpenSpec compatible.

See [[mavlink-prompt-drives-docs-only-requirements]] for the empirical motivation, [[adr041-partial-validation-mavlink-hard-2026-05-31]] for the partial-#37-proof forensics that motivated this restructure, [[reference-bmad-method]] / ADR-038 for the methodology references, and [[cqrs-twofer-pattern]] for the graph-first storage architecture this ADR builds on (rather than against).
