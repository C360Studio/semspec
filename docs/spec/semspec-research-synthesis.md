# Semspec Research: State of the Art Analysis

**Purpose**: Capture lessons learned from existing spec-driven and multi-agent development tools to inform Semspec design.

---

## Executive Summary

The AI-assisted development landscape has converged on a key insight: **structure before code**. Multiple tools (SpecKit, OpenSpec, BMAD) have emerged to address the same fundamental problem - AI coding assistants are powerful but unpredictable when requirements live only in chat history.

Our advantage: We have a semantic knowledge graph that can solve the context/memory problem these tools struggle with. We're not building another spec management CLI - we're building an agent with persistent understanding.

---

## Tool Analysis

### 1. GitHub Spec Kit

**What it is**: Open-source CLI toolkit for spec-driven development. Uses slash commands (/speckit.specify, /speckit.plan, /speckit.implement).

**Architecture**:
```
/speckit.constitution → Project principles (guardrails)
/speckit.specify     → Generate specification from intent
/speckit.clarify     → AI asks clarifying questions
/speckit.plan        → Tech stack and architecture
/speckit.tasks       → Implementation breakdown
/speckit.implement   → Execute all tasks
```

**Key Concepts**:
- **Constitution**: Immutable principles governing all development (code quality, testing standards, security policies)
- **Multi-step refinement**: Not one-shot code generation
- **Intent-driven**: Specifications define "what" before "how"

**Problems Reported**:
- "Sea of markdown documents" - overwhelms developers
- Long agent run-times (13+ minutes for implementation)
- Rigid phase gates - can't iterate freely
- When spec is followed but result is wrong, unclear how to proceed
- Python setup complexity

**Lessons**:
- ✅ Constitution concept is valuable (project-wide constraints)
- ✅ Separating intent from implementation works
- ❌ Rigid phase gates frustrate real development
- ❌ Too much markdown/documentation overhead
- ❌ "Reinvented waterfall" criticism

---

### 2. OpenSpec (Fission AI)

**What it is**: Lightweight, portable spec framework. Emphasizes "brownfield-first" (existing codebase evolution, not just greenfield).

**Architecture**:
```
openspec/
├── specs/           # Source of truth (current system state)
│   └── auth/
│       └── spec.md
└── changes/         # Proposed changes (isolated)
    └── add-2fa/
        ├── proposal.md
        ├── tasks.md
        ├── design.md (optional)
        └── specs/   # Delta showing additions
```

**Key Concepts**:
- **Source of Truth vs Change Proposals**: Physical separation in filesystem
- **AGENTS.md**: "README for robots" - instructions AI agents follow
- **OPSX**: Fluid, iterative workflow - no rigid phases
- **Brownfield-first**: Designed for 1→n, not just 0→1
- **Archive workflow**: Changes merge back into specs

**OPSX Evolution** (important lessons):
```
Original Problem:
- Instructions hardcoded in TypeScript
- All-or-nothing commands
- Fixed structure for everyone
- Black box - can't tweak prompts when AI output is bad

OPSX Solution:
- Schema-driven workflows (customizable)
- Test artifacts independently  
- Iterate quickly on templates
- /opsx:explore for thinking before committing
```

**Lessons**:
- ✅ Brownfield-first is crucial (most dev is 1→n)
- ✅ Separating truth from proposals prevents accidents
- ✅ AGENTS.md concept - portable AI instructions
- ✅ Fluid iteration (/opsx:explore → /opsx:ff → /opsx:apply)
- ✅ Schema-driven workflows allow customization
- ❌ Still file-based, no semantic understanding
- ❌ Context still scoped to current change folder

---

### 3. BMAD Method

**What it is**: Multi-agent framework with specialized AI personas (Analyst, PM, Architect, Dev, QA, Scrum Master).

**Architecture**:
```
Two Pillars:
1. Agentic Planning - specialized agents create artifacts
2. Context-Engineered Development - epic sharding for context

Agent Roles:
- Analyst: Requirements gathering
- Product Manager: PRD creation
- Architect: System design, tech choices
- Product Owner: Story acceptance
- Scrum Master: Task breakdown (epic sharding)
- Developer: Implementation
- QA: Testing and validation
```

**Key Concepts**:
- **Virtual Team**: AI agents mimic real agile team roles
- **Epic Sharding**: Break PRD into self-contained dev units to preserve context
- **Context-Engineered Development**: Each story embeds full context
- **Party Mode**: Multiple agents in one session for collaboration
- **Agent-as-Code**: Agents defined in Markdown/YAML files
- **Scale-Adaptive**: Adjusts depth based on project complexity

**Advanced Elicitations** (collaborative techniques):
- Role-Playing: AI adopts user personas to critique features
- Six Thinking Hats: Analyze from multiple perspectives
- Stress-testing: Challenge plans before implementation

**BMAD V6 Innovations**:
- **BMad Core**: Modular platform with pluggable modules
- **Web Bundles**: Compile agents to portable text files
- **Step-file Architecture**: 90% token savings
- **Custom Language**: Define domain-specific agent behavior

**Lessons**:
- ✅ Specialized agents outperform generalist
- ✅ Epic sharding solves context loss
- ✅ Human-as-facilitator model (guide, don't command)
- ✅ Agent-as-Code enables customization
- ✅ Scale-adaptive complexity
- ✅ Version control for all artifacts (audit trail)
- ❌ Complex setup (50+ workflows in v6)
- ❌ "More complex than v4" - learning curve
- ❌ Still prompt-based, not semantically grounded

---

### 4. Aider

**What it is**: Terminal-based AI pair programming tool with deep git integration.

**Architecture**:
```
Key Components:
- Repository Map: Function signatures + file structures
- Chat Modes: code, architect, ask
- Git Integration: Auto-commits with descriptive messages
- Multi-LLM Support: Claude, GPT-4o, DeepSeek, local models
```

**Key Concepts**:
- **Repository Map**: Gives LLM context about entire codebase
- **Architect/Editor Split**: Architect describes solution, Editor translates to edits (SOTA benchmark results)
- **Chat Modes**:
  - Code mode: Direct file edits
  - Architect mode: Plan before implementing
  - Ask mode: Consult without changes

**Lessons**:
- ✅ Repository map for codebase understanding
- ✅ Architect/Editor split improves quality
- ✅ Git-native workflow (auto-commits)
- ✅ Multiple modes for different tasks
- ✅ Terminal-first, editor-agnostic
- ❌ No persistent memory across sessions
- ❌ Single-agent, no coordination
- ❌ Context window limits still apply

---

### 5. Claude Code

**What it is**: Anthropic's agentic coding tool with autonomous multi-step execution.

**Strengths**:
- Level 4 autonomy (execute multi-step plans)
- Iterate on failures
- Complete entire features
- Deep reasoning capability
- Bash and editor tools

**Limitations** (from your experience):
- Can't control plan file location
- Plan mode limitations
- Session-based context (no persistent memory)

**Lessons**:
- ✅ Proven agentic capabilities
- ✅ Multi-step execution works
- ✅ Strong reasoning model
- ❌ Limited customization of workflows
- ❌ No semantic understanding of codebase
- ❌ Context resets between sessions

---

## Cross-Cutting Patterns

### Pattern 1: Structure Before Code
All tools agree: capture intent before implementation.

```
SpecKit:     constitution → specify → plan → tasks → implement
OpenSpec:    proposal → specs → design → tasks → apply → archive
BMAD:        brief → PRD → architecture → stories → implement → QA
```

**Our approach**: Plans, specs, tasks as entities in knowledge graph - not just files.

---

### Pattern 2: Context Preservation
The #1 problem everyone is solving: AI "forgets" as projects grow.

| Tool | Solution | Limitation |
|------|----------|------------|
| SpecKit | Constitution + specs | File-based, no semantic links |
| OpenSpec | AGENTS.md + change folders | Scoped to current change |
| BMAD | Epic sharding + step files | Complex, still text-based |
| Aider | Repository map | Per-session only |

**Our advantage**: Semantic knowledge graph with:
- Entity relationships (what uses what)
- AST-based code understanding
- Persistent memory across sessions
- Query-able context

---

### Pattern 3: Specialized Agents
BMAD proves specialized agents outperform generalists.

| Agent | Focus | Value |
|-------|-------|-------|
| Analyst | Requirements | Catches missing requirements |
| Architect | Design | Ensures consistency |
| Developer | Implementation | Focused context |
| QA | Testing | Different perspective |

**Our approach**: Role-based SLMs with constrained tool access:
- spec-writer: graph_query, ast_query, read_doc
- implementer: + file ops, git ops
- reviewer: read-only tools

---

### Pattern 4: Fluid vs Rigid Workflows
SpecKit criticism: "reinvented waterfall"
OpenSpec response: OPSX with no rigid phases

**Key insight**: Real development is iterative, not linear.

```
What people want:
- Explore ideas freely
- Commit when ready
- Iterate at any phase
- Human checkpoints, not phase gates

What doesn't work:
- Forced sequential phases
- Can't go back to refine
- All-or-nothing commands
```

**Our approach**: Entity state machines with human intervention points, not linear workflows.

---

### Pattern 5: Brownfield > Greenfield
OpenSpec insight: Most development is 1→n, not 0→1.

**Implications**:
- Must understand existing code (AST entities)
- Changes must be isolated (proposals separate from truth)
- Impact analysis matters (what breaks if I change this?)

**Our advantage**: Knowledge graph tracks:
- What code exists
- What calls what
- What would be affected by changes

---

## What We Can Borrow

### From SpecKit
- **Constitution concept**: Project-wide constraints that apply to all work
- **Self-assessment**: Specs checked against constitution before approval

### From OpenSpec  
- **AGENTS.md pattern**: Portable instructions for AI agents
- **Brownfield-first**: Design for existing codebases
- **Source of truth separation**: Current state vs proposed changes
- **Archive workflow**: Changes merge into specs when approved

### From BMAD
- **Specialized agent roles**: Different models for different tasks
- **Epic sharding**: Break large work into context-preserving units
- **Human-as-facilitator**: Collaborative not transactional
- **Advanced elicitations**: Role-playing, Six Hats for better requirements
- **Agent-as-Code**: Agents defined as configuration, not hardcoded

### From Aider
- **Repository map**: Give agent structural understanding
- **Architect/Editor split**: Planning and execution as separate concerns
- **Git-native**: Every change tracked automatically

---

## What We Do Differently

### 1. Semantic Knowledge Graph (not files)
```
Others:
  openspec/specs/auth.md → plain text file
  
Semspec:
  spec:auth-token-refresh
    plan: plan:auth-refactor
    content: objectstore:specs/auth-token-refresh.md
    status: approved
    implemented_by: [code:auth/token.go, code:auth/refresh.go]
    depends_on: [spec:auth-base]
```

The graph KNOWS relationships. Query: "what specs affect auth module?" - instant answer.

### 2. Persistent Memory (not session-based)
```
Others:
  New session = lost context
  Workaround: AGENTS.md, constitution files
  
Semspec:
  Knowledge persists in graph
  Every session starts with full context
  Agent queries graph for relevant history
```

### 3. Training Flywheel (not just execution)
```
Others:
  Generate code, move on
  
Semspec:
  Every trajectory captured
  Human feedback stored
  Approved results → training data
  SLMs improve over time
```

### 4. Multi-Domain Foundation (not dev-only)
```
Others:
  Built for software development
  
Semspec:
  Built on SemStreams
  Same patterns apply to:
    - Maritime operations (Ocean)
    - Robotics (Ops)
    - Any domain with plans → tasks → execution
```

---

## Risks to Avoid

### 1. "Sea of Markdown" (SpecKit problem)
- Don't overwhelm with documentation
- Generate minimal viable specs
- Let graph track relationships, not humans

### 2. "Rigid Phase Gates" (SpecKit problem)
- Allow iteration at any point
- Human checkpoints, not enforced sequences
- /explore → commit when ready

### 3. "Complexity Explosion" (BMAD v6 problem)
- Start simple, add complexity when needed
- Scale-adaptive by default
- Don't require 50+ workflows to get started

### 4. "Context Loss" (everyone's problem)
- This is our main advantage
- Knowledge graph preserves everything
- Query-able, not buried in files

---

## Semspec Design Principles

Based on this research:

1. **Graph-first, files second**: Entities and relationships are primary; files are artifacts
2. **Persistent context**: Every session has full project knowledge
3. **Fluid workflows**: Explore freely, commit when ready
4. **Specialized agents**: Right model for right task
5. **Human checkpoints, not gates**: Review when it matters, don't block progress
6. **Brownfield-native**: Designed for existing codebases
7. **Training-aware**: Every interaction improves the system
8. **Domain-agnostic foundation**: Patterns work beyond dev

---

## Next Steps

1. **Define entity schemas** incorporating lessons (constitution, proposals, specs)
2. **Design workflow state machine** with fluid transitions
3. **Map agent roles** to SLM capabilities and tool access
4. **Implement repository understanding** (AST entities, relationships)
5. **Build MVP** with single-agent first, multi-agent later

---

## References

- [SpecKit](https://github.com/github/spec-kit) - GitHub's spec-driven toolkit
- [OpenSpec](https://github.com/Fission-AI/OpenSpec) - Lightweight portable framework
- [BMAD Method](https://github.com/bmad-code-org/BMAD-METHOD) - Multi-agent framework
- [Aider](https://aider.chat) - Terminal AI pair programming
- [Open Responses](https://openresponses.org) - Agentic loop specification
