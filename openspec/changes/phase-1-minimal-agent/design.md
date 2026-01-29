## Context

Phase 1 bootstraps Semspec as a working dev agent by wiring together existing SemStreams components. The core agentic infrastructure already exists:

**Existing in SemStreams (../semstreams):**
- `processor/input/cli` - CLI input with REPL, Ctrl+C handling
- `processor/router` - Command parsing, loop tracking, permissions
- `processor/agentic-loop` - Agent state machine orchestration
- `processor/agentic-model` - LLM calls via OpenAI-compatible API
- `processor/agentic-tools` - Tool execution with timeout/allowlist

**What Semspec adds:**
- `cmd/semspec` - CLI binary that wires everything together
- Tool executors - File and git operations
- Entity storage - Proposals, tasks, results in NATS KV
- Configuration - Project and model settings

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  SEMSPEC (this repo)                                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │ cmd/semspec                                                          │   │
│  │   - Orchestrates component lifecycle                                 │   │
│  │   - Registers tool executors                                         │   │
│  │   - Loads configuration                                              │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                      │                                      │
│  ┌──────────────┐  ┌──────────────┐  │  ┌──────────────┐                   │
│  │ tools/file   │  │ tools/git    │  │  │ storage/     │                   │
│  │ - file_read  │  │ - git_status │  │  │ entity       │                   │
│  │ - file_write │  │ - git_branch │  │  │ - proposals  │                   │
│  │ - file_list  │  │ - git_commit │  │  │ - tasks      │                   │
│  └──────────────┘  └──────────────┘  │  │ - results    │                   │
│                                      │  └──────────────┘                   │
├──────────────────────────────────────┼──────────────────────────────────────┤
│  SEMSTREAMS (../semstreams) - used as Go module dependency                 │
│                                      │                                      │
│  ┌─────────────┐     ┌──────────────┐│    ┌─────────────┐                  │
│  │ input/cli   │────▶│   router     │┼───▶│ agentic-    │                  │
│  │             │     │              ││    │   loop      │                  │
│  └─────────────┘     └──────────────┘│    └──────┬──────┘                  │
│                                      │           │                          │
│                            ┌─────────┘    ┌──────┴──────┐                  │
│                            │              │             │                   │
│                     ┌──────▼──────┐ ┌─────▼─────┐      │                   │
│                     │ agentic-    │ │ agentic-  │◀─────┘                   │
│                     │   model     │ │   tools   │                          │
│                     │  (Ollama)   │ │           │                          │
│                     └─────────────┘ └───────────┘                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Goals / Non-Goals

**Goals:**
- Wire SemStreams components into a working `semspec` CLI
- Provide useful tool executors (file ops, git ops)
- Store minimal entities for tracking work
- Configure Ollama model and project settings
- Prove end-to-end flow: user input → LLM → tool execution → output

**Non-Goals:**
- Modifying SemStreams components (use as-is)
- Knowledge graph / AST understanding (Phase 2)
- Multi-model orchestration / specialized roles (Phase 3)
- Training data capture (Phase 4)

## Decisions

### Decision 1: Semspec as thin orchestration layer

**Choice:** Semspec imports SemStreams as a Go module and wires components together.

**Rationale:** SemStreams already has the hard parts (agentic loop, model calling, tool execution). Semspec just needs to configure and connect them.

**Alternatives Considered:**
- Fork SemStreams components into Semspec → Duplication, maintenance burden
- Build from scratch → Wasted effort, components already exist

### Decision 2: Embedded NATS by default

**Choice:** Start embedded NATS server unless external URL provided.

**Rationale:** Reduces setup friction for single-user local use. Users don't need to run a separate NATS server.

**Alternatives Considered:**
- Require external NATS → More setup friction
- JetStream only → Embedded is simpler for MVP

### Decision 3: Tool executors as separate packages

**Choice:** `tools/file` and `tools/git` packages implementing `agentic.ToolExecutor` interface.

**Rationale:** Clean separation, easy to test, follows SemStreams patterns.

```go
// Registration in cmd/semspec
toolsComponent.Register(file.NewExecutor(repoPath))
toolsComponent.Register(git.NewExecutor(repoPath))
```

**Alternatives Considered:**
- Single tools package → Harder to test, less modular
- Register tools via config → More complex, less type-safe

### Decision 4: YAML configuration with layered loading

**Choice:** Load config from `~/.config/semspec/config.yaml` (user) and `./semspec.yaml` (project), with project overriding user.

**Rationale:** Standard pattern, allows user defaults with project-specific overrides.

**Alternatives Considered:**
- JSON config → YAML is more readable for humans
- Environment variables only → Less flexible for complex config
- Single config location → No user defaults

### Decision 5: Entity storage in NATS KV

**Choice:** Store proposals, tasks, results in NATS KV buckets.

**Rationale:** Already using NATS, KV is simple and sufficient for Phase 1. Migrate to graph-based storage in Phase 2.

**Alternatives Considered:**
- SQLite → Additional dependency
- Files → Harder to query
- Full graph store now → Premature

## Risks / Trade-offs

**[SemStreams coupling]** → Semspec depends heavily on SemStreams internals.
- *Mitigation:* Use stable interfaces. SemStreams is also our codebase.

**[Embedded NATS limitations]** → Can't scale to multi-user without external NATS.
- *Mitigation:* Support external NATS via config. Single-user is fine for Phase 1.

**[Single model for all tasks]** → May produce lower quality than specialized models.
- *Mitigation:* Acceptable for MVP. Phase 3 adds role-based model selection.

## Open Questions

1. **System prompt content** - What context/instructions should be in the default system prompt?
2. **Error handling UX** - How should tool failures be presented to users?
3. **Config schema validation** - How strict should config validation be?
