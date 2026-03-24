# Plan API Reference

REST API for managing plans, requirements, scenarios, and change proposals. All endpoints are
served by the `plan-api` component at `/plan-api/`.

## Plans

Plans are the top-level entity. Created via `/plan` command or REST API.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/plans` | Create a plan (triggers planner) |
| `GET` | `/plans` | List all plans |
| `GET` | `/plans/{slug}` | Get plan by slug |
| `PATCH` | `/plans/{slug}` | Update plan fields (title, goal, context) |
| `DELETE` | `/plans/{slug}` | Delete a plan |
| `POST` | `/plans/{slug}/promote` | Approve a plan for execution |
| `POST` | `/plans/{slug}/execute` | Trigger scenario-based execution |
| `GET` | `/plans/{slug}/reviews` | Get plan review history |
| `GET` | `/plans/{slug}/phases/retrospective` | Retrospective view (requirement/scenario/task tree) |

### Plan Lifecycle

```
created → planning → reviewed → approved → ready_for_execution → implementing → completed
                        │
                        └── needs_changes → (revision cycle)
```

After plan-coordinator produces a plan, the plan-reviewer validates it against project SOPs.
If `auto_approve` is `true` (default), approved plans flow directly to `ready_for_execution`.
If `false`, the pipeline pauses until a human calls `POST /plans/{slug}/promote`.

## Requirements

Requirements are plan-scoped behavioral specifications generated during planning. Each
requirement describes a capability the system should have. Requirements are the parent
entity for scenarios.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plans/{slug}/requirements` | List all requirements |
| `GET` | `/plans/{slug}/requirements/{reqId}` | Get requirement by ID |
| `POST` | `/plans/{slug}/requirements` | Create a requirement |
| `PATCH` | `/plans/{slug}/requirements/{reqId}` | Update requirement fields |
| `DELETE` | `/plans/{slug}/requirements/{reqId}` | Delete a requirement |
| `POST` | `/plans/{slug}/requirements/{reqId}/deprecate` | Deprecate a requirement |
| `GET` | `/plans/{slug}/requirements/{reqId}/scenarios` | List scenarios for requirement |

### Create Requirement

```bash
curl -X POST http://localhost:8080/plan-api/plans/my-plan/requirements \
  -H "Content-Type: application/json" \
  -d '{
    "title": "User authentication",
    "description": "Users can log in with email and password",
    "depends_on": ["req-id-1"]
  }'
```

**Request body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Short description of the requirement |
| `description` | string | no | Detailed explanation |
| `depends_on` | string[] | no | IDs of prerequisite requirements |

**Response:** `201 Created` with the full `Requirement` object.

### Update Requirement

```bash
curl -X PATCH http://localhost:8080/plan-api/plans/my-plan/requirements/req-123 \
  -H "Content-Type: application/json" \
  -d '{"title": "Updated title", "depends_on": ["req-456"]}'
```

All fields are optional. Only provided fields are updated.

### Deprecate Requirement

```bash
curl -X POST http://localhost:8080/plan-api/plans/my-plan/requirements/req-123/deprecate
```

Sets status to `deprecated`. This is a terminal state — deprecated requirements cannot be
reactivated.

### Requirement Status

| Status | Description |
|--------|-------------|
| `active` | Current and actionable (default) |
| `deprecated` | No longer relevant (terminal) |
| `superseded` | Replaced by another requirement (can revert to active) |

### Dependency Validation

Requirements support `depends_on` for ordering. The API validates:
- Referenced requirements must exist in the same plan
- Circular dependencies are rejected
- Dependencies can be cleared by setting `depends_on` to `[]`

## Scenarios

Scenarios are Given/When/Then specifications that describe observable behavior. Each scenario
belongs to a requirement and serves as acceptance criteria validated at review time. The
requirement-executor decomposes each requirement (not each scenario) into a TaskDAG at execution
time; all scenarios for that requirement are verified by the reviewer after DAG nodes complete.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plans/{slug}/scenarios` | List all scenarios (optional `?requirement_id=` filter) |
| `GET` | `/plans/{slug}/scenarios/{scenarioId}` | Get scenario by ID |
| `POST` | `/plans/{slug}/scenarios` | Create a scenario |
| `PATCH` | `/plans/{slug}/scenarios/{scenarioId}` | Update scenario fields |
| `DELETE` | `/plans/{slug}/scenarios/{scenarioId}` | Delete a scenario |

### Create Scenario

```bash
curl -X POST http://localhost:8080/plan-api/plans/my-plan/scenarios \
  -H "Content-Type: application/json" \
  -d '{
    "requirement_id": "req-123",
    "given": "a registered user with valid credentials",
    "when": "they submit the login form",
    "then": ["they receive a JWT token", "they are redirected to the dashboard"]
  }'
```

**Request body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `requirement_id` | string | yes | Parent requirement ID |
| `given` | string | yes | Precondition |
| `when` | string | yes | Action or trigger |
| `then` | string[] | yes | Expected outcomes (at least one) |

**Response:** `201 Created` with the full `Scenario` object.

### Update Scenario

```bash
curl -X PATCH http://localhost:8080/plan-api/plans/my-plan/scenarios/sc-456 \
  -H "Content-Type: application/json" \
  -d '{"when": "they submit the login form with MFA", "status": "passing"}'
```

All fields are optional.

### List by Requirement

```bash
curl http://localhost:8080/plan-api/plans/my-plan/scenarios?requirement_id=req-123
```

Returns only scenarios belonging to the specified requirement.

### Scenario Status

| Status | Description |
|--------|-------------|
| `pending` | Not yet verified (default) |
| `passing` | Verified and passing |
| `failing` | Verified and failing |
| `skipped` | Intentionally skipped |

Transitions: `pending` → `passing`/`failing`/`skipped`, `passing` ↔ `failing`,
`skipped` → `pending`.

## Change Proposals

Change proposals are the mechanism for modifying requirements after a plan is approved.
They track the rationale for changes and trigger cascading updates to affected scenarios
and tasks.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plans/{slug}/change-proposals` | List all change proposals |
| `GET` | `/plans/{slug}/change-proposals/{id}` | Get proposal by ID |
| `POST` | `/plans/{slug}/change-proposals` | Create a change proposal |
| `PATCH` | `/plans/{slug}/change-proposals/{id}` | Update proposal fields |
| `DELETE` | `/plans/{slug}/change-proposals/{id}` | Delete a proposal |
| `POST` | `/plans/{slug}/change-proposals/{id}/accept` | Accept and apply the proposal |
| `POST` | `/plans/{slug}/change-proposals/{id}/reject` | Reject the proposal |

### Create Change Proposal

```bash
curl -X POST http://localhost:8080/plan-api/plans/my-plan/change-proposals \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Add rate limiting to auth endpoints",
    "rationale": "Security review identified missing rate limits",
    "proposed_by": "security-team",
    "affected_requirement_ids": ["req-123", "req-456"]
  }'
```

**Request body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Brief description of the change |
| `rationale` | string | no | Why the change is needed |
| `proposed_by` | string | no | Who proposed it |
| `affected_requirement_ids` | string[] | no | Requirements affected by this change |

### Accept Proposal

```bash
curl -X POST http://localhost:8080/plan-api/plans/my-plan/change-proposals/cp-789/accept
```

Accepting a proposal triggers a cascade: affected scenarios are marked dirty, dependent tasks
are invalidated, and the execution pipeline re-processes the affected scope. The proposal
status transitions to `accepted` and a `change_proposal.accepted` event is published.

### Change Proposal Status

| Status | Description |
|--------|-------------|
| `proposed` | Submitted for review (default) |
| `under_review` | Being evaluated |
| `accepted` | Approved and cascade applied |
| `rejected` | Declined |
| `archived` | Terminal state after accept or reject |

Transitions: `proposed` → `under_review` → `accepted`/`rejected` → `archived`.

## Tasks (Read-Only)

Tasks are created at execution time by the decomposer agent — not via the API. The tasks
endpoint provides read-only access for observability.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plans/{slug}/tasks` | List all tasks for a plan |

## Human-in-the-Loop Flow

The typical human review flow between plan approval and execution:

```
plan approved (ready_for_execution)
       │
       ▼
  Review requirements ──── GET /plans/{slug}/requirements
       │
       ├── Edit ──────────── PATCH /plans/{slug}/requirements/{id}
       ├── Remove ────────── DELETE /plans/{slug}/requirements/{id}
       └── Add ───────────── POST /plans/{slug}/requirements
       │
       ▼
  Review scenarios ──────── GET /plans/{slug}/scenarios
       │
       ├── Edit ──────────── PATCH /plans/{slug}/scenarios/{id}
       ├── Remove ────────── DELETE /plans/{slug}/scenarios/{id}
       └── Add ───────────── POST /plans/{slug}/scenarios
       │
       ▼
  /execute <slug> ────────── POST /plans/{slug}/execute
```

When `auto_approve` is enabled (default), the pipeline skips the human review step and flows
directly from plan approval to execution. Requirements and scenarios are still editable via
the API during execution, but changes after execution starts should use change proposals to
ensure proper cascade handling.

## Response Codes

| Code | Meaning |
|------|---------|
| `200` | Success |
| `201` | Created |
| `204` | Deleted (no content) |
| `400` | Bad request (missing required fields, validation error) |
| `404` | Not found |
| `409` | Conflict (invalid status transition) |
| `500` | Internal error |
