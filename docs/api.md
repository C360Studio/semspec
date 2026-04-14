# API Reference

Semspec exposes a REST API at `http://localhost:8080`. All endpoints are documented in the
generated OpenAPI 3.0 spec.

**Interactive docs**: `http://localhost:8080/docs` (Swagger UI playground)
**OpenAPI spec**: `http://localhost:8080/openapi.json` (machine-readable)

## API Groups

### Plans — `/plan-api/plans`

Full plan lifecycle: create, list, get, update, delete, promote, execute, unarchive.
SSE stream at `/plan-manager/plans/{slug}/stream` for real-time plan stage updates.

| Endpoint | What it does |
|----------|-------------|
| `POST /plan-api/plans` | Create a plan (triggers the planner agent) |
| `GET /plan-api/plans` | List all plans with current status |
| `GET /plan-api/plans/{slug}` | Get plan with workflow stage and active loops |
| `POST /plan-api/plans/{slug}/promote` | Approve a plan for execution |
| `POST /plan-api/plans/{slug}/execute` | Trigger scenario-based execution |
| `GET /plan-api/plans/{slug}/reviews` | Aggregated review findings |

### Requirements — `/plan-api/plans/{slug}/requirements`

CRUD for plan requirements. Requirements are the unit of execution — each gets decomposed
into a TaskDAG at runtime.

### Scenarios — `/plan-api/plans/{slug}/scenarios`

CRUD for Given/When/Then scenarios. Scenarios are acceptance criteria attached to requirements,
validated at review time. Filter by `?requirement_id=`.

### Change Proposals — `/plan-api/plans/{slug}/change-proposals`

Submit, review, accept, or reject requirement changes after a plan is approved.
Accepting triggers dirty cascade to affected scenarios and tasks.

### Tasks — `/plan-api/plans/{slug}/tasks`

Read-only. Tasks are created at execution time by the decomposer agent. Lists all tasks
for observability.

### Execution — `/execution-manager`

SSE stream at `/execution-manager/plans/{slug}/stream` for task and requirement updates.

| Endpoint | What it does |
|----------|-------------|
| `GET /execution-manager/lessons` | Lessons learned (filter by `?role=`) |
| `GET /execution-manager/lessons/counts` | Per-category error counts by role |

### Project Setup — `/project-manager`

Project initialization wizard and configuration management.

| Endpoint | What it does |
|----------|-------------|
| `GET /project-manager/status` | Initialization state (which config files exist) |
| `GET /project-manager/wizard` | Supported languages and frameworks |
| `POST /project-manager/detect` | Auto-detect stack from filesystem |
| `POST /project-manager/scaffold` | Create marker files for selected stack |
| `POST /project-manager/generate-standards` | Generate standards from detected stack |
| `POST /project-manager/init` | Write all config files to `.semspec/` |
| `POST /project-manager/approve` | Approve a config file |
| `PATCH /project-manager/config` | Update project.json fields |
| `GET/PATCH /project-manager/checklist` | Read or update quality gate checks |
| `GET/PATCH /project-manager/standards` | Read or update project standards |

### Agent Activity — `/agentic-dispatch`

Real-time agent monitoring and command dispatch.

| Endpoint | What it does |
|----------|-------------|
| `POST /agentic-dispatch/message` | Send a command (e.g., `/plan`, `/execute`) |
| `GET /agentic-dispatch/loops` | List active agent loops |
| `GET /agentic-dispatch/activity` | SSE stream of loop lifecycle events |

### Graph Gateway — `/graph-gateway`

Knowledge graph queries. Used by agents and available for external tooling.

| Endpoint | What it does |
|----------|-------------|
| `POST /graph-gateway/graphql` | GraphQL queries against the knowledge graph |
| `GET /graph-gateway/summary` | Graph overview (entity counts, predicates) |

### Message Logger — `/message-logger`

NATS message inspection for debugging.

| Endpoint | What it does |
|----------|-------------|
| `GET /message-logger/entries?limit=N` | Recent messages (newest first) |
| `GET /message-logger/trace/{traceID}` | All messages in a trace |
| `GET /message-logger/kv/{bucket}` | KV bucket contents |
| `GET /message-logger/stats` | Message counts and rates |

### Component Manager

| Endpoint | What it does |
|----------|-------------|
| `GET /components` | List all registered components and their status |
| `GET /readyz` | Health check (all components started) |

### Infrastructure

| Endpoint | What it does |
|----------|-------------|
| `GET /openapi.json` | Full OpenAPI 3.0 spec (all endpoints above) |
| `GET /docs` | Swagger UI playground |
| `GET /health` | Project health (NATS, streams, KV buckets) |

## Plan Lifecycle

```
created → drafted → reviewed → approved → architecture_generated → requirements_generated
    → scenarios_generated → scenarios_reviewed → ready_for_execution → implementing
    → reviewing_rollup → complete
```

Plans can also reach `revision_needed` (planner retries), `rejected`, or `archived`.

If `auto_approve` is `true` (default), reviewed plans flow directly to execution without
manual promotion.

## SSE Streams

Three SSE endpoints provide real-time updates:

| Stream | Events |
|--------|--------|
| `/plan-manager/plans/{slug}/stream` | `plan_updated` — plan stage changes |
| `/execution-manager/plans/{slug}/stream` | `task_updated`, `requirement_updated` — execution progress |
| `/agentic-dispatch/activity` | Loop lifecycle events (started, completed, failed) |

## Response Codes

| Code | Meaning |
|------|---------|
| `200` | Success |
| `201` | Created |
| `202` | Accepted (async operation started) |
| `204` | Deleted |
| `400` | Bad request |
| `404` | Not found |
| `409` | Conflict (invalid state transition) |
| `500` | Internal error |
| `503` | Service unavailable (component not ready) |
