# Workflow System

This document describes the LLM-driven workflow system in semspec, including capability-based model
selection, the plan-and-execute adversarial loop, and specialized processing components.

## Overview

Semspec uses two complementary patterns for LLM-driven processing:

1. **Components** - Single-shot processors that call LLM, parse structured output, and persist to files
2. **Workflows** - Multi-step orchestration for coordinating multiple agents

See [Architecture: Components vs Workflows](03-architecture.md#components-vs-workflows) for when to
use each pattern.

## Current Workflow: Plan and Execute (ADR-003)

The `plan-and-execute` workflow implements an adversarial developer/reviewer loop:

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────────┐
│  developer  │────▶│  reviewer   │────▶│  verdict_check      │
│  (implement)│     │  (evaluate) │     │                     │
└─────────────┘     └─────────────┘     └──────────┬──────────┘
                                                   │
                    ┌──────────────────────────────┼──────────────────┐
                    │                              │                  │
                    ▼                              ▼                  ▼
            ┌───────────────┐            ┌───────────────┐    ┌───────────────┐
            │   approved    │            │   fixable     │    │  misscoped/   │
            │   → complete  │            │   → retry     │    │  too_big      │
            └───────────────┘            └───────────────┘    │  → escalate   │
                                                              └───────────────┘
```

**Workflow definition**: `configs/workflows/plan-and-execute.json`

**Key features:**

- Adversarial loop: developer implements, reviewer evaluates
- Conditional routing based on verdict (approved, fixable, misscoped, too_big)
- Retry with feedback for fixable issues (max 3 iterations)
- Escalation to user for architectural or scoping issues
- Built-in failure handling via `on_fail` steps

## Specialized Processing Components

For single-shot LLM operations that require structured output parsing, semspec uses dedicated
components instead of workflow steps. This is because agentic-loop returns raw text and cannot
parse structured JSON responses.

| Component | Trigger | Processing | Output |
|-----------|---------|------------|--------|
| `plan-coordinator` | `/plan <title>` | Multi-planner orchestration → Goal/Context/Scope | `plan.json` |
| `planner` | (fallback path) | Single LLM → Goal/Context/Scope | `plan.json` |
| `plan-reviewer` | `/approve <slug>` | SOP validation → Verdict | Review result |
| `task-generator` | After approval | LLM pipeline: Requirements → Scenarios → Tasks | `tasks.json` |
| `task-dispatcher` | `/execute <slug>` | Dependency-aware dispatch | Agent tasks |
| `context-builder` | (shared service) | Graph + filesystem → Context | Token-budgeted context |

Each component:

1. Subscribes to `workflow.trigger.<name>` subject
2. Calls LLM with domain-specific prompts
3. Parses JSON from markdown-wrapped responses
4. Validates required fields
5. Saves to filesystem via `workflow.Manager`
6. Publishes completion to `workflow.result.<name>.<slug>`

See [Components](04-components.md) for detailed documentation of each component.

## Capability-Based Model Selection

Instead of specifying models directly, workflow commands use semantic capabilities that map to
appropriate models.

### Capabilities

| Capability | Description | Default Model |
|------------|-------------|---------------|
| `planning` | High-level reasoning, architecture decisions | claude-opus |
| `writing` | Documentation, proposals, specifications | claude-sonnet |
| `coding` | Code generation, implementation | claude-sonnet |
| `reviewing` | Code review, quality analysis | claude-sonnet |
| `fast` | Quick responses, simple tasks | claude-haiku |

### Role-to-Capability Mapping

| Role | Default Capability |
|------|-------------------|
| planner | planning |
| developer | coding |
| reviewer | reviewing |
| task-generator | planning |

### Usage

```bash
# Default (uses role's default capability)
/plan Add user authentication
# → planning capability → claude-opus

# Direct model override (power user)
/plan Add auth --model qwen
# → bypasses registry, uses qwen directly
```

### Configuration

Configure the model registry in `configs/semspec.json`:

```json
{
  "model_registry": {
    "capabilities": {
      "planning": {
        "description": "High-level reasoning, architecture decisions",
        "preferred": ["claude-opus", "claude-sonnet"],
        "fallback": ["qwen", "llama3.2"]
      },
      "writing": {
        "description": "Documentation, proposals, specifications",
        "preferred": ["claude-sonnet"],
        "fallback": ["claude-haiku", "qwen"]
      }
    },
    "endpoints": {
      "claude-sonnet": {
        "provider": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 200000
      },
      "qwen": {
        "provider": "ollama",
        "url": "http://localhost:11434/v1",
        "model": "qwen2.5-coder:14b",
        "max_tokens": 128000
      }
    },
    "defaults": {
      "model": "qwen"
    }
  }
}
```

### Fallback Chain

When the primary model fails, the system tries fallback models in order:

```
claude-opus (unavailable) → claude-sonnet → qwen → llama3.2
```

## Planning Workflow

The planning workflow uses specialized components for LLM-assisted content generation.

### How It Works

```bash
/plan Add auth
# Creates plan stub, triggers plan-coordinator component
# LLM generates Goal/Context/Scope and saves to plan.json

/plan Add auth -m
# Creates plan stub only, no LLM processing
# User manually edits plan.json
```

### Plan Status Flow

Plans progress through a defined sequence of statuses. ADR-024 inserted the requirements and
scenario generation steps between approval and phase generation:

```
created → drafted → reviewed → approved
  → requirements_generated → scenarios_generated
  → phases_generated → phases_approved
  → tasks_generated → tasks_approved
  → implementing → complete
```

The two new statuses (`requirements_generated`, `scenarios_generated`) represent distinct LLM
pipeline steps. Each concern uses a separate, focused prompt so that smaller models produce
higher-quality output than a single combined call would yield.

### Planning Pipeline (ADR-024)

After a plan is approved, the `task-generator` component runs a multi-step pipeline before
producing tasks:

```
Plan approved
  → Generate Requirements   (plan-scoped intent statements)
    → Generate Scenarios     (Given/When/Then per requirement)
      → Generate Phases      (scheduling containers, unchanged)
        → Generate Tasks     (implementation work, linked to Scenarios)
          → Assign Tasks to Phases
```

Each step is a separate LLM call with a focused prompt. The pipeline is configurable via the
`pipeline_mode` field on `task-generator`; the default is `pipeline`. A single-shot mode is
available for latency-sensitive environments.

**Why separate steps?** Requirements describe *intent*. Scenarios describe *observable behavior*.
Tasks describe *implementation work*. Phases describe *scheduling*. Separating concerns produces
higher-quality output and makes each artifact independently queryable in the graph.

### Task Statuses (ADR-024)

Two new task statuses were added:

| Status | Set By | Meaning |
|--------|--------|---------|
| `dirty` | ChangeProposal acceptance cascade | One or more linked Scenarios were mutated; task needs re-evaluation |
| `blocked` | Dependency resolution | An explicit upstream dependency has not completed |

`dirty` tasks are not failed — they are flagged for re-evaluation after a ChangeProposal changes
the behavioral contracts (Scenarios) the task was written to satisfy.

## Document Validation

Generated documents are validated before proceeding to the next step.

### Document Type Requirements

#### Plan (`plan.json`)

| Field | Required | Description |
|-------|----------|-------------|
| `goal` | yes | What to achieve |
| `context` | yes | Relevant background |
| `scope` | yes | Boundaries of the change |

#### Tasks (`tasks.json`)

| Field | Required | Description |
|-------|----------|-------------|
| `title` | yes | Task title |
| `description` | yes | Implementation description |
| `scenarioIDs` | yes | IDs of Scenarios this task satisfies (ADR-024; replaces embedded acceptance criteria) |

### Validation Warnings

The validator also checks for:

- Placeholder text (TODO, FIXME, TBD, etc.)
- Minimum document length
- Empty sections

### Auto-Retry on Validation Failure

When validation fails, the system automatically retries with feedback:

```
Loop completes → Validate document
    ↓
Valid? → Clear retry state → Continue to next step
    ↓
Invalid? → Check retry count
    ↓
Can retry? → Wait for backoff → Retry with feedback
    ↓
Max retries exceeded? → Notify user of failure
```

### Retry Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `max_retries` | 3 | Maximum retry attempts |
| `backoff_base_seconds` | 5 | Initial backoff duration |
| `backoff_multiplier` | 2.0 | Exponential multiplier |

Backoff progression: 5s → 10s → 20s

### Validation Feedback

When retrying, the LLM receives detailed feedback:

```markdown
## Validation Failed

The generated document is missing required sections or content.

### Missing or Incomplete Sections

- Why: Section too short (min 50 chars, got 10)
- What Changes: What Changes section listing modifications

### Warnings

- Contains placeholder text: TODO

Please regenerate the document addressing these issues.

Attempt 2 of 3. Please ensure all required sections are present
and meet minimum content requirements.
```

## Component Architecture

### Planning Message Flow

```
User Command (/plan "Add auth")
    |
    v
agentic-dispatch (receives message, creates plan stub)
    |
    v
workflow.trigger.plan-coordinator
    |
    v
[plan-coordinator]
    |-- Requests context from context-builder
    |-- LLM: determine focus areas (1-3)
    |-- Runs planners in parallel (goroutines, NOT planner component)
    |-- LLM: synthesize if multiple results
    |-- Saves plan.json
    |-- Publishes: workflow.result.plan-coordinator.<slug>
    |
    v
User notified, can review plan
    |
    v
User Command (/approve <slug>)
    |
    v
workflow.trigger.plan-reviewer
    |
    v
[plan-reviewer]
    |-- Queries graph for SOPs matching plan scope
    |-- LLM: validates plan against each SOP requirement
    |-- Returns: "approved" or "needs_changes"
    |
    v (if needs_changes, retry up to 3 times)
    v (if approved)
    |
workflow.trigger.task-generator
    |
    v
[task-generator]  -- ADR-024 pipeline --
    |-- Requests context from context-builder
    |-- LLM: generates Requirements from plan Goal/Context/Scope
    |       Publishes requirement.created events; plan status → requirements_generated
    |-- LLM: generates Scenarios (Given/When/Then) per Requirement
    |       Publishes scenario.created events; plan status → scenarios_generated
    |-- LLM: generates Phases (scheduling containers, unchanged)
    |-- LLM: generates Tasks linked to Scenarios (ScenarioIDs, not embedded criteria)
    |-- Saves tasks.json
    |-- Publishes: workflow.result.task-generator.<slug>
```

### Execution Message Flow (Plan-and-Execute)

```
User Command (/execute <slug>)
    ↓
ExecuteCommand.Execute()
    └── Publishes to workflow.trigger.plan-and-execute
    ↓
[SEMSTREAMS] workflow-processor
    ↓
Executes plan-and-execute.json steps
    ├── developer → agentic-loop → implementation
    ├── reviewer → agentic-loop → evaluation
    ├── verdict_check → conditional routing
    ├── retry_developer (if fixable)
    └── escalate (if misscoped/too_big)
    ↓
Task completion or escalation to user
```

### Key Components

| Component | Purpose |
|-----------|---------|
| `processor/plan-coordinator/` | Multi-planner orchestration |
| `processor/planner/` | Single-planner fallback path |
| `processor/plan-reviewer/` | SOP-aware plan validation |
| `processor/context-builder/` | Token-budgeted context assembly |
| `processor/source-ingester/` | Document/SOP ingestion |
| `processor/task-generator/` | BDD task generation |
| `processor/task-dispatcher/` | Dependency-aware task execution |
| `workflow/` | Workflow types, prompts, validation |
| `model/` | Capability-based model selection |

**Semstreams components used:**

- `workflow-processor` — Multi-step workflow execution (plan-and-execute)
- `agentic-loop` — Generic LLM execution with tool use

### NATS Subjects

| Subject | Purpose |
|---------|---------|
| `workflow.trigger.plan-coordinator` | Plan orchestration trigger |
| `workflow.trigger.planner` | Simple planner trigger |
| `workflow.trigger.plan-reviewer` | Plan review trigger |
| `workflow.trigger.task-generator` | Task generation trigger |
| `workflow.trigger.task-dispatcher` | Task dispatch trigger |
| `workflow.trigger.change-proposal-loop` | ChangeProposal OODA loop trigger |
| `workflow.result.<component>.<slug>` | Component completion |
| `context.build.>` | Context build requests |
| `context.built.<request_id>` | Context build responses |
| `agent.task.development` | Agent task dispatch |
| `requirement.created` | New requirement published |
| `requirement.updated` | Requirement mutated by ChangeProposal |
| `scenario.created` | New scenario published |
| `scenario.status.updated` | Scenario status changed |
| `task.dirty` | Dirty cascade: task IDs affected by ChangeProposal |
| `change_proposal.created` | New ChangeProposal submitted |
| `change_proposal.accepted` | Proposal accepted; cascade complete |
| `change_proposal.rejected` | Proposal rejected; no graph mutations |
| `scenario.orchestrate.*` | Scenario orchestration trigger (per plan slug) |
| `workflow.trigger.scenario-execution-loop` | Per-Scenario execution trigger |
| `workflow.trigger.dag-execution` | DAG execution trigger |
| `workflow.async.scenario-decomposer` | Decompose request dispatched to agentic loop |
| `scenario.decomposed.*` | Decomposition result (DAG) from agentic loop |
| `dag.node.complete.*` | Individual DAG node completed |
| `dag.node.failed.*` | Individual DAG node failed |
| `dag.execution.complete.*` | Entire DAG completed successfully |
| `dag.execution.failed.*` | DAG failed (at least one node failed) |
| `scenario.complete.*` | Scenario execution completed |
| `scenario.failed.*` | Scenario execution failed |
| `agent.signal.cancel.*` | Cancellation signal to a running loop |

## Reactive Workflows (ADR-025)

ADR-025 introduces two reactive workflows for runtime scenario decomposition and execution. Both
are built with the semstreams reactive engine and follow the OODA-loop pattern.

### Plan Status with Reactive Mode

When `task-generator` runs with `reactive_mode=true`, the plan status flow takes a shortcut after
Scenario generation:

```
created → drafted → reviewed → approved
  → requirements_generated → scenarios_generated
  → ready_for_execution      ← reactive mode shortcut (no tasks.json)
    → implementing → complete
```

The `ready_for_execution` status signals the `scenario-orchestrator` to begin dispatching
`scenario-execution-loop` workflows for each pending Scenario.

### scenario-execution-loop

**Workflow ID**: `scenario-execution-loop`

**Purpose**: Drives the full lifecycle of executing a single Scenario. Decomposes the Scenario
into a `TaskDAG` via the `decompose_task` tool (LLM call), then triggers `dag-execution-loop`.

**Phases**: `decomposing` → `decomposed` → `executing` → `complete` | `failed`

**Rules** (7 total):

| Rule | Trigger | Action |
|------|---------|--------|
| `accept-trigger` | `workflow.trigger.scenario-execution-loop` | Initialize state, set phase → `decomposing` |
| `dispatch-decompose` | KV watch (phase = `decomposing`) | Publish `ScenarioDecomposeRequest` to `workflow.async.scenario-decomposer`; set phase → `decomposed` |
| `handle-decomposed` | `scenario.decomposed.*` | Validate DAG; publish `DAGExecutionTriggerPayload` to `workflow.trigger.dag-execution`; set phase → `executing` |
| `handle-dag-complete` | `dag.execution.complete.*` | Store completed nodes; set phase → `complete` |
| `handle-dag-failed` | `dag.execution.failed.*` | Store failed nodes; set phase → `failed` |
| `handle-complete` | KV watch (phase = `complete`) | Publish `ScenarioCompletePayload` to `scenario.complete.<scenarioID>`; mark done |
| `handle-failed` | KV watch (phase = `failed`) | Publish `ScenarioFailedPayload` to `scenario.failed.<scenarioID>`; mark done |

**State key pattern**: `scenario-execution.<scenarioID>`

**Timeout**: 90 minutes

```mermaid
flowchart LR
    A[accept-trigger] --> B[dispatch-decompose]
    B --> C[handle-decomposed]
    C -->|DAG trigger| D[dag-execution-loop]
    D -->|complete| E[handle-dag-complete]
    D -->|failed| F[handle-dag-failed]
    E --> G[handle-complete\nscenario.complete.*]
    F --> H[handle-failed\nscenario.failed.*]
```

### dag-execution-loop

**Workflow ID**: `dag-execution-loop`

**Purpose**: Executes a `TaskDAG` reactively: dispatches ready nodes (pending + all dependencies
completed), tracks per-node completion, and transitions to terminal state when done.

**Phases**: `executing` → `complete` | `failed`

**Rules** (6 total):

| Rule | Trigger | Action |
|------|---------|--------|
| `accept-trigger` | `workflow.trigger.dag-execution` | Initialize all node states to `pending`; set phase → `executing` |
| `dispatch-ready-nodes` | KV watch (phase = `executing`) | Find nodes with all deps completed; mark them `running`; detect terminal state |
| `handle-node-complete` | `dag.node.complete.*` | Mark node `completed`; KV write re-triggers `dispatch-ready-nodes` |
| `handle-node-failed` | `dag.node.failed.*` | Mark node `failed`; KV write re-triggers `dispatch-ready-nodes` |
| `handle-complete` | KV watch (phase = `complete`) | Publish `DAGExecutionCompletePayload` to `dag.execution.complete.<executionID>`; mark done |
| `handle-failed` | KV watch (phase = `failed`) | Publish `DAGExecutionFailedPayload` to `dag.execution.failed.<executionID>`; mark done |

**State key pattern**: `dag-execution.<executionID>`

**Node states**: `pending` → `running` → `completed` | `failed`

**Terminal conditions** (evaluated by `dispatch-ready-nodes`):

- All nodes `completed` → phase → `complete`
- No ready nodes, no running nodes, at least one `failed` → phase → `failed`

**Timeout**: 60 minutes

```mermaid
flowchart LR
    A[accept-trigger\nall nodes: pending] --> B[dispatch-ready-nodes]
    B -->|ready nodes found| C[mark running\nKV write]
    C --> B
    B -->|all complete| D[handle-complete\ndag.execution.complete.*]
    B -->|nodes failed + idle| E[handle-failed\ndag.execution.failed.*]
    F[handle-node-complete\ndag.node.complete.*] --> B
    G[handle-node-failed\ndag.node.failed.*] --> B
```

### ChangeProposal Cancellation in Reactive Mode

When a ChangeProposal is accepted while Scenarios are executing reactively, the cascade logic
publishes `CancellationSignal` messages to stop affected loops before re-queuing:

```
ChangeProposal accepted
  │
  ├── dirty cascade: mark affected Tasks and Scenarios dirty
  ├── publish CancellationSignal to agent.signal.cancel.<loopID>
  │     for each running scenario-execution-loop or dag-execution-loop
  └── scenario-orchestrator re-triggered for the plan to pick up dirty Scenarios
```

The `CancellationSignal` is published on Core NATS (ephemeral) to the specific loop's cancel
subject. Loops that observe the signal transition to their `failed` terminal state with the
cancellation reason included in the failure event.

### Implementation Files (Reactive Execution)

| File | Purpose |
|------|---------|
| `workflow/reactive/cancellation.go` | `CancellationSignal` payload type |
| `tools/bash/executor.go` | `bash` tool: universal shell (files, git, builds, tests) |
| `tools/terminal/executor.go` | `submit_work`, `ask_question` — terminal tools (StopLoop=true) |
| `tools/decompose/executor.go` | `decompose_task` tool: validates LLM-provided TaskDAG |
| `tools/spawn/executor.go` | `spawn_agent` tool: spawns and awaits a child loop |
| `tools/review/executor.go` | `review_scenario` tool: scenario review verdict |
| `agentgraph/graph.go` | Graph helper: records spawn, status, tree queries |
| `processor/scenario-orchestrator/` | Entry point component for reactive execution |
| `processor/scenario-executor/` | Decomposes scenarios into DAGs, drives serial node execution |
| `processor/execution-orchestrator/` | TDD pipeline per node: tester → builder → validator → reviewer |

## ChangeProposal Lifecycle (ADR-024)

A ChangeProposal is a first-class graph node that represents a mid-stream change to one or more
Requirements. It follows an OODA (Observe-Orient-Decide-Act) reactive workflow, consistent with
the ADR-005 reactive engine pattern.

### When a ChangeProposal Is Created

Three sources can submit a proposal (all publish to `workflow.trigger.change-proposal-loop`):

1. **User via UI** — manual proposal from the Requirement panel (Phase 5 scope)
2. **Agent during execution** — developer detects a misscoped requirement and proposes a change
   instead of escalating (future work)
3. **Reviewer during review** — code reviewer identifies a behavioral gap that warrants a new
   Requirement (future work)

### OODA Loop

```
Observe:  Proposal created → workflow.trigger.change-proposal-loop published
Orient:   Reviewer evaluates proposal against current graph state (LLM or human gate)
Decide:   Accept or reject
Act:      If accepted → cascade dirty status; if rejected → archive, no mutations
```

### Reactive Rules

```
KV key pattern:   change-proposal.*
Trigger subject:  workflow.trigger.change-proposal-loop

accept-trigger          → populate state from proposal payload
dispatch-review         → workflow.async.change-proposal-reviewer
review-completed        → evaluate verdict
handle-accepted         → execute cascade (graph traversal + dirty marking)
handle-rejected         → archive proposal, no graph mutations
handle-escalation       → user.signal.escalate
handle-error            → user.signal.error
```

### Cascade Logic (handle-accepted)

When a proposal is accepted, the reactive engine performs a graph traversal and marks affected
work as `dirty`:

1. For each Requirement in `AffectedReqIDs`:
   - Publish `requirement.updated` event
   - Traverse `HAS_SCENARIO` edges to find affected Scenarios
   - For each Scenario: traverse `SATISFIED_BY` edges to find affected Tasks
1. For each affected Task: set status to `dirty`, persist updated task
1. Publish `task.dirty` with all affected task IDs (batched single event)
1. Publish `change_proposal.accepted`
1. Set proposal status to `archived`

`dirty` tasks are flagged for re-evaluation — they are not failed or cancelled. The developer
agent can inspect which Scenarios changed and revise its implementation accordingly.

### ChangeProposal Struct

```go
type ChangeProposal struct {
    ID             string               // "change-proposal.{plan_slug}.{sequence}"
    PlanID         string
    Title          string
    Rationale      string
    Status         ChangeProposalStatus // proposed, under_review, accepted, rejected, archived
    ProposedBy     string               // agent role or "user"
    AffectedReqIDs []string             // one proposal can span multiple Requirements
    CreatedAt      time.Time
    ReviewedAt     *time.Time
    DecidedAt      *time.Time
}
```

### Graph Edges

| Edge | From | To | Meaning |
|------|------|----|---------|
| `BELONGS_TO` | ChangeProposal | Plan | Scoped to plan |
| `MUTATES` | ChangeProposal | Requirement | Requirements changed by this proposal |
| `ADDS_SCENARIO` | ChangeProposal | Scenario | Scenarios introduced |
| `REMOVES_SCENARIO` | ChangeProposal | Scenario | Scenarios removed |
| `INVALIDATES` | ChangeProposal | Task | Tasks dirtied on acceptance (computed) |

### Implementation Files

These files are created or modified as part of the ChangeProposal lifecycle implementation:

| File | Status | Purpose |
|------|--------|---------|
| `workflow/reactive/change_proposal.go` | New | Reactive workflow rules |
| `workflow/reactive/change_proposal_actions.go` | New | Cascade logic |
| `processor/workflow-api/http_change_proposal.go` | New | HTTP handlers |
| `configs/semspec.json` | Modified | `change-proposal-loop` configuration |

## Context Building

The context-builder is a shared service that assembles token-budgeted LLM context. Every component
that needs LLM context uses it.

### How Context is Requested

Components publish a `ContextBuildRequest` to `context.build.<type>` (on the AGENT stream). The
context-builder selects a strategy based on `type`:

| Type | Strategy | Priority Content |
|------|----------|-----------------|
| `planning` | PlanningStrategy | File tree, codebase summary, arch docs, specs, code patterns, SOPs |
| `plan-review` | PlanReviewStrategy | SOPs (all-or-nothing), plan content, file tree |
| `implementation` | ImplementationStrategy | Spec entity, target files, related patterns |
| `review` | ReviewStrategy | SOPs (all-or-nothing), changed files, tests, conventions |
| `exploration` | ExplorationStrategy | Codebase summary, matching entities, related docs |
| `question` | QuestionStrategy | Matching entities, source docs, codebase summary |

### Token Budget

Default: 32,000 tokens with 6,400 headroom. Each strategy allocates tokens by priority — high-priority
items get budget first, lower-priority items fill remaining space.

### Graph Readiness

On first request, the context-builder probes the graph pipeline with exponential backoff
(250ms → 2s, configurable budget). When the graph is not ready, strategies skip graph queries
cleanly instead of timing out.

## SOP Enforcement

SOPs (Standard Operating Procedures) are project rules enforced structurally during planning
and review.

### How SOPs Enter the System

1. Author SOPs as Markdown with YAML frontmatter in `.semspec/sources/docs/`
2. Source-ingester parses frontmatter and publishes to the knowledge graph
3. Context-builder retrieves matching SOPs when assembling context

### Enforcement Points

- **Planning**: SOPs included best-effort in planning context so the LLM generates SOP-aware plans
- **Plan Review**: SOPs required (all-or-nothing). Plan-reviewer validates each SOP requirement.
  Severity "error" blocks approval.
- **Code Review**: SOPs required (all-or-nothing). Pattern-matched against changed files.

See [SOP System](09-sop-system.md) for the complete SOP lifecycle documentation.

## Extending the System

### Adding a New Capability

1. Add to `model/capability.go`:

```go
const CapabilityCustom Capability = "custom"
```

2. Configure in `configs/semspec.json`:

```json
"custom": {
  "description": "Custom capability",
  "preferred": ["model-a"],
  "fallback": ["model-b"]
}
```

### Adding a New Processing Component

For single-shot LLM operations that need structured output parsing, create a new component
following the planner pattern:

1. Create component directory:

```
processor/<name>/
├── component.go   # Main processing logic
├── config.go      # Configuration
└── factory.go     # Registration
```

2. Implement the processing pattern:

```go
// 1. Subscribe to trigger subject
consumer, _ := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
    Durable:       "my-component",
    FilterSubject: "workflow.trigger.my-component",
})

// 2. Call LLM with domain-specific prompt
func (c *Component) generate(ctx context.Context, trigger *WorkflowTriggerPayload) (*MyContent, error) {
    // Build prompt, call LLM, parse JSON response
}

// 3. Save to filesystem
func (c *Component) save(ctx context.Context, content *MyContent) error {
    // Use workflow.Manager or direct file I/O
}

// 4. Publish completion
c.natsClient.Publish(ctx, "workflow.result.my-component."+slug, result)
```

3. Register in `cmd/semspec/main.go`:

```go
mycomponent.Register(registry)
```

4. Add config to `configs/semspec.json`

See `processor/planner/` as the canonical reference implementation.

### Adding a New Workflow Step

Edit `configs/workflows/plan-and-execute.json` to add new steps. See semstreams
workflow-processor documentation for the full schema.

For steps that need specialized output processing, have the workflow trigger a component:

```json
{
  "name": "generate_custom",
  "action": {
    "type": "publish",
    "subject": "workflow.trigger.my-component",
    "payload": {
      "slug": "${trigger.payload.slug}",
      "title": "${trigger.payload.title}"
    }
  },
  "on_success": "next_step",
  "on_fail": "error_handler"
}
```

## Troubleshooting

### Component Not Processing

1. Check component logs:

```bash
docker logs semspec 2>&1 | grep "plan-coordinator\|plan-reviewer\|task-generator"
```

2. Verify trigger was published:

```bash
curl http://localhost:8080/message-logger/entries?limit=50 \
  | jq '.[] | select(.subject | contains("workflow.trigger"))'
```

3. Check for processing errors:

```bash
curl http://localhost:8080/message-logger/entries?limit=50 \
  | jq '.[] | select(.type == "error")'
```

### Plan Not Updated

If `/plan` runs but `plan.json` does not have Goal/Context/Scope:

1. Check plan-coordinator component is registered:

```bash
docker logs semspec 2>&1 | grep "plan-coordinator started"
```

2. Check LLM response parsing:

```bash
# Look for JSON parsing errors
docker logs semspec 2>&1 | grep "parse plan"
```

3. Verify plan.json exists:

```bash
cat .semspec/plans/<slug>/plan.json
```

### Workflow Not Progressing

1. Check workflow-processor logs:

```bash
docker logs semspec 2>&1 | grep "workflow-processor"
```

2. Check for agent failures:

```bash
curl http://localhost:8080/message-logger/entries?limit=50 \
  | jq '.[] | select(.subject | contains("agent.failed"))'
```

### Model Selection Issues

1. Check registry configuration:

```bash
cat configs/semspec.json | jq '.model_registry'
```

2. Verify endpoint availability:

```bash
# For Ollama
curl http://localhost:11434/v1/models

# For Anthropic - check API key is set
echo $ANTHROPIC_API_KEY
```

### LLM Failures

When no LLM is configured or all models fail, verify:

1. API keys are set (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`)
2. Ollama is running if using local models
3. Model names match configuration in `configs/semspec.json`
4. Check component-specific error messages in logs
