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

Standards (`standards.json`) are not gated — `POST /project-manager/init` seeds 17 baseline
standards (5 OWASP security + 12 language-agnostic engineering rules) so you have something
useful from day one. You can append project-specific standards as you learn what agents get
wrong, or start with `{"items": []}` if you want a clean slate.

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

# Empty standards — `POST /project-manager/init` seeds the baseline; this empty form opts out
echo '{"version":"1.0.0","items":[]}' > .semspec/standards.json

# Empty checklist — add quality gates for your stack
echo '{"version":"1","checks":[]}' > .semspec/checklist.json
```

## API-Driven Setup

The project-manager provides endpoints for automated setup. See the
[API Reference](api.md#project-setup--project-manager) or the Swagger UI at `/docs`.

```bash
curl -X POST http://localhost:8080/project-manager/detect          # Auto-detect stack
curl -X POST http://localhost:8080/project-manager/generate-standards  # Generate from detected stack
curl -X POST http://localhost:8080/project-manager/init \           # Write all three files
  -H "Content-Type: application/json" \
  -d '{"name": "my-project", "description": "..."}'
```

## project.json

Project metadata used for context assembly and prompt generation. Detected automatically
via `POST /project-manager/detect` or created manually.

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
| `qa_level` | QA depth applied at plan completion — see below |
| `qa_test_command` | Unit-level test command override (default inferred from language) |

`org` and `platform` are locked after the first plan is created to prevent entity ID divergence.

## QA Level

After a plan's requirements are implemented, semspec runs a final release-readiness
gate via the **qa-reviewer** (Murat, the BMAD QA Test Architect). The depth of that
gate is controlled by `qa_level` on `project.json`, which is snapshotted onto every
plan at creation so policy changes don't retroactively affect in-flight work.

| Level | Executor | What runs | When to use |
|-------|----------|-----------|-------------|
| `none` | — | No QA gate; plan goes straight to `complete` | Doc-only hotfixes |
| `synthesis` | qa-reviewer only | LLM verdict on plan artifacts, no test execution | Default; fast, content-based check |
| `unit` | sandbox | `go test ./...` (or language default) against the merged worktree | Most projects |
| `integration` | qa-runner + `act` | `.github/workflows/qa.yml` job `integration` — typically tagged integration tests with real service dependencies | Projects with integration suites |
| `full` | qa-runner + `act` | Both `integration` + `e2e` jobs — adds Playwright browser flows | Projects with UI + browser tests |

Configure via `.semspec/project.json`:

```json
{
  "name": "my-project",
  "languages": [{"name": "Go", "primary": true}],
  "qa_level": "unit",
  "qa_test_command": "go test ./... -race"
}
```

Or at runtime:

```bash
curl -X PATCH http://localhost:8080/project-manager/config \
  -H "Content-Type: application/json" \
  -d '{"qa_level": "integration"}'
```

**Sandbox** (level=unit) runs the project's native test command in the same
container semspec uses for per-task structural validation. **qa-runner**
(level=integration/full) invokes nektos/act against `.github/workflows/qa.yml`
using the host Docker daemon via a mounted socket — tests run in real GitHub
Actions runner images (catthehacker/ubuntu:act-latest). The qa.yml template is
scaffolded by `POST /project-manager/init` when missing; customize jobs, add
`services:` entries for databases, or change runner images as needed.

Artifacts from every QA run land at `.semspec/qa-artifacts/{plan-slug}/{run-id}/`:
- `act.log` — combined act stdout+stderr
- Any files the workflow uploads via `actions/upload-artifact` (coverage,
  playwright traces, screenshots, etc.)

The verdict has three outcomes: `approved` (plan → `complete`), `needs_changes`
(plan → `rejected` with `PlanDecisions` describing what to fix), or
`rejected` (plan → `rejected`, escalation to human).

See [ADR-031](adr/ADR-031-qa-test-execution.md) for the full design rationale.

## standards.json

Standards injected into every agent's context — planning, code generation, and review.
`POST /project-manager/init` seeds a 17-rule baseline (5 OWASP security + 12 language-agnostic
engineering standards). Append project-specific standards as you discover what agents get
wrong in your codebase.

```json
{
  "version": "1.0.0",
  "generated_at": "2026-04-14T10:00:00Z",
  "updated_at": "2026-04-14T10:00:00Z",
  "token_estimate": 480,
  "items": [
    {
      "id": "eng-test-coverage",
      "text": "All new or modified behavior must have corresponding tests, and each test must trace back to a specific scenario or requirement (referenced by ID in a comment, test name, or description). Untested code is unfinished code; untraceable tests are unverifiable claims.",
      "severity": "must",
      "category": "engineering",
      "roles": ["developer", "reviewer"],
      "origin": "init"
    },
    {
      "id": "sec-parameterized-queries",
      "text": "Database queries must use parameterized statements. Never concatenate user input into SQL or shell commands.",
      "severity": "must",
      "category": "security",
      "applies_to": ["**/*repo*", "**/*store*", "**/*query*", "**/*db*"],
      "roles": ["developer", "reviewer"],
      "origin": "init"
    },
    {
      "id": "team-prefer-functional-options",
      "text": "Public constructors with more than two parameters must use the functional options pattern (NewX(opts ...Option)).",
      "severity": "should",
      "category": "engineering",
      "roles": ["developer", "reviewer"],
      "origin": "manual"
    }
  ]
}
```

### Top-Level Fields

| Field | Description |
|-------|-------------|
| `version` | Schema version (currently `"1.0.0"`) |
| `generated_at` | RFC3339 timestamp of last full regeneration |
| `updated_at` | RFC3339 timestamp of last mutation (used for graph reconciliation) |
| `approved_at` | Optional RFC3339 timestamp of human approval (omitted = pending) |
| `token_estimate` | Approximate combined token count — used by the context-builder budget |
| `items` | Ordered list of standards |

### Standard Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Stable unique identifier (e.g., `eng-test-coverage`) |
| `text` | yes | The standard, stated as a single concrete requirement |
| `severity` | yes | RFC 2119: `must` (blocks reviewer approval), `should` (flagged but not blocking), `may` (informational) |
| `category` | yes | Grouping. Baseline uses `security` and `engineering` — add your own as needed |
| `origin` | yes | Provenance: `init` (seeded baseline), `manual` (human-authored), `review-pattern` (promoted from recurring review feedback), or `sop:<filename>` (derived from an SOP file) |
| `applies_to` | no | Glob patterns scoping the standard to matching files. Empty/omitted means all files |
| `roles` | no | Restrict to specific agent roles (`developer`, `reviewer`, `scenario-reviewer`, `plan-reviewer`, `architect`). Empty means all roles |

### Severity Behaviour

- `must` — A violation is treated as an error. Reviewer approval is blocked.
- `should` — A violation is surfaced as a warning. Does not block approval but is noted in the review.
- `may` — Informational only. Does not affect approval.

### Tip: Trace Tests to Specs

The baseline `eng-test-coverage` standard requires every test to reference the scenario or
requirement it verifies. Conventions that satisfy this:

- Test name embeds the ID: `func TestUserLogin_scenario_user_login_3(t *testing.T)`
- Comment block above the test: `// scenario.user-login.3 — invalid password rejected`
- BDD framework description: `it('verifies scenario.user-login.3', ...)`

Reviewers (and the rollup-reviewer) check for these references.

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
| `working_dir` | Optional. Directory in which to run the command, relative to repo root. Defaults to `"."` |

## Related Documentation

| Document | Description |
|----------|-------------|
| [How It Works](how-it-works.md) | Pipeline overview and component groups |
| [Model Configuration](model-configuration.md) | LLM capability-to-model mapping |
| [API Reference](api.md) | Full API surface including project setup endpoints |
