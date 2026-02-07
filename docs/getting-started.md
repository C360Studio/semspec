# Getting Started with Semspec

This guide walks you through setting up semspec and creating your first proposal.

## Before You Start

If you're new to semspec, read [How Semspec Works](how-it-works.md) first. It explains:
- Why semstreams is required (semspec extends it)
- Where LLM calls happen (in Docker, not in semspec binary)
- What happens when you run commands (async message flow)

## Prerequisites

- Go 1.21 or later
- Docker (for NATS and semstreams services)
- An LLM provider (see [LLM Setup](#llm-setup))
- A project directory to work with

## Infrastructure Setup

Semspec requires external infrastructure running in Docker. The docker-compose file runs:
- **NATS JetStream**: Message bus for all communication
- **Semstreams services**: Components that call LLMs and manage the graph

```bash
# Clone semstreams if you haven't already
git clone https://github.com/c360/semstreams.git ../semstreams

# Start infrastructure
cd ../semstreams
docker-compose -f docker/compose/e2e.yml up -d
```

Verify NATS is running:
```bash
curl http://localhost:8222/healthz
# Should return "ok"
```

## LLM Setup

An LLM is required to generate proposals, designs, and specifications. Choose one:

**Option A: Ollama (Default)**
```bash
ollama serve
ollama pull qwen2.5-coder:14b
```

Semspec is designed for edge scenarios with local LLMs.

**Option B: Claude API (Optional)**

For cloud-connected environments:
```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

See [How Semspec Works](how-it-works.md#llm-configuration) for model selection details.

## Build and Run Semspec

```bash
# In the semspec directory
go build -o semspec ./cmd/semspec

# Start semspec
./semspec --repo /path/to/your/project
```

### Open the Web UI

Navigate to **http://localhost:8080** in your browser.

You'll see the chat interface ready to accept commands.

### Alternative: CLI Mode

For terminal-based interaction:

```bash
./semspec cli --repo /path/to/your/project
```

You should see:
```
Semspec CLI ready
version: 0.1.0
repo_path: /path/to/your/project
```

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
cd ../semstreams
docker-compose -f docker/compose/e2e.yml up -d
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
