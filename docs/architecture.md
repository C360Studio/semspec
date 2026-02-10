# Semspec Architecture

> **New to semspec?** Read [How Semspec Works](how-it-works.md) first for a progressive introduction to the system.

Semspec is a **semstreams extension** - it imports semstreams as a library, registers custom components, and runs them via the component lifecycle.

## System Overview

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  DOCKER COMPOSE (infrastructure)                                              │
│  ┌──────────────────────────────────────────────────────────────────────────┐│
│  │  NATS JetStream (required)                                                ││
│  │  Stream: AGENT                                                            ││
│  │  Subjects: tool.*, graph.ingest.*                                        ││
│  └──────────────────────────────────────────────────────────────────────────┘│
│  ┌──────────────────────────────────────────────────────────────────────────┐│
│  │  Optional: Ollama (LLM), semembed (embeddings)                           ││
│  └──────────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
                                    ▲
                                    │ NATS
                                    │
┌───────────────────────────────────┴──────────────────────────────────────────┐
│  SEMSPEC BINARY                                                               │
│                                                                               │
│  cmd/semspec/main.go                                                          │
│  ├── Loads config (JSON or defaults)                                         │
│  ├── Creates component.Registry                                              │
│  ├── Registers semstreams components (componentregistry.Register)            │
│  ├── Registers semspec components (ast-indexer, semspec-tools)               │
│  └── Starts components via lifecycle (Initialize → Start → Stop)             │
│                                                                               │
│  ┌─────────────────────────────┐  ┌─────────────────────────────────────────┐│
│  │  processor/ast-indexer/     │  │  processor/semspec-tools/               ││
│  │  ├── Parses Go source files │  │  ├── Subscribes to tool.execute.*      ││
│  │  ├── Extracts code entities │  │  ├── Executes file/git operations      ││
│  │  ├── Watches for changes    │  │  ├── Publishes to tool.result.*        ││
│  │  └── Publishes to graph.*   │  │  └── Sends heartbeats                  ││
│  └─────────────────────────────┘  └─────────────────────────────────────────┘│
│                                                                               │
│  ┌─────────────────────────────┐  ┌─────────────────────────────────────────┐│
│  │  workflow/                  │  │  processor/constitution/                ││
│  │  ├── Core workflow logic    │  │  ├── Loads project rules                ││
│  │  ├── Triggers, templates    │  │  ├── Handles /check requests            ││
│  │  ├── Question routing       │  │  └── Publishes to graph                 ││
│  │  └── Gap detection          │  └─────────────────────────────────────────┘│
│  └─────────────────────────────┘                                              │
│                                                                               │
│  ┌─────────────────────────────┐  ┌─────────────────────────────────────────┐│
│  │  processor/workflow-valid-  │  │  output/workflow-documents/             ││
│  │           ator/             │  │  ├── Subscribes output.workflow.docs   ││
│  │  ├── Request/reply service  │  │  ├── Transforms JSON → markdown        ││
│  │  ├── Validates doc structure│  │  └── Writes .semspec/changes/          ││
│  └─────────────────────────────┘  └─────────────────────────────────────────┘│
│                                                                               │
│  ┌─────────────────────────────┐  ┌─────────────────────────────────────────┐│
│  │  processor/rdf-export/      │  │  tools/                                 ││
│  │  ├── Consumes graph.ingest  │  │  ├── file/executor.go                   ││
│  │  ├── Serializes to RDF      │  │  │   file_read, file_write, file_list  ││
│  │  └── Publishes graph.export │  │  └── git/executor.go                    ││
│  └─────────────────────────────┘  │       git_status, git_branch, git_commit││
│                                    └─────────────────────────────────────────┘│
│  ┌─────────────────────────────┐                                              │
│  │  processor/ast/             │                                              │
│  │  ├── parser.go              │                                              │
│  │  ├── entities.go            │                                              │
│  │  ├── watcher.go             │                                              │
│  │  └── predicates.go          │                                              │
│  └─────────────────────────────┘                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Component Registration Pattern

```go
// cmd/semspec/main.go
func run() {
    // 1. Create component registry
    registry := component.NewRegistry()

    // 2. Register ALL semstreams components (graph, agentic, etc.)
    componentregistry.Register(registry)

    // 3. Register semspec-specific components
    astindexer.Register(registry)
    semspectools.Register(registry)

    // 4. Create components from config and start them
    for name, cfg := range config.Components {
        comp, _ := registry.CreateComponent(name, cfg, deps)
        comp.Start(ctx)
    }
}
```

## Semstreams Relationship

Semspec **imports semstreams as a library** and extends it with custom components.

### What Semstreams Provides

| Package/Component | Purpose | How Semspec Uses It |
|-------------------|---------|---------------------|
| `component.Registry` | Component lifecycle management | Creates and manages all components |
| `componentregistry.Register()` | Registers all semstreams components | Gives access to graph, agentic, etc. |
| `natsclient` | NATS connection with circuit breaker | All NATS operations |
| `config.Loader` | Flow configuration loading | Loads `configs/semspec.json` |
| `config.StreamsManager` | JetStream stream management | Creates AGENT stream |
| `pkg/errs` | Error classification | Retry decisions (Nak vs Term) |
| `agentic.ToolCall/ToolResult` | Tool message types | Tool execution protocol |
| `message.Triple` | Graph triple format | AST entity storage |

### Contract with agentic-tools

Semspec's `semspec-tools` and semstreams' `agentic-tools` can coexist:

| Aspect | semspec-tools | agentic-tools |
|--------|---------------|---------------|
| **Consumer names** | `semspec-tool-*` | `agentic-tools-*` |
| **Tools handled** | `file_*`, `git_*` | `graph_query`, internal tools |
| **Registration** | Advertises via `tool.register.*` | Tracks external tools |

Different consumer names prevent message competition.

### Deployment Models

| Model | Components Running | Use Case |
|-------|-------------------|----------|
| **Semspec Standalone** | ast-indexer + semspec-tools | Simple development agent |
| **With Semstreams** | Above + agentic-loop + agentic-model + graph-* | Full agentic system |
| **Full Stack** | All above + service-manager + UI | Production deployment |

## Tool Dispatch Flow

```
agentic-loop                    NATS                         semspec-tools
     │                            │                            │
     │ ──tool.execute.file_read──▶│──────────────────────────▶│
     │                            │                            │
     │                            │                  Execute(ctx, call)
     │                            │                            │
     │ ◀──tool.result.{call_id}───│◀─────────────────────────│
```

## NATS Subject Patterns

| Subject | Direction | Purpose |
|---------|-----------|---------|
| `tool.execute.<name>` | Input | Tool execution requests |
| `tool.result.<call_id>` | Output | Execution results |
| `tool.register.<name>` | Output | Tool advertisement |
| `tool.heartbeat.semspec` | Output | Provider health signal |
| `graph.ingest.entity` | Output | AST entities for graph storage |
| `workflow.trigger.>` | Input | Workflow trigger messages |
| `workflow.validate.>` | Input | Document validation requests |
| `output.workflow.documents` | Input | Document export messages |

## Provenance Flow

Tool executors emit PROV-O triples to enable "who changed what when" queries:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  PROVENANCE FLOW: Tool Execution → Graph                                     │
│                                                                              │
│  1. USER REQUEST                                                            │
│     User → agentic-loop (via /message HTTP or CLI)                         │
│             │                                                               │
│             │ prov:wasAssociatedWith                                        │
│             ▼                                                               │
│  2. LOOP CREATES TOOL CALL                                                  │
│     Loop → tool.execute.file_write                                          │
│             │                                                               │
│             │ agent.activity.loop                                           │
│             ▼                                                               │
│  3. TOOL EXECUTOR RUNS                                                      │
│     semspec-tools executes file_write                                       │
│             │                                                               │
│             │ Emits provenance triples:                                     │
│             │ • prov.generation.activity → tool_call_id                    │
│             │ • prov.attribution.agent → loop_id                           │
│             │ • prov.time.generated → timestamp                            │
│             ▼                                                               │
│  4. GRAPH STORES PROVENANCE                                                 │
│     graph-ingest receives triples                                           │
│     graph-index makes queryable                                            │
│             │                                                               │
│             ▼                                                               │
│  5. QUERY PROVENANCE                                                        │
│     "What files did loop X create?"                                        │
│     "Who modified auth.go since Tuesday?"                                  │
│     "Show audit trail for this proposal"                                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Provenance Predicates

| Predicate | Type | Direction | Usage |
|-----------|------|-----------|-------|
| `prov.generation.activity` | entity → tool_call | Output | File was generated by this tool call |
| `prov.attribution.agent` | entity → loop | Output | Entity attributed to this loop |
| `prov.usage.entity` | tool_call → entity | Input | Tool call used this entity as input |
| `prov.time.generated` | entity → datetime | Metadata | When entity was created |
| `prov.time.started` | activity → datetime | Metadata | When activity started |
| `prov.time.ended` | activity → datetime | Metadata | When activity ended |

## Related Documentation

| Document | Description |
|----------|-------------|
| [Workflow System](workflow-system.md) | LLM-driven workflows, model selection, validation |
| [Question Routing](question-routing.md) | Knowledge gap resolution, SLA, escalation |
| [Components](components.md) | Component configuration and creation |
| [Vocabulary Spec](spec/semspec-vocabulary-spec.md) | BFO/CCO/PROV-O vocabulary |
| [Getting Started](getting-started.md) | Quick start guide |
