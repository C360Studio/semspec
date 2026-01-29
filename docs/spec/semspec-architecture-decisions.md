# Semspec Architecture Decisions

**Document Purpose**: Capture key architectural decisions and lessons learned to prevent re-exploration of evaluated paths.

---

## What is Semspec?

Semspec is a **dev agent** built on SemStreams primitives - not infrastructure waiting for other agents to use it.

```
NOT THIS:                           THIS:
─────────                           ─────
SemMem (infrastructure)             Semspec (agent)
    ↑                                   │
Claude Code queries via MCP         Human: "add token refresh"
                                        │
                                        ▼
                                    Semspec understands, plans,
                                    implements, delivers
```

**Why the shift?** Tools like SpecKit, OpenSpec, BMAD are all patching gaps in existing agents (Claude Code, Cursor). We control the full stack - we can build the agent we want.

---

## Our Key Differentiator: Semantic Knowledge Graph

Every other tool struggles with **context loss**:

| Tool | Context Solution | Limitation |
|------|------------------|------------|
| SpecKit | Constitution + spec files | File-based, no semantic links |
| OpenSpec | AGENTS.md + change folders | Scoped to current change |
| BMAD | Epic sharding + step files | Complex, still text-based |
| Aider | Repository map | Per-session only |
| Claude Code | Plan mode | Can't control location, limited |

**Our solution**: Persistent semantic knowledge graph

```
Others:
  New session = rebuild context from files
  AI re-reads AGENTS.md, constitution, specs...
  
Semspec:
  Knowledge persists in graph
  Query: "what code implements auth refresh?" → instant
  Query: "what specs affect this module?" → instant
  Query: "what did we decide about token expiry?" → instant
```

This is not a feature - it's the foundation.

---

## Decision 1: Agent-First, Not Infrastructure-First

**Context**: Should we build infrastructure (SemMem) that agents use, or build the agent (Semspec)?

**Decision**: Build Semspec as the product.

**Rationale**:
- SemMem without agentic coordination is just another MCP server
- We'd still depend on Claude Code/Cursor for actual agent behavior
- Those tools have limitations we can't control
- Building the agent lets us own the full experience

**What this means**:
- Semspec has a CLI interface
- Semspec drives work, not Claude Code
- SemStreams provides primitives, Semspec uses them
- Later: MCP interface for integration (optional)

---

## Decision 2: Local-First, No Cloud API Dependencies

**Context**: Should we use cloud APIs (Claude, GPT-4) or local models?

**Decision**: Local models via Ollama from day one.

**Rationale**:
- **Cost**: Cloud APIs add up quickly during development and testing
- **Privacy**: Code never leaves local machine
- **Latency**: No network round-trip
- **Availability**: Works offline
- **Alignment**: Matches SemStreams' edge-first philosophy

**Implementation**:
- All models accessed via OpenAI-compatible API
- Ollama serves this API locally
- LiteLLM can proxy if needed (for testing other providers)
- Same code works with any backend

**Recommended Models**:
| Model | Size | Use Case | VRAM |
|-------|------|----------|------|
| `qwen2.5-coder:32b` | 32B | Planner, Implementer | ~20GB |
| `deepseek-coder-v2:16b` | 16B | Spec-writer | ~10GB |
| `codellama:13b` | 13B | Lighter alternative | ~8GB |

**Fallback**: If local GPU insufficient, can use LiteLLM to proxy to cloud, but this is opt-in, not default.

---

## Decision 3: Lessons from Existing Tools

### From SpecKit: Constitution Concept ✅

Project-wide constraints that apply to all work:

```yaml
constitution:
  code_quality:
    - "All public functions must have doc comments"
    - "No function longer than 50 lines"
  testing:
    - "Minimum 80% coverage for new code"
    - "Integration tests for all API endpoints"
  security:
    - "No secrets in code"
    - "All inputs validated"
  architecture:
    - "New components must follow SemStreams patterns"
    - "No CGO dependencies"
```

Stored as entity, checked during spec approval.

### From OpenSpec: Brownfield-First ✅

Most development is 1→n (evolving existing code), not 0→1 (greenfield).

**Implication**: Semspec must understand existing codebase:
- AST entities track what code exists
- Relationships track what calls what
- Impact analysis: "what breaks if I change this?"
- Source of truth vs proposals separation

### From OpenSpec: Fluid Workflows ✅

**Problem with SpecKit**: Rigid phase gates feel like "reinvented waterfall"

**Solution**: Fluid state machine with human checkpoints

```
/explore    → think freely, no commitment
/plan       → when ready, create plan
/spec       → refine specification
/implement  → execute (can return to spec)
/review     → human checkpoint
```

Not enforced sequences - checkpoints when valuable.

### From BMAD: Specialized Agents ✅

Different models (or prompts) for different tasks:

| Role | Focus | Model Size |
|------|-------|------------|
| Planner | Requirements, reasoning | Larger (32b) |
| Spec-writer | Structured output | Medium (16b) |
| Implementer | Code generation | Larger (32b) |
| Reviewer | Analysis | Larger (32b) |

**Local-first approach**: All roles use local models via Ollama. No cloud API costs.

**Not BMAD's complexity**: We use role config, not 50+ workflows.

### From Aider: Architect/Editor Split ✅

SOTA benchmark results come from separating:
- **Architect**: Describes how to solve the problem
- **Editor**: Translates description into file edits

**Our implementation**:
- Planner agent reasons about approach
- Implementer agent executes the plan
- Clear handoff via task entities

### From BMAD: Human-as-Facilitator ✅

The relationship is collaborative, not transactional:

```
NOT: "AI, do this" → code
BUT: "Let's figure out the best approach" → iterate → code
```

Semspec asks clarifying questions, proposes alternatives, explains tradeoffs.

---

## Decision 4: Agentic Components Architecture

**Context**: How should the agentic loop be structured?

**Decision**: Three core agentic components in SemStreams + interaction layer

**Rationale**:
- Clean separation of concerns: loop orchestration, model calling, tool execution
- Each component owns specific NATS subjects and KV buckets
- Independently scalable
- Easy to test in isolation

**Components** (in `processor/`):
```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        AGENTIC COMPONENTS                                   │
│                                                                              │
│  agent.task.*                                                               │
│       │                                                                      │
│       ▼                                                                      │
│  ┌─────────────┐   agent.request.*   ┌──────────────┐                       │
│  │             │ ─────────────────▶ │              │                       │
│  │  agentic-   │                     │   agentic-   │   HTTP                │
│  │    loop     │ ◀───────────────── │    model     │ ◀────▶ Ollama        │
│  │             │   agent.response.*  │              │                       │
│  └──────┬──────┘                     └──────────────┘                       │
│         │                                                                    │
│         │ tool.execute.*                                                     │
│         ▼                                                                    │
│  ┌─────────────┐                                                            │
│  │  agentic-   │                                                            │
│  │   tools     │ ────▶ Tool Executors (file, graph, git)                   │
│  │             │                                                            │
│  └──────┬──────┘                                                            │
│         │ tool.result.*                                                      │
│         ▼                                                                    │
│  ┌─────────────┐                                                            │
│  │  agentic-   │                                                            │
│  │    loop     │ ────▶ agent.complete.*                                    │
│  └─────────────┘                                                            │
│                                                                              │
│  KV Buckets: AGENT_LOOPS, AGENT_TRAJECTORIES                               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

**agentic-loop**: Orchestrates state machine, tracks pending tools, captures trajectories
**agentic-model**: Routes requests to Ollama (OpenAI-compatible API), handles tool calling
**agentic-tools**: Executes tools with timeout, allowlist filtering

**State Machine**:
```
exploring → planning → architecting → executing → reviewing → complete
                                                            ↘ failed
                                                            ↘ paused
                                                            ↘ cancelled
```

States are fluid checkpoints - backward transitions allowed (except from terminal states).

---

## Decision 5: MCP at Boundary Only

**Context**: How should agents access tools?

**Decision**: MCP only for external integration, not internal communication

**Rationale**:
- MCP exists to fix interop problems we don't have internally
- Semspec executing its own tools shouldn't go through adapters
- NATS + direct calls are simpler and faster

**Implementation**:
- graph-gateway exposes /mcp for external tools (VS Code, other agents)
- Internal tool execution uses processor/tool-executor directly
- No MCP between Semspec components

---

## Decision 6: Training Flywheel as Core Feature

**Context**: How do we improve over time?

**Decision**: Every interaction captured for training

**Rationale**: (from research) No other tool does this well

**Implementation**:
```
Every agent request → stored
Every tool call → stored  
Every result → stored
Human feedback → stored

Approved results → training data export
Fine-tune SLMs → better performance
Repeat
```

This is why we build the agent - we control the data pipeline.

---

## Decision 7: Entity Schemas Incorporate Research Lessons

### Constitution Entity (from SpecKit)
```
constitution:{project}
  code_quality: [rules]
  testing: [rules]
  security: [rules]
  architecture: [rules]
  checked_at: timestamp
```

### Proposal Entity (from OpenSpec)
```
proposal:{id}
  title: string
  description: string
  status: exploring | drafted | ready | approved | rejected
  rationale: string  # why we're doing this
  impact: [entity_id]  # what's affected
  created_by: human | agent
```

### Source of Truth Separation (from OpenSpec)
```
spec:{id}
  status: current | proposed | superseded
  version: number
  
# Current specs in specs/ 
# Proposed changes link to current, show delta
```

### Task with Fluid States (from OpenSpec OPSX)
```
task:{id}
  status: exploring | planned | ready | in_progress | 
          blocked | review | complete | abandoned
  # No enforced sequence - can move between states
```

---

## Decision 8: Interaction Layer (Input + Router)

**Context**: How do users interact with the agentic system? How do we handle commands, signals, permissions?

**Decision**: Separate input components (channel adapters) + centralized router (policy enforcement)

**Rationale**:
- **Clean separation**: Inputs normalize channel-specific protocols (CLI, Slack, Discord)
- **Single policy point**: Router handles commands, permissions, context building
- **Moltbot pattern**: Proven architecture from multi-channel assistants
- **Extensibility**: Add new channels without changing agentic core

**Architecture**:
```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  Terminal ──► input/cli ──┐                                                 │
│  Slack    ──► input/slack ┼──► user.message.* ──► router ──► agent.task.*  │
│  Discord  ──► input/discord                         │                       │
│                                                     │                       │
│  Ctrl+C   ──────────────────► user.signal.*  ──────►│──► agent.signal.*    │
│  Reactions ─────────────────►                       │                       │
│                                                     │                       │
│  agent.complete.* ──────────────────────────────────┴──► user.response.*   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key Types**:
- `UserMessage`: Normalized input from any channel
- `UserSignal`: Control signals (cancel, pause, approve, reject)
- `UserResponse`: Output to send back to user
- `AgentSignal`: Signal forwarded to agentic-loop

**Commands**:
| Command | Signal | Description |
|---------|--------|-------------|
| `/cancel` | cancel | Stop execution immediately |
| `/pause` | pause | Pause at next checkpoint |
| `/resume` | resume | Continue paused loop |
| `/approve` | approve | Approve result |
| `/reject [reason]` | reject | Reject with reason |
| `/status` | - | Show loop status |
| `/loops` | - | List active loops |

**Permission Model**:
```json
{
  "roles": {
    "admin": ["*"],
    "user": ["submit", "view", "cancel_own", "feedback"],
    "reviewer": ["submit", "view", "cancel_own", "approve"]
  }
}
```

**Phase 1 Scope**: CLI input + basic router (single user = admin)

---



### 1. "Sea of Markdown" (SpecKit)
- **Problem**: Overwhelms developers with documentation
- **Our approach**: Minimal specs, graph tracks relationships
- **Metric**: If human has to read more than 1 page to understand task, it's too much

### 2. "Rigid Phase Gates" (SpecKit)  
- **Problem**: Feels like reinvented waterfall
- **Our approach**: Fluid workflows, checkpoints not gates
- **Test**: Can developer jump from explore → implement if they know what they want?

### 3. "Complexity Explosion" (BMAD v6)
- **Problem**: 50+ workflows, steep learning curve
- **Our approach**: Start simple, complexity is opt-in
- **Test**: Can someone use Semspec in first 5 minutes?

### 4. "Context Loss" (Everyone)
- **Problem**: AI forgets as projects grow
- **Our solution**: This is our main differentiator
- **Test**: After 100 tasks, does Semspec still know project context?

---

## What Semspec Is NOT

1. **Not another spec file format**: Graph stores relationships, not just text
2. **Not an MCP server waiting for Claude Code**: Semspec IS the agent
3. **Not SpecKit/OpenSpec/BMAD clone**: We have semantic understanding they lack
4. **Not single-model**: Specialized models for specialized tasks
5. **Not session-based**: Persistent memory across all interactions

---

## Summary: Our Position

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  OTHERS                              SEMSPEC                                │
│  ──────                              ───────                                │
│                                                                              │
│  Files + prompts                     Semantic knowledge graph              │
│  Session-based context               Persistent memory                     │
│  Generic agent                       Specialized roles                     │
│  Infrastructure for agents           IS the agent                          │
│  Rigid phases OR chaos               Fluid with checkpoints               │
│  Execute and forget                  Training flywheel                     │
│  Dev-only                            Domain-agnostic foundation            │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```
