# Project Setup

Semspec requires a `.semspec/` directory in your repository with three configuration files.
These tell agents what languages you use, what standards to follow, and what quality gates
to enforce. You can create them manually or via the API.

```
.semspec/
├── project.json          # Project metadata (languages, frameworks, tooling)
├── standards.json        # Rules injected into every agent's context
└── checklist.json        # Shell commands that run after each agent task
```

Without these files, semspec will start but agents won't have project-specific context.

**Recommended:** The Web UI at `/settings` handles setup automatically — it auto-detects
your stack and creates all three config files. The manual steps below are for scripted or
CI environments.

## Quick Start (UI)

The Web UI at `/settings` lets you edit project config, standards, and checklist directly.
On first launch, the UI auto-detects your stack and creates the three config files.

The UI **hard-gates** on three fields — if any are missing, it redirects to `/settings`
and shows a warning banner until they're set:

| Required | Why |
|----------|-----|
| `name` | Slugified into `platform` — the second segment of every entity ID |
| `org` | First segment of every entity ID — can't be changed after the first plan |
| `checklist.json` | Without quality gates, the structural-validator has nothing to run — code passes unchecked |

Standards (`standards.json`) are not gated — you can start with an empty rules array and
add rules as you learn what agents get wrong. Without standards, the plan-reviewer still
runs but has no project-specific rules to validate against, so reviews won't be tailored
to your codebase.

## Quick Start (Manual)

```bash
cd /path/to/your/project
mkdir -p .semspec

# Project metadata
cat > .semspec/project.json << 'EOF'
{
  "name": "my-project",
  "org": "mycompany",
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
  "org": "mycompany",
  "platform": "myapp",
  "languages": [
    {"name": "Go", "version": "1.25", "primary": true},
    {"name": "TypeScript", "version": "5.4", "primary": false}
  ],
  "frameworks": [
    {"name": "SvelteKit", "language": "TypeScript"}
  ],
  "tooling": {
    "task_runner": "Taskfile",
    "linters": ["revive", "eslint"],
    "test_frameworks": ["testing", "vitest"],
    "ci": "GitHub Actions",
    "container": "Docker Compose"
  },
  "repository": {
    "url": "github.com/mycompany/myapp",
    "default_branch": "main"
  }
}
```

### project.json Fields

| Field | Description |
|-------|-------------|
| `name` | Human-readable project name |
| `description` | Brief project description |
| `version` | Config schema version (always `"1"`) |
| `org` | Organization segment for entity IDs (default: `"semspec"`) |
| `platform` | Project slug for entity IDs — must be unique per org when federating (default: derived from name) |
| `languages` | Array of `{name, version, primary}` — detected languages |
| `frameworks` | Array of `{name, language}` — detected frameworks |
| `tooling` | `{task_runner, linters[], test_frameworks[], ci, container}` |
| `repository` | `{url, default_branch}` — VCS metadata |

`org` and `platform` are locked after the first plan is created to prevent entity ID divergence.

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

Start empty and add rules as you discover what agents get wrong. The `roles` field lets
you restrict rules to specific agent roles (e.g., only `developer` or `reviewer`).

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

## Related Documentation

| Document | Description |
|----------|-------------|
| [How It Works](how-it-works.md) | Pipeline overview and component groups |
| [Model Configuration](model-configuration.md) | LLM capability-to-model mapping |
| [API Reference](api.md) | Full API surface including project setup endpoints |
