# Semspec Components

> **How does the planning pipeline self-coordinate?** See [Architecture: KV-Driven Pipeline](03-architecture.md#kv-driven-pipeline)
> for the decision framework and status transition table.

---

## Indexing

> **Note**: Code indexing (AST parsing, source ingestion) is now handled by **semsource**, an
> external service that watches per-scenario branches and publishes `code.artifact.*` entities
> to the graph. The `processor/ast/` parsing library remains for local tool use.

---

## Project Initialization

### project-manager

**Purpose**: Project initialization API — stack detection, marker file scaffolding, standards
generation, and per-file approval tracking. Used by the setup wizard UI before a project is ready
for planning. Follows the manager pattern with versioned config reconciliation.

**Location**: `processor/project-manager/`

#### Configuration

```json
{
  "repo_path": "",
  "ports": null
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `repo_path` | string | `SEMSPEC_REPO_PATH` or cwd | Repository root path to inspect and write into |
| `ports` | PortConfig | — | Optional HTTP port overrides |

#### HTTP Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/project-manager/project/status` | Initialization state: which files exist, approval timestamps |
| `GET` | `/project-manager/project/wizard` | Supported languages and frameworks for the setup wizard |
| `POST` | `/project-manager/project/scaffold` | Create language/framework marker files in the repo |
| `POST` | `/project-manager/project/detect` | Filesystem-based stack detection (no LLM) |
| `POST` | `/project-manager/project/generate-standards` | Generate project standards rules (stub — LLM Phase 3) |
| `POST` | `/project-manager/project/init` | Write `project.json`, `checklist.json`, `standards.json` to `.semspec/` |
| `POST` | `/project-manager/project/approve` | Set `approved_at` on one of the three config files |

#### Behavior

1. **Detect**: Scans the filesystem for language markers (`go.mod`, `tsconfig.json`, etc.) and
   returns a `DetectionResult` without making LLM calls.
2. **Scaffold**: Creates minimal marker files so that detection works on empty projects.
3. **Init**: Writes all three config files atomically from a single wizard submission. Also creates
   `.semspec/sources/docs/` for future SOP documents.
4. **Approve**: Stamps `approved_at` on individual config files. Returns `all_approved: true` once
   all three files carry a timestamp — this gates the planning workflow.

No NATS subjects consumed or published. All state is filesystem-based.

---

## Planning

### planner

**Purpose**: Generates Goal/Context/Scope for plans using LLM. Self-triggers by watching the
PLAN_STATES KV bucket for newly created plans (revision 1), eliminating the need for an external
coordinator.

**Location**: `processor/planner/`

#### Configuration

```json
{
  "stream_name": "WORKFLOWS",
  "consumer_name": "planner",
  "trigger_subject": "workflow.trigger.planner",
  "default_capability": "planning"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOWS` | JetStream stream name |
| `consumer_name` | string | `planner` | Durable consumer name |
| `trigger_subject` | string | `workflow.trigger.planner` | Legacy explicit trigger subject |
| `default_capability` | string | `planning` | Default model capability |

#### Behavior

1. **Watches PLAN_STATES KV**: A background watcher fires whenever a plan entry has revision 1
   (i.e., the plan was just created by `plan-manager`). Revision > 1 entries (status updates,
   review saves) are ignored.
2. **Loads Plan**: Reads the new plan's goal and metadata from PLAN_STATES.
3. **Generates Content**: Calls LLM with planner system prompt to produce Goal/Context/Scope.
4. **Parses Response**: Extracts JSON from markdown-fenced LLM output (up to 5 format retries).
5. **Saves Plan**: Writes generated content to `.semspec/plans/{slug}/plan.json` and sets
   PLAN_STATES status to `drafted`.

The JetStream consumer on `workflow.trigger.planner` remains active as a secondary trigger path
for explicit invocations.

#### LLM Response Format

The component expects the LLM to return JSON, optionally wrapped in markdown code fences:

```json
{
  "goal": "Clear statement of what the plan accomplishes",
  "context": "Current state and relevant background",
  "scope": {
    "include": ["files/areas to modify"],
    "exclude": ["files/areas to avoid"],
    "do_not_touch": ["critical files to preserve"]
  }
}
```

#### Self-Trigger Pattern

```
plan-manager writes PLAN_STATES[slug] (revision 1, status=created)
  │
  └── planner KV watcher fires
        │
        ├── Call LLM → parse JSON
        ├── Write plan.json
        └── Update PLAN_STATES[slug] → status=drafted
```

---

### plan-reviewer

**Purpose**: SOP-aware plan review before approval. Validates plans against project SOPs and flags
scope hallucination. Self-triggers by watching the PLAN_STATES KV bucket for plans that have
reached `drafted` or `scenarios_generated` status.

**Location**: `processor/plan-reviewer/`

#### Configuration

```json
{
  "stream_name": "WORKFLOWS",
  "consumer_name": "plan-reviewer",
  "trigger_subject": "workflow.trigger.plan-reviewer",
  "result_subject_prefix": "workflow.result.plan-reviewer",
  "plan_state_bucket": "PLAN_STATES",
  "graph_gateway_url": "http://localhost:8082",
  "context_token_budget": 4000,
  "default_capability": "reviewing",
  "llm_timeout": "120s"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOWS` | JetStream stream for workflow triggers |
| `consumer_name` | string | `plan-reviewer` | Durable consumer name |
| `trigger_subject` | string | `workflow.trigger.plan-reviewer` | Legacy explicit trigger subject |
| `result_subject_prefix` | string | `workflow.result.plan-reviewer` | Subject prefix for review results |
| `plan_state_bucket` | string | `PLAN_STATES` | KV bucket watched for status changes |
| `graph_gateway_url` | string | `http://localhost:8082` | Graph gateway URL for context queries |
| `context_token_budget` | int | `4000` | Token budget for additional graph context |
| `default_capability` | string | `reviewing` | Default model capability for plan review |
| `llm_timeout` | string | `120s` | Timeout for LLM calls |
| `context_build_timeout` | string | `30s` | Timeout for context building requests |

#### Behavior

1. **Watches PLAN_STATES KV**: A background watcher fires on status changes. The component
   reviews on `drafted` (after planner completes) and on `scenarios_generated` (after the
   scenario-generator publishes scenarios). This is the "KV Twofer" — the plan-manager write
   IS the trigger.
2. **Enriches context**: Queries graph for related plans and code patterns.
3. **Auto-approves**: If no SOP context and no graph context are available, returns `approved`
   immediately.
4. **Validates**: Calls LLM (temperature 0.3) to verify the plan against each SOP requirement.
5. **Checks scope**: Compares scope paths against the actual project file tree to detect
   hallucinated paths.
6. **Returns verdict**: `approved` or `needs_changes` with a `findings` array.

The JetStream consumer on `workflow.trigger.plan-reviewer` remains active as a secondary trigger
path for explicit invocations.

Each finding has the shape:

```json
{
  "sop_id": "SOP-001",
  "sop_title": "Testing Standards",
  "severity": "error",
  "status": "violation",
  "issue": "No test tasks included",
  "suggestion": "Add unit test tasks for new functions",
  "evidence": "scope includes processor/ but tasks.json has no test entries"
}
```

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.trigger.plan-reviewer` | JetStream (WORKFLOWS) | Input | Plan review triggers |
| `workflow.result.plan-reviewer.<slug>` | JetStream | Output | Review results (ordering guarantee) |

---

### requirement-generator

**Purpose**: Generates structured Requirements from approved plans. Runs after plan approval and
publishes `workflow.events.requirements.generated` when complete. Part of the reactive planning
pipeline that replaces the monolithic task-generator.

**Location**: `processor/requirement-generator/`

#### Configuration

```json
{
  "stream_name": "WORKFLOW",
  "consumer_name": "requirement-generator",
  "trigger_subject": "workflow.async.requirement-generator",
  "default_capability": "planning"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOW` | JetStream stream for workflow triggers |
| `consumer_name` | string | `requirement-generator` | Durable consumer name |
| `trigger_subject` | string | `workflow.async.requirement-generator` | Subject for generation triggers |
| `default_capability` | string | `planning` | Model capability for requirement generation |
| `ports` | PortConfig | (see defaults) | Input/output port definitions |

#### Behavior

1. **Consumes trigger**: Receives a plan slug and goal/context/scope from the trigger payload.
2. **Calls LLM**: Generates a structured list of Requirements using the planning model capability.
3. **Persists**: Writes Requirements to the plan's filesystem state.
4. **Publishes event**: Sends `workflow.events.requirements.generated` to advance the pipeline.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.async.requirement-generator` | JetStream (WORKFLOW) | Input | Generation triggers |
| `workflow.events.requirements.generated` | Core NATS | Output | Requirements-generated completion |

---

### scenario-generator

**Purpose**: Generates Given/When/Then scenarios from Requirements. Runs after requirements are
generated and publishes `workflow.events.scenarios.generated` when complete.

**Location**: `processor/scenario-generator/`

#### Configuration

```json
{
  "stream_name": "WORKFLOW",
  "consumer_name": "scenario-generator",
  "trigger_subject": "workflow.async.scenario-generator",
  "default_capability": "planning"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOW` | JetStream stream name |
| `consumer_name` | string | `scenario-generator` | Durable consumer name |
| `trigger_subject` | string | `workflow.async.scenario-generator` | Subject for generation triggers |
| `default_capability` | string | `planning` | Model capability for scenario generation |
| `ports` | PortConfig | (see defaults) | Input/output port definitions |

#### Behavior

1. **Consumes trigger**: Receives the plan slug and its generated Requirements.
2. **Calls LLM**: Produces one or more Given/When/Then scenarios per Requirement.
3. **Persists**: Writes Scenarios to the plan's filesystem state with parent `RequirementID` links.
4. **Publishes event**: Sends `workflow.events.scenarios.generated` to advance the pipeline.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.async.scenario-generator` | JetStream (WORKFLOW) | Input | Generation triggers |
| `workflow.events.scenarios.generated` | Core NATS | Output | Scenarios-generated completion |

---

## Plan API

### plan-manager

**Purpose**: REST API for plans, requirements, scenarios, change proposals, Q&A, and execution
triggers. The primary HTTP interface used by the UI and CLI for all plan lifecycle operations.
Owns plan entities via the manager pattern: `planStore`, `requirementStore`, and `scenarioStore`
each maintain a `sync.Map` cache backed by `WriteTriple` durability in the graph. Plan-manager is
the **single writer** for plan state — generators publish typed events
(`RequirementsGeneratedEvent`, `ScenariosForRequirementGeneratedEvent`) and plan-manager persists
all transitions.

**Location**: `processor/plan-manager/`

#### Configuration

```json
{
  "execution_bucket_name": "WORKFLOW_EXECUTIONS",
  "event_stream_name": "WORKFLOW",
  "user_stream_name": "USER",
  "sandbox_url": ""
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `execution_bucket_name` | string | `WORKFLOW_EXECUTIONS` | KV bucket for workflow execution state |
| `event_stream_name` | string | `WORKFLOW` | JetStream stream for workflow events |
| `user_stream_name` | string | `USER` | JetStream stream for user signals (escalation, errors) |
| `sandbox_url` | string | `` | Sandbox server URL for workspace browser (empty = disabled) |

#### HTTP Endpoints

**Plans**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plan-manager/plans` | List all plans |
| `POST` | `/plan-manager/plans` | Create a new plan |
| `GET` | `/plan-manager/plans/{slug}` | Get plan by slug |
| `PUT` | `/plan-manager/plans/{slug}` | Update plan |
| `DELETE` | `/plan-manager/plans/{slug}` | Delete plan |
| `POST` | `/plan-manager/plans/{slug}/promote` | Approve plan and trigger planning pipeline |
| `POST` | `/plan-manager/plans/{slug}/execute` | Trigger execution for an approved plan |
| `GET` | `/plan-manager/plans/{slug}/reviews` | Get plan review synthesis result |
| `GET` | `/plan-manager/plans/{slug}/tasks` | List tasks for a plan |
| `GET` | `/plan-manager/plans/{slug}/phases/retrospective` | Get execution retrospective |

**Requirements**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plan-manager/plans/{slug}/requirements` | List requirements |
| `POST` | `/plan-manager/plans/{slug}/requirements` | Create requirement |
| `GET` | `/plan-manager/plans/{slug}/requirements/{id}` | Get requirement |
| `PUT` | `/plan-manager/plans/{slug}/requirements/{id}` | Update requirement |
| `DELETE` | `/plan-manager/plans/{slug}/requirements/{id}` | Delete requirement |
| `POST` | `/plan-manager/plans/{slug}/requirements/{id}/deprecate` | Deprecate requirement |

**Scenarios**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plan-manager/plans/{slug}/scenarios` | List scenarios (optionally filtered by requirement) |
| `POST` | `/plan-manager/plans/{slug}/scenarios` | Create scenario |
| `GET` | `/plan-manager/plans/{slug}/scenarios/{id}` | Get scenario |
| `PUT` | `/plan-manager/plans/{slug}/scenarios/{id}` | Update scenario |
| `DELETE` | `/plan-manager/plans/{slug}/scenarios/{id}` | Delete scenario |

**Change Proposals**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/plan-manager/plans/{slug}/change-proposals` | List change proposals |
| `POST` | `/plan-manager/plans/{slug}/change-proposals` | Create change proposal |
| `GET` | `/plan-manager/plans/{slug}/change-proposals/{id}` | Get change proposal |
| `PUT` | `/plan-manager/plans/{slug}/change-proposals/{id}` | Update change proposal |
| `DELETE` | `/plan-manager/plans/{slug}/change-proposals/{id}` | Delete change proposal |
| `POST` | `/plan-manager/plans/{slug}/change-proposals/{id}/submit` | Submit for review |
| `POST` | `/plan-manager/plans/{slug}/change-proposals/{id}/accept` | Accept and trigger cascade |
| `POST` | `/plan-manager/plans/{slug}/change-proposals/{id}/reject` | Reject proposal |

**Q&A and Workspace**

| Method | Path | Description |
|--------|------|-------------|
| `*` | `/plan-manager/questions/*` | Q&A endpoints (delegated to question handler) |
| `GET` | `/plan-manager/workspace/tasks` | Active agent task list (sandbox proxy) |
| `GET` | `/plan-manager/workspace/tree` | Workspace file tree (sandbox proxy) |
| `GET` | `/plan-manager/workspace/file` | Read a workspace file (sandbox proxy) |
| `GET` | `/plan-manager/workspace/download` | Download workspace archive (sandbox proxy) |

#### Behavior

The component subscribes to workflow and user signal streams to keep plan state up to date
in real time:

- **Workflow events**: `plan.approved`, `requirements.generated`, `scenarios.generated`,
  `scenario.execution.complete`, `task.execution.complete`, `plan.rollup.complete` — advance
  plan status and update scenario/task state in the filesystem.
- **User signals**: escalation and error events published on the USER stream update plan and
  task status without requiring a polling round-trip.
- **Workspace endpoints**: proxied to the sandbox server. Returns `503` when `sandbox_url` is
  not configured.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.events.>` | JetStream (WORKFLOW) | Input | Plan lifecycle events |
| `user.signal.>` | JetStream (USER) | Input | Escalation and error signals |
| `workflow.trigger.change-proposal-cascade` | JetStream (WORKFLOW) | Output | Cascade trigger on accept |

---

## Sources

> **Note**: Source/document ingestion is now handled by **semsource**. The `vocabulary/source/`
> predicate namespace is shared between semspec and semsource. Context-builder strategies
> discover semsource-published entities via `QueryEntitiesByPredicate("source.doc")`.

---

## Execution

### scenario-orchestrator

**Purpose**: Entry point for reactive execution (ADR-025). Receives an orchestration trigger for
a plan, and fires a `requirement-execution-loop` trigger for each pending or dirty Requirement.
Only active when `reactive_mode=true` on `task-generator`. Scenarios are acceptance criteria
validated at review time—they are not dispatched as execution units.

**Location**: `processor/scenario-orchestrator/`

#### Configuration

```json
{
  "stream_name": "WORKFLOW",
  "consumer_name": "scenario-orchestrator",
  "trigger_subject": "scenario.orchestrate.*",
  "workflow_trigger_subject": "workflow.trigger.requirement-execution-loop",
  "execution_timeout": "120s",
  "max_concurrent": 5
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOW` | JetStream stream for orchestration triggers |
| `consumer_name` | string | `scenario-orchestrator` | Durable consumer name |
| `trigger_subject` | string | `scenario.orchestrate.*` | Pattern for per-plan triggers |
| `workflow_trigger_subject` | string | `workflow.trigger.requirement-execution-loop` | Subject for per-requirement triggers |
| `execution_timeout` | string | `120s` | Maximum time for a single orchestration cycle |
| `max_concurrent` | int | `5` | Maximum parallel requirement executions triggered per cycle (1–20) |

#### Trigger Payload

```json
{
  "plan_slug": "add-user-authentication",
  "requirements": [
    {
      "requirement_id": "requirement.add-user-authentication.1",
      "prompt": "Implement JWT-based login ...",
      "role": "developer",
      "model": "qwen"
    }
  ],
  "trace_id": "abc123"
}
```

#### Behavior

1. **Receives trigger**: Consumes `OrchestratorTrigger` from `scenario.orchestrate.<planSlug>`
1. **Dispatches concurrently**: Fires one `RequirementExecutionRequest` per Requirement, bounded
   by `max_concurrent`
1. **ACKs on success**: NAKs on any dispatch failure (message will be redelivered, max 3 attempts)

The orchestrator does not track execution results. Once triggers are dispatched it is done.
The `requirement-executor` and `execution-manager` components handle the rest.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `scenario.orchestrate.*` | JetStream (WORKFLOW) | Input | Per-plan orchestration triggers |
| `workflow.trigger.requirement-execution-loop` | JetStream (WORKFLOW) | Output | Per-requirement execution triggers |

---

### requirement-executor

**Purpose**: Receives a per-requirement execution trigger, runs a decomposer agent to build a
TaskDAG, then dispatches each DAG node serially to the `execution-manager`. Runs a
requirement-level review after all nodes complete. Scenarios attached to the requirement are
used as acceptance criteria by the reviewer, not as execution units.

**Location**: `processor/requirement-executor/`

#### Configuration

```json
{
  "timeout_seconds": 3600,
  "model": "default",
  "decomposer_model": "",
  "sandbox_url": "",
  "max_requirement_retries": 2
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `timeout_seconds` | int | `3600` | Per-requirement timeout covering the full decompose → execute pipeline |
| `model` | string | `default` | Model endpoint name for agent tasks |
| `decomposer_model` | string | `model` fallback | Separate model for the decomposer agent (allows independent mock fixtures) |
| `sandbox_url` | string | `` | Sandbox server URL for per-requirement branch management |
| `max_requirement_retries` | int | `2` | Max requirement-level retries after reviewer rejection (0–5) |
| `ports` | PortConfig | (see defaults) | Input/output port definitions |

#### Behavior

1. **Receives trigger**: Consumes `RequirementExecutionRequest` from
   `workflow.trigger.requirement-execution-loop`.
2. **Creates branch**: If `sandbox_url` is configured, creates a per-requirement git worktree
   branch for isolation.
3. **Runs decomposer**: Dispatches a decomposer agent task (`agent.task.development`) that calls
   `decompose_task` to produce a validated `TaskDAG` JSON payload.
4. **Executes nodes serially**: Dispatches each DAG node in topological order to
   `workflow.trigger.task-execution-loop`. Waits for each node's `agent.complete.*` event before
   dispatching the next.
5. **Requirement review**: When all nodes complete, runs the requirement reviewer agent, which
   validates the changeset against the requirement's scenarios as acceptance criteria. On
   rejection, the `error_category` in the verdict determines whether dirty nodes are re-run
   (`fixable`) or the entire DAG is re-decomposed (`restructure`), up to `max_requirement_retries`.
6. **Publishes completion**: Writes terminal phase triples; the rule processor sets final status
   and publishes `workflow.events.scenario.execution_complete`.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.trigger.requirement-execution-loop` | JetStream (WORKFLOW) | Input | Per-requirement execution triggers |
| `agent.complete.>` | JetStream (AGENT) | Input | Agentic loop completion events |
| `agent.task.development` | JetStream (AGENT) | Output | Decomposer agent tasks |
| `workflow.trigger.task-execution-loop` | JetStream (WORKFLOW) | Output | DAG node dispatch to execution-manager |
| `graph.mutation.triple.add` | Core NATS | Output | Entity state triples |
| `workflow.events.scenario.execution_complete` | JetStream | Output | Requirement execution complete |

---

### execution-manager

**Purpose**: Runs the 4-stage TDD pipeline for a single DAG node: **Tester** → **Builder** →
**Structural Validator** → **Code Reviewer**. Manages retry budget and routes rejections back to
the appropriate stage based on error category. Renamed from `execution-orchestrator` for
consistency with the manager pattern used across semspec components.

**Location**: `processor/execution-manager/`

#### Configuration

```json
{
  "max_iterations": 3,
  "timeout_seconds": 1800,
  "model": "default",
  "sandbox_url": "",
  "graph_gateway_url": "",
  "indexing_budget": "60s",
  "lesson_threshold": 2
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_iterations` | int | `3` | Max develop→validate→review cycles before escalation |
| `timeout_seconds` | int | `1800` | Per-task timeout covering the full pipeline (30 min) |
| `model` | string | `default` | Model endpoint name passed to dispatched agents |
| `sandbox_url` | string | `` | Sandbox server URL for worktree isolation (empty = disabled) |
| `graph_gateway_url` | string | `` | Graph gateway URL for indexing gate (empty = disabled) |
| `indexing_budget` | string | `60s` | Max wait for semsource to index a merge commit |
| `lesson_threshold` | int | `2` | Per-role per-category error count that triggers a recurring-error notification |
| `ports` | PortConfig | (see defaults) | Input/output port definitions |

#### Pipeline Stages

| Stage | Agent Task Subject | Phase Triple | Description |
|-------|-------------------|--------------|-------------|
| Tester | `agent.task.testing` | `testing` | Writes failing unit tests (TDD red phase) |
| Builder | `agent.task.building` | `building` | Implements code to make tests pass (TDD green phase) |
| Structural Validator | `workflow.async.structural-validator` | `validating` | Runs checklist shell commands |
| Code Reviewer | `agent.task.reviewer` | `reviewing` | LLM code review with verdict + feedback |

#### Behavior

1. **Receives trigger**: Consumes `TaskExecutionTrigger` from `workflow.trigger.task-execution-loop`.
2. **Tester stage**: Dispatches tester agent. Fails fast on tester rejection.
3. **Builder stage**: Dispatches builder agent with tester output and task context.
4. **Structural validation**: Publishes to `workflow.async.structural-validator`. On failure, routes
   back to builder if budget remains; escalates on budget exhaustion.
5. **Code review**: Dispatches reviewer agent. On rejection, routes to builder (implementation
   issues) or tester (test issues) based on `error_category` signal. Non-fixable categories
   (`misscoped`, `architectural`, `too_big`) always escalate.
6. **Completion**: Publishes entity triple `workflow.phase = approved` on success. Terminal
   transitions (`completed`, `escalated`, `failed`) are driven by JSON rule processor reacting to
   phase triples.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.trigger.task-execution-loop` | JetStream (WORKFLOW) | Input | Task execution triggers |
| `agent.complete.>` | JetStream (AGENT) | Input | Agentic loop completion events |
| `agent.task.testing` | JetStream | Output | Tester agent dispatch |
| `agent.task.building` | JetStream | Output | Builder agent dispatch |
| `agent.task.reviewer` | JetStream | Output | Reviewer agent dispatch |
| `workflow.async.structural-validator` | JetStream (WORKFLOW) | Output | Structural validation requests |
| `graph.mutation.triple.add` | Core NATS | Output | Entity state triples |

---

### structural-validator

**Purpose**: Deterministic checklist validation using shell commands from `.semspec/checklist.json`.
Runs as part of the TDD pipeline between the builder and code reviewer stages.

**Location**: `processor/structural-validator/`

#### Configuration

```json
{
  "stream_name": "WORKFLOW",
  "consumer_name": "structural-validator",
  "checklist_path": ".semspec/checklist.json",
  "default_timeout": "120s"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOW` | JetStream stream for consuming validation triggers |
| `consumer_name` | string | `structural-validator` | Durable consumer name |
| `repo_path` | string | `SEMSPEC_REPO_PATH` or cwd | Repository root for running checks |
| `checklist_path` | string | `.semspec/checklist.json` | Path to checklist relative to repo root |
| `default_timeout` | string | `120s` | Fallback command timeout when a check has no timeout set |
| `ports` | PortConfig | (see defaults) | Input/output port definitions |

#### Behavior

1. **Consumes trigger**: Receives a validation request from `workflow.async.structural-validator`.
2. **Loads checklist**: Reads `.semspec/checklist.json` from the repo root.
3. **Filters checks**: Selects checks whose `trigger` list matches the current pipeline stage.
4. **Runs commands**: Executes each check's shell command in the repo root, respecting per-check
   and default timeouts.
5. **Publishes result**: Sends pass/fail verdict with per-check output to
   `workflow.result.structural-validator.<id>`.

#### Checklist Format

```json
{
  "version": "1.0.0",
  "checks": [
    {
      "id": "go-build",
      "name": "Build passes",
      "command": "go build ./...",
      "working_dir": ".",
      "timeout": "60s",
      "trigger": ["build", "validate"]
    }
  ]
}
```

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.async.structural-validator` | JetStream (WORKFLOW) | Input | Validation triggers |
| `workflow.result.structural-validator.>` | Core NATS | Output | Validation results |

---

### change-proposal-handler

**Purpose**: Processes the ChangeProposal cascade lifecycle. When a proposal is accepted, runs the
dirty cascade (graph traversal to mark affected requirements/scenarios as dirty), publishes
cancellation signals to running scenario loops, and emits the accepted event.

**Location**: `processor/change-proposal-handler/`

#### Configuration

```json
{
  "stream_name": "WORKFLOW",
  "consumer_name": "change-proposal-handler",
  "trigger_subject": "workflow.trigger.change-proposal-cascade",
  "accepted_subject": "workflow.events.change-proposal.accepted",
  "timeout_seconds": 120
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `WORKFLOW` | JetStream stream for cascade trigger messages |
| `consumer_name` | string | `change-proposal-handler` | Durable consumer name |
| `trigger_subject` | string | `workflow.trigger.change-proposal-cascade` | Subject for cascade requests |
| `accepted_subject` | string | `workflow.events.change-proposal.accepted` | Subject for accepted events after cascade |
| `timeout_seconds` | int | `120` | Cascade timeout in seconds (10–600) |
| `ports` | PortConfig | (see defaults) | Input/output port definitions |

#### Behavior

1. **Receives cascade request**: Consumes `ChangeProposalCascadeRequest` from
   `workflow.trigger.change-proposal-cascade` after a proposal is accepted via the API.
2. **Graph traversal**: Queries the graph to find all Requirements and Scenarios affected by the
   proposal's `affected_requirement_ids`.
3. **Dirty marking**: Marks affected entities with `workflow.dirty = true` triples.
4. **Cancellation signals**: Publishes `agent.signal.cancel.<loopID>` for any scenario execution
   loops that are currently running and cover affected scenarios.
5. **Accepted event**: Publishes `workflow.events.change-proposal.accepted` with a cascade summary
   (affected count, cancelled loops).

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.trigger.change-proposal-cascade` | JetStream (WORKFLOW) | Input | Cascade requests on proposal acceptance |
| `workflow.events.change-proposal.accepted` | Core NATS | Output | Accepted event with cascade summary |
| `agent.signal.cancel.*` | Core NATS | Output | Cancellation signals to running scenario loops |

---

## Support

### question-router

**Purpose**: Routes questions from agents to the appropriate answerer (LLM agent or human) based
on topic patterns configured in `configs/answerers.yaml`. Subscribes to `question.ask.>` on the
AGENT stream. Questions may arrive from any pipeline stage — planning or execution. The router
matches the question's topic field against configured patterns and dispatches to the registered
answerer. It also writes the question entity to the graph as the source of truth before routing.

**Location**: `processor/question-router/`

#### Configuration

```json
{
  "stream_name": "AGENT",
  "consumer_name": "question-router",
  "subject": "question.ask.>"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `AGENT` | JetStream stream name for question events |
| `consumer_name` | string | `question-router` | Durable consumer name |
| `subject` | string | `question.ask.>` | Subject pattern for incoming questions |

#### Answerer Registry

The router loads `configs/answerers.yaml` (or `answerers.json`) at startup. Each entry maps a
topic pattern to a named answerer. The `answerer.Router` selects the first matching rule and
dispatches accordingly.

#### Behavior

1. **Consumes** `question.ask.<id>` events from the AGENT stream.
2. **Writes graph triples**: records `workflow.question.text`, `workflow.question.context`,
   `workflow.question.status`, and `workflow.question.topic` as entity triples (source of truth).
3. **Routes**: calls `answerer.Router.RouteQuestion()` with the question's topic; the router
   matches against the loaded registry and dispatches to the appropriate answerer.
4. **Tracks metrics**: counts `questions_routed` and `routing_failed` for health reporting.

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `question.ask.>` | JetStream (AGENT) | Input | Agent question events |
| `graph.mutation.triple.add` | Core NATS | Output | Question entity triples |

---

### workflow-validator

**Purpose**: Request/reply service for validating workflow documents against their type requirements.
Ensures plans and tasks meet content requirements before workflow progression.

**Location**: `processor/workflow-validator/`

#### Configuration

```json
{
  "base_dir": ".",
  "timeout_secs": 30
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_dir` | string | `SEMSPEC_REPO_PATH` or cwd | Base directory for document paths |
| `timeout_secs` | int | `30` | Request timeout in seconds |

#### Request Format

```json
{
  "slug": "add-auth-refresh",
  "document": "plan",
  "content": "...",
  "path": ".semspec/plans/add-auth-refresh/plan.json"
}
```

Either `content` or `path` must be provided.

#### Response Format

```json
{
  "valid": true,
  "document": "plan",
  "errors": [],
  "warnings": ["Consider adding acceptance criteria"]
}
```

#### Behavior

1. **Receives Request**: Via NATS request/reply on `workflow.validate.*`
2. **Resolves Content**: From `content` field or reads from `path`
3. **Validates Structure**: Checks document against type-specific requirements
4. **Returns Result**: Synchronous response with validation status

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `workflow.validate.*` | Core NATS | Input | Validation requests (wildcard for document type) |
| `workflow.validation.events` | Core NATS | Output | Optional validation event notifications |

#### Security

- **Path validation**: Document paths validated to stay within `base_dir`
- **Path traversal protection**: Blocks attempts to read outside the repository

---

### workflow-documents

**Purpose**: Output component that subscribes to workflow document messages and writes them as files
to the `.semspec/plans/{slug}/` directory.

**Location**: `output/workflow-documents/`

#### Configuration

```json
{
  "base_dir": ".",
  "ports": {
    "inputs": [{
      "name": "documents_in",
      "type": "jetstream",
      "subject": "output.workflow.documents",
      "stream_name": "WORKFLOWS"
    }],
    "outputs": [{
      "name": "documents_written",
      "type": "nats",
      "subject": "workflow.documents.written"
    }]
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_dir` | string | `SEMSPEC_REPO_PATH` or cwd | Base directory for document output |
| `ports` | PortConfig | (see above) | Input/output port configuration |

#### Behavior

1. **Consumes Messages**: From `output.workflow.documents` JetStream subject
2. **Transforms Content**: Converts JSON content to the target format based on document type
3. **Writes File**: Creates `.semspec/plans/{slug}/{document}.json`
4. **Publishes Notification**: Sends `workflow.documents.written` event

#### Document Types

| Type | Output File | Content |
|------|-------------|---------|
| `plan` | `plan.json` | Goal/context/scope |
| `tasks` | `tasks.json` | BDD task list with acceptance criteria |

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `output.workflow.documents` | JetStream (WORKFLOWS) | Input | Document output messages |
| `workflow.documents.written` | Core NATS | Output | File written notifications |

#### File Structure

```
.semspec/
└── plans/
    └── {slug}/
        ├── plan.json
        ├── metadata.json
        └── tasks.json
```

---

### question-answerer

**Purpose**: Answers questions using LLM agents based on topic and capability routing. Part of the
knowledge gap resolution protocol.

**Location**: `processor/question-answerer/`

#### Configuration

```json
{
  "stream_name": "AGENT",
  "consumer_name": "question-answerer",
  "task_subject": "agent.task.question-answerer",
  "default_capability": "reviewing"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `stream_name` | string | `AGENT` | JetStream stream name |
| `consumer_name` | string | `question-answerer` | Durable consumer name |
| `task_subject` | string | `agent.task.question-answerer` | Subject to consume tasks from |
| `default_capability` | string | `reviewing` | Default model capability |

#### Behavior

1. **Consumes Tasks**: Listens on `agent.task.question-answerer` for question-answering tasks
2. **Resolves Model**: Uses capability-based model selection (planning, reviewing, coding, etc.)
3. **Generates Answer**: Calls LLM with question context and topic
4. **Publishes Answer**: Sends answer to `question.answer.<id>`
5. **Updates Store**: Marks question as answered in the QUESTIONS KV bucket

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `agent.task.question-answerer` | JetStream (AGENT) | Input | Question-answering tasks from router |
| `question.answer.<id>` | JetStream | Output | Answer payloads |

#### Dependencies

- `workflow/answerer/` — Task types and routing
- `workflow/question.go` — Question store
- `model/` — Capability-based model selection

---

### question-timeout

**Purpose**: Monitors question SLAs and triggers escalation when questions are not answered in time.
Disabled by default — enable by adding an instance to `configs/semspec.json`.

**Location**: `processor/question-timeout/`

#### Configuration

```json
{
  "check_interval": "1m",
  "default_sla": "24h",
  "answerer_config_path": "configs/answerers.yaml"
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `check_interval` | duration | `1m` | How often to check for timed-out questions |
| `default_sla` | duration | `24h` | Default SLA when not specified in route config |
| `answerer_config_path` | string | (auto-detected) | Path to `answerers.yaml` |

#### Behavior

1. **Periodic Check**: Runs on `check_interval` to find overdue questions
2. **SLA Evaluation**: Compares question age against the route SLA (or default)
3. **Timeout Events**: Publishes `question.timeout.<id>` when SLA is exceeded
4. **Escalation**: If `escalate_to` is configured, reassigns question and publishes
   `question.escalate.<id>`
5. **Notifications**: Can trigger notifications via configured channels

#### NATS Subjects

| Subject | Transport | Direction | Description |
|---------|-----------|-----------|-------------|
| `question.timeout.<id>` | JetStream | Output | Timeout events |
| `question.escalate.<id>` | JetStream | Output | Escalation events |

#### Escalation Flow

When a question's SLA is exceeded:

1. Timeout event published
2. Question reassigned to `escalate_to` answerer
3. Escalation event published
4. Notifications sent (if configured)

#### Dependencies

- `workflow/answerer/registry.go` — Route configuration with SLAs
- `workflow/question.go` — Question store

---

## ChangeProposal Lifecycle

The ChangeProposal lifecycle uses a combination of the `plan-manager` component (HTTP CRUD and
submit/accept/reject actions), the `change-proposal-handler` component (cascade execution), and
JSON rule processing (status transitions).

### Implementation Files

| File | Purpose |
|------|---------|
| `processor/plan-manager/http_change_proposal.go` | HTTP CRUD, submit, accept, reject handlers |
| `processor/change-proposal-handler/` | Cascade execution after acceptance |
| `workflow/reactive/change_proposal_actions.go` | Cascade logic: graph traversal, dirty marking |

### Lifecycle Flow

```
POST .../change-proposals/{id}/submit
  → status: submitted

POST .../change-proposals/{id}/accept
  → publishes workflow.trigger.change-proposal-cascade
  → change-proposal-handler runs dirty cascade
  → publishes workflow.events.change-proposal.accepted

POST .../change-proposals/{id}/reject
  → status: rejected
```

See [Workflow System: ChangeProposal Lifecycle](05-workflow-system.md#changeproposal-lifecycle-adr-024)
for the full lifecycle description including cascade logic.

---

## Creating New Components

### Directory Structure

```
processor/<name>/
├── component.go   # Discoverable + lifecycle implementation
├── config.go      # Configuration schema
└── factory.go     # Component registration
```

### Required Interface

```go
// Must implement component.Discoverable
type Component struct { ... }

func (c *Component) Meta() component.Metadata
func (c *Component) InputPorts() []component.Port
func (c *Component) OutputPorts() []component.Port
func (c *Component) ConfigSchema() component.ConfigSchema
func (c *Component) Health() component.HealthStatus
func (c *Component) DataFlow() component.FlowMetrics

// Optional lifecycle methods
func (c *Component) Initialize() error
func (c *Component) Start(ctx context.Context) error
func (c *Component) Stop(timeout time.Duration) error
```

### Registration

```go
// factory.go
func Register(registry RegistryInterface) error {
    return registry.RegisterWithConfig(component.RegistrationConfig{
        Name:        "my-component",
        Factory:     NewComponent,
        Schema:      mySchema,
        Type:        "processor",
        Protocol:    "custom",
        Domain:      "semantic",
        Description: "My custom component",
        Version:     "0.1.0",
    })
}
```

### Wiring

1. Import in `cmd/semspec/main.go`
2. Call `mycomponent.Register(registry)`
3. Add instance config to `configs/semspec.json`

As of this writing semspec registers **16 components** in `cmd/semspec/main.go`. When you add a
new component, increment this count in the binary's startup log and update CLAUDE.md accordingly.
