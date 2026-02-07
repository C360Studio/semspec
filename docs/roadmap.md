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

## Architecture

Semspec imports semstreams as a library and registers custom components. Infrastructure (NATS, graph storage, message routing) comes from semstreams. Semspec adds:

- Language-specific AST indexers (Go, TypeScript, JavaScript)
- Development tools (file, git)
- Constitution management and enforcement
- Spec-driven workflow commands
- Knowledge gap resolution with question routing
- CLI commands (registered with semstreams CLI input)

See [architecture.md](architecture.md) for the full system diagram.

## Future Directions

### Tool Execution Provenance

Add PROV-O provenance tracking to tool executors for audit trails:
- `prov:wasGeneratedBy` - Track what agent/loop created each file
- `prov:used` - Track inputs to tool operations
- `prov:wasAttributedTo` - Attribution to users/agents
- Timestamps for all operations

**Benefits:**
- Enable "who changed what when" queries
- Support compliance audit trails
- Rich context for multi-agent handoffs

### Spec-Code Linking

Link spec entities to code entities discovered by AST indexer:

```
spec:refresh-token-design
  affects: [code:auth/token.go, code:middleware/auth.go]
```

This enables:
- "What specs affect this code?"
- "What code implements this spec?"
- Impact analysis for proposed changes

### Entity-Specific API

REST endpoints for direct entity management:

```
GET  /api/proposals
POST /api/proposals
GET  /api/proposals/:id
POST /api/proposals/:id/spec

GET  /api/specs
GET  /api/specs/:id
GET  /api/specs/:id/tasks
```

Currently using semstreams' agentic-dispatch HTTP endpoints for message routing.

### Multi-Agent Handoffs

Full multi-agent coordination with graph-based shared memory. Currently have:
- Capability-based model selection (planning, writing, coding, reviewing, fast)
- Question routing between agents with SLA/escalation
- Role-to-capability mapping

Remaining:
- Persistent agent memory in graph
- Structured handoff protocols between specialized agents

### Training Flywheel

Capture trajectories for model improvement:

- Store agent interactions as `result:{id}` entities
- Include context, prompts, outputs, human feedback
- Export approved trajectories as training data
- Quality filtering (only good completions)

### Web UI Completion

Current UI has chat, activity stream, loops, and health indicators. Priority additions:

1. **Entity Browser**
   - Filter by type (code, proposal, spec, task)
   - BFO/CCO classification badges
   - PROV-O relationship display (derivedFrom, generatedBy)
   - Search by capability expression

2. **Question Panel**
   - View pending questions
   - Answer inline
   - Escalation status

3. **Remaining features:**
   - Proposal/spec management views
   - Task board with drag-and-drop
   - Trajectory history and export

## What We're Not Building

Semspec stays focused. These belong elsewhere:

- **Embedded NATS** — Always external via docker-compose
- **Custom graph storage** — Use semstreams graph components
- **Agentic orchestration** — Use semstreams agentic-loop
- **Duplicate tooling** — If semstreams has it, use it
