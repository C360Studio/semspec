# Getting Started with Semspec

This guide walks you through setting up semspec and creating your first plan.

## Before You Start

If you're new to semspec, read [How Semspec Works](01-how-it-works.md) first. It explains:
- Why semstreams is required (semspec extends it)
- Where LLM calls happen (in Docker, not in semspec binary)
- What happens when you run commands (async message flow)

## Prerequisites

- Docker and Docker Compose
- An LLM provider (see [LLM Setup](#llm-setup))
- A target project with `.semspec/` configuration (see [Project Initialization](#project-initialization) below)

## Setup Overview

Getting semspec running on your project has three steps:

1. **Initialize your project** — create `.semspec/` config files in your repo
2. **Configure an LLM** — Ollama (local) or an API key (cloud)
3. **Start the stack** — `docker compose up -d` pointing at your repo

The demo mode skips steps 1-2 by using the semspec repo itself with a mock LLM.

## Run Semspec

### Option A: Docker Compose (Recommended)

```bash
# Clone the semspec repo (contains the Docker stack)
git clone https://github.com/c360studio/semspec.git
cd semspec

# Point at your project and start
SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

Open **http://localhost:8080** in your browser.

> Your project must have `.semspec/project.json`, `standards.json`, and `checklist.json` —
> see [Project Initialization](#project-initialization) below if you haven't set these up yet.

> **File permissions:** The sandbox defaults to UID 1000. If that doesn't match your host user,
> add your UID to `.env` so agent-created files have correct ownership:
> ```bash
> echo "SANDBOX_UID=$(id -u)" >> .env
> echo "SANDBOX_GID=$(id -g)" >> .env
> ```

### Option B: Build from Source

For development or customization:

```bash
# Requires Go 1.25+
go build -o semspec ./cmd/semspec

# Start infrastructure first
docker compose up -d nats

# Run semspec locally
./semspec --repo /path/to/your/project
```

Open **http://localhost:8080** in your browser.

## Why Web UI Only?

Semspec uses a Web UI exclusively (no CLI mode). This is intentional:

- **Async workflows**: Commands dispatch work to agent loops that run in the background. Results arrive later via NATS.
- **Real-time updates**: The Web UI uses SSE to push activity, questions, and results as they happen.
- **Interactive questions**: Agents can ask clarifying questions that appear inline in the UI.

A traditional CLI can't provide this feedback loop without constant polling.

## Try Without an LLM (Demo Mode)

Want to explore the UI and workflow before setting up models? Run the demo with a mock LLM — canned responses, no API keys:

```bash
task demo
```

Open **http://localhost:3000**. Navigate to Plans, click New Plan, and type a plan description. The mock LLM generates a plan you can approve, task-generate, and execute. When done: `task demo:down`.

> No `task` command? Install it: `brew install go-task` or see [taskfile.dev](https://taskfile.dev/installation/).

## LLM Setup

An LLM is required to generate plans, tasks, and execute agent loops.

Semspec uses a **capability-based model system** that routes tasks to
appropriate models:

| Capability | Best For                       | Recommended Model   |
| ---------- | ------------------------------ | ------------------- |
| coding     | Code generation, editing       | qwen2.5-coder:14b   |
| planning   | Architecture, design decisions | qwen3:14b           |
| writing    | Proposals, specs, docs         | qwen3:14b           |
| reviewing  | Code review, analysis          | qwen3:14b           |
| fast       | Quick tasks, classification    | qwen3:1.7b          |

### Option A: Ollama (Recommended)

Start Ollama and pull models for different capabilities:

```bash
ollama serve
ollama pull qwen2.5-coder:14b  # Coding tasks
ollama pull qwen3:14b          # Reasoning tasks
ollama pull qwen3:1.7b         # Fast tasks
```

Docker automatically connects to Ollama via `host.docker.internal:11434`.

**Hardware Requirements:**

| Setup       | RAM   | Models                  |
| ----------- | ----- | ----------------------- |
| Minimal     | 16GB  | qwen2.5-coder:7b only   |
| Recommended | 32GB  | All three models above  |
| Full        | 64GB+ | Larger models (30B+)    |

To use a remote Ollama instance:

```bash
OLLAMA_HOST=http://my-ollama-server:11434 docker compose up -d
```

### Option B: Claude API

For cloud-connected environments:

```bash
ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
```

Or create a `.env` file:

```bash
ANTHROPIC_API_KEY=sk-ant-...
```

With an API key set, Claude is used as the primary model with Ollama as fallback.

### Configuration

Models are configured in `configs/semspec.json`. See [Model Configuration](07-model-configuration.md) for:

- Adding new models
- Customizing capability fallbacks
- Troubleshooting model issues

## Verify Setup

Check that services are healthy:

```bash
# NATS health
curl http://localhost:8222/healthz

# Semspec health (service mode only)
curl http://localhost:8080/readyz
```

### Open the Web UI

Navigate to **http://localhost:8080** in your browser.

You'll see the chat interface ready to accept commands.

## Your First Plan

Let's walk through the spec-driven workflow.

### Using the Web UI

1. Open http://localhost:8080
2. In the chat input, type:
   ```
   /plan Add user authentication with JWT tokens
   ```
3. Press Enter or click Send

The system creates your plan and shows progress in the activity stream.

### 1. Create a Plan

```
/plan Add user authentication with JWT tokens
```

Output:

```
✓ Created plan: Add user authentication with JWT tokens

Slug:   add-user-authentication-with-jwt-tokens
Status: planning

The planner is generating requirements and scenarios. Check the Plans
page for progress — components self-trigger via PLAN_STATES as each
phase completes.
```

Plan state is stored in the `PLAN_STATES` KV bucket (source of truth). Artifact files are
written to `.semspec/plans/<slug>/` for git-friendliness, but KV is always authoritative.

The pipeline advances automatically without an explicit coordinator: `planner` generates the
Goal/Context/Scope, `requirement-generator` and `scenario-generator` run next (each watches
`PLAN_STATES` for the status that triggers them), and finally `plan-reviewer` validates the
output against project SOPs.

### 2. Approve the Plan

Once you've reviewed the generated requirements and scenarios, approve:

```
/approve add-user-authentication-with-jwt-tokens
```

If `auto_approve` is enabled (the default), the plan moves to `ready_for_execution`
automatically after the reviewer passes it — no manual approval needed.

### 3. Execute

Start the TDD execution pipeline:

```
/execute add-user-authentication-with-jwt-tokens
```

The `scenario-orchestrator` dispatches pending requirements. For each requirement, the
`requirement-executor` decomposes it into a task DAG and runs stages in order:
developer → validator → reviewer.

## Project Initialization

Before creating plans, your project needs three configuration files in `.semspec/`. These tell
semspec what languages you use, what quality gates to enforce, and what standards agents should
follow.

### Required Files

| File | Purpose | Used By |
|------|---------|---------|
| `project.json` | Detected stack: languages, frameworks, tooling | Context assembly, prompt generation |
| `standards.json` | Rules injected into agent context | Plan reviewer, code reviewer, all agents |
| `checklist.json` | Deterministic quality gates (shell commands) | Structural validator (runs after each task) |

Semspec considers a project initialized when all three files exist. The `GET /api/project/status`
endpoint reports initialization state — the UI uses this to determine what to show.

### Option A: API-Driven Setup

The project-manager provides endpoints for automated project setup:

```bash
# Check initialization status
curl http://localhost:8080/api/project/status | jq .

# Detect languages, frameworks, and tooling
curl -X POST http://localhost:8080/api/project/detect | jq .

# Generate standards from detected stack + existing docs
curl -X POST http://localhost:8080/api/project/generate-standards | jq .

# Initialize all three files at once
curl -X POST http://localhost:8080/api/project/init \
  -H "Content-Type: application/json" \
  -d '{"name": "my-project", "description": "What this project does"}'

# Approve each file after reviewing
curl -X POST http://localhost:8080/api/project/approve \
  -H "Content-Type: application/json" \
  -d '{"file": "project.json"}'
```

### Option B: Manual Setup

Create the files directly. This is the fastest path for a demo or when you know your stack:

```bash
mkdir -p .semspec/sources/docs
```

**`.semspec/project.json`** — Project metadata:

```json
{
  "name": "my-project",
  "description": "Brief description of what this project does",
  "version": "1",
  "languages": [
    {"name": "Go", "version": "1.25", "primary": true}
  ],
  "tooling": {
    "task_runner": "Taskfile",
    "linters": ["revive"],
    "test_frameworks": ["testing"]
  }
}
```

**`.semspec/standards.json`** — Rules enforced by reviewers. Start empty, add as you go:

```json
{
  "rules": [
    {
      "id": "error-handling",
      "text": "All errors must be handled or explicitly propagated. No silently swallowed errors.",
      "severity": "error",
      "category": "code-quality",
      "origin": "manual"
    },
    {
      "id": "test-coverage",
      "text": "All new functions must have corresponding test cases.",
      "severity": "error",
      "category": "testing",
      "origin": "manual"
    }
  ]
}
```

Rule severities: `error` (blocks approval), `warning` (flagged but allowed), `info` (informational).

**`.semspec/checklist.json`** — Deterministic quality gates. These are shell commands that run
after each agent task. A failing `required` check blocks progression to review:

```json
{
  "checks": [
    {
      "name": "go-build",
      "command": "go build ./...",
      "trigger": ["*.go"],
      "category": "compile",
      "required": true,
      "timeout": "120s",
      "description": "Verify Go code compiles"
    },
    {
      "name": "go-test",
      "command": "go test ./...",
      "trigger": ["*.go", "*_test.go"],
      "category": "test",
      "required": true,
      "timeout": "120s",
      "description": "Run Go unit tests"
    }
  ]
}
```

Check categories: `compile`, `lint`, `typecheck`, `test`, `format`, `setup`.

### SOPs (Optional but Recommended)

SOPs are detailed enforcement rules stored as Markdown with YAML frontmatter in
`.semspec/sources/docs/`. They provide richer context than `standards.json` rules — including
ground truth, violation examples, and file-scoped applicability.

See [SOP System](09-sop-system.md) for authoring and enforcement details.

> **Standards vs SOPs**: `standards.json` rules are short, machine-readable statements injected
> into every agent context. SOPs are detailed documents with examples and rationale, retrieved
> selectively by scope and domain. Use standards for universal rules; use SOPs for nuanced,
> context-dependent guidance.

## File Structure

After initialization and running the planning workflow, your `.semspec/` directory looks like:

```
.semspec/
├── project.json              # Project metadata (languages, frameworks, tooling)
├── standards.json            # Agent standards (rules injected into context)
├── checklist.json            # Deterministic quality gates (shell commands)
├── sources/
│   └── docs/                 # SOPs and source documents
│       └── testing-sop.md    # Example SOP (optional)
├── plans/
│   └── add-user-authentication-with-jwt-tokens/
│       ├── metadata.json     # Status, timestamps, author
│       └── plan.json         # Goal, context, scope — artifact only (KV is source of truth)
└── worktrees/                # Agent task worktrees (temporary, auto-cleaned)
```

Plan files are written by `plan-manager` as git-friendly artifacts. The authoritative source of
truth is the `PLAN_STATES` JetStream KV bucket. Tasks are not stored in plan files — they are
created at execution time by the decomposer agent and tracked in `EXECUTION_STATES`.

These files are git-friendly — commit them with your code to preserve context.

## Next Steps

- Read [How Semspec Works](01-how-it-works.md) to understand the full message flow
- Read [SOP System](09-sop-system.md) to write project-specific rules
- Read [Behavioral Controls](10-behavioral-controls.md) to understand how agent behavior is constrained
- Read [Sandbox Security](13-sandbox-security.md) for the execution isolation model
- Read [Plan API](12-plan-api.md) for the REST API reference
- Run `/help` to see all available commands

## Troubleshooting

### NATS Connection Error

If you see:

```
NATS connection failed: connection refused
```

Make sure infrastructure is running:

```bash
docker compose up -d
docker compose logs nats  # Check NATS logs
```

### Command Not Found

If a command returns an error, check:
1. Commands start with `/` (e.g., `/help`, not `help`)
2. Run `/help` to see available commands

### Validation Failures

If validation fails:

1. Check plan state in the KV bucket:

   ```bash
   curl http://localhost:8080/message-logger/kv/PLAN_STATES | jq .
   ```

2. Or inspect the artifact file written for reference:

   ```bash
   cat .semspec/plans/my-feature/plan.json
   ```

3. Plans need `goal`, `context`, and `scope` fields
4. The system retries up to 3 times with reviewer feedback before escalating

### Debugging Requests

For deeper troubleshooting, use the `/debug` command:

```bash
# Check workflow state
/debug workflow add-user-auth

# Export a trace snapshot for support
/debug snapshot <trace-id> --verbose
```

See [How Semspec Works - Debugging](01-how-it-works.md#debugging) for more details.
