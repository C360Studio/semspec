# ADR-023: Graph Completeness Roadmap

**Status:** Proposed
**Date:** 2026-03-01
**Authors:** Coby, Claude
**Supersedes:** None
**Context:** Graph entity and predicate audit during transport alignment work (ADR-022)

## Problem Statement

Semspec's graph is the authoritative source of semantic state. During the transport audit
documented in ADR-022, we also assessed how completely entities and predicates are published to
the graph. The findings reveal three categories of issues:

1. **Format bugs** — Entity IDs and predicates that use the wrong convention, producing
   inconsistencies that will complicate graph queries.
2. **Missing entities** — Core domain objects (tasks, questions) that have full predicate
   vocabularies defined but are never written to the graph.
3. **Incomplete entities** — Objects (plans) that are partially published — link predicates
   exist but rich content predicates are unused.

This ADR captures the findings and defines a sequenced roadmap for resolution. It is not a
commitment to implement all phases immediately; it exists to ensure the debt is visible and
prioritized coherently.

## What is Correctly NOT in the Graph

Before listing gaps, it is important to clarify what *should not* be in the graph:

| Data | Storage | Rationale |
|------|---------|-----------|
| Workflow execution state (`AGENT_WORKFLOW_STATE` KV) | KV only | Ephemeral; sub-second frequency; not semantic |
| Context-builder responses (`CONTEXT_RESPONSES` KV) | KV only | Short-lived cache; derived content, not authoritative |
| Coordination sessions | In-memory / KV | Ephemeral; no semantic value after completion |

These are correctly excluded. The issues below are for data that *is* semantic and *should* be
in the graph but is missing or malformed.

## Finding 1: Entity ID Format Bugs

The semstreams entity ID convention is six parts: `{org}.{product}.{domain}.{category}.{type}.{id}`.
All `workflow/entity.go` helpers follow this:

```go
// Correct: 6-part IDs
func PlanEntityID(slug string) string {
    return fmt.Sprintf("c360.semspec.workflow.plan.plan.%s", slug)
}

func TaskEntityID(slug string, seq int) string {
    return fmt.Sprintf("c360.semspec.workflow.task.task.%s-%d", slug, seq)
}

func PhaseEntityID(slug string, seq int) string {
    return fmt.Sprintf("c360.semspec.workflow.phase.phase.%s-%d", slug, seq)
}
```

Two areas deviate from this convention.

### Bug 1a: Approval entity IDs are 3-part

In `processor/workflow-api/graph.go:73`, approval entities are created with a hand-constructed
ID:

```go
// WRONG: 3-part ID — does not follow entity ID convention
entityID := fmt.Sprintf("semspec.approval.%s", uuid.New().String())
```

The correct form, consistent with all other entity helpers, is:

```go
// CORRECT: 6-part ID
entityID := fmt.Sprintf("c360.semspec.workflow.approval.approval.%s", uuid.New().String())
```

An `ApprovalEntityID()` helper function should be added to `workflow/entity.go` alongside the
existing helpers, and the graph.go call site updated to use it.

### Bug 1b: ProjectID field uses a 4-part format inconsistent with entity helpers

The `Plan.ProjectID` field is set to `semspec.local.project.{slug}` (4-part) throughout the
codebase. The `ExtractProjectSlug()` function in `workflow/plan.go` hardcodes this prefix:

```go
func ExtractProjectSlug(projectID string) string {
    const prefix = "semspec.local.project."
    // ...
}
```

However, the `ProjectEntityID()` helper in `workflow/entity.go` produces a 6-part ID:

```go
func ProjectEntityID(slug string) string {
    return fmt.Sprintf("c360.semspec.workflow.project.project.%s", slug)
}
```

The `ProjectID` field is used as a foreign-key reference to the project entity. If plans store
`semspec.local.project.default` but the project entity was published with ID
`c360.semspec.workflow.project.project.default`, graph queries for `semspec.plan.project` links
will return dangling references.

There are approximately 30 usages of the `semspec.local.project.` prefix across the codebase.
This is a migration requiring coordinated changes to `Plan.ProjectID` initialization, the
`ExtractProjectSlug()` helper, and all call sites that compare or construct project IDs.

## Finding 2: Predicate Format Issues

### Issue 2a: 4-part predicates in ProjectConfig vocabulary

The project-config predicate group in `vocabulary/semspec/predicates.go:88-97` uses 4-part
names:

```go
// WRONG: 4-part predicates — convention is 3-part
ProjectConfigStatus    = "semspec.project.config.status"
ProjectConfigApproved  = "semspec.project.config.approved"
ProjectConfigFile      = "semspec.project.config.file"
ProjectConfigApprovedAt = "semspec.project.config.approved_at"
```

The semstreams predicate convention is `{domain}.{category}.{attribute}` (3 parts). The correct
form for these predicates would be:

```go
// CORRECT: 3-part predicates
ProjectConfigStatus    = "semspec.config.status"
ProjectConfigApproved  = "semspec.config.approved"
ProjectConfigFile      = "semspec.config.file"
ProjectConfigApprovedAt = "semspec.config.approvedat"
```

This is a breaking change — any graph entities already stored with 4-part predicate keys would
not be found by queries using the corrected 3-part predicates. A data migration strategy is
required before this can be fixed.

### Issue 2b: Underscore and hyphen separators in predicate names

Approximately 30 predicates use underscores as word separators, with a smaller number using
hyphens. The semstreams convention is dot-separated identifiers only. Examples:

| Current (non-conforming) | Correct form |
|--------------------------|--------------|
| `semspec.plan.created_at` | `semspec.plan.createdat` |
| `semspec.plan.scope_include` | `semspec.plan.scopeinclude` |
| `semspec.plan.github-epic` | `semspec.plan.githubepic` |
| `semspec.plan.github-repo` | `semspec.plan.githubrepo` |
| `semspec.task.actual_effort` | `semspec.task.actualeffort` |
| `semspec.spec.approved_by` | `semspec.spec.approvedby` |

This affects predicates across plan, task, spec, phase, loop, and approval vocabulary groups.
Because these are stored as triple predicate keys in the graph, renaming them requires a graph
data migration. This is the lowest-priority fix due to its scope and breaking nature.

## Finding 3: Graph Entity Completeness

### Plans: link predicates only, no full entity

When phases are added to a plan, `workflow-api` publishes a `PlanEntityPayload` containing only
link predicates:

```go
// Only link predicates are published for plans
triples := []message.Triple{
    {Subject: planEntityID, Predicate: semspec.PlanHasPhases, Object: true},
    {Subject: planEntityID, Predicate: semspec.PlanPhase, Object: phaseEntityID},
}
```

There are 25+ predicates defined in the plan vocabulary — `PlanTitle`, `PlanDescription`,
`PredicatePlanStatus`, `PlanGoal`, `PlanContext`, `PlanPriority`, `PlanRationale`, `PlanAuthor`,
`PlanSlug`, and more — none of which are published to the graph.

This means a graph query for "all plans in `drafted` status" or "plans by author" returns
nothing. The graph cannot serve as the authoritative source for plan discovery.

**Required:** When a plan is created or its status changes, workflow-api should publish a full
plan entity using all applicable predicates from the plan vocabulary. The plan file on disk
contains all the required data; it just is not being reflected into the graph.

### Tasks: vocabulary defined, zero graph publishing

Task entities are entirely absent from the graph. The supporting infrastructure exists:

- `TaskEntityID(slug, seq)` — entity ID helper in `workflow/entity.go`
- 15+ task predicates — `TaskTitle`, `TaskDescription`, `PredicateTaskStatus`,
  `PredicateTaskType`, `TaskGiven`, `TaskWhen`, `TaskThen`, `TaskSpec`, `TaskAssignee`,
  `TaskCreatedAt`, `TaskUpdatedAt`, and more — defined in `vocabulary/semspec/predicates.go`
- `TasksEntityPayload` — payload type registered for graph ingestion (not shown above but
  exists in entity.go pattern)

None of this is wired up to graph publishing. Tasks are stored in `tasks.json` on disk and
dispatched via NATS, but they never become graph entities. This means:

- Context-builder cannot discover tasks by predicate query.
- Trajectory analysis cannot correlate LLM calls with the task they were working on via graph.
- Cross-task dependency analysis is not possible from the graph.

### Questions: KV only, not in graph

The question store (`workflow/question.go`) writes questions exclusively to the `QUESTIONS` KV
bucket. No graph entities are published for questions. The question's slug, content, requester,
answerer, SLA deadline, and resolution state are invisible to graph queries.

Unlike tasks, there is no existing predicate vocabulary for questions. Publishing question
entities to the graph would require designing a `semspec.question.*` predicate group first.

### Phases: correctly published

Phase entities are correctly published with full predicate coverage in
`processor/workflow-api/graph.go`. This is the reference implementation for the entity
publishing pattern other domain objects should follow.

### Approvals: published with wrong entity ID (see Finding 1a)

Approval entities are published, but with the malformed 3-part entity ID described in Finding
1a. The predicate coverage for approvals appears complete once the ID is corrected.

## Recommended Remediation Phases

The following sequence minimizes risk by fixing correctness bugs before adding new capabilities,
and deferring breaking changes until a migration strategy exists.

### Phase 1: Fix entity ID format bugs (correctness)

Fix the two entity ID format bugs identified in Finding 1. These are correctness bugs — the
data in the graph is currently not queryable by the correct ID.

1. Add `ApprovalEntityID()` helper to `workflow/entity.go`
2. Update `processor/workflow-api/graph.go:73` to use the new helper
3. Migrate `Plan.ProjectID` field from `semspec.local.project.{slug}` to
   `c360.semspec.workflow.project.project.{slug}`
4. Update `ExtractProjectSlug()` to match the new prefix
5. Update all call sites (~30 locations) that construct or compare project IDs

### Phase 2: Fix 4-part predicates (correctness, scoped breaking change)

Fix the 4 project-config predicates from 4-part to 3-part format. These predicates are only
used for project initialization config tracking, which is a narrow feature area. A targeted
migration is feasible.

### Phase 3: Publish full plan entities (capability)

When a plan is created or its status changes, publish a full plan entity to the graph using
all applicable plan vocabulary predicates. The workflow-api already has the publishing
infrastructure (`publishPhaseEntity` is the pattern). A new `publishPlanEntity` function
following the same pattern is the primary deliverable.

### Phase 4: Publish task entities (capability)

When tasks are approved and dispatched, publish each task as a graph entity using the task
vocabulary predicates. The task-dispatcher has the task data at dispatch time and is the
correct publication point.

### Phase 5: Consider question entities in graph (design decision)

Evaluate whether questions should be graph entities. Questions are currently operational
(routing, SLA, escalation) and may not need to be graph-queryable. This phase is a design
decision, not a predetermined fix.

### Phase 6: Predicate naming migration (breaking, deferred)

Migrate underscore and hyphen predicates to dot-only convention. This requires:

- A migration script that reads all existing triples with non-conforming predicate keys,
  re-publishes them with corrected keys, and removes the old ones.
- A coordinated update across all vocabulary constants, their usages, and any stored queries
  that reference predicate strings directly.

This should not proceed until a graph migration toolset exists or until the graph is treated as
a rebuildable cache (in which case, simply re-ingesting all sources with corrected predicates
is sufficient).

## Consequences

### Positive

- **Graph as truth** — Completing Phases 1–4 makes the graph the authoritative source for
  plans, tasks, phases, and approvals, enabling context-builder strategies to discover and
  query all domain objects.
- **Correctness** — Fixing entity ID and predicate format bugs eliminates dangling references
  and predicate inconsistencies that silently return wrong results from graph queries.
- **Pattern reference** — Phase entities (`publishPhaseEntity`) serve as the reference
  implementation. Phases 3 and 4 follow the same pattern, reducing implementation risk.

### Negative

- **Breaking changes** — Phases 1 (ProjectID), 2, and 6 are breaking changes that require
  data migration or graph re-ingestion. Existing stored data with old IDs or predicate keys
  will not be found by new queries until migrated.
- **Scope of Phase 1** — The ProjectID migration touches ~30 locations. It requires careful
  coordination to avoid partial migration states where some plans use the old format and others
  use the new format.

### Risks

- **Partial migration** — If Phase 1 is applied incrementally, the system may have plans with
  both old and new ProjectID formats simultaneously. The `ExtractProjectSlug()` function
  could be made to handle both prefixes during a transition window.
- **Graph as cache vs. truth** — If the graph is rebuilt from filesystem sources on startup,
  predicate naming migrations (Phase 6) become safer: re-ingest with corrected predicates
  and the migration is complete. If the graph is the primary store (not rebuildable), Phase 6
  requires explicit migration tooling.

## References

- `workflow/entity.go` — All entity ID helper functions (correct 6-part format reference)
- `processor/workflow-api/graph.go:73` — Approval entity ID bug (Finding 1a)
- `workflow/plan.go:76` — `ExtractProjectSlug()` with 4-part prefix (Finding 1b)
- `vocabulary/semspec/predicates.go:88-97` — 4-part project-config predicates (Finding 2a)
- `vocabulary/semspec/predicates.go` — Underscore/hyphen predicates throughout (Finding 2b)
- `processor/workflow-api/graph.go` — `publishPhaseEntity()` reference implementation
- ADR-022 — Transport Alignment (audit that surfaced these findings)
- [docs/architecture-graph-first.md](../architecture-graph-first.md) — Graph-first principles
  and data source boundary rules
