# Semspec Application Specification

**Version**: Draft v2  
**Status**: Planning  
**Repository**: `semspec/`

---

## Overview

Semspec is a semantic dev agent that understands your codebase through a knowledge graph, coordinates specialized models for different tasks, and learns from every interaction.

**Core Value Proposition**: Unlike tools that patch gaps in existing agents (SpecKit, OpenSpec, BMAD), Semspec IS the agent - with persistent semantic understanding that other tools lack.

---

## Interface

### CLI (Primary)

```bash
# Start interactive session
semspec

# Quick commands
semspec "add token refresh to auth"
semspec status
semspec review task:123

# Modes
semspec explore "how should we handle session timeout?"
semspec plan "add 2FA support"
semspec implement task:456
```

### Interactive Session

```
$ semspec
Semspec v0.1.0 | Project: semstreams | 847 code entities

> add a health check endpoint to the gateway

Understanding request...
  ✓ Found gateway at gateway/graph/server.go
  ✓ Found existing endpoints: /graphql, /mcp
  ✓ Constitution allows: new endpoints must have tests

I'll create:
  1. GET /health endpoint returning service status
  2. Include: uptime, NATS connection, entity count
  3. Unit test for the endpoint

Proceed? [Y/n/refine] y

Creating proposal... ✓
Writing spec... ✓
Implementing...
  ✓ gateway/graph/health.go (new)
  ✓ gateway/graph/health_test.go (new)
  ✓ gateway/graph/server.go (modified: +2 lines)

Ready for review. Run `semspec review` or check PR #47.
```

---

## Interaction Architecture

### Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  USER INPUT                  ROUTER                    AGENTIC SYSTEM       │
│                                                                              │
│  Terminal ──► input/cli ──┐                                                 │
│                           │                           ┌─────────────┐       │
│  [Future]                 │  user.message.*          │             │       │
│  Slack    ──► input/slack ┼────────────────► router ─┼► agentic-   │       │
│  Discord  ──► input/discord                    │      │    loop     │       │
│  Web      ──► input/web ──┘                    │      │             │       │
│                                                │      └──────┬──────┘       │
│  Ctrl+C  ───────────────────► user.signal.* ──┘             │              │
│                                                              │              │
│                                                              ▼              │
│  user.response.* ◄────────────────────────── agent.complete.*              │
│        │                                                                    │
│        └──► CLI prints / Slack posts / Discord embeds                      │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Commands

| Command | Description | Example |
|---------|-------------|---------|
| `/cancel [loop_id]` | Stop execution | `/cancel` or `/cancel abc123` |
| `/status [loop_id]` | Show loop status | `/status` |
| `/pause [loop_id]` | Pause at checkpoint | `/pause` |
| `/resume [loop_id]` | Resume paused loop | `/resume` |
| `/approve [loop_id]` | Approve pending result | `/approve` |
| `/reject [loop_id] [reason]` | Reject with reason | `/reject needs tests` |
| `/loops` | List active loops | `/loops` |
| `/history [n]` | Recent loop history | `/history 10` |
| `/help` | Show commands | `/help` |

### Signals

```go
const (
    SignalCancel   = "cancel"    // Stop immediately
    SignalPause    = "pause"     // Pause at checkpoint
    SignalResume   = "resume"    // Continue paused
    SignalApprove  = "approve"   // Approve result
    SignalReject   = "reject"    // Reject with reason
    SignalFeedback = "feedback"  // Add feedback
)
```

### CLI Signal Mapping

| User Action | Signal |
|-------------|--------|
| Ctrl+C | `cancel` (if active loop) |
| `/cancel` | `cancel` |
| `/approve` | `approve` |
| `/reject reason` | `reject` with payload |

### Slack Signal Mapping (Future)

| User Action | Signal |
|-------------|--------|
| ✅ reaction | `approve` |
| ❌ reaction | `reject` |
| 🛑 reaction | `cancel` |
| ⏸️ reaction | `pause` |
| ▶️ reaction | `resume` |

---

## Entity Schemas

### constitution:{project}

Project-wide constraints checked during all work. (Borrowed from SpecKit)

```json
{
  "id": "constitution:semstreams",
  "type": "constitution",
  "project": "semstreams",
  
  "code_quality": [
    "All public functions must have doc comments",
    "No function longer than 50 lines without justification",
    "Errors must be wrapped with context"
  ],
  
  "testing": [
    "New code requires tests",
    "Integration tests for component interactions",
    "No test file larger than 500 lines"
  ],
  
  "security": [
    "No secrets in code",
    "All external inputs validated",
    "No CGO dependencies without approval"
  ],
  
  "architecture": [
    "Components follow SemStreams patterns",
    "No circular dependencies between packages",
    "Interfaces defined in consuming package"
  ],
  
  "updated_at": "2025-01-27T..."
}
```

**Usage**: Specs are validated against constitution before approval.

---

### proposal:{id}

Why we're doing something and what's changing. (Borrowed from OpenSpec)

```json
{
  "id": "proposal:auth-token-refresh",
  "type": "proposal",
  "title": "Add Token Refresh Support",
  
  "status": "exploring | drafted | ready | approved | rejected | abandoned",
  
  "description": "Add refresh token support to prevent session interruption",
  "rationale": "Users report being logged out during long operations",
  
  "impact": [
    "code:auth/token.go",
    "code:gateway/auth.go",
    "spec:auth-session"
  ],
  
  "questions": [
    "Should refresh tokens be stored server-side or stateless?",
    "What's the refresh token lifetime?"
  ],
  
  "decisions": [
    {
      "question": "Storage approach",
      "decision": "Stateless JWT with rotation",
      "rationale": "Simpler, matches current auth pattern"
    }
  ],
  
  "created_by": "human",
  "created_at": "2025-01-27T..."
}
```

---

### spec:{id}

Detailed specification for a change. Separates current truth from proposals. (Borrowed from OpenSpec)

```json
{
  "id": "spec:auth-token-refresh",
  "type": "spec",
  "title": "Token Refresh Specification",
  
  "proposal": "proposal:auth-token-refresh",
  "supersedes": "spec:auth-token-v1",
  
  "status": "draft | in_review | approved | implemented | superseded",
  "version": 2,
  
  "content_ref": "objectstore:specs/auth-token-refresh.md",
  
  "requirements": [
    {
      "id": "REQ-1",
      "description": "System SHALL issue refresh token on login",
      "scenarios": [
        {
          "given": "Valid credentials submitted",
          "when": "Login succeeds",
          "then": "Response includes access_token and refresh_token"
        }
      ]
    }
  ],
  
  "acceptance_criteria": [
    "Refresh token lifetime configurable (default 7 days)",
    "Access token lifetime unchanged (15 minutes)",
    "Refresh rotates both tokens (sliding window)"
  ],
  
  "constitution_check": {
    "passed": true,
    "checked_at": "2025-01-27T...",
    "violations": []
  },
  
  "created_by": "agent:spec-writer",
  "approved_by": "human:coby",
  "created_at": "2025-01-27T..."
}
```

---

### task:{id}

Unit of work with fluid states. (Improved from rigid phase gates)

```json
{
  "id": "task:implement-refresh-token",
  "type": "task",
  "title": "Implement RefreshToken struct and methods",
  
  "spec": "spec:auth-token-refresh",
  "parent_task": null,
  "subtasks": ["task:impl-1", "task:impl-2"],
  
  "task_type": "explore | plan | spec | implement | review | test",
  
  "status": "exploring | planned | ready | claimed | in_progress | blocked | review | complete | abandoned",
  
  "assigned_role": "planner | spec-writer | architect | implementer | reviewer",
  "claimed_by": "agent:implementer | human:coby",
  
  "depends_on": ["task:design-token-structure"],
  "blocks": ["task:update-gateway-auth"],
  
  "context": {
    "files": ["auth/token.go", "auth/session.go"],
    "entities": ["code:auth/token.go#Token", "spec:auth-session"]
  },
  
  "created_at": "2025-01-27T...",
  "updated_at": "2025-01-27T..."
}
```

**Fluid States**: No enforced sequence. Task can move:
- `exploring` → `planned` (ready to specify)
- `exploring` → `ready` (simple, skip planning)
- `in_progress` → `exploring` (need to rethink)
- `review` → `in_progress` (changes requested)

---

### result:{id}

Output of completed work with full trajectory for training.

```json
{
  "id": "result:task-123-output",
  "type": "result",
  "task": "task:implement-refresh-token",
  
  "status": "pending_review | approved | rejected | needs_revision",
  
  "artifacts": [
    {
      "type": "code",
      "path": "auth/refresh.go",
      "ref": "objectstore:results/task-123/refresh.go",
      "action": "created"
    },
    {
      "type": "code", 
      "path": "auth/token.go",
      "ref": "objectstore:results/task-123/token.go",
      "action": "modified",
      "diff_ref": "objectstore:results/task-123/token.go.diff"
    }
  ],
  
  "git": {
    "branch": "semspec/task-123-refresh-token",
    "commits": ["abc123", "def456"],
    "pr": 47
  },
  
  "trajectory": {
    "ref": "objectstore:trajectories/task-123.jsonl",
    "tool_calls": 12,
    "tokens_in": 15420,
    "tokens_out": 8930
  },
  
  "completed_by": "agent:implementer",
  "reviewed_by": "human:coby",
  "feedback": "Good implementation. Consider adding metrics.",
  
  "training_eligible": true,
  "created_at": "2025-01-27T..."
}
```

---

### code:{path}

AST-based understanding of codebase. (Our differentiator)

```json
{
  "id": "code:auth/token.go",
  "type": "code",
  "file": "auth/token.go",
  "language": "go",
  "package": "auth",
  
  "hash": "sha256:abc123...",
  "lines": 145,
  
  "symbols": [
    {
      "id": "code:auth/token.go#Token",
      "name": "Token",
      "kind": "struct",
      "exported": true,
      "line_start": 12,
      "line_end": 24,
      "doc": "Token represents an authentication token"
    },
    {
      "id": "code:auth/token.go#NewToken",
      "name": "NewToken",
      "kind": "func",
      "exported": true,
      "line_start": 26,
      "line_end": 45,
      "signature": "func NewToken(userID string, claims Claims) (*Token, error)"
    }
  ],
  
  "updated_at": "2025-01-27T..."
}
```

**Relationships** (stored separately):
```
code:auth/token.go#NewToken  calls      code:auth/claims.go#ValidateClaims
code:auth/token.go#Token     implements interface:auth.Credential
code:gateway/auth.go#Handler imports    code:auth/token.go
```

**This enables**:
- "What calls NewToken?" → instant query
- "What would break if I change Token struct?" → impact analysis
- "Show me all auth-related code" → semantic search

---

## Agent Roles

### Planner (larger local model)

**Purpose**: High-level reasoning, understand requests, create plans

**Model**: `qwen2.5-coder:32b` or similar (needs strong reasoning)

**Tools**: `graph_query`, `ast_query`, `read_doc`, `file_list`

**Behavior**:
- Understands natural language requests
- Queries graph for context
- Identifies affected code and specs
- Creates proposals and task breakdown
- Asks clarifying questions when ambiguous

### Spec-Writer (medium local model)

**Purpose**: Write detailed specifications

**Model**: `deepseek-coder-v2:16b` or similar (structured output)

**Tools**: `graph_query`, `ast_query`, `read_doc`

**Behavior**:
- Takes proposal, writes detailed spec
- Includes requirements with Given/When/Then scenarios
- Checks against constitution
- Identifies edge cases and assumptions

### Architect (larger local model)

**Purpose**: Design decisions, technical approach

**Model**: Same as planner (needs reasoning)

**Tools**: `graph_query`, `ast_query`, `read_doc`

**Behavior**:
- Reviews spec for technical feasibility
- Proposes implementation approach
- Identifies dependencies and risks
- Documents decisions with rationale

### Implementer (larger local model)

**Purpose**: Write and modify code

**Model**: `qwen2.5-coder:32b` or similar (needs code generation)

**Tools**: All including `file_read`, `file_write`, `git_*`

**Behavior**:
- Follows spec and design
- Writes code matching project patterns
- Creates tests
- Commits with clear messages

### Reviewer (larger local model)

**Purpose**: Check quality before human review

**Model**: Same as planner

**Tools**: `graph_query`, `ast_query`, `file_read` (read-only)

**Behavior**:
- Checks result against spec
- Validates constitution compliance
- Identifies potential issues
- Prepares summary for human

---

## Configuration

```json
{
  "project": "semstreams",
  "repo_path": "/home/user/projects/semstreams",
  
  "models": {
    "planner": {
      "provider": "ollama",
      "endpoint": "http://localhost:11434/v1",
      "model": "qwen2.5-coder:32b",
      "temperature": 0.3
    },
    "spec-writer": {
      "provider": "ollama",
      "endpoint": "http://localhost:11434/v1",
      "model": "deepseek-coder-v2:16b",
      "temperature": 0.2
    },
    "implementer": {
      "provider": "ollama",
      "endpoint": "http://localhost:11434/v1",
      "model": "qwen2.5-coder:32b",
      "temperature": 0.1
    }
  },
  
  "roles": {
    "planner": {
      "model": "planner",
      "tools": ["graph_query", "ast_query", "read_doc", "file_list"]
    },
    "spec-writer": {
      "model": "spec-writer",
      "tools": ["graph_query", "ast_query", "read_doc"],
      "requires_review": true
    },
    "implementer": {
      "model": "implementer",
      "tools": ["graph_query", "ast_query", "file_read", "file_write", "git_branch", "git_commit", "git_status"],
      "requires_review": true
    },
    "reviewer": {
      "model": "planner",
      "tools": ["graph_query", "ast_query", "file_read"]
    }
  },
  
  "workflows": {
    "default_flow": ["explore", "plan", "spec", "implement", "review"],
    "quick_fix": ["implement", "review"],
    "research": ["explore"]
  },
  
  "checkpoints": {
    "spec_approval": "human",
    "implementation_review": "human",
    "merge": "human"
  }
}
```

**Model Notes**:
- All models accessed via OpenAI-compatible API (Ollama serves this)
- Can swap in LiteLLM as proxy for more flexibility
- 32b models need ~20GB VRAM; 16b models need ~10GB
- For smaller GPUs: use `codellama:13b` or `deepseek-coder:6.7b`

---

## Components

### Interaction Layer

#### processor/input/cli

**Purpose**: Normalize terminal input, handle Ctrl+C signals

**Input**: stdin, terminal signals

**Output**: `user.message.cli.*`, `user.signal.*`

```go
type CLIInput struct {
    userID     string
    sessionID  string
    activeLoop string
}

func (c *CLIInput) Run(ctx context.Context) error {
    // Setup interrupt handler (Ctrl+C → cancel signal)
    // Read lines from stdin
    // Publish as UserMessage
    // Render UserResponse to terminal
}
```

#### processor/router

**Purpose**: Command parsing, permissions, routing, response delivery

**Input**: `user.message.>`, `user.signal.>`, `agent.complete.*`

**Output**: `agent.task.*`, `agent.signal.*`, `user.response.*`

```go
type Router struct {
    commands    map[string]CommandConfig
    permissions PermissionConfig
    loopTracker *LoopTracker
}

func (r *Router) HandleMessage(msg UserMessage) error {
    // 1. Check if command (starts with /)
    // 2. Check permissions
    // 3. Build task or dispatch command
    // 4. Track loop
    // 5. Publish to agent.task.*
}

func (r *Router) HandleCompletion(completion CompletionEvent) error {
    // 1. Find original channel
    // 2. Format response
    // 3. Publish to user.response.*
}
```

### Agentic System

#### processor/agentic-loop

**Purpose**: Orchestrate model calls, tool execution, state machine

**Input**: `agent.task.*`, `agent.response.*`, `tool.result.*`, `agent.signal.*`

**Output**: `agent.request.*`, `tool.execute.*`, `agent.complete.*`

**KV Buckets**: `AGENT_LOOPS`, `AGENT_TRAJECTORIES`

**States**: `exploring → planning → architecting → executing → reviewing → complete|failed|cancelled|paused`

#### processor/agentic-model

**Purpose**: Call LLM endpoints (Ollama)

**Input**: `agent.request.*`

**Output**: `agent.response.*`

**Features**: Multi-endpoint routing, tool definition marshaling, retry logic

#### processor/agentic-tools

**Purpose**: Execute tool calls with timeout and filtering

**Input**: `tool.execute.*`

**Output**: `tool.result.*`

**Features**: Executor registry, allowlist filtering, concurrent execution

### Domain Components

### processor/planner

**Purpose**: Entry point for requests, high-level reasoning

**Input**: User requests via CLI or `request.new.*`

**Output**: Proposals, task breakdowns

```go
type Planner struct {
    modelClient  *ModelClient  // Claude API
    queryEngine  *QueryEngine
    entityStore  *EntityStore
}

func (p *Planner) HandleRequest(request string) (*Proposal, error) {
    // 1. Understand request
    // 2. Query graph for context
    // 3. Check constitution
    // 4. Create proposal
    // 5. Break into tasks if approved
}
```

### processor/tool-executor

**Purpose**: Execute tools on behalf of agents

**Input**: `tool.execute.*`

**Output**: `tool.result.*`

**Tools Implemented**:

| Tool | Description | Roles |
|------|-------------|-------|
| `graph_query` | Query entities and relationships | All |
| `ast_query` | Semantic code search | All |
| `read_doc` | Read from ObjectStore | All |
| `file_read` | Read file from repo | implementer, reviewer |
| `file_write` | Write file to repo | implementer |
| `git_branch` | Create branch | implementer |
| `git_commit` | Commit changes | implementer |
| `git_status` | Check status | implementer |
| `git_diff` | Show changes | implementer, reviewer |

### processor/ast

**Purpose**: Parse code, maintain code entities

**Input**: File changes (watcher or webhook)

**Output**: `code:{path}` entities, relationships

**Implementation**: Start with `go/ast` for Go, add tree-sitter later for multi-language.

### processor/result

**Purpose**: Capture agent outputs, store trajectories

**Input**: `agent.complete.*`

**Output**: `result:{id}` entities with full trajectory

### rules/semspec

**Purpose**: Domain-specific workflow rules

**Rules**:
```yaml
# Task dependencies
- name: task-ready-when-deps-complete
  when:
    event: task.completed.*
  then:
    for_each: tasks depending on completed
    if: all dependencies complete
    update: status = ready

# Constitution check
- name: spec-constitution-check
  when:
    entity_changed: spec:*
    condition: status == 'in_review'
  then:
    validate: against constitution
    update: constitution_check result

# Human checkpoint
- name: require-human-approval
  when:
    entity_changed: result:*
    condition: status == 'pending_review'
  then:
    notify: human review required
    # Does NOT auto-approve

# Fluid transitions (no rigid gates)
- name: allow-backward-transition
  when:
    entity_changed: task:*
    condition: status changed
  then:
    # Allow any valid transition
    # in_progress → exploring is valid
    # review → in_progress is valid
```

---

## Workflow Example

```
Human: "add rate limiting to the API"

┌─────────────────────────────────────────────────────────────────────┐
│ EXPLORE PHASE                                                        │
│                                                                      │
│ Planner queries graph:                                              │
│   - What API endpoints exist? → 12 endpoints in gateway            │
│   - Any existing rate limiting? → None found                       │
│   - Constitution constraints? → "All inputs validated"             │
│                                                                      │
│ Planner asks:                                                       │
│   "Should rate limiting be per-user, per-IP, or both?"             │
│   "What limits make sense? (requests/minute)"                      │
│                                                                      │
│ Human: "Per-IP, 100/min for most, 1000/min for authenticated"      │
│                                                                      │
│ Creates: proposal:api-rate-limiting                                 │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ SPEC PHASE                                                          │
│                                                                      │
│ Spec-writer creates: spec:api-rate-limiting                        │
│   Requirements:                                                     │
│   - REQ-1: Rate limit middleware for all endpoints                 │
│   - REQ-2: Configurable limits per endpoint                        │
│   - REQ-3: 429 response with Retry-After header                    │
│                                                                      │
│ Constitution check: ✓ Passed                                        │
│                                                                      │
│ Human review: Approved                                              │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ IMPLEMENT PHASE                                                      │
│                                                                      │
│ Tasks created:                                                      │
│   task:1 - Create rate limiter middleware                          │
│   task:2 - Add configuration options                               │
│   task:3 - Update endpoint handlers                                │
│   task:4 - Add tests                                               │
│                                                                      │
│ Implementer works on task:1:                                        │
│   - Queries: "show me existing middleware patterns" → graph_query  │
│   - Creates: gateway/middleware/ratelimit.go                       │
│   - Commits: "Add rate limiting middleware"                        │
│                                                                      │
│ Result captured with full trajectory                                │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│ REVIEW PHASE                                                         │
│                                                                      │
│ Reviewer checks:                                                    │
│   - Code matches spec? ✓                                           │
│   - Constitution compliance? ✓                                     │
│   - Tests pass? ✓                                                  │
│                                                                      │
│ Human review: Approved with feedback                                │
│   "Consider adding metrics for rate limit hits"                    │
│                                                                      │
│ Feedback stored in result entity                                    │
│ Trajectory marked training_eligible = true                          │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Training Flywheel

Every interaction feeds improvement:

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  1. CAPTURE                                                         │
│     Every request, tool call, result stored                        │
│                                                                      │
│  2. FEEDBACK                                                        │
│     Human approves/rejects with comments                           │
│     Stored in result entity                                         │
│                                                                      │
│  3. EXPORT                                                          │
│     Query: approved results with feedback                          │
│     Format: task + trajectory + outcome                            │
│                                                                      │
│  4. TRAIN                                                           │
│     Fine-tune SLMs on approved trajectories                        │
│     Role-specific training (spec-writer, implementer)              │
│                                                                      │
│  5. DEPLOY                                                          │
│     Updated models serve same roles                                │
│     Better performance on similar tasks                            │
│                                                                      │
│  6. REPEAT                                                          │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## What Makes This Different

| Others | Semspec |
|--------|---------|
| Files track specs | Graph tracks relationships |
| Session context | Persistent knowledge |
| One model | Specialized roles |
| Rigid phases | Fluid with checkpoints |
| Execute and forget | Training flywheel |
| Patch existing agents | IS the agent |
