# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Semspec is a semantic development agent built as a **semstreams extension**. It imports semstreams as a library, registers custom components, and runs them via the component lifecycle.

**Key differentiator**: Persistent knowledge graph eliminates context loss.

## Work Standards

**Report incidental bugs**: If you find a bug, failing test, dead code, or suspicious pattern while working on something else — always include it in your response summary under an "Other issues found" section. Don't silently move on. Include file, line, and observed behavior so someone can act on it.

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/01-how-it-works.md](docs/01-how-it-works.md) | How semspec works (start here) |
| [docs/02-getting-started.md](docs/02-getting-started.md) | Setup and first plan |
| [docs/03-architecture.md](docs/03-architecture.md) | System architecture, component registration, semstreams relationship |
| [docs/04-components.md](docs/04-components.md) | Component reference (16 components), schema tags, adding new components |
| [docs/05-workflow-system.md](docs/05-workflow-system.md) | Workflow system, plan coordination, validation |
| [docs/06-question-routing.md](docs/06-question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [docs/07-model-configuration.md](docs/07-model-configuration.md) | LLM model and capability configuration |
| [docs/08-observability.md](docs/08-observability.md) | Observability, trajectory tracking, token metrics |
| [docs/09-sop-system.md](docs/09-sop-system.md) | SOP authoring, ingestion, and enforcement |
| [docs/10-behavioral-controls.md](docs/10-behavioral-controls.md) | Behavioral controls for autonomous agents |
| [docs/11-execution-pipeline.md](docs/11-execution-pipeline.md) | Execution pipeline: NATS subjects, consumers, payload types |
| [docs/12-plan-api.md](docs/12-plan-api.md) | Plan API: requirements, scenarios, change proposals |
| [docs/13-sandbox-security.md](docs/13-sandbox-security.md) | Sandbox security model: boundaries, isolation, threat model |

## Component Architecture — Manager Pattern

Entity-owning components follow the same 3-layer pattern:

```
*-manager {
    cache        sync.Map              // hot read path — all runtime reads
    tripleWriter *graphutil.TripleWriter  // durable write-through to ENTITY_STATES
}
Start()   → reconcile(ctx)    // populate cache from ENTITY_STATES
OnEvent() → cache + triples   // write-through on every mutation
HTTP GET  → cache only        // never hits graph at runtime
```

Rules (JSON in `configs/rules/`) own terminal transitions. Components own phase progression.

| Component | Entities | Pattern |
|-----------|----------|---------|
| `plan-manager` | Plans, Requirements, Scenarios, ChangeProposals | Full manager with entity stores |
| `execution-manager` | Task executions | Full manager with sync.Map |
| `requirement-executor` | Requirement executions | Full manager with sync.Map |
| `project-manager` | Project config | Manager pattern, entity store |
| `scenario-orchestrator` | (dispatcher, no owned state) | Reads from graph, dispatches |

**Single-writer pattern**: Generators (`requirement-generator`, `scenario-generator`) publish typed
events (`RequirementsGeneratedEvent`, `ScenariosForRequirementGeneratedEvent`). `plan-manager` is
the sole persister — it consumes those events and writes all entity state. Generators do not write
directly to the graph.

**KV Twofer**: `ENTITY_STATES` KV bucket is the source of truth for plan state, replacing
`.semspec/plans/*.json`. Managers write triples to `ENTITY_STATES` on every mutation and
reconcile from it on startup.

**workflow/ package**: Shared domain contracts only (types, entity IDs, subjects, payloads). NOT a
state management layer. Components own their entity lifecycle.

## Registered Components (16)

| Component | Directory | Role |
|-----------|-----------|------|
| `workflow-validator` | `processor/workflow-validator/` | Validates workflow configurations |
| `workflow-documents` | (semstreams built-in) | Document management |
| `question-answerer` | `processor/question-answerer/` | LLM-backed question answering |
| `question-router` | `processor/question-router/` | Routes questions to appropriate answerers |
| `question-timeout` | `processor/question-timeout/` | SLA monitoring and escalation |
| `requirement-generator` | `processor/requirement-generator/` | Generates requirements from plans |
| `scenario-generator` | `processor/scenario-generator/` | Generates scenarios for requirements |
| `planner` | `processor/planner/` | Single-planner path |
| `plan-manager` | `processor/plan-manager/` | Plan/Requirement/Scenario/ChangeProposal lifecycle |
| `plan-reviewer` | `processor/plan-reviewer/` | SOP-aware plan validation |
| `project-manager` | `processor/project-manager/` | Project config (stack, standards, checklist) |
| `structural-validator` | `processor/structural-validator/` | Governance and checklist enforcement |
| `execution-manager` | `processor/execution-manager/` | TDD pipeline: tester → builder → validator → reviewer |
| `requirement-executor` | `processor/requirement-executor/` | DAG decomposition and serial node execution |
| `scenario-orchestrator` | `processor/scenario-orchestrator/` | Dispatches pending requirements for execution |
| `change-proposal-handler` | `processor/change-proposal-handler/` | ChangeProposal OODA loop |

## What Semspec is NOT

- **NOT embedded NATS** — Always external via docker-compose
- **NOT custom entity storage** — Use graph components with vocabulary predicates
- **NOT rebuilding agentic processors** — Reuses semstreams components

## Quick Start

```bash
docker compose up -d nats              # Start NATS infrastructure
go build -o semspec ./cmd/semspec      # Build binary
./semspec --repo .                     # Run semspec
docker compose up -d                   # Or run full stack
```

## Build Commands

```bash
go build -o semspec ./cmd/semspec   # Build binary
go build ./...                       # Build all packages
go test ./...                        # Run all tests
go mod tidy                          # Update dependencies
task generate:openapi                # Regenerate schemas + OpenAPI specs
```

## Semstreams Relationship (CRITICAL)

Semspec **imports semstreams as a library**. See [docs/03-architecture.md](docs/03-architecture.md) for details.

| Package | Purpose |
|---------|---------|
| `natsclient` | NATS connection with circuit breaker |
| `pkg/retry` | Exponential backoff with jitter |
| `pkg/errs` | Error classification (transient/invalid/fatal) |
| `component.Registry` | Component lifecycle management |
| `vocabulary` | Predicate registration and metadata |

**Bash-first tools**: Agents use `bash` for all file, git, and shell operations. Terminal tools
(`submit_work`, `ask_question`, `decompose_task`) set `StopLoop: true` to signal loop completion.
Tools are registered globally via `_ "github.com/c360studio/semspec/tools"` init imports.

## Adding Components

1. Create `processor/<name>/` with component.go, config.go, factory.go
2. Implement `component.Discoverable` interface
3. Call `yourcomponent.Register(registry)` in main.go
4. Add instance config to `configs/semspec.json`
5. Add schema tags to Config struct (see [docs/04-components.md](docs/04-components.md))
6. Import component in `cmd/openapi-generator/main.go`

## Environment Variables

Configuration supports env var expansion: `"${LLM_API_URL:-http://localhost:11434}/v1"`

| Variable | Purpose |
|----------|---------|
| `LLM_API_URL` | OpenAI-compatible API endpoint (Ollama, vLLM, OpenRouter, etc.) |
| `NATS_URL` | NATS server URL |
| `GRAPH_SOURCES` | JSON array of external graph sources for federated knowledge graph |
| `SEMSOURCE_URL` | Legacy single semsource URL (used when `GRAPH_SOURCES` is not set) |
| `SANDBOX_URL` | Sandbox container URL; without it agents operate directly on host |

Key config flag: `task-generator.reactive_mode` (default `true`) — skip task generation, advance plan to `ready_for_execution` for reactive execution via scenario-orchestrator.

## Graph-First Architecture

Graph is source of truth. Use semstreams graph components with vocabulary predicates.

**Rule of thumb**: If it has semantic meaning, it belongs in the graph. If it's structural or ephemeral, use the filesystem.

| Graph | Filesystem |
|-------|------------|
| Architecture docs, SOPs, code patterns, specs/plans, source documents | Project file tree, git diffs, explicitly requested files |

**Anti-patterns**: Hardcoded file path lists in strategies, reading docs from filesystem when they could be graph entities, bypassing graph because "it's faster."

Import vocabulary packages to auto-register predicates via `init()`. Use predicate constants, not inline strings. See existing packages in `vocabulary/` for patterns.

## NATS Messaging Patterns (CRITICAL)

See [docs/11-execution-pipeline.md](docs/11-execution-pipeline.md) for complete subject reference.

| Use Case | Transport | Why |
|----------|-----------|-----|
| Fire-and-forget, heartbeats, tool registration | Core NATS | Ephemeral, no delivery guarantee needed |
| Task dispatch, workflow triggers, context builds | **JetStream** | Order matters, must not lose messages |
| Any message with dependencies | **JetStream** | Must confirm delivery before signaling completion |

**CRITICAL**: Core NATS `Publish()` is async/buffered — messages may reorder. Use JetStream publish when order matters or when subsequent logic assumes delivery.

All payloads must be registered with semstreams via `component.RegisterPayload` in `init()` and implement `message.Payload` interface. Wrap in `message.BaseMessage` before publishing. See [docs/04-components.md](docs/04-components.md) for examples.

## Testing Patterns

- Tests create temp directories with `t.TempDir()`
- Git tests use `setupTestRepo()` helper
- Use `context.WithTimeout` for async operations
- Test both success and failure paths

## E2E Testing

Use [Task](https://taskfile.dev) commands — they handle infrastructure lifecycle automatically. Do NOT manually run `task e2e:up`.

```bash
# Tier 1: Component tests (no LLM — seconds)
task e2e:run -- plan-workflow         # REST API CRUD
task e2e:run -- scenario-execution    # Requirement/Scenario CRUD + workflow trigger

# Tier 2: Pipeline tests (mock LLM — ~1 min)
task e2e:mock -- hello-world          # Run hello-world with mock LLM
task e2e:mock -- plan-phase           # Full plan pipeline
task e2e:mock -- execution-phase      # Full execution pipeline

# All scenarios
task e2e:default

# UI E2E (Playwright) — real-LLM scenarios run here
task e2e:ui                           # Run all UI tests
task e2e:ui -- --ui                   # Interactive mode

# Debugging (keeps infra running)
task e2e:debug                        # Start infra and tail logs
task e2e:run -- hello-world           # Run against running infra
task e2e:down                         # Stop when done
```

### E2E Active Monitoring Protocol (MANDATORY)

**E2E tests are long-running. MUST monitor actively — never block in foreground.**

1. Launch via `run_in_background: true`
2. Monitor three sources every 20-30s:
   - **Test output**: `TaskOutput` (non-blocking)
   - **Semspec logs**: `docker compose -p semspec-e2e -f docker/compose/e2e.yml logs --since=30s semspec 2>&1 | grep -iE '(workflow|agentic|loop|error|fail|complet|dispatch)' | tail -30`
   - **Message logger**: `curl -s http://localhost:8180/message-logger/entries?limit=10 | jq '.[].subject'`
3. Dump evidence to `/tmp/` for post-mortem (logs, messages, workflows, loops)
4. **Abort early** if stuck in loops or burning tokens on retries
5. **Report with evidence** — quote log lines, never guess at root cause

### Port Allocation

| Stack | NATS | Monitor | HTTP | Mock LLM | Other |
|-------|------|---------|------|----------|-------|
| **Backend E2E** | 4322 | 8322 | 8180 | 11535 | sandbox: 8190 |
| **UI E2E** | 4223 | 8223 | — | 11534 | caddy: 3000 |
| **Semdragon** | 4222 | 8222 | 8081 | 9090 | caddy: 80 |
| **Ollama (native)** | — | — | — | 11434 | — |
| **Production** | 4222 | 8222 | 8080 | — | — |

## Debugging

Use observability tools, not grep. See [docs/08-observability.md](docs/08-observability.md).

| Command / Endpoint | Purpose |
|--------------------|---------|
| `task e2e:status` | Check service health |
| `/debug trace <id>` | Query messages by trace ID |
| `/debug workflow <slug>` | Show workflow state |
| `/debug loop <id>` | Show agent loop state from KV |
| `/debug snapshot <id> [--verbose]` | Export trace to .semspec/debug/ |
| `GET /message-logger/entries?limit=N` | Recent messages (newest first) |
| `GET /message-logger/trace/{traceID}` | All messages in a trace |
| `GET /message-logger/kv/{bucket}` | KV bucket contents (WORKFLOWS, AGENT_LOOPS) |
| `GET :8222/jsz?consumers=true` | JetStream consumer state |

**Message logger returns entries newest first.** Sort by timestamp for chronological order.

## Infrastructure

| Service | Dev Port | E2E Port | Purpose |
|---------|----------|----------|---------|
| NATS JetStream | 4222 | 4322 | Messaging |
| NATS Monitoring | 8222 | 8322 | HTTP monitoring |
| Semspec HTTP | 8080 | 8180 | Gateway / API (includes graph-gateway at /graph-gateway) |
| SemSource | internal | internal | Source graph indexing (AST, git, docs, config) — shared NATS |
| Sandbox | 8090 | 8190 | Code execution |
| Mock LLM | — | 11535 | Deterministic test fixtures |
| Ollama (native) | 11434 | — | LLM inference |

SemSource watches the workspace and publishes entities to `graph.ingest.entity` on the shared NATS.
Graph-ingest processes these into ENTITY_STATES, making them queryable via graph-gateway.
