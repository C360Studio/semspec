# Semspec

Semspec is a spec-driven development agent with persistent memory.

The problem it addresses: AI coding assistants are powerful but forget everything between sessions. When you're working on a project over days or weeks, or handing off between different agents, that context loss is painful. You end up re-explaining the codebase, re-stating decisions, re-discovering what was already figured out.

Semspec stores everything in a knowledge graph—code entities, specs, proposals, decisions, relationships. Agents query the graph instead of starting from scratch. One agent explores the codebase and notes how auth works; a different agent picks that up later without asking again.

## What's Working Now

**AST Indexing** — Parses source files and extracts entities (functions, types, classes) into the graph. Currently supports Go, with JavaScript and Python in progress.

**Tools** — File and git operations that agents can call:
- `file_read`, `file_write`, `file_list`
- `git_status`, `git_branch`, `git_commit`

**Constitution** — Define project rules (coding standards, architectural constraints) and check code against them.

**Web UI** — SvelteKit interface for chat and entity browsing.

## What's In Progress

**Spec-Driven Workflow** — Proposals, specs, and tasks as graph entities. The idea is "structure before code" without rigid phase gates. Explore freely, spec when it helps, implement when ready.

**Multi-Agent Coordination** — Specialized agents for different tasks (architect plans, implementer codes, reviewer validates). Right model for the right job, with the graph as shared memory.

**Training Flywheel** — Capture trajectories and feedback to improve models over time. Good completions become training data.

## Getting Started

You'll need NATS running (semstreams provides docker-compose for this):

```bash
# In the semstreams repo
docker-compose -f docker/compose/e2e.yml up -d
```

Then build and run:

```bash
go build -o semspec ./cmd/semspec
./semspec --repo /path/to/your/project
```

Semspec connects to NATS at `localhost:4222` by default. Set `NATS_URL` to change this.

## Project Layout

```
semspec/
├── cmd/semspec/        # Main binary
├── processor/
│   ├── ast-indexer/    # Source file parsing
│   ├── semspec-tools/  # Tool execution
│   ├── constitution/   # Project rules
│   └── ast/            # Shared parsing code
├── tools/              # Tool implementations
├── ui/                 # Web interface
├── configs/            # Example configs
└── docs/               # Architecture and specs
```

## Design Principles

These come from studying what works and what doesn't in existing tools (SpecKit, OpenSpec, BMAD, Aider):

**Graph-first** — Entities and relationships are primary; files are artifacts. You can query "what specs affect the auth module?" and get an answer.

**Persistent context** — Every session starts with full project knowledge. No more re-explaining.

**Fluid workflows** — Explore freely, commit when ready. Human checkpoints where they matter, not enforced phase gates.

**Brownfield-native** — Designed for existing codebases. Most real work is evolving what exists, not greenfield.

**Specialized agents** — Different models for different tasks. An architect model for planning, a fast model for implementation, a careful model for review.

## More Info

- [docs/architecture.md](docs/architecture.md) — How it fits together
- [docs/roadmap.md](docs/roadmap.md) — What's planned
- [docs/spec/semspec-research-synthesis.md](docs/spec/semspec-research-synthesis.md) — Research behind the design

## License

MIT
