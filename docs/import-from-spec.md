# Import from OpenSpec

Bring an existing OpenSpec change into semspec as a runnable Plan. The
imported Plan goes through semspec's normal pipeline (plan-reviewer →
execution → QA), and the [PR 3 outbound emitter](../output/workflow-documents/openspec/)
regenerates the OpenSpec markdown after execution — closing the
round-trip.

Background: [ADR-040](adr/ADR-040-openspec-aligned-planning-pipeline.md)
Move 4, folded from [ADR-038](adr/ADR-038-openspec-spec-import.md).

## When to use it

You already have an OpenSpec change in your repo
(`openspec/changes/<name>/`) and want semspec to run the implementation
pipeline against it — instead of authoring a fresh semspec plan that
duplicates the capability identity.

You do NOT need this if you're starting from a user prompt; the
standard `POST /plan-manager/plans` flow handles that case (the analyst
sub-phase produces capabilities from scratch).

## Required directory layout

```
openspec/
├── .openspec.yaml                # schema: spec-driven (recommended)
└── changes/
    └── <change-name>/
        ├── proposal.md           # REQUIRED — at least one capability
        ├── design.md             # optional — architecture decisions
        ├── tasks.md              # optional — task checklist (semspec regenerates)
        └── specs/
            ├── <cap-1>/
            │   └── spec.md       # one per capability declared in proposal
            └── <cap-2>/
                └── spec.md
```

`proposal.md` must declare capabilities in one of two recognised shapes:

```markdown
### `cap-name`
description body...

— OR —

- `cap-name` — description
```

Names must be kebab-case (lowercase, hyphen-separated).

## API: POST /plan-manager/plans/from-spec

Three-layer defense per ADR-038 Architectural commitment:

1. **Layer 1 — structural pre-check.** Filesystem-only, no LLM cost.
   Rejects missing proposal, missing spec.md per declared capability,
   unsupported `.openspec.yaml` schema.
2. **Layer 2 — plan-reviewer (existing).** Semantic review against the
   translated Plan + scenarios. The PR 2 capability rules
   (`capability_orphan`, `capability_orphan.docs_only`,
   `capability_dependency_cycle`, `capability_dependency_orphan`)
   automatically apply.
3. **Layer 3 — recovery-agent (existing, ADR-037).** Wedge-recovery net
   if the translated Plan stalls at execution.

### Example

```bash
curl -X POST http://localhost:8080/plan-manager/plans/from-spec \
  -H 'Content-Type: application/json' \
  -d '{
    "change_name": "add-mavlink-driver",
    "title": "MAVLink driver import"
  }'
```

Successful response (201):

```json
{
  "slug": "add-mavlink-driver",
  "plan_id": "...wf.plan.plan.abc123",
  "capability_count": 3,
  "requirement_count": 7,
  "scenario_count": 12,
  "structural_result": { "ok": true, "..." : "..." },
  "external_refs": {
    "capability:mavsdk-bootstrap": "graph.spec.specification.mavsdk-bootstrap",
    "requirement:requirement.add-mavlink-driver.mavsdk-bootstrap.boot": "graph.req..."
  },
  "message": "Imported OpenSpec change \"add-mavlink-driver\" as plan \"add-mavlink-driver\""
}
```

### Failure modes

| Status | Cause |
|---|---|
| 400 | Missing `change_name` body field |
| 400 | Change directory absent or not a directory |
| 400 | Structural pre-check failed (response includes per-finding detail) |
| 409 | A plan with that slug already exists |
| 503 | Graph querier unavailable (semsource not running) |
| 503 | Graph indexing incomplete (no requirement entities yet) — retry after a moment |

## Round-trip identity

The translator emits `semspec.capability.external_spec` and
`semspec.requirement.external_spec` triples linking imported entities
back to their source-graph entity IDs. When the outbound emitter (PR 3)
writes the change back out after execution, regenerated `specs/<cap>/spec.md`
files carry the original capability identity verbatim — only Plan-mutated
content (scenarios refined by reviewer, scope evolved by execution)
reflects semspec-side changes.

## Discovering importable changes

`POST /project-manager/detect` returns `openspec_changes` in its
response — an array of detected `openspec/changes/<name>/` directories
the operator could import. Each entry includes `name`, `path`,
`has_proposal`, `has_design`, `has_tasks`, and `spec_capabilities`.

## What semspec does NOT do during import

- Run LLM calls — import is deterministic graph + filesystem reads
- Mutate the source `openspec/changes/<name>/` directory
- Reconcile in-flight Plans whose status > `explored`

The imported Plan lands at `status=explored` so the planner sub-phase
runs against the imported `Exploration`. From there the pipeline is
identical to plans authored from a prompt.
