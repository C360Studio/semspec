# Semspec Build Plan

**Version**: Draft v3
**Status**: Planning
**Approach**: Thin client on semstreams infrastructure

---

## Philosophy

Semspec is a **thin CLI client** that builds ON TOP OF semstreams. It does NOT embed or rebuild semstreams infrastructure.

**Key insight**: Semstreams already provides NATS, graph components, agentic processors, CLI input, router, and configuration. Semspec only adds domain-specific tool executors and vocabulary.

---

## Critical Architecture Constraint

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  SEMSTREAMS INFRASTRUCTURE (REQUIRED - not optional)                         │
│  ┌──────────┐  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   NATS   │  │ graph-ingest  │  │ agentic-loop │  │agentic-model │       │
│  │ JetStream│  │ graph-index   │  │              │  │   (Ollama)   │       │
│  └──────────┘  └───────────────┘  └──────────────┘  └──────────────┘       │
│                                                                              │
│  ┌──────────────┐  ┌──────────────┐                                         │
│  │ agentic-tools│  │    router    │  ← Commands registered here             │
│  │              │  │  input/cli   │                                         │
│  └──────────────┘  └──────────────┘                                         │
└─────────────────────────────────────────────────────────────────────────────┘
                              ▲
                              │ NATS pub/sub
                              │
┌─────────────────────────────┴──────────────────────────────────────────────┐
│  SEMSPEC (thin CLI client)                                                  │
│  • Tool executors (file, git) → register with agentic-tools                │
│  • Vocabulary definitions (semspec.proposal.*, semspec.task.*)             │
│  • NO embedded NATS, NO custom storage, NO config loader                   │
└────────────────────────────────────────────────────────────────────────────┘
```

### What Semstreams Provides (DO NOT REBUILD)

| Component | Purpose |
|-----------|---------|
| NATS JetStream | Messaging & persistence |
| graph-ingest | Entity/triple ingestion |
| graph-index | Entity querying |
| agentic-loop | Agent state machine orchestration |
| agentic-model | LLM calls (Ollama) |
| agentic-tools | Tool dispatch |
| input/cli | CLI input handling |
| router | Command routing with registration |
| Config loading | Flow-based configuration |

### What Semspec Provides (BUILD THIS)

| Component | Purpose |
|-----------|---------|
| Tool executors | file_read, file_write, git_status, etc. |
| Command registrations | Register with semstreams router |
| Vocabulary definitions | semspec.proposal.*, semspec.task.* predicates |

---

## Prerequisites

### Hardware Requirements

**Your Setup**: MacBook Pro M3 with 32GB unified memory ✓

Apple Silicon with unified memory is excellent for local LLMs - GPU can access most of the 32GB.

| Model | Size | Memory Usage | Speed on M3 |
|-------|------|--------------|-------------|
| `qwen2.5-coder:32b` | 32B | ~20GB | Good |
| `qwen2.5-coder:14b` | 14B | ~10GB | Fast |
| `deepseek-coder-v2:16b` | 16B | ~12GB | Fast |
| `codellama:34b` | 34B | ~22GB | Good |

**Recommended for M3 32GB**:
- **Primary**: `qwen2.5-coder:32b` - fits with room to spare
- **Fast iteration**: `qwen2.5-coder:14b` - when speed matters
- **Alternative**: `deepseek-coder-v2:16b` - different style

### Software Requirements

- **Semstreams** running via docker-compose (REQUIRED)
- **Go 1.22+** for building Semspec
- **Ollama** for running local models (has Metal/MPS support for Apple Silicon)
- **Git** for version control

### Setup Commands

```bash
# Install Ollama (Mac)
brew install ollama

# Or direct download
curl -fsSL https://ollama.com/install.sh | sh

# Start Ollama service
ollama serve

# Pull recommended models for M3 32GB
ollama pull qwen2.5-coder:32b      # Primary - strong reasoning + code
ollama pull qwen2.5-coder:14b      # Fast iteration
ollama pull deepseek-coder-v2:16b  # Alternative

# Verify
ollama list
curl http://localhost:11434/v1/models
```

**Performance Tips for Apple Silicon**:
- Close memory-hungry apps when running 32B models
- Ollama automatically uses Metal acceleration
- First inference is slow (model loading), subsequent calls fast
- Can run multiple smaller models simultaneously if needed

---

## Phase Overview

| Phase | Focus | Outcome |
|-------|-------|---------|
| 0 | Infrastructure | Semstreams running with all processors |
| 1 | Thin CLI client | Tool executors registered, tasks flow through semstreams |
| 2 | Knowledge layer | AST understanding via graph components |
| 3 | Multi-model | Specialized SLMs for different roles |
| 4 | Training flywheel | Capture, feedback, export, improve |
| 5 | Polish & integration | MCP interface, better UI |

---

## Phase 0: Semstreams Infrastructure (PREREQUISITE)

**Goal**: Semstreams running with NATS, graph components, and agentic processors

**This is not optional** - semspec cannot function without semstreams.

### 0.1 Start Semstreams

```bash
cd ../semstreams
docker-compose -f docker/e2e.yml up -d
```

### 0.2 Verify Services

```bash
# Check NATS
curl http://localhost:8222/healthz

# Check that processors are running
docker ps | grep semstreams
```

### 0.3 Configure Ollama

```bash
ollama serve
ollama pull qwen2.5-coder:32b
```

### 0.4 Acceptance Criteria

- [ ] NATS JetStream accessible at `nats://localhost:4222`
- [ ] graph-ingest processor running
- [ ] agentic-loop processor running
- [ ] agentic-model processor running (connected to Ollama)
- [ ] agentic-tools processor running
- [ ] router processor running

---

## Phase 1: Thin CLI Client

**Goal**: Semspec connects to semstreams and provides tool executors

**Outcome**: `semspec "add health check endpoint"` → task flows through semstreams → code generated

**What semspec builds**:
- NATS client connection (no embedded server)
- Tool executors (file, git)
- Tool registration with agentic-tools
- Vocabulary definitions for semspec entities

**What semstreams provides** (already exists):
- input/cli, router, agentic-loop, agentic-model, agentic-tools
- Graph components for entity storage
- Configuration loading

### 1.1 NATS Connection

Semspec connects to semstreams NATS (no embedded server):

```go
// main.go - require NATS URL
if natsURL == "" {
    return fmt.Errorf("NATS URL required - semstreams must be running")
}

// app.go - connect to external NATS
nc, err := nats.Connect(natsURL)
```

### 1.2 Interaction Layer (PROVIDED BY SEMSTREAMS)

These components already exist in semstreams - do NOT rebuild:

**input/cli** (semstreams):
- Reads from terminal, publishes `user.message.cli.*`
- Handles Ctrl+C → publishes `user.signal.cancel.*`
- Renders `user.response.cli.*` to terminal

**router** (semstreams):
- Parses commands (`/cancel`, `/status`, `/help`)
- Routes messages to `agent.task.*`
- Routes signals to `agent.signal.*`
- Delivers `agent.complete.*` to `user.response.*`

Semspec may need to **register commands** with the router - check semstreams docs for registration mechanism.

### 1.2 CLI Interface

```bash
# One-shot mode
semspec "add health check endpoint"

# REPL mode
semspec
semspec> add health check endpoint
[loop:abc123] Understanding request...
semspec> /status
Loop: abc123, State: executing, Iteration: 2
semspec> /cancel
[loop:abc123] Cancelled
semspec> 

# Options
semspec --model qwen2.5-coder:32b "add health endpoint"
```

**Implementation**:
- Go CLI using cobra
- input/cli component for normalization
- router component for dispatch

### 1.3 Agentic Components (existing code)

Use the agentic components already implemented:

**agentic-loop**: Orchestrates state machine, handles signals
- Subscribe to `agent.task.*`, `agent.signal.*`
- Publish to `agent.request.*`, `tool.execute.*`, `agent.complete.*`
- Track pending tools, iteration limits
- Handle cancel/pause signals

**agentic-model**: Calls Ollama
- Subscribe to `agent.request.*`
- Publish to `agent.response.*`
- OpenAI-compatible API (works with Ollama)

**agentic-tools**: Executes tools
- Subscribe to `tool.execute.*`
- Publish to `tool.result.*`
- Timeout handling, allowlist filtering

### 1.4 Tool Executors

**Minimal tools for Phase 1**:

| Tool | Description |
|------|-------------|
| `file_read` | Read file contents |
| `file_write` | Write/create file |
| `file_list` | List directory |
| `git_status` | Check repo status |
| `git_commit` | Commit changes |
| `git_branch` | Create/switch branch |

**Implementation**:
```go
type FileTools struct {
    repoPath string
}

func (t *FileTools) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
    switch call.Name {
    case "file_read":
        return t.fileRead(ctx, call)
    case "file_write":
        return t.fileWrite(ctx, call)
    // ...
    }
}

func (t *FileTools) ListTools() []agentic.ToolDefinition {
    return []agentic.ToolDefinition{
        {Name: "file_read", Description: "Read file contents", ...},
        {Name: "file_write", Description: "Write file contents", ...},
    }
}
```

### 1.5 Basic Entity Storage

**Minimal entities for Phase 1**:
- `proposal:{id}` - what we're doing
- `task:{id}` - units of work  
- `result:{id}` - what was produced

**Storage**: NATS KV (AGENT_LOOPS, AGENT_TRAJECTORIES already exist)

### 1.6 Model Configuration

```json
{
  "models": {
    "planner": {
      "provider": "ollama",
      "endpoint": "http://localhost:11434/v1",
      "model": "qwen2.5-coder:32b",
      "temperature": 0.2
    },
    "implementer": {
      "provider": "ollama", 
      "endpoint": "http://localhost:11434/v1",
      "model": "qwen2.5-coder:32b",
      "temperature": 0.1
    }
  }
}
```

### 1.7 Test End-to-End

**Test cases**:
```
semspec "add a README.md file"
  → Creates README.md with project description

semspec "add a /health endpoint to server.go"
  → Modifies server.go, adds health handler

Ctrl+C during execution
  → Loop cancelled, clean exit
```

**Acceptance criteria**:
- [ ] CLI starts and accepts input
- [ ] Commands parsed (/cancel, /status, /help)
- [ ] Ollama called with context
- [ ] Tool calls executed correctly
- [ ] Files created/modified as expected
- [ ] Cancel signal stops execution
- [ ] Status shows loop state

---

## Phase 2: Knowledge Layer

**Goal**: Semspec understands codebase structure, persists knowledge

**Duration**: 3-4 weeks

**Outcome**: Queries like "what calls this function?" work instantly

### 2.1 AST Processor

**Purpose**: Parse code into entities with relationships

**Initial scope**: Go only (we're building SemStreams in Go)

```go
type ASTProcessor struct {
    entityStore *EntityStore
}

func (a *ASTProcessor) ProcessFile(path string) error {
    // Parse Go file
    fset := token.NewFileSet()
    node, _ := parser.ParseFile(fset, path, nil, parser.ParseComments)
    
    // Create file entity
    a.entityStore.Put("code:"+path, CodeEntity{
        File:     path,
        Language: "go",
        Package:  node.Name.Name,
    })
    
    // Extract symbols
    ast.Inspect(node, func(n ast.Node) bool {
        switch x := n.(type) {
        case *ast.FuncDecl:
            a.processFunc(path, x)
        case *ast.TypeSpec:
            a.processType(path, x)
        }
        return true
    })
    
    // Extract relationships (calls, imports)
    // ...
}
```

### 2.2 Relationship Extraction

**Relationships to extract**:

| Relationship | Example |
|--------------|---------|
| `calls` | `func A` calls `func B` |
| `imports` | `file X` imports `package Y` |
| `implements` | `type T` implements `interface I` |
| `embeds` | `type T` embeds `type S` |
| `uses` | `func A` uses `type T` |

### 2.3 Initial Scan

**On project init**:
```bash
semspec init /path/to/project
  Scanning...
  Found 147 Go files
  Extracted 892 symbols
  Mapped 2,341 relationships
  Ready.
```

### 2.4 File Watcher

**Keep graph in sync**:
```go
type FileWatcher struct {
    astProcessor *ASTProcessor
    watcher      *fsnotify.Watcher
}

func (f *FileWatcher) Watch() {
    for event := range f.watcher.Events {
        if isGoFile(event.Name) {
            f.astProcessor.ProcessFile(event.Name)
        }
    }
}
```

### 2.5 Enhanced Queries

**Context building now uses graph**:

```go
func (p *Planner) buildContext(request string) Context {
    // Extract likely relevant entities
    keywords := extractKeywords(request)
    
    // Query graph
    relevant := p.queryEngine.Query(
        "entities matching " + strings.Join(keywords, " OR ")
    )
    
    // Get related entities
    for _, e := range relevant {
        related := p.queryEngine.Query(
            "entities that call or are called by " + e.ID
        )
        // ...
    }
    
    return Context{
        Entities: relevant,
        Related:  related,
        Constitution: p.getConstitution(),
    }
}
```

### 2.6 Constitution Entity

**Add project constraints**:

```bash
semspec constitution init
  Creating constitution for semstreams...
  
  Code quality rules? 
  > All public functions need docs
  > No function over 50 lines
  
  Testing rules?
  > New code needs tests
  
  Saved: constitution:semstreams
```

### 2.7 Test Knowledge Layer

**Test cases**:
```
semspec "what calls the Refresh method?"
  → Lists all callers with file:line

semspec "what would break if I change Token struct?"
  → Impact analysis showing affected code

semspec "show me all authentication-related code"
  → Semantic search across codebase
```

**Acceptance criteria**:
- [ ] All Go files parsed on init
- [ ] Symbols extracted correctly
- [ ] Relationships mapped
- [ ] Queries return relevant results
- [ ] File changes update graph
- [ ] Constitution enforced

---

## Phase 3: Multi-Model / Specialized Roles

**Goal**: Different models for different tasks (all local), architect/editor split

**Duration**: 3-4 weeks

**Outcome**: Planner uses larger model, implementer uses faster model; architect spawns editor

**Note**: Since we're local-first from Phase 1, Phase 3 is about *specialization*, not switching from cloud to local.

### 3.1 Architect/Editor Split

Implement the architect/editor pattern (from Aider research - SOTA results):

**Architect role**:
- Reasons about how to solve the problem
- Queries graph for existing patterns
- Produces detailed plan

**Editor role**:
- Receives architect's plan
- Translates into actual file edits
- Spawned automatically when architect completes

```go
// In agentic-loop handleModelResponse
if entity.Role == "architect" && response.Status == "complete" {
    // Architect complete - spawn editor
    editorLoopID := spawnEditorLoop(entity, response.Message.Content)
    // Editor receives architect output as context
}
```

### 3.2 Role Configuration

```json
{
  "roles": {
    "planner": {
      "model": "qwen2.5-coder:32b",
      "provider": "ollama",
      "tools": ["graph_query", "ast_query", "read_doc"],
      "system_prompt": "You are a senior engineer planning changes..."
    },
    "architect": {
      "model": "qwen2.5-coder:32b",
      "provider": "ollama", 
      "tools": ["graph_query", "ast_query", "file_read"],
      "system_prompt": "You design the technical approach...",
      "spawns_editor": true
    },
    "editor": {
      "model": "qwen2.5-coder:32b",
      "provider": "ollama",
      "tools": ["file_read", "file_write", "git_*"],
      "system_prompt": "You implement the architect's plan..."
    },
    "reviewer": {
      "model": "qwen2.5-coder:32b",
      "provider": "ollama",
      "tools": ["graph_query", "ast_query", "file_read"],
      "system_prompt": "You review code for quality..."
    }
  }
}
```

**Model Selection Strategy**:
- **Planner**: Needs strong reasoning → larger model (32b)
- **Architect**: Design decisions → larger model (32b)
- **Editor**: Code generation → larger model (32b)
- **Fast tasks**: Can use smaller model (14b)

### 3.3 Task Routing in Router

```go
func (r *Router) buildTask(msg agentic.UserMessage, ctx *TaskContext) agentic.TaskMessage {
    // Determine role from request type
    role := r.determineRole(msg.Content)
    
    return agentic.TaskMessage{
        LoopID: uuid.New().String(),
        TaskID: uuid.New().String(),
        Role:   role,
        Model:  r.config.Roles[role].Model,
        Prompt: msg.Content,
    }
}

func (r *Router) determineRole(content string) string {
    // Simple heuristics for Phase 3
    if strings.Contains(content, "design") || strings.Contains(content, "architect") {
        return "architect"
    }
    if strings.Contains(content, "review") {
        return "reviewer"
    }
    return "planner"  // Default
}
```

### 3.4 agentic-loop Updates

Add role-based behavior:

```go
func (c *Component) handleModelResponse(ctx context.Context, data []byte) {
    // ... existing code ...
    
    switch response.Status {
    case "complete":
        // Check if this role spawns another
        if c.config.Roles[entity.Role].SpawnsEditor {
            editorLoopID := c.spawnEditorLoop(entity, response.Message.Content)
            // Mark architect as complete but continue with editor
        }
        // ... rest of completion handling
    }
}
```

### 3.5 Test Multi-Model

**Test cases**:
```
semspec "add caching to query engine"
  → Planner (qwen2.5-coder:32b): understands, creates proposal
  → Architect (qwen2.5-coder:32b): designs approach
  → Editor (qwen2.5-coder:32b): implements code
  → Reviewer (qwen2.5-coder:32b): checks quality
  
semspec --role architect "design the auth system"
  → Directly starts with architect role
```

**Acceptance criteria**:
- [ ] Different roles serve different purposes
- [ ] Architect spawns editor automatically
- [ ] Tool access restricted by role
- [ ] Handoff between roles works
- [ ] Output quality maintained

---

## Phase 4: Training Flywheel

**Goal**: Every interaction improves the system

**Duration**: 2-3 weeks

**Outcome**: Export approved trajectories, fine-tune SLMs

### 4.1 Trajectory Capture

**Store every step**:
```json
{
  "request_id": "req-123",
  "task": "task:implement-auth",
  "role": "implementer",
  "steps": [
    {
      "type": "model_call",
      "input": "...",
      "output": "...",
      "tokens": { "in": 1523, "out": 892 }
    },
    {
      "type": "tool_call",
      "tool": "file_read",
      "args": { "path": "auth/token.go" },
      "result": "..."
    }
  ]
}
```

### 4.2 Feedback Storage

```go
type Result struct {
    // ...
    Feedback        string
    FeedbackType    string  // approved, rejected, needs_revision
    FeedbackBy      string
    FeedbackAt      time.Time
    TrainingEligible bool
}
```

### 4.3 Training Export

```bash
semspec training export --role implementer --since 2025-01-01
  Exporting approved trajectories...
  Found 234 eligible results
  Output: training-data/implementer-2025-01.jsonl
```

**Export format**:
```json
{
  "task_type": "implement",
  "input": {
    "system": "You are an implementer...",
    "context": "...",
    "task": "..."
  },
  "trajectory": [...],
  "output": "final code",
  "feedback": "approved"
}
```

### 4.4 Metrics Dashboard

Track:
- Tasks completed per role
- Approval rate
- Time to completion
- Tool usage patterns
- Token costs

### 4.5 Test Flywheel

**Acceptance criteria**:
- [ ] All trajectories captured
- [ ] Feedback stored with results
- [ ] Export produces valid training data
- [ ] Metrics visible

---

## Phase 5: Polish & Integration

**Goal**: Ready for real use, optional multi-channel support

**Duration**: 2 weeks

**Outcome**: Stable CLI, MCP interface, optional Slack/Discord/Web

### 5.1 CLI Polish

- Better output formatting (colors, progress)
- Progress indicators during long operations
- Clear error messages
- `--verbose` mode for debugging
- `--quiet` mode for scripts

### 5.2 Additional Input Channels (Optional)

**input/slack**:
- Integrate with Slack workspaces
- Mention-based activation (@semspec)
- Thread-based conversations
- Reactions for signals (✅ approve, ❌ reject)

**input/discord**:
- Integrate with Discord servers
- Channel-based or DM interaction
- Embeds for rich output

**input/web**:
- REST API for programmatic access
- WebSocket for real-time updates
- Simple web UI for status/history

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  PHASE 5: MULTI-CHANNEL                                                     │
│                                                                              │
│  Terminal ──► input/cli ──────┐                                             │
│  Slack    ──► input/slack ────┼──► router ──► agentic-* ──► user.response.*│
│  Discord  ──► input/discord ──┤                                             │
│  HTTP/WS  ──► input/web ──────┘                                             │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.3 MCP Interface

**For VS Code / other tool integration**:

```go
// MCP tools exposed
- semspec_plan      // Create proposal
- semspec_status    // Check status  
- semspec_query     // Query knowledge graph
- semspec_implement // Execute task
- semspec_cancel    // Cancel active loop
```

### 5.4 Multi-User Permissions

Expand permission system for teams:

```json
{
  "permissions": {
    "roles": {
      "admin": ["*"],
      "developer": ["submit", "view", "cancel_own", "feedback"],
      "reviewer": ["submit", "view", "cancel_own", "approve", "feedback"],
      "readonly": ["view"]
    },
    "users": {
      "coby": "admin",
      "alice": "reviewer",
      "bob": "developer"
    },
    "channel_overrides": {
      "slack.C_CODE_REVIEW": {
        "default_role": "reviewer"
      }
    }
  }
}
```

### 5.5 Documentation

- Getting started guide
- Configuration reference
- How to train SLMs
- Troubleshooting
- Channel setup guides (Slack, Discord)

### 5.6 Constitution Templates

Pre-built constitutions:
- Go project
- TypeScript project
- Generic
- SemStreams-specific

### 5.7 Test Production Use

**Acceptance criteria**:
- [ ] Self-hosting: Semspec develops Semspec
- [ ] Stable over 100+ tasks
- [ ] Documentation complete
- [ ] MCP integration works
- [ ] (Optional) Slack/Discord channels work
- [ ] Multi-user permissions enforced

---

## Dependencies

```
Phase 1 ─────► Phase 2 ─────► Phase 3
                  │              │
                  │              ▼
                  └─────────► Phase 4
                                 │
                                 ▼
                             Phase 5
```

- Phase 1: No dependencies (start here)
- Phase 2: Requires Phase 1 working
- Phase 3: Requires Phase 2 (needs graph for context)
- Phase 4: Requires Phase 2-3 (needs trajectories)
- Phase 5: Requires all above

---

## Success Criteria (MVP = Phase 1-2)

**Phase 1 complete when**:
- [ ] `semspec "add X"` produces working code
- [ ] Git integration works (branch, commit)
- [ ] Basic entities stored

**Phase 2 complete when**:
- [ ] Codebase fully indexed
- [ ] "What calls X?" queries work
- [ ] Constitution checks work
- [ ] Context includes relevant code

**MVP complete when Phase 1-2 done.**

---

## Open Questions

| Question | Phase | Notes |
|----------|-------|-------|
| Which local models work best? | 1 | Test qwen2.5-coder, deepseek-coder, codellama |
| GPU memory requirements? | 1 | 32b models need ~20GB VRAM |
| LiteLLM vs direct Ollama? | 1 | LiteLLM adds flexibility, Ollama is simpler |
| Fine-tune base models? | 4 | After collecting training data |
| Multi-language AST? | 2+ | Start Go-only, add tree-sitter later |
| Human review UI? | 5 | CLI first, web UI if needed |

---

## What We're NOT Building (Yet)

1. **Web UI** - CLI is fine for MVP
2. **Multi-language support** - Go first
3. **Team collaboration** - Single user first
4. **Cloud deployment** - Local first
5. **MCP server** - After CLI works

---

## Comparison to Original Plan

| Original (SemMem) | Revised (Semspec) |
|-------------------|-------------------|
| Infrastructure-first | Agent-first |
| MCP for Claude Code | CLI is primary interface |
| Phase 1: Core components | Phase 1: Working agent |
| Wait for external agents | BE the agent |
| Complex multi-service | Single binary |
