# Project Setup

Semspec requires a `.semspec/` directory in your repository with three configuration files.
These tell agents what languages you use, what standards to follow, and what quality gates
to enforce. You can create them manually or via the API.

```
.semspec/
├── project.json          # Project metadata (languages, frameworks, tooling)
├── standards.json        # Rules injected into every agent's context
├── checklist.json        # Shell commands that run after each agent task
└── sources/
    └── docs/             # SOPs — rich enforcement rules with examples (optional)
```

Without these files, semspec will start but agents won't have project-specific context.

## Quick Start

```bash
cd /path/to/your/project
mkdir -p .semspec/sources/docs

# Project metadata
cat > .semspec/project.json << 'EOF'
{
  "name": "my-project",
  "description": "Brief description of what this project does",
  "version": "1",
  "languages": [{"name": "Go", "primary": true}],
  "tooling": {}
}
EOF

# Empty standards — add rules as you learn what matters
echo '{"rules":[]}' > .semspec/standards.json

# Empty checklist — add quality gates for your stack
echo '{"checks":[]}' > .semspec/checklist.json
```

## API-Driven Setup

The project-manager provides endpoints for automated setup. See the
[API Reference](api.md#project-setup--apiproject) or the Swagger UI at `/docs`.

```bash
curl -X POST http://localhost:8080/api/project/detect          # Auto-detect stack
curl -X POST http://localhost:8080/api/project/generate-standards  # Generate from detected stack
curl -X POST http://localhost:8080/api/project/init \           # Write all three files
  -H "Content-Type: application/json" \
  -d '{"name": "my-project", "description": "..."}'
```

## project.json

Project metadata used for context assembly and prompt generation. Detected automatically
via `POST /api/project/detect` or created manually.

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

## standards.json

Rules injected into every agent's context — planning, code generation, and review. Start
empty and add rules as you discover what agents get wrong.

```json
{
  "rules": [
    {
      "id": "error-handling",
      "text": "All errors must be handled or explicitly propagated. No silently swallowed errors.",
      "severity": "must",
      "category": "code-quality",
      "origin": "manual"
    },
    {
      "id": "test-coverage",
      "text": "All new functions must have corresponding test cases.",
      "severity": "should",
      "category": "testing",
      "origin": "manual"
    }
  ]
}
```

### Rule Fields

| Field | Description |
|-------|-------------|
| `id` | Unique identifier |
| `text` | The rule, stated as a requirement |
| `severity` | `must` (blocks approval), `should` (flagged), `may` (informational) — RFC 2119 |
| `category` | Grouping: `code-quality`, `testing`, `security`, `documentation`, etc. |
| `origin` | `manual` (human-written) or `generated` (from `POST /api/project/generate-standards`) |
| `roles` | Optional array restricting which agent roles see the rule |

Standards are short, machine-readable statements applied globally. For richer, scoped
enforcement with examples, use SOPs (below).

## checklist.json

Deterministic quality gates — shell commands that run after each agent task. A failing
`required` check blocks progression to review. Tailor these to your stack.

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
      "description": "Run Go tests"
    }
  ]
}
```

### Check Fields

| Field | Description |
|-------|-------------|
| `name` | Unique identifier |
| `command` | Shell command to run |
| `trigger` | Glob patterns — check runs only when matching files were modified |
| `category` | `compile`, `lint`, `typecheck`, `test`, `format`, or `setup` |
| `required` | If `true`, failure blocks the review stage |
| `timeout` | Maximum execution time (e.g., `"120s"`) |
| `description` | Human-readable explanation |

## SOPs (Standard Operating Procedures)

SOPs are the advanced version of standards — Markdown files with YAML frontmatter stored
in `.semspec/sources/docs/`. They provide richer context than `standards.json` rules:

- **Scoped**: Target specific file patterns, semantic domains, or workflow stages
- **Structured**: Machine-readable requirements with ground truth and violation examples
- **Auto-ingested**: Semsource watches the directory and indexes new files automatically

### When to Use Standards vs SOPs

| | Standards | SOPs |
|---|-----------|------|
| **Format** | JSON rules in `standards.json` | Markdown files in `.semspec/sources/docs/` |
| **Scope** | Global — injected into every agent | Scoped by file pattern, domain, and workflow stage |
| **Detail** | Short statements | Full documents with examples and rationale |
| **Use for** | Universal rules (error handling, naming) | Nuanced, context-dependent guidance (migration procedures, API conventions) |

### SOP Format

Every SOP begins with YAML frontmatter:

```markdown
---
category: sop
scope: all
severity: warning
applies_to:
  - "api/**"
domain:
  - testing
  - api-design
requirements:
  - "All API endpoints must have corresponding tests"
  - "API responses must use JSON format"
---

# API Testing Standards

## Ground Truth

- The project uses Flask for the API backend
- Tests are expected alongside endpoint definitions

## Rules

1. Every API endpoint must have a corresponding test file
2. All API responses must return JSON format

## Violations

- Adding an endpoint without a test file
- Returning plain text instead of JSON from an API endpoint
```

### Frontmatter Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `category` | string | yes | Must be `"sop"` |
| `scope` | string | yes | `"plan"`, `"code"`, or `"all"` |
| `severity` | string | yes | `"error"` (blocks), `"warning"` (flags), `"info"` (context only) |
| `applies_to` | array | no | Glob patterns for file matching |
| `domain` | array | no | Semantic domains for cross-cutting matches |
| `requirements` | array | yes | Checkable rules, each a complete sentence |

**Scope**: `"plan"` = planning and plan review only. `"code"` = code review only. `"all"` = both.

When `applies_to` is omitted, the SOP matches all files within its scope.

### Body Structure

The three-section convention helps the LLM reason precisely about violations:

1. **Ground Truth** — Factual statements about existing codebase patterns (descriptive)
2. **Rules** — Numbered enforcement rules matching the frontmatter `requirements`
3. **Violations** — Concrete examples that anchor the LLM's judgment

### How SOPs Are Enforced

SOPs are ingested into the knowledge graph by semsource (file watcher or NATS message).
The `context-builder` retrieves matching SOPs during context assembly and injects them
into the LLM prompt.

| Stage | Behavior |
|-------|----------|
| Planning | Best-effort — included if token budget allows |
| Plan Review | All-or-nothing — review fails if SOPs can't be loaded |
| Code Review | All-or-nothing — pattern + domain + cross-domain matching |

The plan-reviewer validates plans against each SOP requirement and returns structured
findings. Violations trigger the planner to regenerate with the findings as context.
This retry loop runs up to three times before escalating to the user.

## Related Documentation

| Document | Description |
|----------|-------------|
| [How It Works](how-it-works.md) | Pipeline overview and component groups |
| [Model Configuration](model-configuration.md) | LLM capability-to-model mapping |
| [API Reference](api.md) | Full API surface including project setup endpoints |
