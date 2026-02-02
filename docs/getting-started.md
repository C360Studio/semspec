# Getting Started with Semspec

This guide walks you through setting up semspec and creating your first proposal.

## Prerequisites

- Go 1.21 or later
- Docker (for NATS infrastructure)
- A project directory to work with

## Setup (5 minutes)

### 1. Start Infrastructure

Semspec requires NATS JetStream. The semstreams repository provides a docker-compose file:

```bash
# Clone semstreams if you haven't already
git clone https://github.com/c360/semstreams.git ../semstreams

# Start NATS infrastructure
cd ../semstreams
docker-compose -f docker/compose/e2e.yml up -d
```

Verify NATS is running:
```bash
curl http://localhost:8222/healthz
# Should return "ok"
```

### 2. Build Semspec

```bash
# In the semspec directory
go build -o semspec ./cmd/semspec
```

### 3. Start CLI Mode

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

- Read [architecture.md](architecture.md) to understand how semspec fits with semstreams
- Check [roadmap.md](roadmap.md) for upcoming features
- Run `/help` to see all available commands

## Troubleshooting

### NATS Connection Error

If you see:
```
NATS connection failed: connection refused
```

Make sure NATS is running:
```bash
cd ../semstreams
docker-compose -f docker/compose/e2e.yml up -d
```

### Command Not Found

If a command returns an error, check:
1. You're in CLI mode (`./semspec cli --repo .`)
2. Commands start with `/` (e.g., `/help`, not `help`)
3. Run `/help` to see available commands
