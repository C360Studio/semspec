# Getting Started with Semspec

This guide walks you through setting up semspec and creating your first proposal.

## Before You Start

If you're new to semspec, read [How Semspec Works](how-it-works.md) first. It explains:
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
# Requires Go 1.21+
go build -o semspec ./cmd/semspec

# Start infrastructure first
docker compose up -d nats

# Run semspec locally
./semspec --repo /path/to/your/project
```

## LLM Setup

An LLM is required to generate proposals, designs, and specifications.

**Option A: Ollama (Default)**

Start Ollama on your host machine:

```bash
ollama serve
ollama pull qwen2.5-coder:14b
```

Docker automatically connects to Ollama via `host.docker.internal:11434`.

To use a remote Ollama instance:

```bash
OLLAMA_HOST=http://my-ollama-server:11434 docker compose up -d
```

**Option B: Claude API**

For cloud-connected environments:

```bash
ANTHROPIC_API_KEY=sk-ant-... docker compose up -d
```

Or create a `.env` file:

```bash
ANTHROPIC_API_KEY=sk-ant-...
```

See [How Semspec Works](how-it-works.md#llm-configuration) for model selection details.

## Verify Setup

Check that services are healthy:

```bash
# NATS health
curl http://localhost:8222/healthz

# Semspec health (service mode only)
curl http://localhost:8080/readyz
```

> **Note:** In CLI mode, the `/readyz` endpoint may report "NOT READY" even when
> the CLI is working correctly. This is expected—CLI mode runs a minimal set of
> components. The health endpoints are designed for service mode.

### Open the Web UI

Navigate to **http://localhost:8080** in your browser.

You'll see the chat interface ready to accept commands.

### Alternative: CLI Mode

For terminal-based interaction (requires building from source):

```bash
./semspec cli --repo /path/to/your/project
```

You should see:

```
Semspec CLI ready
version: 0.1.0
repo_path: /path/to/your/project
```

> **Note:** CLI mode provides a command prompt but does not serve the web UI.
> The web UI is only available in service mode (Option A with Docker Compose).

## Your First Proposal

Let's walk through the spec-driven workflow.

### Using the Web UI

1. Open http://localhost:8080
2. In the chat input, type:
   ```
   Add user authentication with JWT tokens
   ```
3. Press Enter or click Send

The system creates your proposal and shows progress in the activity stream.

### Using the CLI

```bash
./semspec cli --repo .
/propose Add user authentication with JWT tokens
```

### 1. Create a Proposal

```
/propose Add user authentication with JWT tokens
```

Output:
```
✓ Created proposal: Add user authentication with JWT tokens

Change slug: add-user-authentication-with-jwt-tokens
Status: created

Files created:
- .semspec/changes/add-user-authentication-with-jwt-tokens/proposal.md
- .semspec/changes/add-user-authentication-with-jwt-tokens/metadata.json

Next steps:
1. Edit proposal.md to describe Why, What Changes, and Impact
2. Run /design add-user-authentication-with-jwt-tokens to create technical design
3. Run /spec add-user-authentication-with-jwt-tokens to create specification
4. Run /check add-user-authentication-with-jwt-tokens to validate against constitution
```

### Autonomous Mode

For faster iteration, use `--auto` to run the full workflow automatically:

```
/propose Add user authentication --auto
```

This generates all documents in sequence: proposal.md → design.md → spec.md → tasks.md

The system validates each document before proceeding. If validation fails, it automatically retries with feedback. See [workflow-system.md](workflow-system.md) for details.

### 2. Check Status

See all active changes:
```
/changes
```

See details for a specific change:
```
/changes add-user-authentication-with-jwt-tokens
```

### 3. Create a Design

```
/design add-user-authentication-with-jwt-tokens
```

This creates `.semspec/changes/{slug}/design.md` with sections for:
- Overview
- Components
- API Design
- Data Model
- Error Handling
- Security Considerations

### 4. Create a Specification

```
/spec add-user-authentication-with-jwt-tokens
```

This creates `.semspec/changes/{slug}/spec.md` with GIVEN/WHEN/THEN scenarios:
- Preconditions (GIVEN)
- Actions (WHEN)
- Expected outcomes (THEN)

### 5. Validate Against Constitution

If your project has a constitution (`.semspec/constitution.md`), validate your change:
```
/check add-user-authentication-with-jwt-tokens
```

### 6. Generate Tasks

Break the spec into implementable tasks:
```
/tasks add-user-authentication-with-jwt-tokens
```

This creates `.semspec/changes/{slug}/tasks.md` with a checklist.

### 7. Approve for Implementation

```
/approve add-user-authentication-with-jwt-tokens
```

### 8. Sync with GitHub (Optional)

Create GitHub issues from your tasks:
```
/github sync add-user-authentication-with-jwt-tokens
```

### 9. Export to RDF (Optional)

Export your proposal as RDF for semantic web tools or knowledge graph integration:
```
/export add-user-authentication-with-jwt-tokens turtle cco
```

Supported formats: `turtle`, `ntriples`, `jsonld`
Supported profiles: `minimal` (PROV-O only), `bfo` (adds BFO), `cco` (adds CCO)

## File Structure

After working through the workflow, your `.semspec/` directory looks like:

```
.semspec/
├── constitution.md           # Project rules (optional)
└── changes/
    └── add-user-authentication-with-jwt-tokens/
        ├── metadata.json     # Status, timestamps, author
        ├── proposal.md       # Problem statement, impact
        ├── design.md         # Technical design
        ├── spec.md           # GIVEN/WHEN/THEN scenarios
        └── tasks.md          # Implementation checklist
```

These files are git-friendly—commit them with your code to preserve context.

## Next Steps

- Read [How Semspec Works](how-it-works.md) to understand the architecture
- Read [workflow-system.md](workflow-system.md) for autonomous mode and validation
- Check [roadmap.md](roadmap.md) for upcoming features
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
1. You're in CLI mode (`./semspec cli --repo .`)
2. Commands start with `/` (e.g., `/help`, not `help`)
3. Run `/help` to see available commands

### Validation Failures

If autonomous mode reports validation failures:

1. Check the generated document:
   ```bash
   cat .semspec/changes/my-feature/proposal.md
   ```

2. Verify section headers exist (case-insensitive):
   - Proposals need: `## Why`, `## What Changes`, `## Impact`
   - Designs need: `## Technical Approach`, `## Components Affected`
   - Specs need: `## Requirements`, `GIVEN/WHEN/THEN` scenarios

3. The system retries up to 3 times with feedback. If it still fails, fix the document manually and re-run the step.
