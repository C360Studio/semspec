# Semspec Roadmap

## Overview

Semspec is a semantic development agent built as a semstreams extension. This roadmap combines valid items from archived planning documents with a gap analysis of the current implementation.

## Current Status Summary

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 0 | Infrastructure | Complete (docker-compose, NATS) |
| Phase 1 | Core Components | Complete (tools, AST parsing, type resolution) |
| Phase 2 | Knowledge Layer | 60% (parsing, constitution, query engine done) |
| Phase 3 | Multi-Model | Not started |
| Phase 4 | Training Flywheel | Not started |
| Phase 5 | Polish & Integration | Not started |

---

## Phase 1: Core Components (Complete)

### Completed

- NATS connection with circuit breaker and health checks
- JetStream stream provisioning
- Component lifecycle (Initialize -> Start -> Stop)
- Tool executors: `file_read`, `file_write`, `file_list`, `git_status`, `git_branch`, `git_commit`
- AST indexer with file watcher
- Entity extraction (functions, methods, structs, interfaces, constants, variables)
- ICS 206-01 vocabulary predicates
- Configuration system (file-based and programmatic)
- Type resolution with import mapping (`typeNameToEntityID()` in `processor/ast/parser.go`)
  - Resolves imports to build proper entity IDs
  - Handles cross-package type references
  - Distinguishes builtin types, local types, and external types

---

## Phase 2: Knowledge Layer (60% Complete)

### Completed

- AST parsing for Go code entities
- File watcher for real-time updates
- Basic relationship tracking (contains, embeds)
- Constitution component (`processor/constitution/`)
  - Project-wide constraints and preferences
  - YAML/JSON file loading
  - Rule enforcement by priority (must/should/may)
  - Check requests via NATS messaging
- Query engine component (`processor/query/`)
  - Entity queries by ID
  - Relationship traversal (depends_on, depended_by, implements, contains)
  - Text search across entities
  - In-memory index with inverted lookups

### Remaining

#### Entity Schema Implementation

| Entity | Purpose | Priority | Status |
|--------|---------|----------|--------|
| `constitution:{project}` | Project constraints and preferences | High | Done |
| `proposal:{id}` | Change proposals with status tracking | Medium | Pending |
| `spec:{id}` | Detailed specifications | Medium | Pending |
| `task:{id}` | Fluid-state task tracking | Medium | Pending |
| `result:{id}` | Execution results for training data | Low (Phase 4) | Pending |

#### Relationship Deepening

- [x] **Call graph extraction** - Resolves call targets to entity IDs
- [ ] **Interface implementation tracking** - Which types implement which interfaces
- [x] **Import resolution** - Full cross-package reference mapping
- [ ] **Test semantics** - Extract test function metadata

---

## Phase 3: Multi-Model Architecture (Not Started)

### Design (from archived docs - still valid)

**Architect/Editor Split:**

- **Architect model**: Plans, designs, reviews (larger model, qwen2.5-coder:32b)
- **Editor model**: Implements, executes (faster model, deepseek-coder)

**Specialized Roles:**

| Role | Responsibility |
|------|----------------|
| Planner | Break down requests into tasks |
| Spec-Writer | Produce detailed specifications |
| Architect | Design solutions |
| Implementer | Write code |
| Reviewer | Validate changes |

**Task Router:**

- Route tasks to appropriate role based on type
- Manage handoffs between roles
- Track task state through fluid workflow

### Implementation Tasks

- [ ] Role configuration schema
- [ ] Task routing logic
- [ ] Model selection per role
- [ ] Ollama integration (already in semstreams)
- [ ] Constitution enforcement (check constraints before execution)

---

## Phase 4: Training Flywheel (Not Started)

### Design (from archived docs - still valid)

**Trajectory Capture:**

- Store all agent interactions as `result:{id}` entities
- Include context, prompts, outputs, user feedback

**Feedback Loop:**

- User approval/rejection signals
- Edit distance from generated to final code
- Success metrics per task type

**Export Format:**

- JSONL trajectories for fine-tuning
- Compatible with standard training pipelines

### Implementation Tasks

- [ ] Result entity storage
- [ ] Feedback collection mechanism
- [ ] Trajectory export command
- [ ] Quality filtering (good trajectories only)

---

## Phase 5: Polish & Integration (Not Started)

### Items (from archived docs)

- [ ] CLI polish (better output formatting, progress indicators)
- [ ] Multi-channel support (HTTP/SSE in addition to CLI)
- [ ] MCP server interface (external tool integration)
- [ ] Web UI implementation

### Web UI Status

**Spec exists** (`docs/spec/semspec-web-ui-spec.md`) but **zero code written**.

Specified features:

- SvelteKit 2 / Svelte 5 with runes
- Chat interface for agent interaction
- Dashboard with project overview
- Task management and history
- Settings management

---

## Priority Recommendations

### Immediate (Phase 2 completion)

1. ~~**Constitution entity** - Enables constraint checking~~ Done
2. ~~**Type resolution** - Completes AST extraction~~ Done
3. ~~**Query engine** - Makes knowledge graph useful~~ Done
4. **Proposal/Spec/Task entities** - Complete entity schema implementation
5. **Interface implementation tracking** - Which types implement which interfaces

### Near-term (Phase 3 start)

6. **Role configuration** - Define architect/editor split
7. **Task routing** - Basic multi-step workflow

### Later

8. Training flywheel (Phase 4)
9. Web UI (Phase 5)

---

## Recent Changes

| File | Change |
|------|--------|
| `processor/ast/parser.go` | Implemented `typeNameToEntityID()` with import resolution |
| `processor/constitution/` | Added constitution entity storage and checking |
| `processor/query/` | Added graph query component |
| `docs/roadmap.md` | Created this roadmap document |
