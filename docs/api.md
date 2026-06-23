# API Reference

Semspec exposes a REST API at `http://localhost:8080`. Routes are mounted at
`/{component-name}/...`, where the component name is the instance key from
`configs/semspec.json`.

**Interactive docs**: `http://localhost:8080/docs` (Swagger UI playground)
**OpenAPI spec**: `http://localhost:8080/openapi.json` (machine-readable)

## API Groups

### Plans — `/plan-manager/plans`

Plan lifecycle CRUD plus action endpoints for promotion, execution, retry, and
review aggregation.

| Endpoint | What it does |
|----------|-------------|
| `POST /plan-manager/plans` | Create a plan (triggers the planner agent) |
| `GET /plan-manager/plans` | List all plans with current status |
| `GET /plan-manager/plans/{slug}` | Get plan with workflow stage and active loops |
| `PATCH /plan-manager/plans/{slug}` | Update plan fields |
| `DELETE /plan-manager/plans/{slug}` | Delete a plan |
| `POST /plan-manager/plans/{slug}/promote` | Approve a plan for execution |
| `POST /plan-manager/plans/{slug}/execute` | Trigger scenario-based execution |
| `POST /plan-manager/plans/{slug}/retry` | Retry a failed/rejected plan (optional cherry-pick) |
| `POST /plan-manager/plans/{slug}/complete` | Force-complete a plan |
| `POST /plan-manager/plans/{slug}/reject` | Reject a plan |
| `POST /plan-manager/plans/{slug}/archive` | Archive a plan |
| `POST /plan-manager/plans/{slug}/unarchive` | Restore an archived plan |
| `POST /plan-manager/plans/{slug}/export-specs` | Export plan specs |
| `POST /plan-manager/plans/{slug}/infra-reconcile` | Reconcile infra state for a plan |
| `GET /plan-manager/plans/{slug}/reviews` | Aggregated review findings |
| `GET /plan-manager/plans/{slug}/branches` | Per-requirement branch + diff metadata (files view) |
| `GET /plan-manager/plans/{slug}/git-audit` | Git audit log for the plan |
| `GET /plan-manager/plans/{slug}/phases/retrospective` | Phase retrospective data |
| `GET /plan-manager/plans/{slug}/artifacts` | List phase artifacts written by workflow-documents |
| `GET /plan-manager/plans/{slug}/artifacts/{name}` | Read a specific artifact |

### Requirements — `/plan-manager/plans/{slug}/requirements`

CRUD for plan requirements. Requirements are the unit of execution — each gets
decomposed into a TaskDAG at runtime by the requirement-executor.

| Endpoint | What it does |
|----------|-------------|
| `GET /plan-manager/plans/{slug}/requirements` | List requirements |
| `POST /plan-manager/plans/{slug}/requirements` | Create a requirement |
| `GET /plan-manager/plans/{slug}/requirements/{reqId}` | Get a requirement |
| `PATCH /plan-manager/plans/{slug}/requirements/{reqId}` | Update a requirement |
| `DELETE /plan-manager/plans/{slug}/requirements/{reqId}` | Delete a requirement |
| `POST /plan-manager/plans/{slug}/requirements/{reqId}/deprecate` | Deprecate a requirement |
| `GET /plan-manager/plans/{slug}/requirements/{reqId}/file-diff` | Per-file diff for a requirement branch |
| `GET /plan-manager/plans/{slug}/requirements/{reqId}/scenarios` | Scenarios attached to this requirement |

### Scenarios — `/plan-manager/plans/{slug}/scenarios`

CRUD for Given/When/Then scenarios. Scenarios are acceptance criteria attached
to requirements, validated at review time. Filter by `?requirement_id=`.

| Endpoint | What it does |
|----------|-------------|
| `GET /plan-manager/plans/{slug}/scenarios` | List scenarios (optional `?requirement_id=`) |
| `POST /plan-manager/plans/{slug}/scenarios` | Create a scenario |
| `GET /plan-manager/plans/{slug}/scenarios/{scenarioId}` | Get a scenario |
| `PATCH /plan-manager/plans/{slug}/scenarios/{scenarioId}` | Update a scenario |
| `DELETE /plan-manager/plans/{slug}/scenarios/{scenarioId}` | Delete a scenario |

### Plan Decisions — `/plan-manager/plans/{slug}/plan-decisions`

Submit, review, accept, or reject requirement changes after a plan is approved.
Accepting triggers a cascade to affected scenarios and tasks. (Formerly called
"Change Proposals" — broadened to cover execution-exhausted decisions too.)

| Endpoint | What it does |
|----------|-------------|
| `GET /plan-manager/plans/{slug}/plan-decisions` | List plan decisions (optional `?status=`) |
| `POST /plan-manager/plans/{slug}/plan-decisions` | Create a plan decision |
| `GET /plan-manager/plans/{slug}/plan-decisions/{id}` | Get a plan decision |
| `PATCH /plan-manager/plans/{slug}/plan-decisions/{id}` | Update a plan decision |
| `DELETE /plan-manager/plans/{slug}/plan-decisions/{id}` | Delete a plan decision |
| `POST /plan-manager/plans/{slug}/plan-decisions/{id}/submit` | Submit a draft for review |
| `POST /plan-manager/plans/{slug}/plan-decisions/{id}/accept` | Accept (triggers cascade) |
| `POST /plan-manager/plans/{slug}/plan-decisions/{id}/reject` | Reject |

### Workspace Browser — `/plan-manager/workspace`

Proxied to the sandbox container; returns 503 when no sandbox is configured.

| Endpoint | What it does |
|----------|-------------|
| `GET /plan-manager/workspace/tasks` | List sandbox tasks |
| `GET /plan-manager/workspace/tree` | Workspace file tree |
| `GET /plan-manager/workspace/file` | Read a file |
| `GET /plan-manager/workspace/download` | Download a file |

### Execution — `/execution-manager`

Execution observability. Tasks are created at execution time by the decomposer
agent; there is no pre-generated task list.

| Endpoint | What it does |
|----------|-------------|
| `GET /execution-manager/plans/{slug}/tasks` | List active task executions |
| `GET /execution-manager/plans/{slug}/requirements` | List active requirement executions |
| `GET /execution-manager/lessons` | Lessons learned (filter by `?role=`) |
| `GET /execution-manager/lessons/counts` | Per-category error counts by role |

### Project Setup — `/project-manager`

Project initialization wizard, configuration management, and infra health.

| Endpoint | What it does |
|----------|-------------|
| `GET /project-manager/status` | Initialization state (which config files exist) |
| `GET /project-manager/wizard` | Supported languages and frameworks |
| `POST /project-manager/detect` | Auto-detect stack from filesystem |
| `POST /project-manager/scaffold` | Create marker files for selected stack |
| `POST /project-manager/generate-standards` | Generate standards from detected stack |
| `POST /project-manager/init` | Write all config files to `.semspec/` |
| `POST /project-manager/approve` | Approve a config file |
| `GET/PATCH /project-manager/config` | Read or update project.json fields |
| `GET/PATCH /project-manager/checklist` | Read or update quality gate checks |
| `GET/PATCH /project-manager/standards` | Read or update project standards |
| `POST /project-manager/test-check` | Run a single checklist entry |
| `GET /project-manager/health` | Infra health (NATS, streams, KV buckets) |
| `GET /project-manager/graph-summary` | Federated graph summary used by the UI |

### Q&A — `/question-manager`

Question routing and answer collection.

| Endpoint | What it does |
|----------|-------------|
| `GET /question-manager/questions/` | List questions (filters: `status`, `topic`, `category`, `limit`) |
| `GET /question-manager/questions/{id}` | Get a single question |
| `POST /question-manager/questions/{id}/answer` | Submit an answer |

### Agent Activity — `/agentic-dispatch`

Real-time agent monitoring and command dispatch (provided by semstreams).

| Endpoint | What it does |
|----------|-------------|
| `POST /agentic-dispatch/message` | Send a command (e.g., `/plan`, `/execute`) |
| `GET /agentic-dispatch/commands` | List available slash commands |
| `GET /agentic-dispatch/loops` | List active agent loops |
| `GET /agentic-dispatch/loops/{id}` | Get a single loop |
| `POST /agentic-dispatch/loops/{id}/signal` | Send a signal (cancel, etc.) to a loop |
| `GET /agentic-dispatch/health` | Dispatch health |
| `GET /agentic-dispatch/debug/state` | Debug snapshot of dispatcher state |

### Agent Trajectories — `/agentic-loop`

Per-loop reasoning trajectory data (provided by semstreams).

| Endpoint | What it does |
|----------|-------------|
| `GET /agentic-loop/trajectories` | List trajectories |
| `GET /agentic-loop/trajectories/{loopId}` | Trajectory for a specific loop |

### Graph Gateway — `/graph-gateway`

Knowledge graph queries. Used by agents and available for external tooling.

| Endpoint | What it does |
|----------|-------------|
| `POST /graph-gateway/graphql` | GraphQL queries against the knowledge graph |
| `GET /graph-gateway/mcp` | MCP transport endpoint |
| `GET /graph-gateway/` | GraphQL playground (when `enable_playground=true`) |

For a federated, agent-friendly summary, prefer
`GET /project-manager/graph-summary`.

### Message Logger — `/message-logger`

NATS message inspection for debugging (provided by semstreams).

| Endpoint | What it does |
|----------|-------------|
| `GET /message-logger/entries?limit=N` | Recent messages (newest first) |
| `GET /message-logger/stats` | Message counts and rates |
| `GET /message-logger/subjects` | Distinct subjects observed |
| `GET /message-logger/trace/{traceID}` | All messages in a trace |
| `GET /message-logger/kv/{bucket}` | KV bucket contents |
| `GET /message-logger/kv/{bucket}/{key}` | Single KV entry |
| `GET /message-logger/kv/{bucket}/watch` | KV change stream |

### Component Manager — `/components`

Component lifecycle and flow inspection (provided by semstreams).

| Endpoint | What it does |
|----------|-------------|
| `GET /components/list` | List all registered components and their status |
| `GET /components/health` | Aggregated component health |
| `GET /components/types` | Component type catalogue |
| `GET /components/types/{id}` | Single component type |
| `GET /components/status/{name}` | Status for one component |
| `GET /components/config/{name}` | Config for one component |
| `GET /components/flowgraph` | Inter-component flow graph |
| `GET /components/validate` | Flow validation report |
| `GET /components/gaps` | Detected flow gaps |
| `GET /components/paths` | Computed flow paths |

### System

System-level endpoints registered by the semstreams service manager.

| Endpoint | What it does |
|----------|-------------|
| `GET /openapi.json` | Full OpenAPI 3.0 spec |
| `GET /docs` | Swagger UI playground |
| `GET /health` | Aggregated service health |
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe (all services started) |
| `GET /services` | Registered service list |
| `GET /services/health` | Per-service health |
| `GET /graph/triples` | Operator-facing read-only graph triples |

> `/health` (system) and `/project-manager/health` (project infra) are
> different endpoints. Docker Compose uses `/project-manager/health` as the
> readiness gate because it specifically validates NATS, streams, and KV
> buckets.

## Plan Lifecycle

The plan-manager owns the `PLAN_STATES` KV bucket. Status transitions are
defined in `workflow/types.go`. The happy path:

```
created → drafting → drafted → reviewing_draft → reviewed → approved
       → generating_requirements → requirements_generated
       → reviewing_requirements → requirements_reviewed       (ADR-051 R-req)
       → generating_architecture → architecture_generated
       → reviewing_architecture → architecture_reviewed       (ADR-051 R-arch)
       → preparing_stories → stories_generated
       → generating_scenarios → scenarios_generated
       → reviewing_scenarios → scenarios_reviewed → ready_for_execution
       → implementing
       → ready_for_qa (qa_level=unit/integration only)
       → reviewing_qa
       → complete
```

Requirements precede architecture (the architect designs against the
requirements). R-req and R-arch are the ADR-051 per-phase adversarial reviews —
mandatory pipeline stages like the draft (R1) and scenario (R2) reviews. The
plan-reviewer is the sole claimant of each `*_generated` state; the next
generator claims the post-review `*_reviewed` state.

Other reachable statuses:

| Status | Meaning |
|--------|---------|
| `revision_needed` | Plan-reviewer rejected; planner retries (max 3) |
| `reviewing_requirements` / `requirements_reviewed` | ADR-051 R-req round (requirements review) |
| `reviewing_architecture` / `architecture_reviewed` | ADR-051 R-arch round (architecture review) |
| `awaiting_review` | Human gate (auto-approve disabled) |
| `changed` | Plan was edited mid-flight; re-evaluation pending |
| `rejected` | Reviewer or QA rejected; PlanDecisions describe the issues |
| `archived` | Plan archived via `POST .../archive` |
| `reviewing_rollup` | Legacy QA stage; kept for in-flight plans on upgrade only |

When `auto_approve=true` (default), `reviewed` flows directly to `approved`
without manual promotion. `qa_level` (project config) controls which path is
taken at the implementing→complete transition:

- `none` — straight to `complete`
- `synthesis` — `implementing → reviewing_qa → complete`
- `unit`/`integration`/`full` — `implementing → ready_for_qa → reviewing_qa → complete`

## SSE Streams

Real-time updates use Server-Sent Events.

| Stream | Events |
|--------|--------|
| `/plan-manager/plans/{slug}/stream` | `plan_updated` — plan stage changes |
| `/execution-manager/plans/{slug}/stream` | `task_updated`, `requirement_updated` — execution progress |
| `/agentic-dispatch/activity` | `event: activity` — loop lifecycle (created/updated/deleted) |
| `/question-manager/questions/stream` | Question lifecycle (asked/answered/timeout) |
| `/message-logger/kv/{bucket}/watch` | KV change stream |

## Response Codes

| Code | Meaning |
|------|---------|
| `200` | Success |
| `201` | Created |
| `202` | Accepted (async operation started) |
| `204` | Deleted |
| `400` | Bad request |
| `404` | Not found |
| `405` | Method not allowed |
| `409` | Conflict (invalid state transition) |
| `500` | Internal error |
| `503` | Service unavailable (component not ready, sandbox not configured) |
