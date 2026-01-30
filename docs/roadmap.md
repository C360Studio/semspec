# Semspec Roadmap

## Why We're Building This

AI coding assistants have a memory problem. Every session starts fresh. Work on a project for weeks, and each conversation requires re-explaining the codebase, re-stating decisions, re-discovering what was already figured out. Hand off between agents? Same problem.

Existing spec-driven tools (SpecKit, OpenSpec, BMAD) try to solve this with markdown files and rigid workflows. But they create new problems: "sea of markdown" that overwhelms developers, phase gates that feel like waterfall, no semantic understanding of relationships.

Semspec takes a different approach: a knowledge graph that agents query. Code entities, specs, proposals, decisions—all stored with relationships. Context persists across sessions. Agents share memory instead of starting over.

See [semspec-research-synthesis.md](spec/semspec-research-synthesis.md) for the full analysis.

## Design Principles

These guide our decisions:

- **Graph-first** — Entities and relationships are primary; files are artifacts
- **Persistent context** — Every session starts with full project knowledge
- **Fluid workflows** — Explore freely, spec when helpful, implement when ready. Human checkpoints, not phase gates
- **Brownfield-native** — Designed for existing codebases. Most work is 1→n, not 0→1
- **Specialized agents** — Right model for right task. Architect plans, implementer codes, reviewer validates

## Current State

### Working

| Component | Status | Notes |
|-----------|--------|-------|
| AST Indexer (Go) | Done | Functions, types, interfaces, call graph |
| File Tools | Done | `file_read`, `file_write`, `file_list` |
| Git Tools | Done | `git_status`, `git_branch`, `git_commit` |
| Constitution | Done | Project rules with HTTP API |
| Web UI | Started | SvelteKit chat interface |
| Graph Storage | Via semstreams | Uses graph-ingest, graph-index, graph-gateway |

### Architecture

Semspec imports semstreams as a library and registers custom components. Infrastructure (NATS, graph storage, message routing) comes from semstreams. Semspec adds:

- Language-specific AST indexers
- Development tools (file, git)
- Constitution management
- CLI commands (registered with semstreams CLI input)

## Near-term

### Multi-Language AST

Extend indexing beyond Go:

| Language | Priority | Approach |
|----------|----------|----------|
| JavaScript/TypeScript | High | Tree-sitter or TypeScript compiler API |
| Python | Medium | AST module or tree-sitter |

Same entity model (functions, classes, relationships), different parsers.

### Spec-Driven Entities

Add proposal, spec, and task entities to the graph:

```
proposal:add-refresh-token
  status: exploring | specified | approved | implemented | archived
  spec: spec:refresh-token-design
  tasks: [task:001, task:002]

spec:refresh-token-design
  content: (markdown in object store)
  implements: proposal:add-refresh-token
  affects: [code:auth/token.go, code:middleware/auth.go]

task:001
  status: pending | in_progress | done | blocked
  spec: spec:refresh-token-design
  assignee: implementer
```

The workflow is fluid: create a proposal to explore, spec when design clarifies, break into tasks when ready to implement. No enforced sequence.

### CLI Commands

Semspec registers commands with semstreams' CLI input component via init(), same pattern as component registration:

```go
func init() {
    cli.Register("spec", specCommand)
    cli.Register("propose", proposeCommand)
    cli.Register("constitution", constitutionCommand)
}
```

Planned commands:

| Command | Purpose |
|---------|---------|
| `/propose <idea>` | Create a proposal, start exploring |
| `/spec <proposal>` | Generate spec from proposal |
| `/tasks <spec>` | Break spec into tasks |
| `/constitution` | Show/check project rules |
| `/context <query>` | Query the knowledge graph |

### HTTP Endpoints

Constitution already exposes HTTP. Add similar for proposals/specs:

```
GET  /api/proposals
POST /api/proposals
GET  /api/proposals/:id
POST /api/proposals/:id/spec

GET  /api/specs
GET  /api/specs/:id
GET  /api/specs/:id/tasks
```

## Later

### Multi-Agent Coordination

Specialized agents with different models and tool access:

| Role | Model | Tools | Purpose |
|------|-------|-------|---------|
| Architect | Large (32b) | graph_query, read | Plans, designs, reviews |
| Implementer | Fast (7b) | file_*, git_* | Writes code |
| Reviewer | Medium | graph_query, read | Validates changes |

Task router assigns work based on type. Graph serves as shared memory between agents.

### Training Flywheel

Capture trajectories for model improvement:

- Store agent interactions as `result:{id}` entities
- Include context, prompts, outputs, human feedback
- Export approved trajectories as training data
- Quality filtering (only good completions)

### Web UI Completion

Current UI has chat. Add:

- Entity browser (explore the graph visually)
- Proposal/spec management
- Task board
- Trajectory history and export

## What We're Not Building

Semspec stays focused. These belong elsewhere:

- **Embedded NATS** — Always external via docker-compose
- **Custom graph storage** — Use semstreams graph components
- **Agentic orchestration** — Use semstreams agentic-loop
- **Duplicate tooling** — If semstreams has it, use it

## Status Updates

_Update this section as work progresses._

| Date | Change |
|------|--------|
| 2025-01-30 | Deleted `processor/query/` (uses semstreams graph-query) |
| 2025-01-30 | Added constitution HTTP handlers |
| 2025-01-29 | Started SvelteKit web UI |
| 2025-01-29 | Added constitution component |
| 2025-01-29 | Added type resolution to AST parser |
