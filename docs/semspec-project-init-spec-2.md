# SemSpec Project Initialization Spec

**Status**: Ready for implementation
**Origin**: Architecture session 2025-02-19
**Scope**: Project detection, setup wizard UI, structural quality gates, standards synthesis
**Priority**: MVP — blocks effective agent execution

---

## Problem Statement

SemSpec has no project initialization step. Users clone semspec, start docker, and immediately run `/plan` without any project-specific configuration. This means:

1. **No structural quality gates.** Agents produce code that may not compile, lint, or pass tests. The reviewer role catches some of this but wastes tokens on issues that deterministic checks would catch instantly.
2. **No synthesized standards.** SOPs exist as individual markdown files but agents — especially post-compaction — lose awareness of them. There is no compact, always-injected rules document.
3. **No stack awareness.** SemSpec doesn't know what languages, frameworks, or tooling the target project uses. Context assembly and task generation operate without this foundational knowledge.
4. **Token waste.** Without quality gates and standards, agents cycle through review loops for preventable issues. Every skipped lint error or missed convention costs an LLM round-trip.

## Solution Overview

Add a project initialization flow that produces three configuration artifacts and one set of ingested SOPs. The flow runs as a setup wizard in the web UI, triggered automatically when SemSpec detects an uninitialized project.

### File Layout After Init

```
.semspec/
├── project.json          # Project metadata, detected stack, model preferences
├── checklist.json        # Structural quality gates (deterministic commands)
├── standards.json        # Rules for always-on agent injection (seeded at init, grows over time)
└── sources/
    └── docs/             # SOPs authored by the user as conventions emerge (empty at init)
```

### Design Principles

- **File-first for config, graph-first for queryable knowledge.** `project.json`, `checklist.json`, and `standards.json` are static config files read directly by components. SOPs flow through the source-ingester into the graph for relational queries. This avoids graph overhead for non-relational data.
- **Detect, propose, confirm.** Every configuration is proposed based on detection, then confirmed by the user. Nothing activates without human approval.
- **Progressive enhancement.** SemSpec works without init, but surfaces clear warnings about what's missing. Init is strongly encouraged, not hard-gated.

---

## Architecture

### Detection Layer (No LLM Cost)

Stack detection runs entirely from file-presence checks. No LLM calls, no graph queries.

#### Language Detection

| Marker File | Language | Secondary Signals |
|-------------|----------|-------------------|
| `go.mod` | Go | `go.sum`, `*.go` files |
| `package.json` | Node.js | Check for framework signals below |
| `pyproject.toml` | Python | `setup.py`, `requirements.txt`, `Pipfile` |
| `Cargo.toml` | Rust | `*.rs` files |
| `pom.xml` | Java | `build.gradle`, `*.java` files |
| `composer.json` | PHP | `*.php` files |
| `Gemfile` | Ruby | `*.rb` files |
| `*.csproj` | C# / .NET | `*.sln` files |

#### Framework Detection (Node.js)

When `package.json` is detected, inspect dependencies:

| Signal | Framework |
|--------|-----------|
| `svelte` in dependencies | Svelte/SvelteKit |
| `next` in dependencies | Next.js |
| `react` in dependencies | React |
| `vue` in dependencies | Vue |
| `angular` in dependencies | Angular |
| `express` in dependencies | Express |

#### Tooling Detection

| Marker File | Tool | Category |
|-------------|------|----------|
| `.eslintrc*`, `eslint.config.*` | ESLint | Linter |
| `revive.toml` | Revive | Linter |
| `.prettierrc*` | Prettier | Formatter |
| `Taskfile.yml` | Taskfile | Task runner |
| `Makefile` | Make | Task runner |
| `Justfile` | Just | Task runner |
| `.github/workflows/` | GitHub Actions | CI |
| `Dockerfile` | Docker | Container |
| `docker-compose.yml` | Docker Compose | Container |
| `pytest.ini`, `conftest.py` | Pytest | Test framework |
| `jest.config.*` | Jest | Test framework |
| `vitest.config.*` | Vitest | Test framework |
| `tsconfig.json` | TypeScript | Type checking |
| `.golangci.yml` | golangci-lint | Linter |
| `biome.json` | Biome | Linter/Formatter |
| `ruff.toml`, `pyproject.toml` [tool.ruff] | Ruff | Linter |

#### Existing Documentation Detection

| Path | Type | Usage |
|------|------|-------|
| `README.md` | Project docs | Feed into SOP generation context |
| `CONTRIBUTING.md` | Contribution guide | Extract existing conventions |
| `CLAUDE.md` | Claude instructions | Extract project-specific rules |
| `.semspec/sources/docs/*.md` | Existing SOPs | Skip SOP generation if sufficient |
| `docs/` | Documentation dir | Scan for architecture/convention docs |

### Backend API

#### `GET /api/project/status`

Returns initialization state. Called by UI on load to determine whether to show setup wizard.

```json
{
  "initialized": false,
  "has_project_json": false,
  "has_checklist": false,
  "has_standards": false,
  "sop_count": 0,
  "workspace_path": "/workspace"
}
```

When `initialized: true`, all three config files exist. The UI uses this to decide whether to show the board view or the setup wizard.

#### `POST /api/project/detect`

Triggers stack detection. Returns detected configuration without writing anything.

**Request**: Empty body (uses configured repo path)

**Response**:

```json
{
  "languages": [
    {
      "name": "Go",
      "version": "1.25",
      "marker": "go.mod",
      "confidence": "high"
    },
    {
      "name": "TypeScript",
      "version": null,
      "marker": "tsconfig.json",
      "confidence": "high"
    }
  ],
  "frameworks": [
    {
      "name": "SvelteKit",
      "language": "TypeScript",
      "marker": "svelte.config.js",
      "confidence": "high"
    }
  ],
  "tooling": [
    {
      "name": "Revive",
      "category": "linter",
      "language": "Go",
      "marker": "revive.toml"
    },
    {
      "name": "ESLint",
      "category": "linter",
      "language": "TypeScript",
      "marker": "eslint.config.js"
    },
    {
      "name": "Taskfile",
      "category": "task_runner",
      "marker": "Taskfile.yml"
    },
    {
      "name": "Docker Compose",
      "category": "container",
      "marker": "docker-compose.yml"
    }
  ],
  "existing_docs": [
    {
      "path": "README.md",
      "type": "project_docs",
      "size_bytes": 4200
    },
    {
      "path": "CLAUDE.md",
      "type": "claude_instructions",
      "size_bytes": 8500
    }
  ],
  "proposed_checklist": [
    {
      "name": "go-build",
      "command": "go build ./...",
      "trigger": ["*.go", "go.mod", "go.sum"],
      "language": "Go",
      "category": "compile",
      "required": true,
      "description": "Compile all Go packages"
    },
    {
      "name": "go-vet",
      "command": "go vet ./...",
      "trigger": ["*.go"],
      "language": "Go",
      "category": "lint",
      "required": true,
      "description": "Run Go static analysis"
    },
    {
      "name": "go-test",
      "command": "go test ./...",
      "trigger": ["*.go"],
      "language": "Go",
      "category": "test",
      "required": true,
      "description": "Run Go test suite"
    },
    {
      "name": "revive",
      "command": "revive -config revive.toml ./...",
      "trigger": ["*.go"],
      "language": "Go",
      "category": "lint",
      "required": false,
      "description": "Run Revive linter with project config"
    },
    {
      "name": "svelte-check",
      "command": "cd ui && npm run check",
      "trigger": ["ui/src/**/*.svelte", "ui/src/**/*.ts"],
      "language": "TypeScript",
      "category": "typecheck",
      "required": true,
      "description": "Svelte type checking"
    },
    {
      "name": "eslint",
      "command": "cd ui && npx eslint src/",
      "trigger": ["ui/src/**/*.svelte", "ui/src/**/*.ts"],
      "language": "TypeScript",
      "category": "lint",
      "required": false,
      "description": "Run ESLint on UI source"
    }
  ]
}
```

The `proposed_checklist` is generated deterministically from templates keyed to detected languages and tooling. No LLM call needed.

#### Checklist Templates (Internal)

Each detected language/tool maps to a set of default checks. These templates are compiled into the semspec binary, not user-configurable:

```go
// Example template structure (internal to detection package)
var goChecks = []CheckTemplate{
    {Name: "go-build", Command: "go build ./...", Category: "compile", Required: true},
    {Name: "go-vet", Command: "go vet ./...", Category: "lint", Required: true},
    {Name: "go-test", Command: "go test ./...", Category: "test", Required: true},
}

var goReviveChecks = []CheckTemplate{
    {Name: "revive", Command: "revive -config revive.toml ./...", Category: "lint", Required: false},
}

var nodeChecks = []CheckTemplate{
    {Name: "npm-test", Command: "npm test", Category: "test", Required: true},
}
// ... etc
```

When a Taskfile or Makefile is detected, the detection layer inspects it for well-known task names (`test`, `lint`, `check`, `build`) and proposes those as checks instead of raw tool commands. This respects the project's existing conventions.

#### `POST /api/project/generate-standards`

Generates initial standards rules using an LLM call. This is the only LLM-consuming step in init.

**Request**:

```json
{
  "detection": { /* full detection response from /detect */ },
  "existing_docs_content": {
    "README.md": "# My Project\n...",
    "CLAUDE.md": "## Coding Standards\n..."
  }
}
```

The UI reads detected existing docs and sends their content so the LLM has project context.

**Response**:

```json
{
  "rules": [
    {
      "id": "test-coverage",
      "text": "All new code must include tests. Test files must be in the same package as the code they test.",
      "severity": "error",
      "category": "testing",
      "applies_to": ["*.go"],
      "rationale": "Detected Go test files and Taskfile test target."
    },
    {
      "id": "error-wrapping",
      "text": "Errors must be wrapped with fmt.Errorf and %w when crossing package boundaries.",
      "severity": "warning",
      "category": "code-quality",
      "applies_to": ["*.go"],
      "rationale": "Go project with multiple packages. Error wrapping prevents context loss."
    }
  ],
  "token_estimate": 620
}
```

The LLM generates rules directly — no intermediate SOP files. The system prompt for this call should emphasize:

- Generate rules in the standards format (id, text, severity, category, applies_to)
- Focus on conventions that are detectable/enforceable, not aspirational
- Draw from existing docs (README, CONTRIBUTING, CLAUDE.md) when available
- Keep rules concrete and specific to the detected stack
- Fewer high-confidence rules are better than many weak ones
- Each rule should be a single sentence or two that an agent can follow without ambiguity
- Target 5-15 rules maximum for initial standards

#### `POST /api/project/init`

Writes all confirmed configuration to disk. Called once at the end of the wizard.

**Request**:

```json
{
  "project": {
    "name": "semspec",
    "description": "Graph-first agentic dev tool",
    "languages": ["Go", "TypeScript"],
    "frameworks": ["SvelteKit"],
    "repository": "github.com/c360studio/semspec"
  },
  "checklist": [
    {
      "name": "go-build",
      "command": "go build ./...",
      "trigger": ["*.go", "go.mod"],
      "category": "compile",
      "required": true,
      "description": "Compile all Go packages"
    }
  ],
  "standards": {
    "version": "1.0.0",
    "rules": [...]
  }
}
```

**Response**:

```json
{
  "success": true,
  "files_written": [
    ".semspec/project.json",
    ".semspec/checklist.json",
    ".semspec/standards.json"
  ]
}
```

After this endpoint returns, static config files are immediately available to all components. No graph ingestion needed — these are file-first artifacts.

---

## File Schemas

### `.semspec/project.json`

Project metadata and detected stack information. Read by context-builder for stack-aware context assembly and by future components that need project awareness.

```json
{
  "name": "semspec",
  "description": "Graph-first agentic dev tool",
  "version": "1.0.0",
  "initialized_at": "2025-02-19T10:30:00Z",
  "languages": [
    {
      "name": "Go",
      "version": "1.25",
      "primary": true
    },
    {
      "name": "TypeScript",
      "version": null,
      "primary": false
    }
  ],
  "frameworks": [
    {
      "name": "SvelteKit",
      "language": "TypeScript"
    }
  ],
  "tooling": {
    "task_runner": "Taskfile",
    "linters": ["revive", "eslint"],
    "test_frameworks": ["go test", "vitest"],
    "ci": "GitHub Actions",
    "container": "Docker Compose"
  },
  "repository": {
    "url": "github.com/c360studio/semspec",
    "default_branch": "main"
  }
}
```

### `.semspec/checklist.json`

Structural quality gates. Read by the structural validation workflow step.

```json
{
  "version": "1.0.0",
  "created_at": "2025-02-19T10:30:00Z",
  "checks": [
    {
      "name": "go-build",
      "command": "go build ./...",
      "trigger": ["*.go", "go.mod", "go.sum"],
      "category": "compile",
      "required": true,
      "timeout": "120s",
      "description": "Compile all Go packages",
      "working_dir": "."
    },
    {
      "name": "go-vet",
      "command": "go vet ./...",
      "trigger": ["*.go"],
      "category": "lint",
      "required": true,
      "timeout": "60s",
      "description": "Run Go static analysis",
      "working_dir": "."
    },
    {
      "name": "go-test",
      "command": "go test ./...",
      "trigger": ["*.go"],
      "category": "test",
      "required": true,
      "timeout": "300s",
      "description": "Run Go test suite",
      "working_dir": "."
    },
    {
      "name": "svelte-check",
      "command": "npm run check",
      "trigger": ["*.svelte", "*.ts"],
      "category": "typecheck",
      "required": true,
      "timeout": "120s",
      "description": "Svelte/TypeScript type checking",
      "working_dir": "ui"
    }
  ]
}
```

**Field definitions:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique identifier for this check |
| `command` | string | yes | Shell command to execute |
| `trigger` | string[] | yes | Glob patterns for files that trigger this check |
| `category` | enum | yes | `compile`, `lint`, `typecheck`, `test`, `format` |
| `required` | bool | yes | If true, failure blocks progression to reviewer |
| `timeout` | string | yes | Max execution time (Go duration format) |
| `description` | string | yes | Human-readable description |
| `working_dir` | string | no | Working directory relative to repo root (default: `.`) |

**Trigger matching**: When the structural validation step runs, it compares the list of files modified by the developer agent against each check's `trigger` patterns. Only checks whose triggers match at least one modified file are executed. This avoids running Go checks when only TypeScript files changed.

### `.semspec/standards.json`

Rules for always-on agent context injection. Seeded during init from stack detection and existing docs, then grows over time as SOPs are authored and review patterns emerge.

```json
{
  "version": "1.0.0",
  "generated_at": "2025-02-19T10:30:00Z",
  "token_estimate": 620,
  "rules": [
    {
      "id": "test-coverage",
      "text": "All new code must include tests. Test files must be in the same package as the code they test. Use table-driven tests for multiple cases.",
      "severity": "error",
      "category": "testing",
      "applies_to": ["*.go"],
      "origin": "init"
    },
    {
      "id": "error-wrapping",
      "text": "Errors crossing package boundaries must be wrapped with fmt.Errorf and %w. Never discard errors silently. Use sentinel errors for expected conditions.",
      "severity": "warning",
      "category": "code-quality",
      "applies_to": ["*.go"],
      "origin": "init"
    }
  ]
}
```

**Origin tracking**: Each rule tracks where it came from via the `origin` field:

| Origin Value | Meaning |
|-------------|---------|
| `init` | Generated during project initialization |
| `sop:<filename>` | Derived from a user-authored SOP file |
| `review-pattern` | Suggested by the review flywheel (see Roadmap) |
| `manual` | Added by the user directly |

This matters for the future flywheel — when the system suggests a new rule based on review patterns, the user can distinguish machine-proposed rules from ones they wrote or confirmed during init.

**Token budget constraint**: Standards must fit within 4,000 tokens. The generation/synthesis step enforces this. If SOPs produce more rules than fit, they are prioritized by severity (`error` > `warning` > `info`) and the synthesis truncates with a note.

**Injection behavior**: The context-builder reads `standards.json` at the start of every context assembly, regardless of strategy. Standards are injected as a preamble section before any strategy-specific content. This ensures agents always have baseline rules even if graph queries fail or context budget is tight.

**Regeneration**: Standards must regenerate when SOPs change. The source-ingester should trigger standards regeneration after successfully ingesting a new or modified SOP file. This can be a NATS publish to a `standards.regenerate` subject that a handler picks up.

---

## Workflow Integration: Structural Validation Step

### New Workflow Step

Add a `structural_check` step to `plan-and-execute.json` between `developer` and `reviewer`:

```json
{
  "name": "structural_check",
  "action": {
    "type": "structural_validation",
    "checklist_path": ".semspec/checklist.json",
    "files_from": "${steps.developer.output.files_modified}"
  },
  "on_success": "reviewer",
  "on_fail": "structural_feedback",
  "timeout": "5m"
},
{
  "name": "structural_feedback",
  "action": {
    "type": "publish_agent",
    "subject": "agent.task.development",
    "role": "developer",
    "model": "${trigger.payload.model:-qwen}",
    "prompt_template": "developer_structural_fix",
    "prompt_context": {
      "check_results": "${steps.structural_check.output.results}",
      "failed_checks": "${steps.structural_check.output.failed}"
    }
  },
  "on_success": "structural_check",
  "on_fail": "developer_failed",
  "timeout": "10m"
}
```

### Structural Validation Action Type

This is a new action type in the workflow processor — `structural_validation`. Unlike `publish_agent`, this does NOT call an LLM. It:

1. Reads `checklist.json` from the configured path
2. Gets the list of files modified from the developer step output
3. Matches modified files against each check's `trigger` patterns
4. Executes matched checks in the container
5. Collects results (exit code, stdout, stderr per check)
6. Returns pass/fail with structured output

**Output schema:**

```json
{
  "passed": false,
  "checks_run": 3,
  "checks_passed": 2,
  "checks_failed": 1,
  "checks_skipped": 1,
  "results": [
    {
      "name": "go-build",
      "category": "compile",
      "passed": true,
      "exit_code": 0,
      "duration_ms": 2340,
      "stdout": "",
      "stderr": ""
    },
    {
      "name": "go-test",
      "category": "test",
      "passed": false,
      "exit_code": 1,
      "duration_ms": 8200,
      "stdout": "--- FAIL: TestAuth (0.02s)\n    auth_test.go:45: expected token, got nil",
      "stderr": "",
      "required": true
    }
  ],
  "failed": ["go-test"],
  "summary": "1 of 3 checks failed: go-test (test). Fix failing tests before review."
}
```

**Pass/fail logic**: The step passes only if all `required: true` checks pass. Non-required checks that fail are included in the output as warnings but don't block progression.

### Developer Retry Prompt for Structural Failures

When structural checks fail, the developer gets bounced back with the exact error output — no LLM interpretation needed. Create a new prompt template `developer_structural_fix`:

```
You are a developer fixing structural issues found by automated checks.

## Failed Checks

The following automated checks failed. These are deterministic — the exact
errors are shown below. Fix ALL of them before your code can proceed to review.

${check_results}

## Rules

- Fix EVERY failing check
- Do not introduce new failures in passing checks
- Run the failing commands locally if you need more context
- These are automated checks, not subjective feedback — the code must pass them
```

### Graceful Degradation

If `.semspec/checklist.json` does not exist, the `structural_check` step is a **no-op pass** with a warning logged to the activity feed:

```json
{
  "passed": true,
  "checks_run": 0,
  "warning": "No checklist.json found. Structural validation skipped. Run project setup to configure quality gates."
}
```

This warning surfaces in the UI as an attention item so the user knows they're operating without quality gates.

---

## UI: Navigation Change — Activity as Home

### Rationale

The board-as-home decision from the UI redesign was a correct critique of chat-only as the default, but overcorrected. A project management dashboard is a *destination* you navigate to deliberately, not a *starting point*. Activity — where commands happen, agent output streams, and the system shows its work — is where users actually live.

For new users especially, an activity view with a chat input is a more natural entry point than an empty kanban board. The setup wizard renders inline within activity, and once init completes, the user watches initialization results stream in — SOPs ingested, standards generated, checklist configured. Their first experience is the system coming alive, not an empty dashboard.

### Navigation Restructure

**Current nav (from UI redesign):**
```
Board (/) — PRIMARY
Changes (/changes)
Activity (/activity)
History (/history)
Settings (/settings)
```

**Updated nav:**
```
Activity (/) — PRIMARY — chat input, agent output, live feed, setup wizard
Board (/board) — project overview, pipeline status, attention items
History (/history) — past sessions, completed work
Settings (/settings)
```

Changes view is merged into Board — the board shows pipeline cards with status indicators. This reduces nav items and avoids two views that show overlapping plan/change information.

**Attention banner** still shows pending approvals, unanswered questions, and failed tasks. It renders in the Activity view header, giving users the "what needs me" signal without navigating away.

### Route Changes

1. Move `BoardView.svelte` from `/` to `/board` (new route: `ui/src/routes/board/+page.svelte`)
2. Move Activity view to `/` (update `ui/src/routes/+page.svelte`)
3. Update `Sidebar.svelte` nav items to reflect new order
4. Update "New Plan" and "Go to Activity" links throughout the UI

## UI: Setup Wizard

### Entry Point

The setup wizard renders inline within the Activity view when the project is uninitialized. It is NOT a separate route.

**Modification to the Activity view (`+page.svelte` at `/`):**

```
onMount:
  1. Call GET /api/project/status
  2. If initialized: true → show activity feed + chat input as normal
  3. If initialized: false → show SetupWizard component in the content area
```

The activity view is always home. When the project is unconfigured, home becomes the setup wizard. Once init completes, the wizard transitions to the normal activity view where the user can immediately start working.

### Component: `SetupWizard.svelte`

Three-panel wizard with step indicators. Each panel has a confirm action. The user can go back to previous panels to edit.

### Panel 1: "Here's what we found" (Project Detection)

**On mount**: Calls `POST /api/project/detect`

**Displays**:
- Detected languages with version and confidence
- Detected frameworks
- Detected tooling (grouped by category: linters, test frameworks, task runners, CI)
- Existing documentation found

**User actions**:
- Review detection results
- Edit project name and description (text inputs)
- Optionally add repository URL
- Click "Next" to proceed to Panel 2

**No LLM cost.** Everything is deterministic file detection.

### Panel 2: "Your Quality Checklist" (Structural Checks)

**Displays**: The `proposed_checklist` from detection results, rendered as an editable list.

Each check shows:
- Name (editable)
- Command (editable)
- Category badge (compile / lint / test / typecheck / format)
- Required toggle (switch)
- Trigger patterns (editable, shown as tags)
- Delete button

**User actions**:
- Toggle checks on/off (required vs optional)
- Edit commands (for non-standard project layouts)
- Add custom checks (opens a form row)
- Remove checks that don't apply
- Click "Next" to proceed to Panel 3

**No LLM cost.** Proposed checks come from templates.

### Panel 3: "Project Standards" (Rules Generation)

**On entry**: Calls `POST /api/project/generate-standards` with detection results and existing doc content. Shows a loading state ("Analyzing your project and generating standards...").

**Displays**: Generated rules as an editable list. Each rule shows:
- Rule text (editable text area)
- Severity badge (error / warning — toggleable)
- Category tag (testing, code-quality, architecture, etc.)
- Applies-to patterns (editable, shown as tags)
- Rationale (why this rule was proposed — shown as help text)
- Delete button

**User actions**:
- Edit rule text directly
- Change severity
- Edit applies-to patterns
- Delete rules that don't apply
- Add custom rules (opens a form row)
- Request regeneration ("Regenerate" button re-calls the LLM)
- Click "Initialize Project" to finalize

**One LLM call** (or re-calls if user requests regeneration).

**Design note**: This is intentionally NOT an SOP editor. SOPs come later as the user discovers conventions worth codifying in detail. Init-time standards are the seed — a compact set of rules that give agents immediate guardrails. The user can always add SOPs to `.semspec/sources/docs/` later, which will trigger standards regeneration to merge SOP-derived rules alongside the init rules.

### Panel: Completion

After `POST /api/project/init` succeeds:

- Show confirmation with list of files written
- "Your project is ready. Quality gates and standards are active."
- Wizard transitions to the normal activity view (chat input + live feed) — the user can immediately start with `/plan`
- The activity feed shows initialization events streaming in: SOPs ingested, standards generated, checklist configured

### Wizard State Management

Create a new store: `ui/src/lib/stores/setup.svelte.ts`

```typescript
interface SetupState {
  step: 'detecting' | 'detection' | 'checklist' | 'standards' | 'initializing' | 'complete';
  detection: DetectionResult | null;
  project: ProjectConfig;
  checklist: ChecklistItem[];
  sops: GeneratedSOP[];
  standards: StandardsSynthesis | null;
  error: string | null;
  loading: boolean;
}
```

### Uninitialized Warning (Post-Init Skip)

If a user dismisses or bypasses init and runs `/plan` from the activity chat, the plan-coordinator should check for initialization and surface a warning in the response:

```
⚠️ Project not initialized. Agents will operate without quality gates or standards.
Refresh the page to start the setup wizard, or continue at your own risk.
```

This is informational, not blocking.

---

## Standards Injection into Agent Context

### Context-Builder Modification

The context-builder currently assembles context via strategies (Planning, Implementation, Review, etc.). Add a new **pre-strategy step** that runs before any strategy:

```go
// In builder.go, before strategy execution:
func (b *Builder) assembleContext(ctx context.Context, req *ContextBuildRequest) (*ContextResponse, error) {
    budget := b.calculateBudget(req)

    // ALWAYS inject standards first — this is non-negotiable baseline context
    standardsTokens, err := b.injectStandards(ctx, budget)
    if err != nil {
        b.logger.Warn("Failed to inject standards", "error", err)
        // Continue without standards — graceful degradation
    }
    budget.Consume(standardsTokens)

    // Then run strategy-specific assembly with remaining budget
    return b.executeStrategy(ctx, req, budget)
}
```

**Standards injection format** (what goes into the agent prompt):

```markdown
## Project Standards (Always Active)

These rules apply to all work on this project. Violations of error-severity
rules will cause review rejection.

- [ERROR] All new code must include tests. Test files must be in the same
  package as the code they test. Use table-driven tests for multiple cases.
- [WARNING] Errors crossing package boundaries must be wrapped with
  fmt.Errorf and %w. Never discard errors silently.
```

This is plain text, not JSON — optimized for LLM consumption. The context-builder reads `standards.json` and formats it as a markdown section.

### Standards Regeneration

Standards grow over time from two sources: user-authored SOPs and (future) review pattern detection. Regeneration merges all sources into a single `standards.json`.

When SOPs are added to `.semspec/sources/docs/`, the source-ingester detects the new files and triggers regeneration:

1. Source-ingester publishes to `standards.regenerate` after SOP ingestion
2. A handler reads current `standards.json` (preserving init and manual rules)
3. Handler reads all SOPs from `.semspec/sources/docs/`
4. LLM extracts rules from SOPs and merges with existing rules (deduplication by semantic similarity)
5. Writes updated `standards.json` with new rules tagged as `origin: "sop:<filename>"`
6. Context-builder picks up new standards on next read (no caching — reads file each time for simplicity in MVP)

**Merge behavior**: Init rules and manual rules are never removed by regeneration. SOP-derived rules can be added or updated. If an SOP is deleted, its derived rules are removed on next regeneration.

---

## Roadmap: Standards Training Flywheel

**Not in MVP.** This section documents the intended evolution where standards improve automatically from review feedback.

### The Pattern

The reviewer already captures patterns in its output:

```json
{
  "patterns": [
    {
      "name": "Context timeout in handlers",
      "pattern": "All HTTP handlers use context.WithTimeout",
      "applies_to": "handlers/*.go"
    }
  ]
}
```

These patterns are currently logged but not acted on. The flywheel closes this loop.

### How It Would Work

1. **Pattern accumulation**: After each review cycle, reviewer-identified patterns are stored in the graph as `review.pattern` entities with a counter tracking how many times the pattern has appeared.

2. **Threshold detection**: When a pattern appears across N reviews (configurable, default 3), the system flags it as a candidate standard.

3. **User notification**: The activity feed surfaces a prompt:
   ```
   📋 Pattern detected: "All HTTP handlers use context.WithTimeout" 
   has appeared in 3 consecutive reviews. Add as a project standard?
   [Accept] [Dismiss] [Edit first]
   ```

4. **Standard promotion**: If the user accepts, a new rule is added to `standards.json` with `origin: "review-pattern"`. If they edit first, they refine the rule text before it's added.

5. **Anti-pattern detection**: The inverse also works. If the reviewer consistently rejects the same mistake (e.g., "raw SQL instead of query builder"), that can surface as a candidate standard stating the correct approach.

### Why This Matters

This is the debrief loop closing automatically. Instead of requiring the user to manually notice recurring feedback, write an SOP, and wait for standards regeneration, the system:

- Observes what the reviewer is already enforcing
- Proposes codifying it so future agents know upfront
- Only codifies with user consent

The user's role shifts from "author of all standards" to "approver of observed patterns." Standards emerge from practice rather than being prescribed upfront. This matches the Marine Corps model — doctrine comes from lessons learned in the field, not from headquarters inventing rules in isolation.

### Implementation Notes (Future)

- Requires graph storage of review patterns with occurrence counting
- Needs a UI component for pattern notification and accept/dismiss/edit flow
- Standards regeneration must handle `review-pattern` origin rules alongside init and SOP rules
- Consider a decay mechanism — patterns that stop appearing after N reviews could be flagged for removal
- The threshold (N reviews) should be configurable per project in `project.json`

---

## Implementation Plan

### Phase 1: Backend Detection & Config (No UI)

**Estimated scope**: ~500 lines of Go

1. Create `project/detector.go` — file-presence scanner with language/framework/tooling detection
2. Create `project/templates.go` — checklist templates keyed to detected stacks
3. Create `project/config.go` — types for `project.json`, `checklist.json`, `standards.json`
4. Add API endpoints: `/api/project/status`, `/api/project/detect`, `/api/project/init`
5. Write detection tests using existing e2e fixture projects (Go, Python, Java, TypeScript, Svelte)

### Phase 2: Structural Validation Workflow Step

**Estimated scope**: ~300 lines of Go

1. Implement `structural_validation` action type in workflow processor
2. Add trigger-matching logic (file globs against checklist triggers)
3. Add command execution with timeout and output capture
4. Update `plan-and-execute.json` with `structural_check` and `structural_feedback` steps
5. Create `developer_structural_fix` prompt template
6. Add graceful degradation (no checklist = warn and pass)
7. Write unit tests for trigger matching and check execution

### Phase 3: Standards Generation & Injection

**Estimated scope**: ~200 lines of Go + prompt engineering

1. Add `/api/project/generate-standards` endpoint with LLM call
2. Create standards generation prompt (stack-aware, draws from existing docs, produces rules directly)
3. Add standards injection to context-builder (pre-strategy step)
4. Add standards regeneration trigger on SOP file changes (for when users author SOPs later)
5. Write tests for generation and injection

### Phase 4: Setup Wizard UI

**Estimated scope**: ~900 lines of Svelte

1. **Nav restructure**: Move Board to `/board`, make Activity the default at `/`
2. Update `Sidebar.svelte` nav items and ordering
3. Create `SetupWizard.svelte` component with three panels
4. Create `setup.svelte.ts` store for wizard state management
5. Modify Activity view (`/`) to check initialization status and show wizard when uninitialized
6. Panel 1: Detection display with project name/description inputs
7. Panel 2: Checklist editor with add/remove/toggle/edit
8. Panel 3: SOP display with editable text areas and standards preview
9. Completion state with transition to activity feed + chat input
10. Add TypeScript types for all API request/response shapes
11. Write Playwright e2e test for full wizard flow

### Phase 5: Integration Testing

1. E2E scenario: full init flow → plan → execute → structural check → review
2. Verify standards injection appears in agent context
3. Verify structural checks catch real failures (compile error, test failure)
4. Verify graceful degradation when config files are missing
5. Test with multiple fixture projects (Go, Python, Node)
6. Test standards regeneration when user later adds SOPs to `sources/docs/`

---

## Migration: Existing Constitution Code

The existing `processor/constitution/` package, `tools/workflow/constitution.go`, and constitution-related vocabulary predicates should be marked as deprecated. The new standards system replaces the constitution concept entirely.

**Migration steps** (can be done after MVP ships):

1. Add deprecation notices to constitution package
2. Update reviewer prompt to reference standards instead of constitution
3. Remove `workflow_check_constitution` and `workflow_get_principles` tools
4. Remove constitution vocabulary predicates
5. Clean up constitution tests and fixtures

Do NOT remove constitution code during init implementation — just build the new system alongside it. Remove after standards system is validated.

---

## Open Questions for Implementation

1. **Checklist execution environment**: Should checks run in the same container as the agent, or in a separate sandbox? Same container is simpler for MVP but could have side effects.
2. **Multi-repo support**: `project.json` assumes one repo. Monorepo support (multiple language roots) can be deferred but the schema should allow for it. Consider a `roots` array in a future version.
3. **Standards versioning**: When standards regenerate, should the old version be preserved? Git handles this naturally since all files are in `.semspec/`, but explicit versioning in the JSON might help with debugging.
4. **Flywheel threshold tuning**: When the review pattern flywheel is implemented, what's the right default threshold for suggesting a standard? Too low (2 occurrences) creates noise. Too high (10) means slow learning. Likely needs to be configurable and may differ by severity.
