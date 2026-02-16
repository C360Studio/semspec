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
- A project directory to work with

## Run Semspec

### Option A: Docker Compose (Recommended)

The easiest way to run semspec:

```bash
# Clone the repository
git clone https://github.com/c360studio/semspec.git
cd semspec

# Start NATS and semspec
docker compose up -d
```

Open **http://localhost:8080** in your browser.

> **Note:** If the Docker image isn't published yet, use Option B below.

To work with a different project directory:

```bash
SEMSPEC_REPO=/path/to/your/project docker compose up -d
```

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

## LLM Setup

An LLM is required to generate proposals, designs, and specifications.

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

Change slug: add-user-authentication-with-jwt-tokens
Status: created

Files created:
- .semspec/plans/add-user-authentication-with-jwt-tokens/plan.md
- .semspec/plans/add-user-authentication-with-jwt-tokens/metadata.json

Next steps:
1. Review the generated Goal, Context, and Scope
2. Run /approve add-user-authentication-with-jwt-tokens to approve the plan
3. Run /tasks add-user-authentication-with-jwt-tokens to generate tasks
4. Run /execute add-user-authentication-with-jwt-tokens to execute
```

### Autonomous Mode

For faster iteration, use `--auto` to run the full workflow automatically:

```
/plan Add user authentication --auto
```

This generates plan.md → tasks.md and executes approved tasks.

The system validates each document before proceeding. If validation fails, it automatically retries with feedback. See [05-workflow-system.md](05-workflow-system.md) for details.

### 2. Check Status

See all active plans:
```
/changes
```

See details for a specific plan:
```
/changes add-user-authentication-with-jwt-tokens
```

### 3. Validate Against Constitution

If your project has a constitution (`.semspec/constitution.md`), validate your plan:
```
/check add-user-authentication-with-jwt-tokens
```

### 4. Generate Tasks

Break the plan into implementable tasks:
```
/tasks add-user-authentication-with-jwt-tokens
```

This creates `.semspec/plans/{slug}/tasks.md` with a checklist.

### 5. Approve for Execution

```
/approve add-user-authentication-with-jwt-tokens
```

### 6. Execute Tasks

```
/execute add-user-authentication-with-jwt-tokens
```

### 7. Sync with GitHub (Optional)

Create GitHub issues from your tasks:
```
/github sync add-user-authentication-with-jwt-tokens
```

## File Structure

After working through the workflow, your `.semspec/` directory looks like:

```
.semspec/
├── constitution.md           # Project rules (optional)
└── plans/
    └── add-user-authentication-with-jwt-tokens/
        ├── metadata.json     # Status, timestamps, author
        ├── plan.md           # Goal, context, scope
        └── tasks.md          # Implementation checklist
```

These files are git-friendly—commit them with your code to preserve context.

## Next Steps

- Read [How Semspec Works](01-how-it-works.md) to understand the architecture
- Read [05-workflow-system.md](05-workflow-system.md) for autonomous mode and validation
- Check [08-roadmap.md](08-roadmap.md) for upcoming features
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

If autonomous mode reports validation failures:

1. Check the generated document:
   ```bash
   cat .semspec/plans/my-feature/plan.md
   ```

2. Verify section headers exist (case-insensitive):
   - Plans need: `## Goal`, `## Context`, `## Scope`
   - Tasks need proper BDD format with acceptance criteria

3. The system retries up to 3 times with feedback. If it still fails, fix the document manually and re-run the step.

### Debugging Requests

For deeper troubleshooting, use the `/debug` command:

```bash
# Check workflow state
/debug workflow add-user-auth

# Export a trace snapshot for support
/debug snapshot <trace-id> --verbose
```

See [How Semspec Works - Debugging](01-how-it-works.md#debugging) for more details.
